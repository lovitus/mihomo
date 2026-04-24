package tsnet

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/metacubex/mihomo/adapter/inbound"
	CN "github.com/metacubex/mihomo/common/net"
	C "github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/listener/sockscommon"
	"github.com/metacubex/mihomo/log"
	"github.com/metacubex/mihomo/transport/socks5"
	"tailscale.com/ipn/ipnstate"
)

const gatewayInboundName = "TAILSCALE-GATEWAY-SOCKS"

type gatewayTunnel struct {
	rt *runtime
}

func (g *gatewayTunnel) HandleTCPConn(conn net.Conn, metadata *C.Metadata) {
	if g == nil || g.rt == nil {
		_ = conn.Close()
		return
	}
	rt := g.rt
	if rt.closed.Load() || !rt.connected.Load() {
		_ = conn.Close()
		return
	}
	if rt.isGatewaySelfLoop(metadata.String(), metadata.DstPort) {
		rt.logGatewayLimited("gateway_self_loop_blocked", "[Tailscale] event=gateway_self_loop_blocked network=tcp target=%s", metadata.RemoteAddress())
		_ = conn.Close()
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), C.DefaultTCPTimeout)
	defer cancel()
	remote, err := rt.gatewayDialTCP(ctx, metadata.RemoteAddress())
	if err != nil {
		log.Debugln("[Tailscale] gateway socks5 tcp dial failed target=%s err=%v", metadata.RemoteAddress(), err)
		_ = conn.Close()
		return
	}
	CN.Relay(conn, remote)
}

func (g *gatewayTunnel) HandleUDPPacket(packet C.UDPPacket, metadata *C.Metadata) {
	if g == nil || g.rt == nil {
		packet.Drop()
		return
	}
	g.rt.handleGatewayUDPPacket(packet, metadata)
}

func (g *gatewayTunnel) NatTable() C.NatTable {
	return nil
}

func (r *runtime) startGateway() {
	addr := r.cfg.GatewaySocks5
	if isWildcardListenAddr(addr) {
		log.Warnln("[Tailscale] gateway-socks5 listens on %s; tailnet access is exposed to this host network", addr)
	}

	ln, err := r.gatewayListenTCP(addr)
	if err != nil {
		log.Warnln("[Tailscale] gateway-socks5 tcp listen failed addr=%s err=%v", addr, err)
		return
	}

	var udpConn net.PacketConn
	if pc, err := r.gatewayListenUDP(addr); err != nil {
		log.Warnln("[Tailscale] gateway-socks5 udp listen failed addr=%s err=%v", addr, err)
	} else {
		udpConn = pc
	}

	r.mu.Lock()
	if r.closed.Load() {
		r.mu.Unlock()
		_ = ln.Close()
		if udpConn != nil {
			_ = udpConn.Close()
		}
		return
	}
	r.gatewayTCPListener = ln
	r.gatewayUDPConn = udpConn
	r.mu.Unlock()

	tunnel := &gatewayTunnel{rt: r}
	if udpConn != nil {
		go sockscommon.ServePacketConn(udpConn, tunnel, r.closed.Load,
			inbound.WithInName(gatewayInboundName),
			inbound.WithSpecialRules(""),
		)
	}
	go r.serveGatewayTCP(ln, tunnel)
	log.Infoln("[Tailscale] gateway-socks5 tcp listening on %s", ln.Addr())
	if udpConn != nil {
		log.Infoln("[Tailscale] gateway-socks5 udp listening on %s", udpConn.LocalAddr())
	}
}

func (r *runtime) serveGatewayTCP(ln net.Listener, tunnel C.Tunnel) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go r.serveGatewayConn(conn, tunnel)
	}
}

func (r *runtime) serveGatewayConn(conn net.Conn, tunnel C.Tunnel) {
	udpBind := r.currentGatewayUDPSocksBind(conn)
	sockscommon.ServeConn(conn, tunnel, sockscommon.ServeOption{
		AuthStore:                     nil,
		UDPBindAddr:                   udpBind,
		RejectUDPAssociateWithoutBind: true,
		HandshakeTimeout:              tailnetSocksHandshakeTimeout,
		Additions: []inbound.Addition{
			inbound.WithInName(gatewayInboundName),
			inbound.WithSpecialRules(""),
		},
	})
}

func (r *runtime) currentGatewayUDPSocksBind(conn net.Conn) socks5.Addr {
	r.mu.RLock()
	udpConn := r.gatewayUDPConn
	r.mu.RUnlock()
	if udpConn == nil {
		return nil
	}

	udpAddr, ok := udpConn.LocalAddr().(*net.UDPAddr)
	if !ok || udpAddr == nil {
		return socks5.ParseAddrToSocksAddr(udpConn.LocalAddr())
	}
	if !udpAddr.IP.IsUnspecified() {
		return socks5.ParseAddrToSocksAddr(udpAddr)
	}

	tcpAddr, ok := conn.LocalAddr().(*net.TCPAddr)
	if !ok || tcpAddr == nil || tcpAddr.IP == nil || tcpAddr.IP.IsUnspecified() {
		return nil
	}

	return socks5.ParseAddrToSocksAddr(&net.UDPAddr{
		IP:   tcpAddr.IP,
		Port: udpAddr.Port,
	})
}

func isWildcardListenAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	return host == "" || host == "0.0.0.0" || host == "::" || host == "[::]"
}

type gatewayUDPSession struct {
	rt       *runtime
	key      string
	pc       net.PacketConn
	target   netip.AddrPort
	writeMu  sync.Mutex
	write    C.WriteBack
	lastSeen atomic.Int64
	closed   atomic.Bool
	once     sync.Once
	done     chan struct{}
}

func (r *runtime) handleGatewayUDPPacket(packet C.UDPPacket, metadata *C.Metadata) {
	defer packet.Drop()
	if r.closed.Load() || !r.connected.Load() {
		return
	}
	clientAddr := packet.LocalAddr()
	target, ok := r.resolveGatewayUDPAddr(metadata, addrFamilyFromNetAddr(clientAddr))
	if !ok {
		r.logGatewayLimited("gateway_udp_unknown_target", "[Tailscale] event=gateway_udp_unknown_target target=%s", gatewayMetadataTarget(metadata))
		return
	}
	if r.isGatewaySelfLoopAddr(target) {
		r.logGatewayLimited("gateway_self_loop_blocked", "[Tailscale] event=gateway_self_loop_blocked network=udp target=%s", target.String())
		return
	}
	session := r.gatewayUDPSession(clientAddr, target, packet)
	if session == nil {
		return
	}
	session.touch()
	if _, err := session.pc.WriteTo(packet.Data(), net.UDPAddrFromAddrPort(target)); err != nil {
		session.close()
	}
}

func gatewayMetadataTarget(metadata *C.Metadata) string {
	if metadata == nil {
		return "<nil>"
	}
	return metadata.RemoteAddress()
}

func (r *runtime) gatewayUDPSession(clientAddr net.Addr, target netip.AddrPort, write C.WriteBack) *gatewayUDPSession {
	key := clientAddr.String() + "|" + target.String()
	r.gatewayUDPMu.Lock()
	if r.gatewayUDPSessions == nil {
		r.gatewayUDPSessions = make(map[string]*gatewayUDPSession)
	}
	if session := r.gatewayUDPSessions[key]; session != nil {
		session.setWriteBack(write)
		r.gatewayUDPMu.Unlock()
		return session
	}
	if len(r.gatewayUDPSessions) >= gatewayUDPMaxSessions {
		r.gatewayUDPMu.Unlock()
		r.logGatewayLimited("gateway_udp_session_limit", "[Tailscale] event=gateway_udp_session_limit max=%d", gatewayUDPMaxSessions)
		return nil
	}
	r.gatewayUDPMu.Unlock()

	localIP, ok := r.gatewayLocalTailIP(target.Addr())
	if !ok {
		r.logGatewayLimited("gateway_udp_no_local_ip", "[Tailscale] event=gateway_udp_no_local_ip target=%s", target.String())
		return nil
	}
	pc, err := r.gatewayListenPacket("udp", netip.AddrPortFrom(localIP, 0).String())
	if err != nil {
		log.Debugln("[Tailscale] gateway udp listen packet failed target=%s err=%v", target.String(), err)
		return nil
	}
	session := &gatewayUDPSession{
		rt:     r,
		key:    key,
		pc:     pc,
		target: target,
		write:  write,
		done:   make(chan struct{}),
	}
	session.touch()

	r.gatewayUDPMu.Lock()
	if existing := r.gatewayUDPSessions[key]; existing != nil {
		existing.setWriteBack(write)
		r.gatewayUDPMu.Unlock()
		_ = pc.Close()
		return existing
	}
	if len(r.gatewayUDPSessions) >= gatewayUDPMaxSessions || r.closed.Load() {
		r.gatewayUDPMu.Unlock()
		_ = pc.Close()
		r.logGatewayLimited("gateway_udp_session_limit", "[Tailscale] event=gateway_udp_session_limit max=%d", gatewayUDPMaxSessions)
		return nil
	}
	r.gatewayUDPSessions[key] = session
	r.gatewayUDPMu.Unlock()

	go session.readLoop()
	return session
}

func (s *gatewayUDPSession) setWriteBack(write C.WriteBack) {
	s.writeMu.Lock()
	s.write = write
	s.writeMu.Unlock()
}

func (s *gatewayUDPSession) touch() {
	s.lastSeen.Store(time.Now().UnixNano())
}

func (s *gatewayUDPSession) readLoop() {
	defer func() {
		s.rt.deleteGatewayUDPSession(s.key, s)
		_ = s.pc.Close()
		close(s.done)
	}()
	buf := make([]byte, 64*1024)
	for {
		_ = s.pc.SetReadDeadline(time.Now().Add(s.rt.gatewayUDPIdleTimeout()))
		n, from, err := s.pc.ReadFrom(buf)
		if err != nil {
			if isTimeout(err) && time.Since(time.Unix(0, s.lastSeen.Load())) < s.rt.gatewayUDPIdleTimeout() {
				continue
			}
			return
		}
		s.touch()
		s.writeMu.Lock()
		write := s.write
		s.writeMu.Unlock()
		if write == nil {
			continue
		}
		if _, err := write.WriteBack(buf[:n], from); err != nil {
			return
		}
	}
}

func (s *gatewayUDPSession) close() {
	s.once.Do(func() {
		s.closed.Store(true)
		_ = s.pc.Close()
	})
}

func (r *runtime) deleteGatewayUDPSession(key string, session *gatewayUDPSession) {
	r.gatewayUDPMu.Lock()
	if r.gatewayUDPSessions[key] == session {
		delete(r.gatewayUDPSessions, key)
	}
	r.gatewayUDPMu.Unlock()
}

func (r *runtime) closeGatewayUDPSessions() []*gatewayUDPSession {
	r.gatewayUDPMu.Lock()
	sessions := make([]*gatewayUDPSession, 0, len(r.gatewayUDPSessions))
	for _, session := range r.gatewayUDPSessions {
		sessions = append(sessions, session)
	}
	r.gatewayUDPSessions = make(map[string]*gatewayUDPSession)
	r.gatewayUDPMu.Unlock()
	return sessions
}

func isTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func (r *runtime) gatewayDialTCP(ctx context.Context, target string) (net.Conn, error) {
	if r.gatewayDialTCPFn != nil {
		return r.gatewayDialTCPFn(ctx, target)
	}
	return r.server.Dial(ctx, "tcp", target)
}

func (r *runtime) gatewayListenPacket(network, addr string) (net.PacketConn, error) {
	if r.gatewayListenPacketFn != nil {
		return r.gatewayListenPacketFn(network, addr)
	}
	return r.server.ListenPacket(network, addr)
}

func (r *runtime) gatewayListenTCP(addr string) (net.Listener, error) {
	if r.gatewayListenTCPFn != nil {
		return r.gatewayListenTCPFn(addr)
	}
	return net.Listen("tcp", addr)
}

func (r *runtime) gatewayListenUDP(addr string) (net.PacketConn, error) {
	if r.gatewayListenUDPFn != nil {
		return r.gatewayListenUDPFn(addr)
	}
	return net.ListenPacket("udp", addr)
}

func (r *runtime) gatewayUDPIdleTimeout() time.Duration {
	if r.gatewayUDPTimeout > 0 {
		return r.gatewayUDPTimeout
	}
	return C.DefaultUDPTimeout
}

func (r *runtime) gatewayLocalTailIP(remote netip.Addr) (netip.Addr, bool) {
	r.mu.RLock()
	ips := append([]netip.Addr(nil), r.tailIPs...)
	r.mu.RUnlock()
	return localTailIPForFamily(ips, remote)
}

func (r *runtime) resolveGatewayUDPAddr(metadata *C.Metadata, clientFamily addrFamily) (netip.AddrPort, bool) {
	if metadata == nil || metadata.DstPort == 0 {
		return netip.AddrPort{}, false
	}
	if metadata.DstIP.IsValid() {
		return netip.AddrPortFrom(metadata.DstIP.Unmap(), metadata.DstPort), true
	}
	host := normalizeTailnetName(metadata.Host)
	if host == "" {
		return netip.AddrPort{}, false
	}
	ips := r.resolveGatewayName(host, clientFamily)
	if len(ips) == 0 {
		return netip.AddrPort{}, false
	}
	return netip.AddrPortFrom(ips[0], metadata.DstPort), true
}

func (r *runtime) resolveGatewayName(name string, clientFamily addrFamily) []netip.Addr {
	r.mu.RLock()
	identity := r.gatewayIdentity
	r.mu.RUnlock()
	if ips := identity.resolveName(name, clientFamily); len(ips) > 0 {
		return ips
	}

	now := time.Now()
	r.mu.Lock()
	if !r.gatewayRefreshAt.IsZero() && now.Before(r.gatewayRefreshAt) {
		r.mu.Unlock()
		return nil
	}
	r.gatewayRefreshAt = now.Add(30 * time.Second)
	r.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	lc, err := r.server.LocalClient()
	if err != nil {
		return nil
	}
	status, err := lc.Status(ctx)
	if err != nil {
		return nil
	}
	identity = newTailnetIdentity(status)
	r.mu.Lock()
	r.gatewayIdentity = identity
	r.mu.Unlock()
	return identity.resolveName(name, clientFamily)
}

func (r *runtime) isGatewaySelfLoop(host string, port uint16) bool {
	if port == 0 || !r.isGatewaySocksPort(port) {
		return false
	}
	host = normalizeTailnetName(host)
	if host == "" {
		return false
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		return r.isGatewaySelfIP(ip.Unmap())
	}
	r.mu.RLock()
	identity := r.gatewayIdentity
	r.mu.RUnlock()
	return identity.isSelfName(host)
}

func (r *runtime) isGatewaySelfLoopAddr(target netip.AddrPort) bool {
	return r.isGatewaySocksPort(target.Port()) && r.isGatewaySelfIP(target.Addr().Unmap())
}

func (r *runtime) isGatewaySocksPort(port uint16) bool {
	if int(port) == r.cfg.Socks5 {
		return true
	}
	_, gatewayPort, err := net.SplitHostPort(r.cfg.GatewaySocks5)
	if err != nil {
		return false
	}
	return strconv.Itoa(int(port)) == gatewayPort
}

func (r *runtime) isGatewaySelfIP(ip netip.Addr) bool {
	r.mu.RLock()
	identity := r.gatewayIdentity
	r.mu.RUnlock()
	return identity.isSelfIP(ip)
}

func (r *runtime) logGatewayLimited(key, format string, args ...any) {
	now := time.Now()
	r.gatewayLogMu.Lock()
	if r.gatewayLastLogs == nil {
		r.gatewayLastLogs = make(map[string]time.Time)
	}
	if last := r.gatewayLastLogs[key]; !last.IsZero() && now.Sub(last) < gatewayLogInterval {
		r.gatewayLogMu.Unlock()
		return
	}
	r.gatewayLastLogs[key] = now
	r.gatewayLogMu.Unlock()
	log.Warnln(format, args...)
}

type addrFamily int

const (
	addrFamilyUnknown addrFamily = iota
	addrFamily4
	addrFamily6
)

func addrFamilyFromNetAddr(addr net.Addr) addrFamily {
	if ap, ok := addrPortFromNetAddr(addr); ok {
		if ap.Addr().Is4() {
			return addrFamily4
		}
		if ap.Addr().Is6() {
			return addrFamily6
		}
	}
	return addrFamilyUnknown
}

type tailnetIdentity struct {
	selfIPs     map[netip.Addr]struct{}
	selfNames   map[string]struct{}
	names       map[string][]netip.Addr
	hostCounts  map[string]int
	hostNameIPs map[string][]netip.Addr
}

func newTailnetIdentity(status *ipnstate.Status) tailnetIdentity {
	identity := tailnetIdentity{
		selfIPs:     make(map[netip.Addr]struct{}),
		selfNames:   make(map[string]struct{}),
		names:       make(map[string][]netip.Addr),
		hostCounts:  make(map[string]int),
		hostNameIPs: make(map[string][]netip.Addr),
	}
	if status == nil {
		return identity
	}
	addPeer := func(peer *ipnstate.PeerStatus, self bool) {
		if peer == nil {
			return
		}
		ips := normalizeTailnetIPs(peer.TailscaleIPs)
		if self {
			for _, ip := range ips {
				identity.selfIPs[ip] = struct{}{}
			}
		}
		if name := normalizeTailnetName(peer.DNSName); name != "" {
			identity.names[name] = ips
			if self {
				identity.selfNames[name] = struct{}{}
			}
		}
		if host := normalizeTailnetName(peer.HostName); host != "" {
			identity.hostCounts[host]++
			identity.hostNameIPs[host] = ips
			if self {
				identity.selfNames[host] = struct{}{}
			}
		}
	}
	addPeer(status.Self, true)
	for _, peer := range status.Peer {
		addPeer(peer, false)
	}
	return identity
}

func normalizeTailnetIPs(ips []netip.Addr) []netip.Addr {
	out := make([]netip.Addr, 0, len(ips))
	for _, ip := range ips {
		if ip.IsValid() {
			out = append(out, ip.Unmap())
		}
	}
	return out
}

func normalizeTailnetName(name string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), "."))
}

func (i tailnetIdentity) resolveName(name string, clientFamily addrFamily) []netip.Addr {
	name = normalizeTailnetName(name)
	if name == "" {
		return nil
	}
	if ips := i.names[name]; len(ips) > 0 {
		return orderTailnetIPs(ips, clientFamily)
	}
	if i.hostCounts[name] == 1 {
		return orderTailnetIPs(i.hostNameIPs[name], clientFamily)
	}
	return nil
}

func (i tailnetIdentity) isSelfIP(ip netip.Addr) bool {
	if !ip.IsValid() {
		return false
	}
	_, ok := i.selfIPs[ip.Unmap()]
	return ok
}

func (i tailnetIdentity) isSelfName(name string) bool {
	_, ok := i.selfNames[normalizeTailnetName(name)]
	return ok
}

func orderTailnetIPs(ips []netip.Addr, clientFamily addrFamily) []netip.Addr {
	if len(ips) == 0 {
		return nil
	}
	preferred := make([]netip.Addr, 0, len(ips))
	addFamily := func(family addrFamily) {
		for _, ip := range ips {
			ip = ip.Unmap()
			if (family == addrFamily4 && ip.Is4()) || (family == addrFamily6 && ip.Is6()) {
				preferred = append(preferred, ip)
			}
		}
	}
	if clientFamily == addrFamily4 || clientFamily == addrFamily6 {
		addFamily(clientFamily)
	}
	addFamily(addrFamily4)
	addFamily(addrFamily6)
	seen := make(map[netip.Addr]struct{}, len(preferred))
	out := preferred[:0]
	for _, ip := range preferred {
		if _, ok := seen[ip]; ok {
			continue
		}
		seen[ip] = struct{}{}
		out = append(out, ip)
	}
	return out
}
