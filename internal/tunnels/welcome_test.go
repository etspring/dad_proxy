package tunnels

import (
	"net"
	"testing"
	"time"

	"dad_proxy/internal/pb"
	"dad_proxy/internal/protocol"
	"google.golang.org/protobuf/proto"
)

func TestTryMarkWelcomeSentOnlyOnce(t *testing.T) {
	sess := newTCPClientSession(nil)
	if !sess.tryMarkWelcomeSent() {
		t.Fatal("first mark should succeed")
	}
	if sess.tryMarkWelcomeSent() {
		t.Fatal("second mark should fail")
	}
}

func TestMaybeSendWelcomeAnnounceDoesNotQueueImmediately(t *testing.T) {
	m := &Manager{tunnels: make(map[string]*tunnel)}
	tun := &tunnel{info: TunnelInfo{LocalPort: 20201}}
	sess := newTCPClientSession(nil)

	m.maybeSendWelcomeAnnounce(nil, tun, sess)

	select {
	case <-sess.inject:
		t.Fatal("welcome should not be queued immediately")
	case <-time.After(10 * time.Millisecond):
	}
}

func TestDeliverWelcomeAnnounceDelayedQueuesFrame(t *testing.T) {
	m := &Manager{tunnels: make(map[string]*tunnel)}
	tun := &tunnel{info: TunnelInfo{LocalPort: 20201}}
	sess := newTCPClientSession(nil)
	sess.initIdentity("127.0.0.1:51642", 20201)
	tun.tcpClientsMu.Lock()
	if tun.tcpClients == nil {
		tun.tcpClients = make(map[net.Conn]*tcpClientSession)
	}
	tun.tcpClients[nil] = sess
	tun.tcpClientsMu.Unlock()
	m.tunnels["test"] = tun

	headerFrame := protocol.EncodeFrame(uint16(pb.PacketCommand_S2C_ALIVE_RES), nil)
	sess.rememberHeader(headerFrame)

	m.deliverWelcomeAnnounceDelayed(nil, tun.info.LocalPort, "127.0.0.1:51642", 0)

	select {
	case frame := <-sess.inject:
		packetID, err := protocol.ParsePacketID(frame)
		if err != nil {
			t.Fatal(err)
		}
		if packetID != uint16(pb.PacketCommand_S2C_OPERATE_ANNOUNCE_NOT) {
			t.Fatalf("packet id: got %d", packetID)
		}
	default:
		t.Fatal("expected welcome frame in inject queue")
	}
}

func TestWelcomeAnnouncePayloadContainsMessage(t *testing.T) {
	body, err := welcomeAnnouncePayload()
	if err != nil {
		t.Fatal(err)
	}
	var msg pb.SS2C_OPERATE_ANNOUNCE_NOT
	if err := proto.Unmarshal(body, &msg); err != nil {
		t.Fatal(err)
	}
	if len(msg.GetAnnounceList()) != 1 {
		t.Fatalf("announce list len: %d", len(msg.GetAnnounceList()))
	}
	if msg.GetAnnounceList()[0].GetAnnounceMessage() != welcomeAnnounceMessage {
		t.Fatalf("message: got %q", msg.GetAnnounceList()[0].GetAnnounceMessage())
	}
}
