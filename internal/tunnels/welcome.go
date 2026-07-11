package tunnels

import (
	"log/slog"
	"sync"
	"time"

	"dad_proxy/internal/pb"
	"dad_proxy/internal/protocol"
)

const (
	welcomeAnnounceMessage = "Welcome to Progulka`s dad_proxy, details on cadiastands.ru. GLHF!"
	welcomeAnnounceDelay   = 20 * time.Second
)

var (
	welcomeAnnounceBodyOnce sync.Once
	welcomeAnnounceBody     []byte
	welcomeAnnounceBodyErr  error
)

func welcomeAnnouncePayload() ([]byte, error) {
	welcomeAnnounceBodyOnce.Do(func() {
		welcomeAnnounceBody, welcomeAnnounceBodyErr = protocol.BuildOperateAnnounceBody(protocol.AnnounceRequest{
			Message: welcomeAnnounceMessage,
		})
	})
	return welcomeAnnounceBody, welcomeAnnounceBodyErr
}

func (s *tcpClientSession) tryMarkWelcomeSent() bool {
	s.identityMu.Lock()
	defer s.identityMu.Unlock()
	if s.welcomeSent {
		return false
	}
	s.welcomeSent = true
	return true
}

// findTCPClientSessionByPeer ищет активную TCP-сессию по локальному порту и peer.
func (m *Manager) findTCPClientSessionByPeer(localPort int, peer string) (*tunnel, *tcpClientSession) {
	if peer == "" {
		return nil, nil
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, tun := range m.tunnels {
		if localPort != 0 && tun.info.LocalPort != localPort {
			continue
		}
		tun.tcpClientsMu.Lock()
		for _, sess := range tun.tcpClients {
			snap := sess.identitySnapshot(tun.info.LocalPort)
			if snap.Peer == peer {
				tun.tcpClientsMu.Unlock()
				return tun, sess
			}
			if sess.conn != nil && sess.conn.RemoteAddr().String() == peer {
				tun.tcpClientsMu.Unlock()
				return tun, sess
			}
		}
		tun.tcpClientsMu.Unlock()
	}
	return nil, nil
}

// queueAnnounceToSession ставит SS2C_OPERATE_ANNOUNCE_NOT в очередь одной TCP-сессии.
func (t *tunnel) queueAnnounceToSession(logger *slog.Logger, sess *tcpClientSession, body []byte) bool {
	if sess == nil || len(body) == 0 {
		return false
	}
	packetID := uint16(pb.PacketCommand_S2C_OPERATE_ANNOUNCE_NOT)
	header, hasHeader := sess.headerSnapshot()
	frame := protocol.EncodeFrameWithHeader(header, packetID, body, hasHeader)
	select {
	case sess.inject <- frame:
		if logger != nil {
			peer := ""
			if sess.conn != nil {
				peer = sess.conn.RemoteAddr().String()
			}
			logger.Info("announce queued for client",
				"local_port", t.info.LocalPort,
				"remote_ip", t.info.RemoteIP,
				"remote_port", t.info.RemotePort,
				"peer", peer,
				"packet_id", packetID,
				"has_header_template", hasHeader,
				"header_hex", protocol.HeaderHex(frame),
				"frame_len", len(frame),
				"reason", "welcome_lobby",
			)
		}
		return true
	default:
		if logger != nil {
			peer := ""
			if sess.conn != nil {
				peer = sess.conn.RemoteAddr().String()
			}
			logger.Warn("announce not queued: inject channel full",
				"local_port", t.info.LocalPort,
				"peer", peer,
				"reason", "welcome_lobby",
			)
		}
		return false
	}
}

func (m *Manager) deliverWelcomeAnnounceDelayed(logger *slog.Logger, localPort int, peer string, delay time.Duration) {
	if delay > 0 {
		time.Sleep(delay)
	}

	tun, sess := m.findTCPClientSessionByPeer(localPort, peer)
	if tun == nil || sess == nil {
		if logger != nil {
			logger.Info("welcome announce skipped: session disconnected",
				"local_port", localPort,
			)
		}
		return
	}

	body, err := welcomeAnnouncePayload()
	if err != nil {
		if logger != nil {
			logger.Warn("welcome announce skipped: build protobuf failed", "error", err)
		}
		return
	}

	if !tun.queueAnnounceToSession(logger, sess, body) {
		return
	}

	if logger != nil {
		snap := sess.identitySnapshot(tun.info.LocalPort)
		logger.Info("welcome announce delivered",
			"local_port", tun.info.LocalPort,
			"peer", snap.Peer,
			"nick_name", snap.NickName,
			"delay", delay,
		)
	}
}

// maybeSendWelcomeAnnounce планирует приветствие через welcomeAnnounceDelay после входа в лобби.
func (m *Manager) maybeSendWelcomeAnnounce(logger *slog.Logger, tun *tunnel, sess *tcpClientSession) {
	if !sess.tryMarkWelcomeSent() {
		return
	}

	localPort := tun.info.LocalPort
	snap := sess.identitySnapshot(localPort)
	peer := snap.Peer

	if logger != nil {
		logger.Info("welcome announce scheduled",
			"local_port", localPort,
			"peer", peer,
			"nick_name", snap.NickName,
			"delay", welcomeAnnounceDelay,
		)
	}

	go m.deliverWelcomeAnnounceDelayed(logger, localPort, peer, welcomeAnnounceDelay)
}
