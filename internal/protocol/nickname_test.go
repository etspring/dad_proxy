package protocol

import (
	"testing"

	"dad_proxy/internal/pb"
)

func TestDisplayNickNameOriginal(t *testing.T) {
	nick := DisplayNickName(&pb.SACCOUNT_NICKNAME{OriginalNickName: ptr("RogueMain")})
	if nick != "RogueMain" {
		t.Fatalf("got %q", nick)
	}
}

func TestDisplayNickNameStreamingFallback(t *testing.T) {
	nick := DisplayNickName(&pb.SACCOUNT_NICKNAME{StreamingModeNickName: ptr("StreamNick")})
	if nick != "StreamNick" {
		t.Fatalf("got %q", nick)
	}
}

func ptr(s string) *string { return &s }
