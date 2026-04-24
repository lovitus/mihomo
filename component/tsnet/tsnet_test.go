package tsnet

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	C "github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/transport/socks5"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/types/key"
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

func TestGatewayWildcardListenAddr(t *testing.T) {
	for _, addr := range []string{":1667", "0.0.0.0:1667", "[::]:1667"} {
		if !isWildcardListenAddr(addr) {
			t.Fatalf("isWildcardListenAddr(%q) = false, want true", addr)
		}
	}
	for _, addr := range []string{"127.0.0.1:1667", "[::1]:1667"} {
		if isWildcardListenAddr(addr) {
			t.Fatalf("isWildcardListenAddr(%q) = true, want false", addr)
		}
	}
}

func TestTailnetIdentityResolvesDNSNameAndUniqueHostName(t *testing.T) {
	identity := newTailnetIdentity(&ipnstate.Status{
		Self: &ipnstate.PeerStatus{
			HostName:     "self",
			DNSName:      "self.tail.example.",
			TailscaleIPs: []netip.Addr{netip.MustParseAddr("100.64.0.1")},
		},
		Peer: map[key.NodePublic]*ipnstate.PeerStatus{
			key.NewNode().Public(): {
				HostName:     "Peer",
				DNSName:      "Peer.tail.example.",
				TailscaleIPs: []netip.Addr{netip.MustParseAddr("100.64.0.2"), netip.MustParseAddr("fd7a:115c:a1e0::2")},
			},
		},
	})

	if got := identity.resolveName("PEER.tail.example", addrFamily6); len(got) == 0 || got[0] != netip.MustParseAddr("fd7a:115c:a1e0::2") {
		t.Fatalf("DNSName resolve mismatch: got %v", got)
	}
	if got := identity.resolveName("peer", addrFamily4); len(got) == 0 || got[0] != netip.MustParseAddr("100.64.0.2") {
		t.Fatalf("HostName resolve mismatch: got %v", got)
	}
	if !identity.isSelfName("SELF.tail.example.") || !identity.isSelfIP(netip.MustParseAddr("100.64.0.1")) {
		t.Fatal("self identity was not recorded")
	}
}

func TestTailnetIdentityRejectsConflictingHostName(t *testing.T) {
	identity := newTailnetIdentity(&ipnstate.Status{
		Peer: map[key.NodePublic]*ipnstate.PeerStatus{
			key.NewNode().Public(): {
				HostName:     "dup",
				TailscaleIPs: []netip.Addr{netip.MustParseAddr("100.64.0.2")},
			},
			key.NewNode().Public(): {
				HostName:     "dup",
				TailscaleIPs: []netip.Addr{netip.MustParseAddr("100.64.0.3")},
			},
		},
	})
	if got := identity.resolveName("dup", addrFamily4); got != nil {
		t.Fatalf("conflicting hostname resolved: %v", got)
	}
}

func TestGatewayUDPSessionCapDropsNewSessions(t *testing.T) {
	rt := &runtime{
		gatewayUDPSessions: make(map[string]*gatewayUDPSession),
	}
	for i := 0; i < gatewayUDPMaxSessions; i++ {
		key := net.JoinHostPort("127.0.0.1", strconv.Itoa(10000+i)) + "|100.64.0.1:53"
		rt.gatewayUDPSessions[key] = &gatewayUDPSession{}
	}
	session := rt.gatewayUDPSession(
		&net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 60000},
		netip.MustParseAddrPort("100.64.0.2:53"),
		nil,
	)
	if session != nil {
		t.Fatal("new session was created after gateway UDP cap")
	}
	if len(rt.gatewayUDPSessions) != gatewayUDPMaxSessions {
		t.Fatalf("session map size changed: got %d", len(rt.gatewayUDPSessions))
	}
}

func TestGatewayTCPConnectRelaysData(t *testing.T) {
	rt := newGatewayTestRuntime()
	var remoteConn net.Conn
	dialTarget := make(chan string, 1)
	rt.gatewayDialTCPFn = func(ctx context.Context, target string) (net.Conn, error) {
		left, right := net.Pipe()
		remoteConn = right
		dialTarget <- target
		return left, nil
	}
	rt.gatewayListenUDPFn = func(addr string) (net.PacketConn, error) {
		return nil, errors.New("udp disabled in test")
	}
	rt.startGateway()
	if rt.gatewayTCPListener == nil {
		t.Fatal("gateway TCP listener was not started")
	}
	defer rt.gatewayTCPListener.Close()

	client, err := net.Dial("tcp", rt.gatewayTCPListener.Addr().String())
	if err != nil {
		t.Fatalf("dial gateway: %v", err)
	}
	defer client.Close()

	target := socks5.ParseAddr("100.64.0.2:22")
	if _, err := socks5.ClientHandshake(client, target, socks5.CmdConnect, nil); err != nil {
		t.Fatalf("SOCKS5 connect: %v", err)
	}
	select {
	case got := <-dialTarget:
		if got != "100.64.0.2:22" {
			t.Fatalf("dial target = %q, want 100.64.0.2:22", got)
		}
	case <-time.After(time.Second):
		t.Fatal("gateway did not dial TCP target")
	}
	defer remoteConn.Close()

	if _, err := client.Write([]byte("ping")); err != nil {
		t.Fatalf("client write: %v", err)
	}
	buf := make([]byte, 4)
	if _, err := io.ReadFull(remoteConn, buf); err != nil {
		t.Fatalf("remote read: %v", err)
	}
	if string(buf) != "ping" {
		t.Fatalf("remote payload = %q, want ping", string(buf))
	}
	if _, err := remoteConn.Write([]byte("pong")); err != nil {
		t.Fatalf("remote write: %v", err)
	}
	if _, err := io.ReadFull(client, buf); err != nil {
		t.Fatalf("client read: %v", err)
	}
	if string(buf) != "pong" {
		t.Fatalf("client payload = %q, want pong", string(buf))
	}
}

func TestGatewayTCPDialFailureClosesConnection(t *testing.T) {
	rt := newGatewayTestRuntime()
	var dialCalls atomic.Int32
	rt.gatewayDialTCPFn = func(ctx context.Context, target string) (net.Conn, error) {
		dialCalls.Add(1)
		return nil, errors.New("dial failed")
	}
	client, server := net.Pipe()
	defer client.Close()
	go (&gatewayTunnel{rt: rt}).HandleTCPConn(server, &C.Metadata{
		DstIP:   netip.MustParseAddr("100.64.0.2"),
		DstPort: 22,
	})
	buf := make([]byte, 1)
	if _, err := client.Read(buf); err == nil {
		t.Fatal("client connection stayed open after dial failure")
	}
	if dialCalls.Load() != 1 {
		t.Fatalf("dial calls = %d, want 1", dialCalls.Load())
	}
}

func TestGatewayTCPSelfLoopBlocksDial(t *testing.T) {
	rt := newGatewayTestRuntime()
	rt.gatewayIdentity = newTailnetIdentity(&ipnstate.Status{
		Self: &ipnstate.PeerStatus{
			HostName:     "self",
			DNSName:      "self.tail.example.",
			TailscaleIPs: []netip.Addr{netip.MustParseAddr("100.64.0.1")},
		},
	})
	var dialCalls atomic.Int32
	rt.gatewayDialTCPFn = func(ctx context.Context, target string) (net.Conn, error) {
		dialCalls.Add(1)
		return nil, errors.New("unexpected dial")
	}
	client, server := net.Pipe()
	go (&gatewayTunnel{rt: rt}).HandleTCPConn(server, &C.Metadata{
		Host:    "self.tail.example",
		DstPort: uint16(rt.cfg.Socks5),
	})
	if _, err := client.Read(make([]byte, 1)); err == nil {
		t.Fatal("self-loop TCP connection stayed open")
	}
	if dialCalls.Load() != 0 {
		t.Fatalf("dial calls = %d, want 0", dialCalls.Load())
	}
}

func TestGatewayUDPAssociateRequiresBind(t *testing.T) {
	rt := newGatewayTestRuntime()
	client, server := net.Pipe()
	defer client.Close()
	go rt.serveGatewayConn(server, &gatewayTunnel{rt: rt})

	if _, err := socks5.ClientHandshake(client, socks5.ParseAddr("0.0.0.0:0"), socks5.CmdUDPAssociate, nil); err == nil {
		t.Fatal("UDP ASSOCIATE succeeded without gateway UDP bind")
	}
}

func TestGatewayUDPAssociateReturnsBindAddress(t *testing.T) {
	rt := newGatewayTestRuntime()
	rt.gatewayUDPConn = &fakePacketConn{localAddr: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1667}}
	client, server := net.Pipe()
	defer client.Close()
	go rt.serveGatewayConn(server, &gatewayTunnel{rt: rt})

	reply, err := socks5.ClientHandshake(client, socks5.ParseAddr("0.0.0.0:0"), socks5.CmdUDPAssociate, nil)
	if err != nil {
		t.Fatalf("UDP ASSOCIATE: %v", err)
	}
	if got := reply.String(); got != "127.0.0.1:1667" {
		t.Fatalf("UDP ASSOCIATE bind = %s, want 127.0.0.1:1667", got)
	}
}

func TestGatewayUDPAssociateWildcardBindUsesTCPConnAddr(t *testing.T) {
	rt := newGatewayTestRuntime()
	rt.gatewayUDPConn = &fakePacketConn{localAddr: &net.UDPAddr{IP: net.IPv4zero, Port: 1667}}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen gateway: %v", err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		rt.serveGatewayConn(conn, &gatewayTunnel{rt: rt})
	}()

	client, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial gateway: %v", err)
	}
	defer client.Close()

	reply, err := socks5.ClientHandshake(client, socks5.ParseAddr("0.0.0.0:0"), socks5.CmdUDPAssociate, nil)
	if err != nil {
		t.Fatalf("UDP ASSOCIATE: %v", err)
	}
	host, _, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}
	if got := reply.String(); got != net.JoinHostPort(host, "1667") {
		t.Fatalf("UDP ASSOCIATE bind = %s, want %s", got, net.JoinHostPort(host, "1667"))
	}
}

func TestGatewayCurrentUDPSocksBindUsesTCPConnAddrForWildcardIPv4(t *testing.T) {
	rt := newGatewayTestRuntime()
	rt.gatewayUDPConn = &fakePacketConn{localAddr: &net.UDPAddr{IP: net.IPv4zero, Port: 1667}}
	bind := rt.currentGatewayUDPSocksBind(&fakeConn{localAddr: &net.TCPAddr{IP: net.IPv4(192, 168, 1, 10), Port: 40000}})
	if bind == nil {
		t.Fatal("gateway UDP bind was nil")
	}
	if got := bind.String(); got != "192.168.1.10:1667" {
		t.Fatalf("wildcard IPv4 bind = %s, want 192.168.1.10:1667", got)
	}
}

func TestGatewayCurrentUDPSocksBindUsesTCPConnAddrForWildcardIPv6(t *testing.T) {
	rt := newGatewayTestRuntime()
	rt.gatewayUDPConn = &fakePacketConn{localAddr: &net.UDPAddr{IP: net.IPv6zero, Port: 1667}}
	bind := rt.currentGatewayUDPSocksBind(&fakeConn{localAddr: &net.TCPAddr{IP: net.ParseIP("::1"), Port: 40000}})
	if bind == nil {
		t.Fatal("gateway UDP bind was nil")
	}
	if got := bind.String(); got != "[::1]:1667" {
		t.Fatalf("wildcard IPv6 bind = %s, want [::1]:1667", got)
	}
}

func TestGatewayCurrentUDPSocksBindRejectsWildcardWithoutConcreteTCPAddr(t *testing.T) {
	rt := newGatewayTestRuntime()
	rt.gatewayUDPConn = &fakePacketConn{localAddr: &net.UDPAddr{IP: net.IPv4zero, Port: 1667}}
	if bind := rt.currentGatewayUDPSocksBind(&fakeConn{localAddr: &net.TCPAddr{IP: net.IPv4zero, Port: 40000}}); bind != nil {
		t.Fatalf("wildcard bind without concrete TCP addr = %s, want nil", bind.String())
	}
}

func TestGatewayUDPSessionCreateReuseAndSplitByTarget(t *testing.T) {
	rt := newGatewayTestRuntime()
	var listenCalls atomic.Int32
	rt.gatewayListenPacketFn = func(network, addr string) (net.PacketConn, error) {
		listenCalls.Add(1)
		return newBlockingFakePacketConn(), nil
	}
	clientAddr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 60000}
	targetA := netip.MustParseAddrPort("100.64.0.2:53")
	targetB := netip.MustParseAddrPort("100.64.0.2:54")

	sessionA := rt.gatewayUDPSession(clientAddr, targetA, nil)
	if sessionA == nil {
		t.Fatal("first UDP session was not created")
	}
	defer sessionA.close()
	if got := rt.gatewayUDPSession(clientAddr, targetA, nil); got != sessionA {
		t.Fatal("same client and target did not reuse UDP session")
	}
	sessionB := rt.gatewayUDPSession(clientAddr, targetB, nil)
	if sessionB == nil {
		t.Fatal("second UDP session was not created")
	}
	defer sessionB.close()
	if sessionB == sessionA {
		t.Fatal("different target reused the first UDP session")
	}
	if listenCalls.Load() != 2 {
		t.Fatalf("ListenPacket calls = %d, want 2", listenCalls.Load())
	}
}

func TestGatewayUDPSessionReadLoopWritesBackAndCleansOnError(t *testing.T) {
	rt := newGatewayTestRuntime()
	pc := newBlockingFakePacketConn()
	rt.gatewayListenPacketFn = func(network, addr string) (net.PacketConn, error) {
		return pc, nil
	}
	writeBack := &recordWriteBack{}
	session := rt.gatewayUDPSession(
		&net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 60000},
		netip.MustParseAddrPort("100.64.0.2:53"),
		writeBack,
	)
	if session == nil {
		t.Fatal("UDP session was not created")
	}
	from := &net.UDPAddr{IP: net.IPv4(100, 64, 0, 2), Port: 53}
	pc.pushRead([]byte("reply"), from)
	eventually(t, time.Second, func() bool { return bytes.Equal(writeBack.bytes(), []byte("reply")) })
	if got := writeBack.addr().String(); got != from.String() {
		t.Fatalf("writeback source = %s, want %s", got, from)
	}

	writeBack.fail.Store(true)
	pc.pushRead([]byte("fail"), from)
	select {
	case <-session.done:
	case <-time.After(time.Second):
		t.Fatal("UDP session did not stop after WriteBack error")
	}
	if !pc.closed.Load() {
		t.Fatal("UDP PacketConn was not closed")
	}
	if len(rt.gatewayUDPSessions) != 0 {
		t.Fatalf("UDP session map was not cleaned: %d", len(rt.gatewayUDPSessions))
	}
}

func TestGatewayUDPSessionIdleTimeoutCleansSession(t *testing.T) {
	rt := newGatewayTestRuntime()
	rt.gatewayUDPTimeout = 5 * time.Millisecond
	pc := &fakePacketConn{
		localAddr: &net.UDPAddr{IP: net.IPv4(100, 64, 0, 1), Port: 1},
		readFn: func([]byte) (int, net.Addr, error) {
			return 0, nil, timeoutErr{}
		},
	}
	rt.gatewayListenPacketFn = func(network, addr string) (net.PacketConn, error) {
		return pc, nil
	}
	session := rt.gatewayUDPSession(
		&net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 60000},
		netip.MustParseAddrPort("100.64.0.2:53"),
		nil,
	)
	if session == nil {
		t.Fatal("UDP session was not created")
	}
	select {
	case <-session.done:
	case <-time.After(time.Second):
		t.Fatal("UDP session did not idle-timeout")
	}
	if len(rt.gatewayUDPSessions) != 0 {
		t.Fatalf("UDP session map was not cleaned: %d", len(rt.gatewayUDPSessions))
	}
}

func TestGatewayHandleUDPPacketDropsNewSessionOverCapButKeepsExisting(t *testing.T) {
	rt := newGatewayTestRuntime()
	existingTarget := netip.MustParseAddrPort("100.64.0.2:53")
	existingClient := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 60000}
	existingKey := existingClient.String() + "|" + existingTarget.String()
	existingPC := &fakePacketConn{localAddr: &net.UDPAddr{IP: net.IPv4(100, 64, 0, 1), Port: 1}}
	rt.gatewayUDPSessions[existingKey] = &gatewayUDPSession{pc: existingPC}
	for i := 1; i < gatewayUDPMaxSessions; i++ {
		key := net.JoinHostPort("127.0.0.1", strconv.Itoa(10000+i)) + "|100.64.0.1:53"
		rt.gatewayUDPSessions[key] = &gatewayUDPSession{}
	}
	rt.handleGatewayUDPPacket(&fakeUDPPacket{
		data: []byte("new"),
		addr: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 60001},
	}, &C.Metadata{DstIP: netip.MustParseAddr("100.64.0.3"), DstPort: 53})
	if len(rt.gatewayUDPSessions) != gatewayUDPMaxSessions {
		t.Fatalf("session map size changed after cap drop: %d", len(rt.gatewayUDPSessions))
	}
	rt.handleGatewayUDPPacket(&fakeUDPPacket{
		data: []byte("existing"),
		addr: existingClient,
	}, &C.Metadata{DstIP: existingTarget.Addr(), DstPort: existingTarget.Port()})
	if got := existingPC.writes(); len(got) != 1 || !bytes.Equal(got[0], []byte("existing")) {
		t.Fatalf("existing session write mismatch: %q", got)
	}
}

func TestGatewayStartSkippedWhenGatewaySocks5Empty(t *testing.T) {
	resetTsnetTestState(t)
	rt := newClosedRunTestRuntime()
	rt.cfg = Config{GatewaySocks5: ""}
	rt.gatewayListenTCPFn = func(addr string) (net.Listener, error) {
		t.Fatal("gateway TCP listen should not be called when gateway-socks5 is empty")
		return nil, errors.New("unexpected")
	}
	current.Store(rt)
	rt.startConnectedServices([]netip.Addr{netip.MustParseAddr("100.64.0.1")})
	rt.Close()
}

func TestCloseGatewayUDPSessionsClearsMap(t *testing.T) {
	rt := &runtime{
		gatewayUDPSessions: map[string]*gatewayUDPSession{
			"a": {pc: &fakePacketConn{}},
			"b": {pc: &fakePacketConn{}},
		},
	}
	sessions := rt.closeGatewayUDPSessions()
	if len(sessions) != 2 {
		t.Fatalf("closed sessions = %d, want 2", len(sessions))
	}
	if len(rt.gatewayUDPSessions) != 0 {
		t.Fatalf("session map was not cleared: %d", len(rt.gatewayUDPSessions))
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

func TestAuthNodeKeyFromURL(t *testing.T) {
	if got := authNodeKeyFromURL("http://headscale.example/register/nodekey:abc123"); got != "nodekey:abc123" {
		t.Fatalf("auth nodekey mismatch: got %q", got)
	}
	if got := authNodeKeyFromURL("http://headscale.example/register"); got != "-" {
		t.Fatalf("missing auth nodekey mismatch: got %q", got)
	}
}

func TestReadStateDiagnostic(t *testing.T) {
	stateDir := t.TempDir()

	diag := readStateDiagnostic(stateDir)
	if diag.exists || diag.storeReadable || diag.hasState {
		t.Fatalf("missing state diagnostic mismatch: %+v", diag)
	}

	if err := os.WriteFile(filepath.Join(stateDir, "tailscaled.state"), []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write empty state: %v", err)
	}
	diag = readStateDiagnostic(stateDir)
	if !diag.exists || !diag.storeReadable || diag.hasState {
		t.Fatalf("empty state diagnostic mismatch: %+v", diag)
	}

	if err := os.WriteFile(filepath.Join(stateDir, "tailscaled.state"), []byte(`{"_current-profile":"AQID"}`), 0o600); err != nil {
		t.Fatalf("write populated state: %v", err)
	}
	diag = readStateDiagnostic(stateDir)
	if !diag.exists || !diag.storeReadable || !diag.hasState {
		t.Fatalf("populated state diagnostic mismatch: %+v", diag)
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
	eventually(t, time.Second, func() bool { return !DefaultResolverFailClosed() })

	if got := current.Load().(*runtime); got != nil {
		t.Fatalf("current runtime mismatch: got %#v, want nil", got)
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

func newGatewayTestRuntime() *runtime {
	runDone := make(chan struct{})
	close(runDone)
	rt := &runtime{
		state:                 StateConnected,
		cancelCtx:             make(chan struct{}),
		runDone:               runDone,
		connectedCh:           make(chan struct{}),
		gatewayUDPSessions:    make(map[string]*gatewayUDPSession),
		gatewayLastLogs:       make(map[string]time.Time),
		gatewayUDPTimeout:     0,
		gatewayRefreshAt:      time.Time{},
		gatewayIdentity:       tailnetIdentity{},
		gatewayTCPListener:    nil,
		gatewayUDPConn:        nil,
		gatewayListenTCPFn:    nil,
		gatewayListenUDPFn:    nil,
		gatewayDialTCPFn:      nil,
		gatewayListenPacketFn: nil,
	}
	rt.cfg = Config{
		Socks5:        1666,
		GatewaySocks5: "127.0.0.1:0",
	}
	rt.connected.Store(true)
	rt.tailIPs = []netip.Addr{netip.MustParseAddr("100.64.0.1")}
	return rt
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

type fakePacketConn struct {
	localAddr net.Addr
	readFn    func([]byte) (int, net.Addr, error)
	writeFn   func([]byte, net.Addr) (int, error)
	closeFn   func() error
	closed    atomic.Bool
	mu        sync.Mutex
	written   [][]byte
	writeAddr []net.Addr
	readCh    chan packetRead
	done      chan struct{}
}

type fakeConn struct {
	localAddr net.Addr
}

func (c *fakeConn) Read([]byte) (int, error)        { return 0, io.EOF }
func (c *fakeConn) Write([]byte) (int, error)       { return 0, io.EOF }
func (c *fakeConn) Close() error                    { return nil }
func (c *fakeConn) LocalAddr() net.Addr             { return c.localAddr }
func (c *fakeConn) RemoteAddr() net.Addr            { return &net.TCPAddr{} }
func (c *fakeConn) SetDeadline(time.Time) error     { return nil }
func (c *fakeConn) SetReadDeadline(time.Time) error { return nil }
func (c *fakeConn) SetWriteDeadline(time.Time) error {
	return nil
}

func (c *fakePacketConn) ReadFrom(b []byte) (int, net.Addr, error) {
	if c.readFn != nil {
		return c.readFn(b)
	}
	return 0, nil, io.EOF
}

func (c *fakePacketConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	if c.writeFn != nil {
		return c.writeFn(b, addr)
	}
	c.mu.Lock()
	c.written = append(c.written, append([]byte(nil), b...))
	c.writeAddr = append(c.writeAddr, addr)
	c.mu.Unlock()
	return len(b), nil
}

func (c *fakePacketConn) Close() error {
	c.closed.Store(true)
	if c.closeFn != nil {
		return c.closeFn()
	}
	return nil
}

func (c *fakePacketConn) LocalAddr() net.Addr {
	if c.localAddr != nil {
		return c.localAddr
	}
	return &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}
}

func (c *fakePacketConn) SetDeadline(time.Time) error {
	return nil
}

func (c *fakePacketConn) SetReadDeadline(time.Time) error {
	return nil
}

func (c *fakePacketConn) SetWriteDeadline(time.Time) error {
	return nil
}

func (c *fakePacketConn) writes() [][]byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([][]byte, len(c.written))
	for i := range c.written {
		out[i] = append([]byte(nil), c.written[i]...)
	}
	return out
}

type packetRead struct {
	data []byte
	addr net.Addr
}

func newBlockingFakePacketConn() *fakePacketConn {
	pc := &fakePacketConn{
		localAddr: &net.UDPAddr{IP: net.IPv4(100, 64, 0, 1), Port: 1},
		readCh:    make(chan packetRead, 8),
		done:      make(chan struct{}),
	}
	pc.readFn = func(b []byte) (int, net.Addr, error) {
		select {
		case read := <-pc.readCh:
			return copy(b, read.data), read.addr, nil
		case <-pc.done:
			return 0, nil, io.ErrClosedPipe
		}
	}
	pc.closeFn = func() error {
		select {
		case <-pc.done:
		default:
			close(pc.done)
		}
		return nil
	}
	pc.writeFn = func(b []byte, addr net.Addr) (int, error) {
		pc.mu.Lock()
		pc.written = append(pc.written, append([]byte(nil), b...))
		pc.writeAddr = append(pc.writeAddr, addr)
		pc.mu.Unlock()
		return len(b), nil
	}
	return pc
}

func (c *fakePacketConn) pushRead(data []byte, addr net.Addr) {
	if c == nil || c.readCh == nil {
		return
	}
	c.readCh <- packetRead{data: append([]byte(nil), data...), addr: addr}
}

type timeoutErr struct{}

func (timeoutErr) Error() string {
	return "timeout"
}

func (timeoutErr) Timeout() bool {
	return true
}

func (timeoutErr) Temporary() bool {
	return true
}

type recordWriteBack struct {
	mu   sync.Mutex
	data []byte
	from net.Addr
	fail atomic.Bool
}

func (w *recordWriteBack) WriteBack(b []byte, addr net.Addr) (int, error) {
	if w.fail.Load() {
		return 0, errors.New("writeback failed")
	}
	w.mu.Lock()
	w.data = append([]byte(nil), b...)
	w.from = addr
	w.mu.Unlock()
	return len(b), nil
}

func (w *recordWriteBack) bytes() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]byte(nil), w.data...)
}

func (w *recordWriteBack) addr() net.Addr {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.from
}

type fakeUDPPacket struct {
	data []byte
	addr net.Addr
}

func (p *fakeUDPPacket) Data() []byte {
	return p.data
}

func (p *fakeUDPPacket) WriteBack([]byte, net.Addr) (int, error) {
	return 0, nil
}

func (p *fakeUDPPacket) Drop() {}

func (p *fakeUDPPacket) LocalAddr() net.Addr {
	return p.addr
}
