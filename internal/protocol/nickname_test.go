package protocol

import (
	"testing"

	"dad_proxy/internal/pb"
)

func TestDisplayNickNamePrefersOriginal(t *testing.T) {
	nick := DisplayNickName(&pb.SACCOUNT_NICKNAME{OriginalNickName: "RogueMain"})
	if nick != "RogueMain" {
		t.Fatalf("got %q", nick)
	}
}

func TestDisplayNickNameFallsBackToStreaming(t *testing.T) {
	nick := DisplayNickName(&pb.SACCOUNT_NICKNAME{StreamingModeNickName: "StreamNick"})
	if nick != "StreamNick" {
		t.Fatalf("got %q", nick)
	}
}
