package protocol

import (
	"encoding/binary"
	"testing"

	"dad_proxy/internal/pb"
	"google.golang.org/protobuf/proto"
)

func TestEncodeFrameRoundTrip(t *testing.T) {
	body := []byte{0x0a, 0x02, 0x08, 0x01}
	frame := EncodeFrame(uint16(pb.PacketCommand_S2C_ALIVE_RES), body)

	packetID, err := ParsePacketID(frame)
	if err != nil {
		t.Fatalf("ParsePacketID: %v", err)
	}
	if packetID != uint16(pb.PacketCommand_S2C_ALIVE_RES) {
		t.Fatalf("packetID=%d want %d", packetID, pb.PacketCommand_S2C_ALIVE_RES)
	}

	gotBody, err := FrameBody(frame)
	if err != nil {
		t.Fatalf("FrameBody: %v", err)
	}
	if string(gotBody) != string(body) {
		t.Fatalf("body mismatch")
	}
}

func TestBuildOperateAnnounceFrame(t *testing.T) {
	frame, err := BuildOperateAnnounceFrame(AnnounceRequest{Message: "test"})
	if err != nil {
		t.Fatalf("BuildOperateAnnounceFrame: %v", err)
	}

	packetID, err := ParsePacketID(frame)
	if err != nil {
		t.Fatalf("ParsePacketID: %v", err)
	}
	if packetID != uint16(pb.PacketCommand_S2C_OPERATE_ANNOUNCE_NOT) {
		t.Fatalf("packetID=%d", packetID)
	}

	body, err := FrameBody(frame)
	if err != nil {
		t.Fatalf("FrameBody: %v", err)
	}

	msg := &pb.SS2C_OPERATE_ANNOUNCE_NOT{}
	if err := proto.Unmarshal(body, msg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(msg.AnnounceList) != 1 {
		t.Fatalf("announce list len=%d", len(msg.AnnounceList))
	}
	if msg.AnnounceList[0].GetAnnounceMessage() != "test" {
		t.Fatalf("message=%q", msg.AnnounceList[0].GetAnnounceMessage())
	}
}

func TestEncodeFramePacketIDAtOffset4(t *testing.T) {
	body := []byte{0x0a, 0x02, 0x08, 0x01}
	packetID := uint16(pb.PacketCommand_S2C_OPERATE_ANNOUNCE_NOT)
	frame := EncodeFrame(packetID, body)

	got := binary.LittleEndian.Uint16(frame[4:6])
	if got != packetID {
		t.Fatalf("packetID at offset 4 = %d want %d", got, packetID)
	}
}

func TestEncodeFrameWithHeaderTemplate(t *testing.T) {
	template := [8]byte{0x20, 0x00, 0x00, 0x00, 0x00, 0x00, 0x0c, 0x00}
	body := []byte{0x08, 0x01}
	frame := EncodeFrameWithHeader(template, uint16(pb.PacketCommand_S2C_OPERATE_ANNOUNCE_NOT), body, true)

	if binary.LittleEndian.Uint32(frame[0:4]) != uint32(len(frame)) {
		t.Fatalf("unexpected total length")
	}
	if binary.LittleEndian.Uint16(frame[4:6]) != uint16(pb.PacketCommand_S2C_OPERATE_ANNOUNCE_NOT) {
		t.Fatalf("packet id not updated in template frame")
	}
}
