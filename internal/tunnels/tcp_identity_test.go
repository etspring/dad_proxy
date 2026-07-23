package tunnels

import (
	"testing"

	"dad_proxy/internal/pb"
	"dad_proxy/internal/protocol"
	"google.golang.org/protobuf/proto"
)

func TestObserveLobbyEnterReqSetsCharacterID(t *testing.T) {
	body, err := proto.Marshal(&pb.SC2S_LOBBY_ENTER_REQ{CharacterId: "char-42"})
	if err != nil {
		t.Fatal(err)
	}
	frame := protocol.EncodeFrame(uint16(pb.PacketCommand_C2S_LOBBY_ENTER_REQ), body)

	sess := newTCPClientSession(nil)
	sess.initIdentity("1.2.3.4:5555", 18000)

	m := &Manager{}
	tun := &tunnel{info: TunnelInfo{LocalPort: 18000}}
	m.observeTCPFrameFromClient(nil, tun, sess, frame)

	snap := sess.identitySnapshot(18000)
	if snap.CharacterID != "char-42" {
		t.Fatalf("character id: got %q", snap.CharacterID)
	}
}

func TestObserveLobbyEnterReqSetsNickName(t *testing.T) {
	body, err := proto.Marshal(&pb.SC2S_LOBBY_ENTER_REQ{CharacterId: "char-42"})
	if err != nil {
		t.Fatal(err)
	}
	frame := protocol.EncodeFrame(uint16(pb.PacketCommand_C2S_LOBBY_ENTER_REQ), body)

	sess := newTCPClientSession(nil)
	sess.initIdentity("1.2.3.4:5555", 18000)
	sess.patchIdentity(18000, func(_ *TCPSessionIdentity, nicks map[string]string) {
		nicks["char-42"] = "TestHero"
	})

	m := &Manager{}
	tun := &tunnel{info: TunnelInfo{LocalPort: 18000}}
	m.observeTCPFrameFromClient(nil, tun, sess, frame)

	snap := sess.identitySnapshot(18000)
	if snap.NickName != "TestHero" {
		t.Fatalf("nick: got %q", snap.NickName)
	}
}
