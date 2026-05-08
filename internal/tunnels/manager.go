package tunnels

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"dad_proxy/internal/config"
)

type TunnelInfo struct {
	RemoteIP                 string    `json:"remoteIp"`
	RemotePort               int       `json:"remotePort"`
	LocalPort                int       `json:"localPort"`
	CreatedAt                time.Time `json:"createdAt"`
	LastActivityAt           time.Time `json:"lastActivityAt"`
	ActiveTCPConnections     int64     `json:"activeTcpConnections"`
	TotalTCPConnections      int64     `json:"totalTcpConnections"`
	ActiveUDPSessions        int64     `json:"activeUdpSessions"`
	TotalUDPSessions         int64     `json:"totalUdpSessions"`
	UDPDatagramsFromClients  int64     `json:"udpDatagramsFromClients"`
	UDPDatagramsToClients    int64     `json:"udpDatagramsToClients"`
	BytesFromClientsToRemote int64     `json:"bytesFromClientsToRemote"`
	BytesFromRemoteToClients int64     `json:"bytesFromRemoteToClients"`
}

type tunnel struct {
	info TunnelInfo

	tcpListener net.Listener
	udpConn     *net.UDPConn
	closeOnce   sync.Once

	lastActivityUnixNano     atomic.Int64
	activeTCPConnections     atomic.Int64
	totalTCPConnections      atomic.Int64
	activeUDPSessions        atomic.Int64
	totalUDPSessions         atomic.Int64
	udpDatagramsFromClients  atomic.Int64
	udpDatagramsToClients    atomic.Int64
	bytesFromClientsToRemote atomic.Int64
	bytesFromRemoteToClients atomic.Int64
}

func (t *tunnel) close() {
	t.closeOnce.Do(func() {
		if t.tcpListener != nil {
			_ = t.tcpListener.Close()
		}
		if t.udpConn != nil {
			_ = t.udpConn.Close()
		}
	})
}

func (t *tunnel) markActivity() {
	t.lastActivityUnixNano.Store(time.Now().UTC().UnixNano())
}

func (t *tunnel) snapshot() TunnelInfo {
	info := t.info
	info.ActiveTCPConnections = t.activeTCPConnections.Load()
	info.TotalTCPConnections = t.totalTCPConnections.Load()
	info.ActiveUDPSessions = t.activeUDPSessions.Load()
	info.TotalUDPSessions = t.totalUDPSessions.Load()
	info.UDPDatagramsFromClients = t.udpDatagramsFromClients.Load()
	info.UDPDatagramsToClients = t.udpDatagramsToClients.Load()
	info.BytesFromClientsToRemote = t.bytesFromClientsToRemote.Load()
	info.BytesFromRemoteToClients = t.bytesFromRemoteToClients.Load()
	if unixNano := t.lastActivityUnixNano.Load(); unixNano > 0 {
		info.LastActivityAt = time.Unix(0, unixNano).UTC()
	}
	return info
}

type Manager struct {
	logger *slog.Logger
	config *config.Config

	mu      sync.RWMutex
	tunnels map[string]*tunnel
}

func NewManager(cfg *config.Config, logger *slog.Logger) *Manager {
	return &Manager{
		logger:  logger,
		config:  cfg,
		tunnels: make(map[string]*tunnel),
	}
}

func (m *Manager) EnsureTunnel(remoteIP string, remotePort int) (TunnelInfo, error) {
	key := tunnelKey(remoteIP, remotePort)

	m.mu.RLock()
	existing, ok := m.tunnels[key]
	m.mu.RUnlock()
	if ok {
		return existing.info, nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	existing, ok = m.tunnels[key]
	if ok {
		return existing.info, nil
	}

	tun, err := m.createTunnel(remoteIP, remotePort)
	if err != nil {
		return TunnelInfo{}, err
	}

	m.tunnels[key] = tun
	m.logger.Info("Tunnel created",
		"remote_ip", tun.info.RemoteIP,
		"remote_port", tun.info.RemotePort,
		"local_port", tun.info.LocalPort,
	)

	return tun.info, nil
}

func (m *Manager) ListTunnels() []TunnelInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]TunnelInfo, 0, len(m.tunnels))
	for _, tun := range m.tunnels {
		out = append(out, tun.snapshot())
	}
	return out
}

func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for key, tun := range m.tunnels {
		tun.close()
		delete(m.tunnels, key)
	}
}

func (m *Manager) createTunnel(remoteIP string, remotePort int) (*tunnel, error) {
	tcpListener, udpConn, localPort, err := m.listenInConfiguredRange()
	if err != nil {
		return nil, err
	}

	tun := &tunnel{
		info: TunnelInfo{
			RemoteIP:   remoteIP,
			RemotePort: remotePort,
			LocalPort:  localPort,
			CreatedAt:  time.Now().UTC(),
		},
		tcpListener: tcpListener,
		udpConn:     udpConn,
	}
	tun.markActivity()

	go m.serveTCP(tun)
	go m.serveUDP(tun)

	return tun, nil
}

func (m *Manager) listenInConfiguredRange() (net.Listener, *net.UDPConn, int, error) {
	for port := m.config.PortsRangeStart; port <= m.config.PortsRangeEnd; port++ {
		tcpListener, err := net.Listen("tcp4", fmt.Sprintf("0.0.0.0:%d", port))
		if err != nil {
			continue
		}

		udpConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: port})
		if err != nil {
			_ = tcpListener.Close()
			continue
		}

		return tcpListener, udpConn, port, nil
	}

	return nil, nil, 0, fmt.Errorf(
		"no free ports in DAD_PROXY_PORTS_RANGE %d,%d",
		m.config.PortsRangeStart,
		m.config.PortsRangeEnd,
	)
}

func (m *Manager) serveTCP(tun *tunnel) {
	remoteAddr := net.JoinHostPort(tun.info.RemoteIP, fmt.Sprintf("%d", tun.info.RemotePort))

	for {
		clientConn, err := tun.tcpListener.Accept()
		if err != nil {
			m.logger.Debug("TCP accept loop stopped", "error", err, "local_port", tun.info.LocalPort)
			return
		}

		go func() {
			defer func() { _ = clientConn.Close() }()
			tun.activeTCPConnections.Add(1)
			tun.totalTCPConnections.Add(1)
			tun.markActivity()
			defer tun.activeTCPConnections.Add(-1)

			upstreamConn, err := net.DialTimeout("tcp4", remoteAddr, 10*time.Second)
			if err != nil {
				m.logger.Warn("TCP upstream dial failed",
					"local_port", tun.info.LocalPort,
					"remote_addr", remoteAddr,
					"error", err,
				)
				return
			}
			defer func() { _ = upstreamConn.Close() }()

			copyDone := make(chan struct{}, 2)

			go func() {
				copied, _ := io.Copy(upstreamConn, clientConn)
				if copied > 0 {
					tun.bytesFromClientsToRemote.Add(copied)
					tun.markActivity()
				}
				copyDone <- struct{}{}
			}()
			go func() {
				copied, _ := io.Copy(clientConn, upstreamConn)
				if copied > 0 {
					tun.bytesFromRemoteToClients.Add(copied)
					tun.markActivity()
				}
				copyDone <- struct{}{}
			}()

			<-copyDone
		}()
	}
}

func (m *Manager) serveUDP(tun *tunnel) {
	remoteAddr := &net.UDPAddr{IP: net.ParseIP(tun.info.RemoteIP), Port: tun.info.RemotePort}
	if remoteAddr.IP == nil {
		m.logger.Error("Invalid tunnel remote IP for UDP", "remote_ip", tun.info.RemoteIP)
		return
	}

	type udpSession struct {
		conn *net.UDPConn
	}

	var (
		sessionsMu sync.Mutex
		sessions   = make(map[string]*udpSession)
	)

	buffer := make([]byte, 64*1024)
	for {
		n, clientAddr, err := tun.udpConn.ReadFromUDP(buffer)
		if err != nil {
			m.logger.Debug("UDP read loop stopped", "error", err, "local_port", tun.info.LocalPort)
			return
		}

		clientKey := clientAddr.String()

		sessionsMu.Lock()
		session, ok := sessions[clientKey]
		if !ok {
			upstreamConn, dialErr := net.DialUDP("udp4", nil, remoteAddr)
			if dialErr != nil {
				sessionsMu.Unlock()
				m.logger.Warn("UDP upstream dial failed",
					"local_port", tun.info.LocalPort,
					"remote_ip", tun.info.RemoteIP,
					"remote_port", tun.info.RemotePort,
					"error", dialErr,
				)
				continue
			}

			session = &udpSession{conn: upstreamConn}
			sessions[clientKey] = session
			tun.activeUDPSessions.Add(1)
			tun.totalUDPSessions.Add(1)
			tun.markActivity()

			go func(client *net.UDPAddr, sess *udpSession, key string) {
				defer func() {
					_ = sess.conn.Close()
					sessionsMu.Lock()
					delete(sessions, key)
					sessionsMu.Unlock()
					tun.activeUDPSessions.Add(-1)
				}()

				replyBuf := make([]byte, 64*1024)
				for {
					readBytes, readErr := sess.conn.Read(replyBuf)
					if readErr != nil {
						return
					}
					_, writeErr := tun.udpConn.WriteToUDP(replyBuf[:readBytes], client)
					if writeErr != nil {
						return
					}
					tun.udpDatagramsToClients.Add(1)
					tun.bytesFromRemoteToClients.Add(int64(readBytes))
					tun.markActivity()
				}
			}(clientAddr, session, clientKey)
		}
		sessionsMu.Unlock()

		if _, err = session.conn.Write(buffer[:n]); err != nil {
			m.logger.Warn("UDP write to upstream failed",
				"client", clientKey,
				"local_port", tun.info.LocalPort,
				"error", err,
			)
			continue
		}
		tun.udpDatagramsFromClients.Add(1)
		tun.bytesFromClientsToRemote.Add(int64(n))
		tun.markActivity()
	}
}

func tunnelKey(remoteIP string, remotePort int) string {
	return net.JoinHostPort(remoteIP, fmt.Sprintf("%d", remotePort))
}

func MarshalTunnelInfos(infos []TunnelInfo) ([]byte, error) {
	return json.Marshal(infos)
}
