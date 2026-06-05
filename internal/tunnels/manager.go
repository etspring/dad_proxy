package tunnels

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"dad_proxy/internal/config"
)

var upstreamHTTPURLPattern = regexp.MustCompile(`http://((?:\d{1,3}\.){3}\d{1,3}):(\d{1,5})(/[A-Za-z0-9._~:/?#\[\]@!$&'()*+,;=%-]*)?`)

type TunnelInfo struct {
	RemoteIP                 string    `json:"remoteIp"`
	RemotePort               int       `json:"remotePort"`
	LocalPort                int       `json:"localPort"`
	UDPClientPort            int       `json:"udpClientPort,omitempty"`
	CreatedAt                time.Time `json:"createdAt"`
	LastActivityAt           time.Time `json:"lastActivityAt"`
	ActiveTCPConnections     int64     `json:"activeTcpConnections"`
	TotalTCPConnections      int64     `json:"totalTcpConnections"`
	ActiveUDPSessions        int64     `json:"activeUdpSessions"`
	TotalUDPSessions         int64     `json:"totalUdpSessions"`
	BytesFromClientsToRemote int64     `json:"bytesFromClientsToRemote"`
	BytesFromRemoteToClients int64     `json:"bytesFromRemoteToClients"`
	UDPDatagramsFromClients  int64     `json:"udpDatagramsFromClients"`
	UDPDatagramsToClients    int64     `json:"udpDatagramsToClients"`
	UDPLocalListenAddr       string    `json:"udpLocalListenAddr,omitempty"`
}

// ClientAdvertisedPort is the TCP port when the tunnel has TCP (payload rewrites, HTTP); otherwise the UDP client port.
func (ti TunnelInfo) ClientAdvertisedPort() int {
	if ti.LocalPort > 0 {
		return ti.LocalPort
	}
	if ti.UDPClientPort > 0 {
		return ti.UDPClientPort
	}
	return 0
}

// UDPTunnelStats describes the UDP leg of one tunnel for API consumers.
type UDPTunnelStats struct {
	RemoteIP             string `json:"remoteIp"`
	RemotePort           int    `json:"remotePort"`
	LocalPort            int    `json:"localPort"`
	UDPClientPort        int    `json:"udpClientPort,omitempty"`
	LocalListenAddr      string `json:"localListenAddr,omitempty"`
	UpstreamAddr         string `json:"upstreamAddr"`
	ActiveSessions       int64  `json:"activeSessions"`
	TotalSessions        int64  `json:"totalSessions"`
	DatagramsFromClients int64  `json:"datagramsFromClients"`
	DatagramsToClients   int64  `json:"datagramsToClients"`
}

// UDPTunnelStatsFromInfo builds a UDP summary from a tunnel snapshot.
func UDPTunnelStatsFromInfo(ti TunnelInfo) UDPTunnelStats {
	return UDPTunnelStats{
		RemoteIP:             ti.RemoteIP,
		RemotePort:           ti.RemotePort,
		LocalPort:            ti.LocalPort,
		UDPClientPort:        ti.UDPClientPort,
		LocalListenAddr:      ti.UDPLocalListenAddr,
		UpstreamAddr:         net.JoinHostPort(ti.RemoteIP, fmt.Sprintf("%d", ti.RemotePort)),
		ActiveSessions:       ti.ActiveUDPSessions,
		TotalSessions:        ti.TotalUDPSessions,
		DatagramsFromClients: ti.UDPDatagramsFromClients,
		DatagramsToClients:   ti.UDPDatagramsToClients,
	}
}

type tunnel struct {
	info TunnelInfo

	splitUDP    bool
	tcpListener net.Listener
	udpClient   *net.UDPConn
	udpUpstream *net.UDPConn
	closeOnce   sync.Once

	lastActivityUnixNano     atomic.Int64
	activeTCPConnections     atomic.Int64
	totalTCPConnections      atomic.Int64
	activeUDPSessions        atomic.Int64
	totalUDPSessions         atomic.Int64
	bytesFromClientsToRemote atomic.Int64
	bytesFromRemoteToClients atomic.Int64
	udpDatagramsFromClients  atomic.Int64
	udpDatagramsToClients    atomic.Int64
}

func (t *tunnel) close() {
	t.closeOnce.Do(func() {
		t.closeUDPLeg()
		if t.tcpListener != nil {
			_ = t.tcpListener.Close()
		}
	})
}

func (t *tunnel) closeUDPLeg() {
	if t.udpClient != nil {
		_ = t.udpClient.Close()
		t.udpClient = nil
	}
	if t.udpUpstream != nil {
		_ = t.udpUpstream.Close()
		t.udpUpstream = nil
	}
	t.splitUDP = false
	t.info.UDPClientPort = 0
	t.activeUDPSessions.Store(0)
}

func (t *tunnel) hasUDP() bool {
	return t.udpClient != nil
}

func (t *tunnel) idleSince() (time.Time, bool) {
	unixNano := t.lastActivityUnixNano.Load()
	if unixNano <= 0 {
		return time.Time{}, false
	}
	return time.Unix(0, unixNano).UTC(), true
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
	info.BytesFromClientsToRemote = t.bytesFromClientsToRemote.Load()
	info.BytesFromRemoteToClients = t.bytesFromRemoteToClients.Load()
	info.UDPDatagramsFromClients = t.udpDatagramsFromClients.Load()
	info.UDPDatagramsToClients = t.udpDatagramsToClients.Load()
	if t.udpClient != nil {
		if la := t.udpClient.LocalAddr(); la != nil {
			info.UDPLocalListenAddr = la.String()
		}
	}
	if unixNano := t.lastActivityUnixNano.Load(); unixNano > 0 {
		info.LastActivityAt = time.Unix(0, unixNano).UTC()
	}
	return info
}

type Manager struct {
	logger *slog.Logger
	config *config.Config

	mu           sync.RWMutex
	tunnels      map[string]*tunnel
	idleStop     chan struct{}
	idleStopOnce sync.Once
}

func NewManager(cfg *config.Config, logger *slog.Logger) *Manager {
	m := &Manager{
		logger:   logger,
		config:   cfg,
		tunnels:  make(map[string]*tunnel),
		idleStop: make(chan struct{}),
	}
	if cfg.UDPIdleTimeout > 0 {
		go m.runUDPIdleReaper()
	}
	return m
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
		"udp_client_port", tun.info.UDPClientPort,
		"udp_game_port_tunnel", m.isRemoteGameUDPPort(remotePort),
		"udp_split", tun.splitUDP,
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
	m.idleStopOnce.Do(func() { close(m.idleStop) })

	m.mu.Lock()
	defer m.mu.Unlock()

	for key, tun := range m.tunnels {
		tun.close()
		delete(m.tunnels, key)
	}
}

func (m *Manager) runUDPIdleReaper() {
	timeout := m.config.UDPIdleTimeout
	interval := timeout / 6
	if interval < 30*time.Second {
		interval = 30 * time.Second
	}
	if interval > time.Minute {
		interval = time.Minute
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-m.idleStop:
			return
		case <-ticker.C:
			m.evictIdleUDPTunnels()
		}
	}
}

func (m *Manager) evictIdleUDPTunnels() {
	timeout := m.config.UDPIdleTimeout
	if timeout <= 0 {
		return
	}

	now := time.Now().UTC()

	m.mu.Lock()
	defer m.mu.Unlock()

	var removeKeys []string
	for key, tun := range m.tunnels {
		if !tun.hasUDP() {
			continue
		}
		if tun.activeTCPConnections.Load() > 0 {
			continue
		}
		last, ok := tun.idleSince()
		if !ok || now.Sub(last) < timeout {
			continue
		}

		idleFor := now.Sub(last)
		if tun.tcpListener != nil {
			tun.closeUDPLeg()
			m.logger.Info("UDP tunnel leg closed due to idle timeout",
				"remote_ip", tun.info.RemoteIP,
				"remote_port", tun.info.RemotePort,
				"tcp_local_port", tun.info.LocalPort,
				"idle_for", idleFor,
				"idle_timeout", timeout,
			)
			continue
		}

		udpClientPort := tun.info.UDPClientPort
		tun.close()
		removeKeys = append(removeKeys, key)
		m.logger.Info("UDP tunnel closed due to idle timeout",
			"remote_ip", tun.info.RemoteIP,
			"remote_port", tun.info.RemotePort,
			"udp_client_port", udpClientPort,
			"idle_for", idleFor,
			"idle_timeout", timeout,
		)
	}

	for _, key := range removeKeys {
		delete(m.tunnels, key)
	}
}

func (m *Manager) isRemoteGameUDPPort(port int) bool {
	if port < m.config.UDPPortsRangeStart || port > m.config.UDPPortsRangeEnd {
		return false
	}
	// If DAD_PROXY_UDP_PORTS_RANGE overlaps DAD_PROXY_PORTS_RANGE, never treat pool ports UDP.
	if port >= m.config.PortsRangeStart && port <= m.config.PortsRangeEnd {
		return false
	}
	return true
}

func (m *Manager) isRemotePortInTunnelTCPPool(port int) bool {
	return port >= m.config.PortsRangeStart && port <= m.config.PortsRangeEnd
}

// tunnelBindIPv4 returns the local IPv4 to bind tunnel listeners on. When DAD_PROXY_IP is a
// concrete IPv4 (public or private), we bind to it so replies use that source address; otherwise 0.0.0.0.
func (m *Manager) tunnelBindIPv4() net.IP {
	ip := net.ParseIP(strings.TrimSpace(m.config.ProxyIP))
	if ip == nil {
		return net.IPv4zero
	}
	ip4 := ip.To4()
	if ip4 == nil {
		return net.IPv4zero
	}
	if ip4.IsUnspecified() {
		return net.IPv4zero
	}
	return ip4
}

// listenUDPEphemeral binds an ephemeral UDP port on bindIP for upstream relay (no connect - we use
// ReadFromUDP so replies from alternate source ports on the same server IP are still accepted).
func (m *Manager) listenUDPEphemeral(bindIP net.IP) (*net.UDPConn, error) {
	return net.ListenUDP("udp4", &net.UDPAddr{IP: bindIP, Port: 0})
}

func (m *Manager) createTunnel(remoteIP string, remotePort int) (*tunnel, error) {
	bindIP := m.tunnelBindIPv4()
	remoteUDP := &net.UDPAddr{IP: net.ParseIP(remoteIP), Port: remotePort}
	if remoteUDP.IP == nil {
		return nil, fmt.Errorf("invalid remote IP for tunnel: %q", remoteIP)
	}

	if m.isRemoteGameUDPPort(remotePort) {
		// sync UDP only - no TCP listener on the proxy for this upstream port.
		udpClient, clientUDPPort, err := m.listenUDPInPortRange(
			m.config.UDPClientBindRangeStart, m.config.UDPClientBindRangeEnd, "DAD_PROXY_UDP_CLIENT_BIND_RANGE")
		if err != nil {
			return nil, err
		}
		udpUpstream, err := m.listenUDPEphemeral(bindIP)
		if err != nil {
			_ = udpClient.Close()
			return nil, fmt.Errorf("udp bind upstream relay on %s: %w", bindIP, err)
		}

		tun := &tunnel{
			info: TunnelInfo{
				RemoteIP:      remoteIP,
				RemotePort:    remotePort,
				UDPClientPort: clientUDPPort,
				CreatedAt:     time.Now().UTC(),
			},
			splitUDP:    true,
			udpClient:   udpClient,
			udpUpstream: udpUpstream,
		}
		tun.markActivity()
		go m.serveUDPSplit(tun)
		return tun, nil
	}

	tcpListener, tcpPort, err := m.listenTCPInPortRange(
		m.config.PortsRangeStart, m.config.PortsRangeEnd, "DAD_PROXY_PORTS_RANGE", remotePort)
	if err != nil {
		return nil, err
	}

	// Upstream ports inside the TCP pool range are almost always TCP-only (API, control plane).
	// Do not bind a UDP listener on those port numbers - it duplicates pool ports and jerk clients.
	if m.isRemotePortInTunnelTCPPool(remotePort) {
		m.logger.Info("UDP relay skipped: upstream port in DAD_PROXY_PORTS_RANGE (TCP-only tunnel)",
			"remote_ip", remoteIP,
			"remote_port", remotePort,
			"tcp_local_port", tcpPort,
			"port_preserved", tcpPort == remotePort,
		)
		tun := &tunnel{
			info: TunnelInfo{
				RemoteIP:   remoteIP,
				RemotePort: remotePort,
				LocalPort:  tcpPort,
				CreatedAt:  time.Now().UTC(),
			},
			splitUDP:    false,
			tcpListener: tcpListener,
		}
		tun.markActivity()
		go m.serveTCP(tun)
		return tun, nil
	}

	udpClient, clientUDPPort, err := m.listenUDPInPortRange(
		m.config.UDPClientBindRangeStart, m.config.UDPClientBindRangeEnd, "DAD_PROXY_UDP_CLIENT_BIND_RANGE")
	if err != nil {
		_ = tcpListener.Close()
		m.logger.Info("UDP side disabled for tunnel (TCP only)",
			"remote_ip", remoteIP,
			"remote_port", remotePort,
			"tcp_local_port", tcpPort,
			"error", err,
		)
		tun := &tunnel{
			info: TunnelInfo{
				RemoteIP:   remoteIP,
				RemotePort: remotePort,
				LocalPort:  tcpPort,
				CreatedAt:  time.Now().UTC(),
			},
			splitUDP:    false,
			tcpListener: tcpListener,
		}
		tun.markActivity()
		go m.serveTCP(tun)
		return tun, nil
	}

	udpUpstream, err := m.listenUDPEphemeral(bindIP)
	if err != nil {
		_ = tcpListener.Close()
		_ = udpClient.Close()
		return nil, fmt.Errorf("udp bind upstream relay on %s: %w", bindIP, err)
	}

	tun := &tunnel{
		info: TunnelInfo{
			RemoteIP:      remoteIP,
			RemotePort:    remotePort,
			LocalPort:     tcpPort,
			UDPClientPort: clientUDPPort,
			CreatedAt:     time.Now().UTC(),
		},
		splitUDP:    true,
		tcpListener: tcpListener,
		udpClient:   udpClient,
		udpUpstream: udpUpstream,
	}
	tun.markActivity()
	go m.serveTCP(tun)
	go m.serveUDPSplit(tun)
	return tun, nil
}

func (m *Manager) listenTCPInPortRange(rangeStart, rangeEnd int, rangeLabel string, preferredPort int) (net.Listener, int, error) {
	bindIP := m.tunnelBindIPv4()
	bindHost := bindIP.String()

	tryBind := func(port int) (net.Listener, int, bool) {
		if port < rangeStart || port > rangeEnd {
			return nil, 0, false
		}
		tcpListener, err := net.Listen("tcp4", net.JoinHostPort(bindHost, fmt.Sprintf("%d", port)))
		if err != nil {
			return nil, 0, false
		}
		return tcpListener, port, true
	}

	if ln, port, ok := tryBind(preferredPort); ok {
		return ln, port, nil
	}
	for port := rangeStart; port <= rangeEnd; port++ {
		if port == preferredPort {
			continue
		}
		if ln, p, ok := tryBind(port); ok {
			return ln, p, nil
		}
	}
	return nil, 0, fmt.Errorf(
		"no free TCP ports in %s %d,%d",
		rangeLabel,
		rangeStart,
		rangeEnd,
	)
}

func (m *Manager) listenUDPInPortRange(rangeStart, rangeEnd int, rangeLabel string) (*net.UDPConn, int, error) {
	bindIP := m.tunnelBindIPv4()
	for port := rangeStart; port <= rangeEnd; port++ {
		c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: bindIP, Port: port})
		if err != nil {
			continue
		}
		return c, port, nil
	}
	return nil, 0, fmt.Errorf(
		"no free UDP ports in %s %d,%d",
		rangeLabel,
		rangeStart,
		rangeEnd,
	)
}

func (m *Manager) serveTCP(tun *tunnel) {
	if tun.tcpListener == nil {
		return
	}
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

			m.logger.Info("TCP tunnel session started",
				"listen_local_port", tun.info.LocalPort,
				"upstream_addr", remoteAddr,
				"client_peer", clientConn.RemoteAddr().String(),
				"tcp_payload_rewrite", m.config.TCPPayloadRewrite,
			)

			var wg sync.WaitGroup
			wg.Add(2)

			go func() {
				defer wg.Done()
				n, _ := io.Copy(upstreamConn, clientConn)
				if n > 0 {
					tun.bytesFromClientsToRemote.Add(n)
					tun.markActivity()
				}
			}()

			go func() {
				defer wg.Done()
				var n int64
				if m.config.TCPPayloadRewrite {
					n, _ = m.copyTCPRemoteToClientFramed(tun, clientConn, upstreamConn)
				} else {
					n, _ = io.Copy(clientConn, upstreamConn)
				}
				if n > 0 {
					tun.bytesFromRemoteToClients.Add(n)
					tun.markActivity()
				}
			}()

			wg.Wait()
			m.logger.Info("TCP tunnel session ended",
				"listen_local_port", tun.info.LocalPort,
				"upstream_addr", remoteAddr,
				"client_peer", clientConn.RemoteAddr().String(),
			)
		}()
	}
}

// copyTCPRemoteToClientFramed reads framed messages from upstream, rewrites payloads for the client, and writes framed messages to the client.
// The first u32 is the total byte length of the frame including those 4 bytes (matches Wireshark "Data: 86 bytes" with payload starting 56 00 00 00).
func (m *Manager) copyTCPRemoteToClientFramed(tun *tunnel, clientConn net.Conn, upstreamConn net.Conn) (int64, error) {
	reader := bufio.NewReader(upstreamConn)
	var totalWritten int64

	for {
		lenPrefix := make([]byte, 4)
		if _, err := io.ReadFull(reader, lenPrefix); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				return totalWritten, nil
			}
			return totalWritten, err
		}

		totalSize := int(binary.LittleEndian.Uint32(lenPrefix))
		if totalSize < 4 || totalSize > 2*1024*1024 {
			m.logger.Info("TCP upstream not using u32-le self-sized framing; switching to transparent relay",
				"declared_total", totalSize,
				"header_hex", hex.EncodeToString(lenPrefix),
				"local_port", tun.info.LocalPort,
			)
			n, writeErr := clientConn.Write(lenPrefix)
			totalWritten += int64(n)
			if writeErr != nil {
				return totalWritten, writeErr
			}
			copied, copyErr := io.Copy(clientConn, reader)
			totalWritten += copied
			return totalWritten, copyErr
		}

		rest := make([]byte, totalSize-4)
		if _, err := io.ReadFull(reader, rest); err != nil {
			m.logger.Warn("TCP upstream framed read failed",
				"expected_total", totalSize,
				"error", err,
				"local_port", tun.info.LocalPort,
			)
			return totalWritten, err
		}

		payload := append(append([]byte(nil), lenPrefix...), rest...)
		rewrittenPayload := m.rewriteTunnelPayload(payload, tun.info.LocalPort, "remote->client")

		n, writeErr := clientConn.Write(rewrittenPayload)
		totalWritten += int64(n)
		if writeErr != nil {
			return totalWritten, writeErr
		}
	}
}

// lobbyProtobufContext describes START payloads where field 1 (varint) is the game UDP port
// and field 2 (string) is a bare IPv4 - the client combines them into ip:port.
type lobbyProtobufContext struct {
	GameIP      string
	GameUDPPort int
	SplitFormat bool
	tunnel      *TunnelInfo
}

// rewriteTunnelPayload applies game payload rewriting
func (m *Manager) rewriteTunnelPayload(payload []byte, baseLocalPort int, direction string) []byte {
	if !m.config.TCPPayloadRewrite {
		return payload
	}
	out := payload
	if len(payload) >= 8 {
		lobbyCtx := m.parseLobbyProtobufContext(payload[8:])
		if lobbyCtx.SplitFormat && direction == "remote->client" {
			ti, err := m.EnsureTunnel(lobbyCtx.GameIP, lobbyCtx.GameUDPPort)
			if err != nil {
				m.logger.Warn("Failed to create UDP tunnel from split lobby address",
					"ip", lobbyCtx.GameIP,
					"port", lobbyCtx.GameUDPPort,
					"error", err,
				)
			} else {
				lobbyCtx.tunnel = &ti
			}
		}
		newRest, ok := m.tryRewriteProtobuf(payload[8:], baseLocalPort, direction, 0, lobbyCtx)
		if ok {
			out = make([]byte, 8+len(newRest))
			copy(out[:8], payload[:8])
			copy(out[8:], newRest)
			// First u32 in the game frame is the total byte length of the frame (matches captured packets).
			binary.LittleEndian.PutUint32(out[0:4], uint32(len(out)))
		} else {
			out = m.rewriteTLVPayload(payload, baseLocalPort, direction)
		}
	} else {
		out = m.rewriteTLVPayload(payload, baseLocalPort, direction)
	}
	return m.rewriteHTTPURLBytes(out, baseLocalPort)
}

// parseLobbyProtobufContext detects split lobby addresses: protobuf field 1 = UDP port, field 2 = bare IP.
func (m *Manager) parseLobbyProtobufContext(body []byte) lobbyProtobufContext {
	var ctx lobbyProtobufContext
	if !protobufWireConsumesFully(body) {
		return ctx
	}

	var field1Varint uint64
	var field1Seen bool
	var field2Str string

	i := 0
	for i < len(body) {
		tagStart := i
		_, tagLen := consumeProtobufVarint(body[i:])
		if tagLen == 0 || i+tagLen > len(body) {
			return ctx
		}
		tagVal, _ := consumeProtobufVarint(body[tagStart : tagStart+tagLen])
		fieldNum := tagVal >> 3
		i += tagLen
		wt := tagVal & 7

		switch wt {
		case 0:
			val, valLen := consumeProtobufVarint(body[i:])
			if valLen == 0 || i+valLen > len(body) {
				return ctx
			}
			if fieldNum == 1 {
				field1Varint = val
				field1Seen = true
			}
			i += valLen
		case 1:
			if i+8 > len(body) {
				return ctx
			}
			i += 8
		case 5:
			if i+4 > len(body) {
				return ctx
			}
			i += 4
		case 2:
			ln, lnLen := consumeProtobufVarint(body[i:])
			if lnLen == 0 || ln > uint64(len(body)-i-lnLen) {
				return ctx
			}
			i += lnLen
			if fieldNum == 2 {
				field2Str = strings.TrimSpace(string(bytes.TrimRight(body[i:i+int(ln)], "\x00")))
			}
			i += int(ln)
		default:
			return ctx
		}
	}

	if field2Str == "" || strings.Contains(field2Str, ":") {
		return ctx
	}
	ip := net.ParseIP(field2Str)
	if ip == nil || ip.To4() == nil || field2Str == m.config.ProxyIP {
		return ctx
	}
	port := int(field1Varint)
	if !field1Seen || !m.isRemoteGameUDPPort(port) {
		return ctx
	}

	ctx.GameIP = field2Str
	ctx.GameUDPPort = port
	ctx.SplitFormat = true
	return ctx
}

// tryRewriteProtobuf rewrites messages (length-delimited UTF-8 strings with IPv4:port, nested messages, etc.).
func (m *Manager) tryRewriteProtobuf(body []byte, baseLocalPort int, direction string, depth int, lobbyCtx lobbyProtobufContext) ([]byte, bool) {
	if depth > 12 || len(body) == 0 {
		return body, true
	}
	if !protobufWireConsumesFully(body) {
		return nil, false
	}
	nestedCtx := lobbyProtobufContext{}
	if depth == 0 {
		nestedCtx = lobbyCtx
	}
	var out []byte
	i := 0
	for i < len(body) {
		tagStart := i
		_, tagLen := consumeProtobufVarint(body[i:])
		if tagLen == 0 || i+tagLen > len(body) {
			return nil, false
		}
		tagBytes := body[tagStart : tagStart+tagLen]
		tagVal, _ := consumeProtobufVarint(tagBytes)
		fieldNum := tagVal >> 3
		i += tagLen
		wt := tagVal & 7
		switch wt {
		case 0: // varint
			valStart := i
			val, valLen := consumeProtobufVarint(body[i:])
			if valLen == 0 || i+valLen > len(body) {
				return nil, false
			}
			if depth == 0 && fieldNum == 1 && nestedCtx.SplitFormat && nestedCtx.tunnel != nil &&
				direction == "remote->client" && nestedCtx.tunnel.UDPClientPort > 0 {
				newPort := uint64(nestedCtx.tunnel.UDPClientPort)
				if val != newPort {
					m.logger.Info("Rewrote lobby UDP port varint",
						"from", val,
						"to", newPort,
						"upstream", fmt.Sprintf("%s:%d", nestedCtx.GameIP, nestedCtx.GameUDPPort),
						"direction", direction,
					)
				}
				out = append(out, tagBytes...)
				out = appendProtobufVarint(out, newPort)
				i += valLen
				continue
			}
			out = append(out, tagBytes...)
			out = append(out, body[valStart:valStart+valLen]...)
			i += valLen
		case 1: // 64-bit
			if i+8 > len(body) {
				return nil, false
			}
			out = append(out, tagBytes...)
			out = append(out, body[i:i+8]...)
			i += 8
		case 5: // 32-bit
			if i+4 > len(body) {
				return nil, false
			}
			out = append(out, tagBytes...)
			out = append(out, body[i:i+4]...)
			i += 4
		case 2: // length-delimited
			ln, lnLen := consumeProtobufVarint(body[i:])
			if lnLen == 0 || ln > uint64(len(body)-i-lnLen) {
				return nil, false
			}
			i += lnLen
			chunk := body[i : i+int(ln)]
			i += int(ln)
			var newChunk []byte
			if depth == 0 && fieldNum == 2 && nestedCtx.SplitFormat && nestedCtx.tunnel != nil &&
				direction == "remote->client" {
				s := strings.TrimSpace(string(bytes.TrimRight(chunk, "\x00")))
				if s == nestedCtx.GameIP {
					newChunk = []byte(m.config.ProxyIP)
					m.logger.Info("Rewrote bare lobby UDP IP in protobuf",
						"from", s,
						"to", m.config.ProxyIP,
						"upstream_port", nestedCtx.GameUDPPort,
						"client_udp_port", nestedCtx.tunnel.UDPClientPort,
						"direction", direction,
					)
				} else {
					newChunk = m.rewriteProtobufLenDelim(chunk, baseLocalPort, direction, depth)
				}
			} else {
				newChunk = m.rewriteProtobufLenDelim(chunk, baseLocalPort, direction, depth)
			}
			out = append(out, tagBytes...)
			out = appendProtobufVarint(out, uint64(len(newChunk)))
			out = append(out, newChunk...)
		default:
			return nil, false
		}
	}
	return out, true
}

func (m *Manager) rewriteProtobufLenDelim(chunk []byte, baseLocalPort int, direction string, depth int) []byte {
	if len(chunk) == 0 {
		return chunk
	}
	if depth < 12 && len(chunk) > 2 && protobufWireConsumesFully(chunk) {
		if nested, ok := m.tryRewriteProtobuf(chunk, baseLocalPort, direction, depth+1, lobbyProtobufContext{}); ok {
			return nested
		}
	}
	s := strings.TrimSpace(string(bytes.TrimRight(chunk, "\x00")))
	if m.isIPPort(s) {
		return m.rewriteIPAddress(chunk, direction, 0x12)
	}
	if strings.HasPrefix(s, "http://") {
		return m.rewriteHTTPURL(chunk, baseLocalPort, direction)
	}
	if ip := net.ParseIP(s); ip != nil && ip.To4() != nil && direction == "remote->client" && !strings.Contains(s, ":") && s != m.config.ProxyIP {
		return m.rewriteIPAddress(chunk, direction, 0x12)
	}
	return chunk
}

func protobufWireConsumesFully(body []byte) bool {
	i := 0
	for i < len(body) {
		_, n := consumeProtobufVarint(body[i:])
		if n == 0 || i+n > len(body) {
			return false
		}
		i += n
		tag, _ := consumeProtobufVarint(body[i-n:])
		wt := tag & 7
		switch wt {
		case 0:
			_, n2 := consumeProtobufVarint(body[i:])
			if n2 == 0 || i+n2 > len(body) {
				return false
			}
			i += n2
		case 1:
			if i+8 > len(body) {
				return false
			}
			i += 8
		case 2:
			ln, n2 := consumeProtobufVarint(body[i:])
			if n2 == 0 || ln > uint64(len(body)-i-n2) {
				return false
			}
			i += n2 + int(ln)
		case 5:
			if i+4 > len(body) {
				return false
			}
			i += 4
		default:
			return false
		}
	}
	return true
}

func consumeProtobufVarint(buf []byte) (value uint64, n int) {
	var s uint
	for i := 0; i < len(buf) && i < 10; i++ {
		b := buf[i]
		if s == 63 && b > 1 {
			return 0, 0
		}
		value |= uint64(b&0x7f) << s
		s += 7
		n++
		if b < 0x80 {
			return value, n
		}
	}
	return 0, 0
}

func appendProtobufVarint(b []byte, x uint64) []byte {
	for x >= 0x80 {
		b = append(b, byte(x)|0x80)
		x >>= 7
	}
	return append(b, byte(x))
}

func (m *Manager) rewriteTLVPayload(payload []byte, baseLocalPort int, direction string) []byte {
	if len(payload) < 8 {
		return payload
	}
	out8, ok8 := m.rewriteTLVPayloadWithWidth(payload, baseLocalPort, direction, 1)
	if ok8 {
		return out8
	}
	out16, ok16 := m.rewriteTLVPayloadWithWidth(payload, baseLocalPort, direction, 2)
	if ok16 {
		return out16
	}
	return out8
}

// rewriteTLVPayloadWithWidth parses TLV
func (m *Manager) rewriteTLVPayloadWithWidth(payload []byte, baseLocalPort int, direction string, lenWidth int) ([]byte, bool) {
	const msgHeader = 8
	if len(payload) < msgHeader {
		return payload, true
	}
	m.precreateTunnelsFromTLVPayload(payload, lenWidth)

	offset := msgHeader
	result := make([]byte, msgHeader)
	copy(result, payload[:msgHeader])

	for offset < len(payload) {
		hdr := 1 + lenWidth
		if offset+hdr > len(payload) {
			result = append(result, payload[offset:]...)
			return result, false
		}
		tag := payload[offset]
		var length int
		if lenWidth == 1 {
			length = int(payload[offset+1])
		} else {
			length = int(binary.LittleEndian.Uint16(payload[offset+1 : offset+hdr]))
		}
		valStart := offset + hdr
		if length < 0 || valStart+length > len(payload) {
			m.logger.Warn("Incomplete TLV",
				"len_width", lenWidth,
				"tag", fmt.Sprintf("0x%02x", tag),
				"expected_length", length,
				"remaining", len(payload)-valStart,
			)
			result = append(result, payload[offset:]...)
			return result, false
		}
		value := payload[valStart : valStart+length]
		offset = valStart + length

		newValue := m.processTLVValue(tag, value, baseLocalPort, direction)
		if lenWidth == 1 && len(newValue) > 255 {
			m.logger.Warn("TLV rewrite value exceeds u8 length; output truncated",
				"tag", fmt.Sprintf("0x%02x", tag),
				"new_len", len(newValue),
			)
			newValue = newValue[:255]
		}
		if lenWidth == 2 && len(newValue) > 65535 {
			m.logger.Warn("TLV rewrite value exceeds u16 length; truncating",
				"tag", fmt.Sprintf("0x%02x", tag),
				"new_len", len(newValue),
			)
			newValue = newValue[:65535]
		}

		result = append(result, tag)
		if lenWidth == 1 {
			result = append(result, byte(len(newValue)))
		} else {
			result = binary.LittleEndian.AppendUint16(result, uint16(len(newValue)))
		}
		result = append(result, newValue...)
	}
	return result, true
}

func (m *Manager) processTLVValue(tag byte, value []byte, baseLocalPort int, direction string) []byte {
	valueStr := strings.TrimSpace(string(bytes.TrimRight(value, "\x00")))

	switch tag {
	case 0x0c, 0x12: // IP or IP:port - game UDP address from lobby
		return m.rewriteIPAddress(value, direction, tag)
	case 0x1b: // HTTP URL
		return m.rewriteHTTPURL(value, baseLocalPort, direction)
	default:
		if strings.Contains(valueStr, ":") && m.isIPPort(valueStr) {
			return m.rewriteIPAddress(value, direction, tag)
		}
		return value
	}
}

func (m *Manager) tunnelForRemoteIP(remoteIP string) (TunnelInfo, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var udpMatch, tcpMatch *TunnelInfo
	for _, tun := range m.tunnels {
		if tun.info.RemoteIP != remoteIP {
			continue
		}
		info := tun.info
		if info.UDPClientPort > 0 {
			udpMatch = &info
		} else if info.LocalPort > 0 {
			tcpMatch = &info
		}
	}
	if udpMatch != nil {
		return *udpMatch, true
	}
	if tcpMatch != nil {
		return *tcpMatch, true
	}
	return TunnelInfo{}, false
}

func (m *Manager) precreateTunnelsFromTLVPayload(payload []byte, lenWidth int) {
	const msgHeader = 8
	if len(payload) < msgHeader {
		return
	}

	offset := msgHeader
	for offset < len(payload) {
		hdr := 1 + lenWidth
		if offset+hdr > len(payload) {
			return
		}
		tag := payload[offset]
		var length int
		if lenWidth == 1 {
			length = int(payload[offset+1])
		} else {
			length = int(binary.LittleEndian.Uint16(payload[offset+1 : offset+hdr]))
		}
		valStart := offset + hdr
		if length < 0 || valStart+length > len(payload) {
			return
		}
		m.precreateTunnelFromTLVValue(tag, payload[valStart:valStart+length])
		offset = valStart + length
	}
}

func (m *Manager) precreateTunnelFromTLVValue(tag byte, value []byte) {
	valueStr := strings.TrimSpace(string(bytes.TrimRight(value, "\x00")))

	switch tag {
	case 0x0c, 0x12:
		m.precreateTunnelFromIPPortString(valueStr)
	default:
		if strings.Contains(valueStr, ":") && m.isIPPort(valueStr) {
			m.precreateTunnelFromIPPortString(valueStr)
		}
	}
}

func (m *Manager) precreateTunnelFromIPPortString(valueStr string) {
	if !m.isIPPort(valueStr) {
		return
	}
	parts := strings.SplitN(valueStr, ":", 2)
	ip := parts[0]
	port, err := strconv.Atoi(parts[1])
	if err != nil || port <= 0 || port > 65535 || ip == m.config.ProxyIP {
		return
	}
	if !m.isRemoteGameUDPPort(port) {
		return
	}
	_, _ = m.EnsureTunnel(ip, port)
}

// rewriteIPAddress rewrites game UDP addresses (ip:port or bare IP) for lobby START payload.
func (m *Manager) rewriteIPAddress(value []byte, direction string, tag byte) []byte {
	valueStr := strings.TrimSpace(string(bytes.TrimRight(value, "\x00")))

	if strings.Contains(valueStr, ":") {
		parts := strings.SplitN(valueStr, ":", 2)
		if len(parts) == 2 {
			ip := parts[0]
			port, err := strconv.Atoi(parts[1])
			if err == nil && port > 0 && port <= 65535 {
				if ip != m.config.ProxyIP {
					if !m.isRemoteGameUDPPort(port) {
						m.logger.Info("Skipped non-UDP IP:port in lobby payload",
							"tag", fmt.Sprintf("0x%02x", tag),
							"value", valueStr,
							"direction", direction,
						)
						return value
					}
					tunnelInfo, err := m.EnsureTunnel(ip, port)
					if err == nil {
						newValue := fmt.Sprintf("%s:%d", m.config.ProxyIP, tunnelInfo.ClientAdvertisedPort())
						m.logger.Info("Rewrote UDP IP:port in TLV",
							"tag", fmt.Sprintf("0x%02x", tag),
							"from", valueStr,
							"to", newValue,
							"direction", direction,
						)
						return []byte(newValue)
					}
					m.logger.Warn("Failed to create UDP tunnel",
						"ip", ip,
						"port", port,
						"error", err,
					)
				}
				return value
			}
		}
	}

	if ip := net.ParseIP(valueStr); ip != nil && ip.To4() != nil && valueStr != m.config.ProxyIP {
		if direction != "remote->client" {
			return value
		}
		if tunnelInfo, ok := m.tunnelForRemoteIP(valueStr); ok && tunnelInfo.UDPClientPort > 0 {
			if tag == 0x0c {
				m.logger.Info("Rewrote bare UDP IP to proxy IP without port",
					"tag", fmt.Sprintf("0x%02x", tag),
					"from", valueStr,
					"to", m.config.ProxyIP,
					"upstream_port", tunnelInfo.RemotePort,
					"direction", direction,
				)
				return []byte(m.config.ProxyIP)
			}
			newValue := fmt.Sprintf("%s:%d", m.config.ProxyIP, tunnelInfo.UDPClientPort)
			m.logger.Info("Rewrote bare UDP IP in TLV",
				"tag", fmt.Sprintf("0x%02x", tag),
				"from", valueStr,
				"to", newValue,
				"upstream_port", tunnelInfo.RemotePort,
				"direction", direction,
			)
			return []byte(newValue)
		}
		if tag == 0x0c {
			m.logger.Info("Rewrote bare UDP IP to proxy IP without port",
				"tag", fmt.Sprintf("0x%02x", tag),
				"from", valueStr,
				"to", m.config.ProxyIP,
				"direction", direction,
			)
			return []byte(m.config.ProxyIP)
		}
		m.logger.Warn("TCP payload bare UDP IP left unchanged (no tunnel yet)",
			"tag", fmt.Sprintf("0x%02x", tag),
			"ip", valueStr,
			"direction", direction,
		)
	}

	return value
}

func (m *Manager) rewriteHTTPURL(value []byte, baseLocalPort int, direction string) []byte {
	valueStr := strings.TrimSpace(string(bytes.TrimRight(value, "\x00")))

	if strings.HasPrefix(valueStr, "http://") {
		remaining := strings.TrimPrefix(valueStr, "http://")

		pathStart := strings.Index(remaining, "/")
		var hostPart, pathPart string

		if pathStart >= 0 {
			hostPart = remaining[:pathStart]
			pathPart = remaining[pathStart:]
		} else {
			hostPart = remaining
			pathPart = ""
		}

		if strings.Contains(hostPart, ":") {
			hostParts := strings.SplitN(hostPart, ":", 2)
			if len(hostParts) == 2 {
				ip := hostParts[0]
				port, err := strconv.Atoi(hostParts[1])
				if err == nil && port > 0 && port <= 65535 {
					if ip != m.config.ProxyIP {
						tunnelInfo, err := m.EnsureTunnel(ip, port)
						if err == nil {
							newURL := fmt.Sprintf("http://%s:%d%s", m.config.ProxyIP, tunnelInfo.ClientAdvertisedPort(), pathPart)
							m.logger.Info("Rewrote HTTP URL",
								"from", valueStr,
								"to", newURL,
								"direction", direction,
							)
							return []byte(newURL)
						}
					}
				}
			}
		}
	}

	return value
}

func (m *Manager) rewriteHTTPURLBytes(payload []byte, baseTunnelLocalPort int) []byte {
	if len(payload) == 0 {
		return payload
	}
	matches := upstreamHTTPURLPattern.FindAllSubmatchIndex(payload, -1)
	if len(matches) == 0 {
		return payload
	}
	rewritten := append([]byte(nil), payload...)
	for i := len(matches) - 1; i >= 0; i-- {
		match := matches[i]
		fullStart, fullEnd := match[0], match[1]
		ipStart, ipEnd := match[2], match[3]
		portStart, portEnd := match[4], match[5]
		pathStart, pathEnd := -1, -1
		if len(match) >= 8 && match[6] >= 0 && match[7] >= 0 {
			pathStart, pathEnd = match[6], match[7]
		}

		remoteIP := string(rewritten[ipStart:ipEnd])
		if parsedIP := net.ParseIP(remoteIP); parsedIP == nil || parsedIP.To4() == nil {
			continue
		}

		remotePort, err := strconv.Atoi(string(rewritten[portStart:portEnd]))
		if err != nil || remotePort < 1 || remotePort > 65535 {
			continue
		}

		if remoteIP == m.config.ProxyIP {
			continue
		}

		tunnelInfo, err := m.EnsureTunnel(remoteIP, remotePort)
		if err != nil {
			m.logger.Warn("Failed to ensure nested tunnel for upstream URL",
				"remote_ip", remoteIP,
				"remote_port", remotePort,
				"error", err,
			)
			continue
		}

		path := ""
		if pathStart >= 0 && pathEnd >= 0 {
			path = string(rewritten[pathStart:pathEnd])
		}

		advertised := tunnelInfo.ClientAdvertisedPort()
		replacement := []byte(fmt.Sprintf("http://%s:%d%s", m.config.ProxyIP, advertised, path))
		rewritten = append(rewritten[:fullStart], append(replacement, rewritten[fullEnd:]...)...)

		m.logger.Info("Rewrote upstream URL inside tunnel payload",
			"from_ip", remoteIP,
			"from_port", remotePort,
			"to_ip", m.config.ProxyIP,
			"to_port", advertised,
			"base_tunnel_local_port", baseTunnelLocalPort,
		)
	}
	return rewritten
}

func (m *Manager) isIPPort(s string) bool {
	s = strings.TrimSpace(s)
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return false
	}
	if net.ParseIP(parts[0]) == nil {
		return false
	}
	port, err := strconv.Atoi(parts[1])
	if err != nil {
		return false
	}
	return port > 0 && port <= 65535
}

func (m *Manager) rewriteUDPFramedIfPresent(payload []byte, _ int, _ string) []byte {
	return payload
}

// udpSplitFromServer is true when addr is the game server host (same IP as upstream). Server replies can arrive on the ephemeral relay socket or on the client-facing port.
func udpSplitFromServer(addr *net.UDPAddr, server *net.UDPAddr) bool {
	if addr == nil || server == nil || server.IP == nil || addr.IP == nil {
		return false
	}
	a4, s4 := addr.IP.To4(), server.IP.To4()
	if a4 != nil && s4 != nil {
		return a4.Equal(s4)
	}
	return addr.IP.Equal(server.IP)
}

func copyUDPAddr(addr *net.UDPAddr) *net.UDPAddr {
	if addr == nil {
		return nil
	}
	ip := append(net.IP(nil), addr.IP...)
	return &net.UDPAddr{IP: ip, Port: addr.Port, Zone: addr.Zone}
}

// serveUDPSplit handles split UDP relays: clients use proxy:UDPClientPort upstream uses a separate UDP socket on an ephemeral local port bound to DAD_PROXY_IP.
func (m *Manager) serveUDPSplit(tun *tunnel) {
	up := tun.udpUpstream
	cl := tun.udpClient
	if up == nil || cl == nil {
		return
	}

	remoteAddr := &net.UDPAddr{IP: net.ParseIP(tun.info.RemoteIP), Port: tun.info.RemotePort}
	if remoteAddr.IP == nil {
		m.logger.Error("UDP split: invalid tunnel remote IP", "remote_ip", tun.info.RemoteIP)
		return
	}

	var (
		clientsMu sync.Mutex
		clients   = make(map[string]*net.UDPAddr)
	)

	clientDestsLocked := func() []*net.UDPAddr {
		dests := make([]*net.UDPAddr, 0, len(clients))
		for _, c := range clients {
			if !udpSplitFromServer(c, remoteAddr) {
				dests = append(dests, c)
			}
		}
		return dests
	}

	go func() {
		buf := make([]byte, 64*1024)
		for {
			n, from, err := up.ReadFromUDP(buf)
			if err != nil {
				m.logger.Debug("UDP upstream read stopped", "error", err,
					"tcp_local_port", tun.info.LocalPort, "udp_client_port", tun.info.UDPClientPort)
				return
			}
			if from == nil {
				continue
			}
			if !udpSplitFromServer(from, remoteAddr) {
				m.logger.Info("UDP split: upstream datagram from unexpected source; forwarding anyway",
					"from", from.String(),
					"expected_server", remoteAddr.String(),
					"udp_client_port", tun.info.UDPClientPort,
					"bytes", n,
				)
			}
			pkt := append([]byte(nil), buf[:n]...)
			clientsMu.Lock()
			dests := clientDestsLocked()
			clientsMu.Unlock()
			if len(dests) == 0 {
				m.logger.Warn("UDP split: upstream datagram dropped, no registered client",
					"from", from.String(),
					"udp_client_port", tun.info.UDPClientPort,
					"bytes", n,
				)
				continue
			}
			for _, c := range dests {
				if _, werr := cl.WriteToUDP(pkt, c); werr != nil {
					m.logger.Warn("UDP split: write toward client failed",
						"client", c.String(),
						"udp_client_port", tun.info.UDPClientPort,
						"from", from.String(),
						"error", werr,
					)
					continue
				}
				tun.udpDatagramsToClients.Add(1)
				tun.bytesFromRemoteToClients.Add(int64(len(pkt)))
			}
			tun.markActivity()
		}
	}()

	buf := make([]byte, 64*1024)
	for {
		n, peer, err := cl.ReadFromUDP(buf)
		if err != nil {
			m.logger.Debug("UDP client-socket read stopped", "error", err,
				"udp_client_port", tun.info.UDPClientPort)
			return
		}
		pkt := append([]byte(nil), buf[:n]...)

		// Game server sometimes sends replies to the advertised client port (proxy:UDPClientPort) instead of
		// the ephemeral relay port - they then appear on cl. They must go to real clients only, not back upstream.
		if udpSplitFromServer(peer, remoteAddr) {
			clientsMu.Lock()
			dests := clientDestsLocked()
			clientsMu.Unlock()
			if len(dests) == 0 {
				m.logger.Warn("UDP split: server datagram on client port dropped, no registered client",
					"from", peer.String(),
					"udp_client_port", tun.info.UDPClientPort,
					"bytes", n,
				)
				continue
			}
			for _, c := range dests {
				if _, werr := cl.WriteToUDP(pkt, c); werr != nil {
					m.logger.Warn("UDP split: server→client write failed",
						"client", c.String(),
						"udp_client_port", tun.info.UDPClientPort,
						"from", peer.String(),
						"error", werr,
					)
					continue
				}
				tun.udpDatagramsToClients.Add(1)
				tun.bytesFromRemoteToClients.Add(int64(len(pkt)))
				m.logger.Info("UDP split: server→client via client port",
					"client", c.String(),
					"from", peer.String(),
					"udp_client_port", tun.info.UDPClientPort,
					"bytes", len(pkt),
				)
			}
			tun.markActivity()
			continue
		}

		clientKey := peer.String()
		clientAddr := copyUDPAddr(peer)
		clientsMu.Lock()
		var staleKeys []string
		for k, c := range clients {
			if udpSplitFromServer(c, remoteAddr) {
				staleKeys = append(staleKeys, k)
			}
		}
		for _, k := range staleKeys {
			delete(clients, k)
		}
		if _, exists := clients[clientKey]; !exists {
			tun.totalUDPSessions.Add(1)
		}
		clients[clientKey] = clientAddr
		tun.activeUDPSessions.Store(int64(len(clients)))
		clientsMu.Unlock()

		if _, werr := up.WriteToUDP(pkt, remoteAddr); werr != nil {
			m.logger.Warn("UDP split: write to upstream failed",
				"client", clientKey,
				"upstream", remoteAddr.String(),
				"udp_client_port", tun.info.UDPClientPort,
				"error", werr,
			)
			continue
		}
		tun.udpDatagramsFromClients.Add(1)
		tun.bytesFromClientsToRemote.Add(int64(len(pkt)))
		tun.markActivity()
	}
}

func tunnelKey(remoteIP string, remotePort int) string {
	return net.JoinHostPort(remoteIP, fmt.Sprintf("%d", remotePort))
}

func MarshalTunnelInfos(infos []TunnelInfo) ([]byte, error) {
	return json.Marshal(infos)
}
