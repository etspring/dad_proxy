package tunnels

import (
	"testing"
	"time"
)

func TestUDPTunnelStatsFromInfoIncludesCreatedAt(t *testing.T) {
	created := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	stats := UDPTunnelStatsFromInfo(TunnelInfo{
		RemoteIP:          "10.0.0.1",
		RemotePort:        7777,
		UDPClientPort:     17001,
		UDPCreatedAt:      created,
		UDPLastActivityAt: created.Add(time.Minute),
	})
	if !stats.CreatedAt.Equal(created) {
		t.Fatalf("createdAt: got %v", stats.CreatedAt)
	}
}
