package tsnet

import (
	"os"
	"testing"
	"time"
)

func TestTailnetSocksActiveConnLimit(t *testing.T) {
	rt := &runtime{}

	for i := 0; i < tailnetSocksMaxActiveConns; i++ {
		if !rt.tryAcquireTailnetSocksConn() {
			t.Fatalf("acquire %d unexpectedly failed", i)
		}
	}
	if rt.tryAcquireTailnetSocksConn() {
		t.Fatal("acquire succeeded after active connection limit")
	}

	rt.releaseTailnetSocksConn()
	if !rt.tryAcquireTailnetSocksConn() {
		t.Fatal("acquire failed after releasing one slot")
	}

	for i := 0; i < tailnetSocksMaxActiveConns; i++ {
		rt.releaseTailnetSocksConn()
	}
	if got := rt.activeSocksConns.Load(); got != 0 {
		t.Fatalf("active connection count mismatch: got %d, want 0", got)
	}
}

func TestTailnetSocksLimitLogInterval(t *testing.T) {
	rt := &runtime{}
	now := time.Unix(1000, 0)

	if !rt.shouldLogTailnetSocksLimit(now) {
		t.Fatal("first limit log should be allowed")
	}
	if rt.shouldLogTailnetSocksLimit(now.Add(tailnetSocksLimitLogInterval - time.Nanosecond)) {
		t.Fatal("limit log should be suppressed inside interval")
	}
	if !rt.shouldLogTailnetSocksLimit(now.Add(tailnetSocksLimitLogInterval)) {
		t.Fatal("limit log should be allowed at interval boundary")
	}
}

func TestDisableTailscaleBackgroundLogUploadsSetsNoLogsKnob(t *testing.T) {
	t.Setenv("TS_NO_LOGS_NO_SUPPORT", "")
	disableTailscaleBackgroundLogUploads()

	if got := os.Getenv("TS_NO_LOGS_NO_SUPPORT"); got != "true" {
		t.Fatalf("TS_NO_LOGS_NO_SUPPORT mismatch: got %q, want %q", got, "true")
	}
}

func TestDefaultResolverCompatibilityModeEnabled(t *testing.T) {
	defaultResolverCompatibilityMode.Store(false)
	t.Cleanup(func() {
		defaultResolverCompatibilityMode.Store(false)
	})

	if DefaultResolverDialAllowed() {
		t.Fatal("resolver compatibility mode unexpectedly enabled")
	}

	enableDefaultResolverCompatibilityMode()

	if !DefaultResolverDialAllowed() {
		t.Fatal("resolver compatibility mode was not enabled")
	}
}
