package tunnels

import (
	"log/slog"
)

func (t *tunnel) upsertUDPPlayer(player udpPlayerBinding) {
	if player.nickName == "" && player.accountID == "" && player.peer == "" {
		return
	}
	t.udpPlayersMu.Lock()
	defer t.udpPlayersMu.Unlock()
	for i, existing := range t.udpPlayers {
		if player.accountID != "" && existing.accountID == player.accountID {
			t.udpPlayers[i] = player
			return
		}
		if player.peer != "" && existing.peer == player.peer {
			t.udpPlayers[i] = player
			return
		}
	}
	t.udpPlayers = append(t.udpPlayers, player)
}

func (t *tunnel) clearUDPPlayers() {
	t.udpPlayersMu.Lock()
	t.udpPlayers = nil
	t.udpPlayersMu.Unlock()
}

// bindUDPPlayerFromEnterGame привязывает игрока к UDP-туннелю dedi-сервера после входа в матч.
func (m *Manager) bindUDPPlayerFromEnterGame(
	logger *slog.Logger,
	lobbyTun *tunnel,
	sess *tcpClientSession,
	gameIP string,
	gamePort int,
	nickName string,
	accountID string,
) {
	if gameIP == "" || gamePort <= 0 {
		return
	}

	if _, err := m.EnsureTunnel(gameIP, gamePort); err != nil {
		if logger != nil {
			logger.Warn("failed to ensure UDP tunnel for enter game bind",
				"game_ip", gameIP,
				"game_port", gamePort,
				"error", err,
			)
		}
		return
	}

	snap := sess.identitySnapshot(lobbyTun.info.LocalPort)
	nick := nickName
	if nick == "" {
		nick = snap.NickName
	}
	acc := accountID
	if acc == "" {
		acc = snap.AccountID
	}
	if nick == "" {
		return
	}

	key := tunnelKey(gameIP, gamePort)
	m.mu.Lock()
	udpTun := m.tunnels[key]
	m.mu.Unlock()
	if udpTun == nil {
		return
	}

	player := udpPlayerBinding{
		nickName:   nick,
		currentMap: sess.currentMap(),
		accountID:  acc,
		peer:       snap.Peer,
	}
	udpTun.upsertUDPPlayer(player)

	if logger != nil {
		logger.Info("udp tunnel player bound",
			"game_ip", gameIP,
			"game_port", gamePort,
			"udp_client_port", udpTun.info.UDPClientPort,
			"lobby_tcp_port", lobbyTun.info.LocalPort,
			"peer", player.peer,
			"account_id", player.accountID,
			"nick_name", player.nickName,
			"current_map", player.currentMap,
		)
	}
}
