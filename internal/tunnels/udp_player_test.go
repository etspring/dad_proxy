package tunnels

import (
	"testing"
	"time"

	"dad_proxy/internal/pb"
	"dad_proxy/internal/protocol"
	"google.golang.org/protobuf/proto"
)

func TestBindUDPPlayerFromEnterGameSetsPlayerCount(t *testing.T) {
	m := &Manager{tunnels: make(map[string]*tunnel)}
	lobbyTun := &tunnel{info: TunnelInfo{LocalPort: 20201, RemoteIP: "10.0.0.1", RemotePort: 7000}}
	sess := newTCPClientSession(nil)
	sess.initIdentity("95.24.181.120:5808", 20201)
	sess.patchIdentity(20201, func(id *TCPSessionIdentity, _ map[string]string) {
		id.AccountID = "4048673"
		id.CharacterID = "17959245"
		id.NickName = "BogKuzya"
	})

	gameTun := &tunnel{
		info: TunnelInfo{
			RemoteIP:      "52.1.2.3",
			RemotePort:    7777,
			UDPClientPort: 17055,
			UDPCreatedAt:  time.Now().UTC(),
		},
	}
	m.tunnels[tunnelKey("52.1.2.3", 7777)] = gameTun

	body, err := proto.Marshal(&pb.SS2C_ENTER_GAME_SERVER_NOT{
		Ip:        ptr("52.1.2.3"),
		Port:      ptrUint32(7777),
		AccountId: ptr("4048673"),
		NickName:  &pb.SACCOUNT_NICKNAME{OriginalNickName: ptr("BogKuzya")},
	})
	if err != nil {
		t.Fatal(err)
	}
	frame := protocol.EncodeFrame(uint16(pb.PacketCommand_S2C_ENTER_GAME_SERVER_NOT), body)
	m.observeTCPFrameFromRemote(nil, lobbyTun, sess, frame)

	info := gameTun.snapshot()
	if len(info.UDPPlayers) != 1 {
		t.Fatalf("players: got %d", len(info.UDPPlayers))
	}
}

func TestBindUDPPlayerFromEnterGameSetsNickName(t *testing.T) {
	m := &Manager{tunnels: make(map[string]*tunnel)}
	lobbyTun := &tunnel{info: TunnelInfo{LocalPort: 20201}}
	sess := newTCPClientSession(nil)
	sess.initIdentity("95.24.181.120:5808", 20201)

	gameTun := &tunnel{info: TunnelInfo{RemoteIP: "52.1.2.3", RemotePort: 7777, UDPClientPort: 17055}}
	m.tunnels[tunnelKey("52.1.2.3", 7777)] = gameTun

	body, err := proto.Marshal(&pb.SS2C_ENTER_GAME_SERVER_NOT{
		Ip:       ptr("52.1.2.3"),
		Port:     ptrUint32(7777),
		NickName: &pb.SACCOUNT_NICKNAME{OriginalNickName: ptr("BogKuzya")},
	})
	if err != nil {
		t.Fatal(err)
	}
	frame := protocol.EncodeFrame(uint16(pb.PacketCommand_S2C_ENTER_GAME_SERVER_NOT), body)
	m.observeTCPFrameFromRemote(nil, lobbyTun, sess, frame)

	info := gameTun.snapshot()
	if info.UDPPlayers[0].NickName != "BogKuzya" {
		t.Fatalf("nick: got %q", info.UDPPlayers[0].NickName)
	}
}

func TestBindUDPPlayerFromEnterGameSetsCurrentMap(t *testing.T) {
	m := &Manager{tunnels: make(map[string]*tunnel)}
	lobbyTun := &tunnel{info: TunnelInfo{LocalPort: 20201}}
	sess := newTCPClientSession(nil)
	sess.initIdentity("95.24.181.120:5808", 20201)

	gameTun := &tunnel{info: TunnelInfo{RemoteIP: "52.1.2.3", RemotePort: 7777, UDPClientPort: 17055, UDPCreatedAt: time.Now().UTC()}}
	m.tunnels[tunnelKey("52.1.2.3", 7777)] = gameTun

	selectBody, err := proto.Marshal(&pb.SC2S_LOBBY_GAME_TYPE_SELECT_REQ{
		GameTypeIndex: ptrUint32(1),
		DungeonIdTag:  ptr("GoblinCave"),
	})
	if err != nil {
		t.Fatal(err)
	}
	selectFrame := protocol.EncodeFrame(uint16(pb.PacketCommand_C2S_LOBBY_GAME_TYPE_SELECT_REQ), selectBody)
	m.observeTCPFrameFromClient(nil, lobbyTun, sess, selectFrame)

	enterBody, err := proto.Marshal(&pb.SS2C_ENTER_GAME_SERVER_NOT{
		Ip:       ptr("52.1.2.3"),
		Port:     ptrUint32(7777),
		NickName: &pb.SACCOUNT_NICKNAME{OriginalNickName: ptr("BogKuzya")},
	})
	if err != nil {
		t.Fatal(err)
	}
	enterFrame := protocol.EncodeFrame(uint16(pb.PacketCommand_S2C_ENTER_GAME_SERVER_NOT), enterBody)
	m.observeTCPFrameFromRemote(nil, lobbyTun, sess, enterFrame)

	info := gameTun.snapshot()
	if info.UDPPlayers[0].CurrentMap != "GoblinCave" {
		t.Fatalf("currentMap: got %q", info.UDPPlayers[0].CurrentMap)
	}
}

func TestUDPTunnelStatsFromInfoIncludesPlayers(t *testing.T) {
	stats := UDPTunnelStatsFromInfo(TunnelInfo{
		UDPPlayers: []UDPTunnelPlayer{{NickName: "BogKuzya"}},
	})
	if len(stats.Players) != 1 {
		t.Fatalf("players: got %d", len(stats.Players))
	}
}

func TestUDPTunnelStatsFromInfoIncludesCurrentMap(t *testing.T) {
	stats := UDPTunnelStatsFromInfo(TunnelInfo{
		UDPPlayers: []UDPTunnelPlayer{{NickName: "BogKuzya", CurrentMap: "GoblinCave"}},
	})
	if stats.Players[0].CurrentMap != "GoblinCave" {
		t.Fatalf("currentMap: got %q", stats.Players[0].CurrentMap)
	}
}

func ptrUint32(v uint32) *uint32 { return &v }
