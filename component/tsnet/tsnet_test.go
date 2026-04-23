package tsnet

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"tailscale.com/ipn/ipnstate"
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

func TestDefaultResolverLifecycleFailClosed(t *testing.T) {
	resetTsnetTestState(t)

	if DefaultResolverFailClosed() {
		t.Fatal("resolver lifecycle unexpectedly active")
	}

	end := beginDefaultResolverLifecycle()
	if !DefaultResolverFailClosed() {
		t.Fatal("resolver lifecycle was not activated")
	}

	end()
	if DefaultResolverFailClosed() {
		t.Fatal("resolver lifecycle did not end")
	}
}

func TestFormatStartupGraceUsesSeconds(t *testing.T) {
	if got := formatStartupGrace(60 * time.Second); got != "60s" {
		t.Fatalf("startup grace format mismatch: got %q, want %q", got, "60s")
	}
}

func TestAuthURLFromTsnetUserLog(t *testing.T) {
	const authURL = "http://headscale.example/register/nodekey:abc"
	msg := "To start this tsnet server, restart with TS_AUTHKEY set, or go to: " + authURL

	got, ok := authURLFromTsnetUserLog(msg)
	if !ok {
		t.Fatal("auth URL was not extracted")
	}
	if got != authURL {
		t.Fatalf("auth URL mismatch: got %q, want %q", got, authURL)
	}

	if _, ok := authURLFromTsnetUserLog("unrelated log"); ok {
		t.Fatal("unrelated log was parsed as auth URL")
	}
}

func TestMarkAuthURLLogSeenDeduplicates(t *testing.T) {
	tailscaleAuthURLLogSeen = sync.Map{}
	t.Cleanup(func() {
		tailscaleAuthURLLogSeen = sync.Map{}
	})

	if !markAuthURLLogSeen("http://headscale.example/register/nodekey:abc") {
		t.Fatal("first auth URL should be logged")
	}
	if markAuthURLLogSeen("http://headscale.example/register/nodekey:abc") {
		t.Fatal("duplicate auth URL should be suppressed")
	}
	if !markAuthURLLogSeen("http://headscale.example/register/nodekey:def") {
		t.Fatal("different auth URL should be logged")
	}
}

func TestDefaultResolverLifecycleRejectsLookupWithoutSystemDial(t *testing.T) {
	resetTsnetTestState(t)

	end := beginDefaultResolverLifecycle()
	defer end()

	var dialCalls atomic.Int32
	oldResolver := net.DefaultResolver
	net.DefaultResolver = &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			dialCalls.Add(1)
			if DefaultResolverFailClosed() {
				return nil, errors.New("net.DefaultResolver disabled while tailscale lifecycle is active")
			}
			t.Fatalf("resolver guard would have used fail-fast path for %s %s", network, address)
			return nil, nil
		},
	}
	t.Cleanup(func() {
		net.DefaultResolver = oldResolver
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := net.DefaultResolver.LookupIPAddr(ctx, "example.com")
	if err == nil {
		t.Fatal("lookup unexpectedly succeeded")
	}
	if !strings.Contains(err.Error(), "tailscale lifecycle is active") {
		t.Fatalf("lookup error mismatch: %v", err)
	}
	if dialCalls.Load() == 0 {
		t.Fatal("resolver guard was not exercised")
	}
}

func TestStartupWatchdogTimeoutDisablesRuntimeAndReleasesLock(t *testing.T) {
	resetTsnetTestState(t)

	stateDir := t.TempDir()
	lock, err := lockStateDir(stateDir)
	if err != nil {
		t.Fatalf("lock state dir: %v", err)
	}
	rt := newClosedRunTestRuntime()
	rt.lock = lock
	current.Store(rt)

	go rt.watchStartupGrace(time.Millisecond)
	eventually(t, time.Second, func() bool { return rt.closed.Load() })

	if got := current.Load().(*runtime); got != nil {
		t.Fatalf("current runtime mismatch: got %#v, want nil", got)
	}
	if DefaultResolverFailClosed() {
		t.Fatal("resolver lifecycle still active after watchdog close")
	}
	lock2, err := lockStateDir(stateDir)
	if err != nil {
		t.Fatalf("state-dir lock was not released: %v", err)
	}
	_ = lock2.Close()
}

func TestStartupWatchdogExitsAfterConnected(t *testing.T) {
	resetTsnetTestState(t)

	rt := newClosedRunTestRuntime()
	current.Store(rt)
	done := make(chan struct{})
	go func() {
		rt.watchStartupGrace(50 * time.Millisecond)
		close(done)
	}()

	status := &ipnstate.Status{
		AuthURL:      "https://headscale.example/register",
		TailscaleIPs: []netip.Addr{netip.MustParseAddr("100.64.0.1")},
	}
	if !rt.markConnected(status) {
		t.Fatal("markConnected unexpectedly failed")
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("watchdog did not exit after connected")
	}
	if got := current.Load().(*runtime); got != rt {
		t.Fatalf("current runtime mismatch: got %#v, want runtime", got)
	}
	if rt.closed.Load() {
		t.Fatal("connected runtime was closed by watchdog")
	}
	snapshot := CurrentSnapshot()
	if !snapshot.Ready || snapshot.State != StateConnected {
		t.Fatalf("snapshot mismatch: ready=%v state=%s", snapshot.Ready, snapshot.State)
	}
	rt.Close()
}

func TestStartupWatchdogDoesNotCloseReloadedRuntime(t *testing.T) {
	resetTsnetTestState(t)

	old := newClosedRunTestRuntime()
	current.Store(old)

	done := make(chan struct{})
	go func() {
		old.watchStartupGrace(time.Millisecond)
		close(done)
	}()

	newRT := newClosedRunTestRuntime()
	current.Store(newRT)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("old watchdog did not exit")
	}
	if got := current.Load().(*runtime); got != newRT {
		t.Fatalf("current runtime mismatch: got %#v, want new runtime", got)
	}
	if newRT.closed.Load() {
		t.Fatal("old watchdog closed the new runtime")
	}
	if !DefaultResolverFailClosed() {
		t.Fatal("resolver lifecycle should remain active while runtimes are still open")
	}

	old.Close()
	if !DefaultResolverFailClosed() {
		t.Fatal("resolver lifecycle ended while new runtime is still open")
	}
	newRT.Close()
	if DefaultResolverFailClosed() {
		t.Fatal("resolver lifecycle still active after both runtimes closed")
	}
}

func TestStartupWatchdogCancelOnClose(t *testing.T) {
	resetTsnetTestState(t)

	rt := newClosedRunTestRuntime()
	current.Store(rt)
	done := make(chan struct{})
	go func() {
		rt.watchStartupGrace(time.Hour)
		close(done)
	}()

	rt.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("watchdog did not exit after Close")
	}
}

func TestLateConnectedAfterWatchdogTimeoutDoesNotReviveRuntime(t *testing.T) {
	resetTsnetTestState(t)

	rt := newClosedRunTestRuntime()
	current.Store(rt)
	rt.disableAfterStartupGrace(time.Millisecond)

	status := &ipnstate.Status{
		AuthURL:      "https://headscale.example/register",
		TailscaleIPs: []netip.Addr{netip.MustParseAddr("100.64.0.1")},
	}
	if rt.markConnected(status) {
		t.Fatal("closed runtime was revived by late connected status")
	}
	if got := current.Load().(*runtime); got != nil {
		t.Fatalf("current runtime mismatch: got %#v, want nil", got)
	}
	snapshot := CurrentSnapshot()
	if snapshot.Ready || snapshot.State != StateDisabled {
		t.Fatalf("snapshot mismatch after timeout: ready=%v state=%s", snapshot.Ready, snapshot.State)
	}
}

func resetTsnetTestState(t *testing.T) {
	old := current.Swap((*runtime)(nil)).(*runtime)
	if old != nil {
		old.Close()
	}
	defaultResolverLifecycle.Store(0)
	t.Cleanup(func() {
		old := current.Swap((*runtime)(nil)).(*runtime)
		if old != nil {
			old.Close()
		}
		defaultResolverLifecycle.Store(0)
	})
}

func newClosedRunTestRuntime() *runtime {
	runDone := make(chan struct{})
	close(runDone)
	return &runtime{
		state:                StateRegistering,
		cancelCtx:            make(chan struct{}),
		runDone:              runDone,
		connectedCh:          make(chan struct{}),
		endResolverLifecycle: beginDefaultResolverLifecycle(),
	}
}

func eventually(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	if condition() {
		return
	}
	t.Fatal("condition was not met before timeout")
}
