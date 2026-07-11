package tunnels

import (
	"testing"
	"time"
)

func TestMarkUDPCreatedSetsIdleClock(t *testing.T) {
	tun := &tunnel{}
	tun.markUDPCreated()
	if tun.info.UDPCreatedAt.IsZero() {
		t.Fatal("udp created at is zero")
	}
}

func TestUDPIIdleSinceAfterMarkUDPActivity(t *testing.T) {
	tun := &tunnel{}
	tun.markUDPActivity()
	_, ok := tun.udpIdleSince()
	if !ok {
		t.Fatal("expected udp idle timestamp")
	}
}

func TestCloseUDPLegClearsUDPCreatedAt(t *testing.T) {
	tun := &tunnel{
		info: TunnelInfo{UDPClientPort: 17000, UDPCreatedAt: time.Now().UTC()},
	}
	tun.closeUDPLeg()
	if !tun.info.UDPCreatedAt.IsZero() {
		t.Fatal("udp created at should be cleared")
	}
}
