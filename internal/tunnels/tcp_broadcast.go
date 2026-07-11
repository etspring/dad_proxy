package tunnels

import (
	"log/slog"
	"net"
	"sync"

	"dad_proxy/internal/pb"
	"dad_proxy/internal/protocol"
)

const tcpInjectQueueSize = 16

// AnnounceStats агрегирует результат постановки объявления в очередь.
type AnnounceStats struct {
	Queued      int
	QueueFull   int
	TCPSessions int
	Tunnels     int
}

// tcpClientSession хранит TCP-клиента, очередь инъекций и шаблон заголовка с upstream.
type tcpClientSession struct {
	conn   net.Conn
	inject chan []byte

	headerMu  sync.Mutex
	header    [8]byte
	hasHeader bool

	identityMu     sync.RWMutex
	identity       TCPSessionIdentity
	characterNicks map[string]string
	welcomeSent    bool
}

func newTCPClientSession(conn net.Conn) *tcpClientSession {
	return &tcpClientSession{
		conn:   conn,
		inject: make(chan []byte, tcpInjectQueueSize),
	}
}

func (s *tcpClientSession) rememberHeader(frame []byte) {
	if len(frame) < 8 {
		return
	}
	s.headerMu.Lock()
	copy(s.header[:], frame[:8])
	s.hasHeader = true
	s.headerMu.Unlock()
}

func (s *tcpClientSession) headerSnapshot() ([8]byte, bool) {
	s.headerMu.Lock()
	defer s.headerMu.Unlock()
	return s.header, s.hasHeader
}

// registerTCPClient добавляет TCP-сессию клиента для S2C-рассылки.
func (t *tunnel) registerTCPClient(sess *tcpClientSession) {
	if sess == nil || sess.conn == nil {
		return
	}
	t.tcpClientsMu.Lock()
	defer t.tcpClientsMu.Unlock()
	if t.tcpClients == nil {
		t.tcpClients = make(map[net.Conn]*tcpClientSession)
	}
	t.tcpClients[sess.conn] = sess
}

// unregisterTCPClient удаляет TCP-сессию при отключении клиента.
func (t *tunnel) unregisterTCPClient(conn net.Conn) {
	if conn == nil {
		return
	}
	t.tcpClientsMu.Lock()
	defer t.tcpClientsMu.Unlock()
	delete(t.tcpClients, conn)
}

func (t *tunnel) drainInjectedFrames(logger *slog.Logger, clientConn net.Conn, sess *tcpClientSession) int {
	written := 0
	for {
		select {
		case frame := <-sess.inject:
			n, err := clientConn.Write(frame)
			if err != nil {
				if logger != nil {
					logger.Warn("announce write to client failed",
						"local_port", t.info.LocalPort,
						"remote_ip", t.info.RemoteIP,
						"remote_port", t.info.RemotePort,
						"peer", sess.conn.RemoteAddr().String(),
						"frame_len", len(frame),
						"header_hex", protocol.HeaderHex(frame),
						"error", err,
					)
				}
				continue
			}
			if n <= 0 {
				continue
			}
			written += n
			packetID, _ := protocol.ParsePacketID(frame)
			if packetID != uint16(pb.PacketCommand_S2C_OPERATE_ANNOUNCE_NOT) {
				continue
			}
			if logger != nil {
				logger.Info("announce sent to client",
					"local_port", t.info.LocalPort,
					"remote_ip", t.info.RemoteIP,
					"remote_port", t.info.RemotePort,
					"peer", sess.conn.RemoteAddr().String(),
					"packet_id", packetID,
					"bytes_written", n,
					"frame_len", len(frame),
					"header_hex", protocol.HeaderHex(frame),
				)
			}
		default:
			return written
		}
	}
}

// broadcastAnnounce ставит SS2C_OPERATE_ANNOUNCE_NOT в очередь каждой TCP-сессии.
func (t *tunnel) broadcastAnnounce(logger *slog.Logger, body []byte) AnnounceStats {
	packetID := uint16(pb.PacketCommand_S2C_OPERATE_ANNOUNCE_NOT)
	stats := AnnounceStats{}

	t.tcpClientsMu.Lock()
	sessions := make([]*tcpClientSession, 0, len(t.tcpClients))
	for _, sess := range t.tcpClients {
		sessions = append(sessions, sess)
	}
	t.tcpClientsMu.Unlock()

	stats.TCPSessions = len(sessions)
	if len(sessions) == 0 {
		if logger != nil {
			logger.Warn("announce skipped: no tcp sessions on tunnel",
				"local_port", t.info.LocalPort,
				"remote_ip", t.info.RemoteIP,
				"remote_port", t.info.RemotePort,
			)
		}
		return stats
	}

	if logger != nil {
		logger.Info("announce broadcasting on tunnel",
			"local_port", t.info.LocalPort,
			"remote_ip", t.info.RemoteIP,
			"remote_port", t.info.RemotePort,
			"tcp_sessions", len(sessions),
			"packet_id", packetID,
			"proto_body_len", len(body),
		)
	}

	for _, sess := range sessions {
		header, hasHeader := sess.headerSnapshot()
		frame := protocol.EncodeFrameWithHeader(header, packetID, body, hasHeader)
		select {
		case sess.inject <- frame:
			stats.Queued++
			if logger != nil {
				logger.Info("announce queued for client",
					"local_port", t.info.LocalPort,
					"remote_ip", t.info.RemoteIP,
					"remote_port", t.info.RemotePort,
					"peer", sess.conn.RemoteAddr().String(),
					"packet_id", packetID,
					"has_header_template", hasHeader,
					"header_hex", protocol.HeaderHex(frame),
					"frame_len", len(frame),
				)
			}
		default:
			stats.QueueFull++
			if logger != nil {
				logger.Warn("announce not queued: inject channel full",
					"local_port", t.info.LocalPort,
					"remote_ip", t.info.RemoteIP,
					"remote_port", t.info.RemotePort,
					"peer", sess.conn.RemoteAddr().String(),
				)
			}
		}
	}
	return stats
}

// BroadcastAnnounce рассылает объявление по туннелям.
func (m *Manager) BroadcastAnnounce(localPort int, body []byte) AnnounceStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	total := AnnounceStats{}
	for _, tun := range m.tunnels {
		if localPort != 0 && tun.info.LocalPort != localPort {
			continue
		}
		total.Tunnels++
		stats := tun.broadcastAnnounce(m.logger, body)
		total.Queued += stats.Queued
		total.QueueFull += stats.QueueFull
		total.TCPSessions += stats.TCPSessions
	}
	if total.Tunnels == 0 && m.logger != nil {
		m.logger.Warn("announce skipped: no matching tunnels",
			"tunnel_port", localPort,
		)
	}
	return total
}

// BroadcastTCP оставлен для совместимости: шлёт готовый кадр через очередь инъекции.
func (t *tunnel) broadcastTCP(frame []byte) int {
	t.tcpClientsMu.Lock()
	sessions := make([]*tcpClientSession, 0, len(t.tcpClients))
	for _, sess := range t.tcpClients {
		sessions = append(sessions, sess)
	}
	t.tcpClientsMu.Unlock()

	sent := 0
	for _, sess := range sessions {
		select {
		case sess.inject <- append([]byte(nil), frame...):
			sent++
		default:
		}
	}
	return sent
}

// BroadcastTCP отправляет кадр клиентам одного туннеля по локальному порту.
func (m *Manager) BroadcastTCP(localPort int, frame []byte) int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	total := 0
	for _, tun := range m.tunnels {
		if localPort != 0 && tun.info.LocalPort != localPort {
			continue
		}
		total += tun.broadcastTCP(frame)
	}
	return total
}
