package tunnels

import (
	"log/slog"
	"strings"
	"time"

	"dad_proxy/internal/pb"
	"dad_proxy/internal/protocol"
	"google.golang.org/protobuf/proto"
)

// TCPSessionIdentity описывает игрока, привязанного к TCP-сессии туннеля.
type TCPSessionIdentity struct {
	Peer         string    `json:"peer"`
	TunnelPort   int       `json:"tunnelPort"`
	AccountID    string    `json:"accountId,omitempty"`
	CharacterID  string    `json:"characterId,omitempty"`
	NickName     string    `json:"nickName,omitempty"`
	ConnectedAt  time.Time `json:"connectedAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
	dungeonIdTag string
	gameType     uint32
}

func (s *tcpClientSession) initIdentity(peer string, tunnelPort int) {
	now := time.Now().UTC()
	s.identityMu.Lock()
	s.identity = TCPSessionIdentity{
		Peer:        peer,
		TunnelPort:  tunnelPort,
		ConnectedAt: now,
		UpdatedAt:   now,
	}
	if s.characterNicks == nil {
		s.characterNicks = make(map[string]string)
	}
	s.identityMu.Unlock()
}

func (s *tcpClientSession) identitySnapshot(tunnelPort int) TCPSessionIdentity {
	s.identityMu.RLock()
	defer s.identityMu.RUnlock()
	out := s.identity
	if out.TunnelPort == 0 {
		out.TunnelPort = tunnelPort
	}
	return out
}

func (s *tcpClientSession) patchIdentity(tunnelPort int, fn func(*TCPSessionIdentity, map[string]string)) {
	s.identityMu.Lock()
	defer s.identityMu.Unlock()
	if s.identity.TunnelPort == 0 {
		s.identity.TunnelPort = tunnelPort
	}
	if s.characterNicks == nil {
		s.characterNicks = make(map[string]string)
	}
	fn(&s.identity, s.characterNicks)
	s.identity.UpdatedAt = time.Now().UTC()
}

func (s *tcpClientSession) patchMatchSelection(dungeonIdTag string, gameType uint32) {
	s.identityMu.Lock()
	defer s.identityMu.Unlock()
	if tag := strings.TrimSpace(dungeonIdTag); tag != "" {
		s.identity.dungeonIdTag = tag
	}
	if gameType != 0 {
		s.identity.gameType = gameType
	}
	s.identity.UpdatedAt = time.Now().UTC()
}

func (s *tcpClientSession) currentMap() string {
	s.identityMu.RLock()
	defer s.identityMu.RUnlock()
	return protocol.FormatCurrentMap(s.identity.dungeonIdTag, s.identity.gameType)
}

// observeTCPFrameFromRemote разбирает S2C-кадры upstream для идентификации игрока.
func (m *Manager) observeTCPFrameFromRemote(logger *slog.Logger, tun *tunnel, sess *tcpClientSession, frame []byte) {
	packetID, err := protocol.ParsePacketID(frame)
	if err != nil {
		return
	}
	body, err := protocol.FrameBody(frame)
	if err != nil {
		return
	}

	port := tun.info.LocalPort
	switch packetID {
	case uint16(pb.PacketCommand_S2C_ACCOUNT_LOGIN_RES):
		var msg pb.SS2C_ACCOUNT_LOGIN_RES
		if err := proto.Unmarshal(body, &msg); err != nil {
			return
		}
		accountID := msg.GetAccountId()
		if accountID == "" {
			return
		}
		sess.patchIdentity(port, func(id *TCPSessionIdentity, _ map[string]string) {
			id.AccountID = accountID
		})
		m.logIdentityUpdate(logger, tun, sess, "account_login", accountID, "")

	case uint16(pb.PacketCommand_S2C_ACCOUNT_CHARACTER_LIST_RES):
		var msg pb.SS2C_ACCOUNT_CHARACTER_LIST_RES
		if err := proto.Unmarshal(body, &msg); err != nil {
			return
		}
		sess.patchIdentity(port, func(_ *TCPSessionIdentity, nicks map[string]string) {
			for _, ch := range msg.GetCharacterList() {
				if ch == nil {
					continue
				}
				cid := ch.GetCharacterId()
				nick := protocol.DisplayNickName(ch.GetNickName())
				if cid != "" && nick != "" {
					nicks[cid] = nick
				}
			}
		})

	case uint16(pb.PacketCommand_S2C_LOBBY_CHARACTER_INFO_RES):
		var msg pb.SS2C_LOBBY_CHARACTER_INFO_RES
		if err := proto.Unmarshal(body, &msg); err != nil {
			return
		}
		data := msg.GetCharacterDataBase()
		if data == nil {
			return
		}
		charID := data.GetCharacterId()
		nick := protocol.DisplayNickName(data.GetNickName())
		accountID := data.GetAccountId()
		sess.patchIdentity(port, func(id *TCPSessionIdentity, nicks map[string]string) {
			if charID != "" {
				id.CharacterID = charID
				if nick != "" {
					id.NickName = nick
					nicks[charID] = nick
				}
			}
			if accountID != "" {
				id.AccountID = accountID
			}
		})
		m.logIdentityUpdate(logger, tun, sess, "lobby_character_info", charID, nick)
		m.maybeSendWelcomeAnnounce(logger, tun, sess)

	case uint16(pb.PacketCommand_S2C_LOBBY_GAME_TYPE_SELECT_RES):
		var msg pb.SS2C_LOBBY_GAME_TYPE_SELECT_RES
		if err := proto.Unmarshal(body, &msg); err != nil {
			return
		}
		sess.patchMatchSelection(msg.GetDungeonIdTag(), msg.GetGameTypeIndex())

	case uint16(pb.PacketCommand_S2C_PARTY_GAME_TYPE_CHANGE_NOT):
		var msg pb.SS2C_PARTY_GAME_TYPE_CHANGE_NOT
		if err := proto.Unmarshal(body, &msg); err != nil {
			return
		}
		sess.patchMatchSelection(msg.GetDungeonIdTag(), msg.GetGameTypeIndex())

	case uint16(pb.PacketCommand_S2C_ENTER_GAME_SERVER_NOT):
		var msg pb.SS2C_ENTER_GAME_SERVER_NOT
		if err := proto.Unmarshal(body, &msg); err != nil {
			return
		}
		nick := protocol.DisplayNickName(msg.GetNickName())
		m.bindUDPPlayerFromEnterGame(logger, tun, sess, msg.GetIp(), int(msg.GetPort()), nick, msg.GetAccountId())
	}
}

// observeTCPFrameFromClient разбирает C2S-кадры клиента (выбор персонажа).
func (m *Manager) observeTCPFrameFromClient(logger *slog.Logger, tun *tunnel, sess *tcpClientSession, frame []byte) {
	packetID, err := protocol.ParsePacketID(frame)
	if err != nil {
		return
	}
	body, err := protocol.FrameBody(frame)
	if err != nil {
		return
	}

	port := tun.info.LocalPort
	switch packetID {
	case uint16(pb.PacketCommand_C2S_LOBBY_ENTER_REQ):
		var msg pb.SC2S_LOBBY_ENTER_REQ
		if err := proto.Unmarshal(body, &msg); err != nil {
			return
		}
		charID := msg.GetCharacterId()
		if charID == "" {
			return
		}
		sess.patchIdentity(port, func(id *TCPSessionIdentity, nicks map[string]string) {
			id.CharacterID = charID
			if nick, ok := nicks[charID]; ok && nick != "" {
				id.NickName = nick
			}
		})
		snap := sess.identitySnapshot(port)
		m.logIdentityUpdate(logger, tun, sess, "character_select", charID, snap.NickName)

	case uint16(pb.PacketCommand_C2S_LOBBY_GAME_TYPE_SELECT_REQ):
		var msg pb.SC2S_LOBBY_GAME_TYPE_SELECT_REQ
		if err := proto.Unmarshal(body, &msg); err != nil {
			return
		}
		sess.patchMatchSelection(msg.GetDungeonIdTag(), msg.GetGameTypeIndex())

	case uint16(pb.PacketCommand_C2S_AUTO_MATCH_REG_REQ):
		var msg pb.SC2S_AUTO_MATCH_REG_REQ
		if err := proto.Unmarshal(body, &msg); err != nil {
			return
		}
		if msg.GetMode() != uint32(pb.SC2S_AUTO_MATCH_REG_REQ_REGISTER) {
			return
		}
		sess.patchMatchSelection(msg.GetDungeonIdTag(), msg.GetGameType())
	}
}

func (m *Manager) logIdentityUpdate(logger *slog.Logger, tun *tunnel, sess *tcpClientSession, event, characterID, nick string) {
	if logger == nil {
		return
	}
	snap := sess.identitySnapshot(tun.info.LocalPort)
	logger.Info("tcp session identity updated",
		"event", event,
		"local_port", tun.info.LocalPort,
		"remote_ip", tun.info.RemoteIP,
		"remote_port", tun.info.RemotePort,
		"peer", snap.Peer,
		"account_id", snap.AccountID,
		"character_id", characterID,
		"nick_name", nick,
	)
}

// ListTCPSessions возвращает снимок всех активных TCP-сессий с идентификацией.
func (m *Manager) ListTCPSessions() []TCPSessionIdentity {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]TCPSessionIdentity, 0)
	for _, tun := range m.tunnels {
		tun.tcpClientsMu.Lock()
		for _, sess := range tun.tcpClients {
			out = append(out, sess.identitySnapshot(tun.info.LocalPort))
		}
		tun.tcpClientsMu.Unlock()
	}
	return out
}
