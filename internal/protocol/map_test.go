package protocol

import "testing"

func TestFormatCurrentMapReturnsTag(t *testing.T) {
	got := FormatCurrentMap("IceCave_HR", 3)
	if got != "IceCave_HR" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatCurrentMapEmptyWithoutTag(t *testing.T) {
	got := FormatCurrentMap("", 3)
	if got != "" {
		t.Fatalf("got %q", got)
	}
}
