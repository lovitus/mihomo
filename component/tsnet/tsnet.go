package tsnet

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/metacubex/http"
	"github.com/metacubex/mihomo/adapter/inbound"
	C "github.com/metacubex/mihomo/constant"
	authStore "github.com/metacubex/mihomo/listener/auth"
	"github.com/metacubex/mihomo/listener/sockscommon"
	"github.com/metacubex/mihomo/log"
	"github.com/metacubex/mihomo/transport/socks5"

	"tailscale.com/envknob"
	"tailscale.com/ipn"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/logtail"
	tsnetlib "tailscale.com/tsnet"
)

type State string

const (
	StateDisabled       State = "disabled"
	StateUnregistered   State = "unregistered"
	StateRegistering    State = "registering"
	StateConnected      State = "connected"
	StateNeedsReauth    State = "needs-reauth"
	StateRegisterFailed State = "register-failed"
)

const (
	meshRetryBaseInterval        = 10 * time.Second
	meshRetryMaxInterval         = 5 * time.Minute
	startupGracePeriod           = 60 * time.Second
	tailnetSocksHandshakeTimeout = 10 * time.Second
	tailnetSocksMaxActiveConns   = 1024
	tailnetSocksLimitLogInterval = 30 * time.Second
	gatewayUDPMaxSessions        = 4096
	gatewayLogInterval           = 30 * time.Second
	tsnetLogBufferSize           = 64
	tailIPRetryAttempts          = 3
	tailIPRetryInterval          = 500 * time.Millisecond
)

type Snapshot struct {
	Ready      bool
	State      State
	Socks5Port int
	dialer     *Dialer
}

func (s Snapshot) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if !s.Ready || s.dialer == nil {
		return nil, errors.New("tailscale is not ready")
	}
	return s.dialer.DialContext(ctx, network, address)
}

func (s Snapshot) ListenPacket(ctx context.Context, network, address string, rAddrPort netip.AddrPort) (net.PacketConn, error) {
	if !s.Ready || s.dialer == nil {
		return nil, errors.New("tailscale is not ready")
	}
	return s.dialer.ListenPacket(ctx, network, address, rAddrPort)
}

type Dialer struct {
	server *tsnetlib.Server
	ips    []netip.Addr
}

type Config struct {
	Enable                 bool
	LoginServer            string
	LoginServerIPFallbacks []string
	StateDir               string
	ExposeController       bool
	Mesh                   bool
	Socks5                 int
	GatewaySocks5          string
	ControllerAddress      string
	ControllerHandler      http.Handler
	Tunnel                 C.Tunnel
}

func (d *Dialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return d.server.Dial(ctx, network, address)
}

func (d *Dialer) ListenPacket(ctx context.Context, network, address string, rAddrPort netip.AddrPort) (net.PacketConn, error) {
	localIP, ok := localTailIPForFamily(d.ips, rAddrPort.Addr())
	if !ok {
		return nil, fmt.Errorf("no local tail IP for UDP relay address family: %s", rAddrPort.Addr())
	}
	return d.server.ListenPacket(network, netip.AddrPortFrom(localIP, 0).String())
}

var current atomic.Value // stores *runtime
var disableTailscaleLogUploadsOnce sync.Once
var defaultResolverLifecycle atomic.Int32
var tailscaleAuthURLLogSeen sync.Map

func init() {
	current.Store((*runtime)(nil))
}

func CurrentSnapshot() Snapshot {
	rt := current.Load().(*runtime)
	if rt == nil {
		return Snapshot{State: StateDisabled}
	}
	return rt.snapshot()
}

func DefaultResolverFailClosed() bool {
	return defaultResolverLifecycle.Load() > 0
}

// NotifyUse marks a runtime usage event.
// Mesh socks5 bind retries are only attempted when there is a usage trigger.
func NotifyUse() {
	rt := current.Load().(*runtime)
	if rt == nil {
		return
	}
	rt.onUse()
}

func ApplyConfig(cfg Config) {
	old := current.Swap((*runtime)(nil)).(*runtime)
	if old != nil {
		old.Close()
	}

	if !cfg.Enable {
		return
	}

	stateDir := C.Path.Resolve(cfg.StateDir)
	lock, err := lockStateDir(stateDir)
	if err != nil {
		log.Warnln("[Tailscale] disabled: state-dir is locked: %s", stateDir)
		log.Warnln("[Tailscale] another mihomo instance may be using the same tailscale state-dir")
		return
	}

	nodeName, err := stableNodeName(stateDir)
	if err != nil {
		lock.Close()
		log.Warnln("[Tailscale] disabled: generate node name failed: %s", err)
		return
	}

	disableTailscaleBackgroundLogUploads()
	selection := selectLoginServer(context.Background(), cfg.LoginServer, cfg.LoginServerIPFallbacks, probeLoginServerKey)
	logLoginServerSelection(selection)
	logLoginServerNameResolution(selection.ActiveLoginServer)

	server := &tsnetlib.Server{
		Dir:        stateDir,
		Hostname:   nodeName,
		ControlURL: selection.ActiveLoginServer,
		Port:       0,
	}
	endResolverLifecycle := beginDefaultResolverLifecycle()
	rt := &runtime{
		server:               server,
		lock:                 lock,
		cfg:                  cfg,
		stateDir:             stateDir,
		nodeName:             nodeName,
		activeLoginServer:    selection.ActiveLoginServer,
		state:                StateRegistering,
		cancelCtx:            make(chan struct{}),
		runDone:              make(chan struct{}),
		connectedCh:          make(chan struct{}),
		endResolverLifecycle: endResolverLifecycle,
		socks5Port:           cfg.Socks5,
		tcpListeners:         nil,
		udpConns:             nil,
		gatewayUDPSessions:   make(map[string]*gatewayUDPSession),
		logs:                 newLogRing(tsnetLogBufferSize),
	}
	server.UserLogf = rt.userLogf
	current.Store(rt)
	rt.logStateDiagnostic("runtime-created", "", nil)
	go rt.run()
	go rt.watchStartupGrace(startupGracePeriod)
}

func disableTailscaleBackgroundLogUploads() {
	disableTailscaleLogUploadsOnce.Do(func() {
		// mihomo intentionally crashes on net.DefaultResolver usage.
		// tsnet initializes logtail on startup and otherwise tries to
		// resolve log.tailscale.com through the stdlib resolver path.
		logtail.Disable()
		envknob.SetNoLogsNoSupport()
		log.Infoln("[Tailscale] disabled upstream logtail uploads for mihomo resolver compatibility")
	})
}

func beginDefaultResolverLifecycle() func() {
	defaultResolverLifecycle.Add(1)
	var once sync.Once
	return func() {
		once.Do(func() {
			defaultResolverLifecycle.Add(-1)
		})
	}
}

func logLoginServerNameResolution(loginServer string) {
	u, err := url.Parse(loginServer)
	if err != nil {
		return
	}
	host := u.Hostname()
	if host == "" || net.ParseIP(host) != nil {
		return
	}
	log.Infoln("[Tailscale] login-server hostname %s resolution is handled by Tailscale internals; startup grace is %s", host, formatStartupGrace(startupGracePeriod))
}

func Stop() {
	old := current.Swap((*runtime)(nil)).(*runtime)
	if old != nil {
		old.Close()
	}
}

type runtime struct {
	mu sync.RWMutex

	server *tsnetlib.Server
	lock   *dirLock
	cfg    Config

	stateDir          string
	nodeName          string
	activeLoginServer string
	state             State
	authURL           string
	tailIPs           []netip.Addr

	socks5Port int

	cancelOnce            sync.Once
	cancelCtx             chan struct{}
	runDone               chan struct{}
	connectedCh           chan struct{}
	closeOnce             sync.Once
	started               atomic.Bool
	connected             atomic.Bool
	connectedOnce         sync.Once
	connectedServicesOnce sync.Once

	tcpListeners []net.Listener
	udpConns     []net.PacketConn
	httpServers  []*http.Server
	closed       atomic.Bool

	transitionMu         sync.Mutex
	endResolverLifecycle func()

	meshMu          sync.Mutex
	meshTCPReady    bool
	meshUDPBinds    udpBindSet
	meshTCPRetrying bool
	meshUDPRetrying bool
	meshTCPRetry    retryBackoff
	meshUDPRetry    retryBackoff

	activeSocksConns atomic.Int32
	limitLogMu       sync.Mutex
	lastLimitLog     time.Time

	gatewayTCPListener net.Listener
	gatewayUDPConn     net.PacketConn
	gatewayUDPMu       sync.Mutex
	gatewayUDPSessions map[string]*gatewayUDPSession
	gatewayIdentity    tailnetIdentity
	gatewayRefreshAt   time.Time
	gatewayLogMu       sync.Mutex
	gatewayLastLogs    map[string]time.Time

	gatewayDialTCPFn      func(ctx context.Context, target string) (net.Conn, error)
	gatewayListenPacketFn func(network, addr string) (net.PacketConn, error)
	gatewayListenTCPFn    func(addr string) (net.Listener, error)
	gatewayListenUDPFn    func(addr string) (net.PacketConn, error)
	gatewayUDPTimeout     time.Duration
	gatewayRetryInterval  time.Duration

	statusFn func(ctx context.Context) (*ipnstate.Status, error)
	logs     *logRing
}

func (r *runtime) snapshot() Snapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ips := append([]netip.Addr(nil), r.tailIPs...)
	return Snapshot{
		Ready:      r.state == StateConnected,
		State:      r.state,
		Socks5Port: r.socks5Port,
		dialer:     &Dialer{server: r.server, ips: ips},
	}
}

func (r *runtime) setState(state State) {
	r.mu.Lock()
	r.state = state
	r.mu.Unlock()
	r.appendLog("info", "state", string(state))
}

func (r *runtime) run() {
	defer close(r.runDone)
	select {
	case <-r.cancelCtx:
		return
	default:
	}

	log.Infoln("[Tailscale] starting node=%s login-server=%s state-dir=%s", r.nodeName, r.loginServerForRuntime(), r.stateDir)
	r.logStateDiagnostic("before-server-start", "", nil)
	if err := r.server.Start(); err != nil {
		r.setState(StateRegisterFailed)
		log.Warnln("[Tailscale] register failed: %s", err)
		r.logStateDiagnostic("server-start-failed", "", nil)
		return
	}
	r.started.Store(true)
	r.logStateDiagnostic("after-server-start", "", nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-r.cancelCtx
		cancel()
	}()
	go r.markUnregisteredIfStillWaiting()

	status, err := r.waitForRunning(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		r.setState(StateRegisterFailed)
		log.Warnln("[Tailscale] status=register-failed login-server=%s state-dir=%s node-name=%s reason=%s", r.loginServerForRuntime(), r.stateDir, r.nodeName, err)
		r.logStateDiagnostic("register-failed", "", nil)
		return
	}

	if !r.markConnected(status) {
		return
	}
	platformAddFirewallRule()

	log.Infoln("[Tailscale] status=connected login-server=%s node-name=%s tail-ip=%s", r.loginServerForRuntime(), r.nodeName, formatTailIPs(status.TailscaleIPs))
	r.logStateDiagnostic("connected", status.AuthURL, status)
	r.startConnectedServices(status.TailscaleIPs)
}

func (r *runtime) loginServerForRuntime() string {
	if r.activeLoginServer != "" {
		return r.activeLoginServer
	}
	return r.cfg.LoginServer
}

func (r *runtime) waitForRunning(ctx context.Context) (*ipnstate.Status, error) {
	lc, err := r.server.LocalClient()
	if err != nil {
		return nil, fmt.Errorf("local client: %w", err)
	}

	watcher, err := lc.WatchIPNBus(ctx, ipn.NotifyInitialState|ipn.NotifyNoPrivateKeys)
	if err != nil {
		return nil, fmt.Errorf("watch state: %w", err)
	}
	defer watcher.Close()

	for {
		n, err := watcher.Next()
		if err != nil {
			return nil, fmt.Errorf("watch state: %w", err)
		}
		if n.BrowseToURL != nil {
			r.setAuthURL(*n.BrowseToURL)
		}
		if n.ErrMessage != nil {
			r.setPendingState(StateRegistering, *n.ErrMessage)
			continue
		}
		if n.State == nil {
			continue
		}

		switch *n.State {
		case ipn.Running:
			status, err := r.waitForTailIP(ctx, lc)
			if err != nil {
				return nil, err
			}
			if err := lc.SetServeConfig(ctx, new(ipn.ServeConfig)); err != nil {
				return nil, fmt.Errorf("clear stale serve config: %w", err)
			}
			return status, nil
		case ipn.NeedsMachineAuth:
			r.setPendingState(StateNeedsReauth, "machine authorization required")
		case ipn.NeedsLogin, ipn.NoState:
			r.setPendingState(StateUnregistered, "waiting for Headscale authorization")
		default:
			r.setPendingState(StateRegistering, "connecting")
		}
	}
}

func (r *runtime) waitForTailIP(ctx context.Context, lc interface {
	Status(context.Context) (*ipnstate.Status, error)
}) (*ipnstate.Status, error) {
	for attempt := 0; attempt < tailIPRetryAttempts; attempt++ {
		status, err := lc.Status(ctx)
		if err != nil {
			return nil, fmt.Errorf("status: %w", err)
		}
		if len(status.TailscaleIPs) > 0 {
			return status, nil
		}
		if attempt < tailIPRetryAttempts-1 {
			timer := time.NewTimer(tailIPRetryInterval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
		}
	}
	return nil, errors.New("running, but no tail IP after retries")
}

func (r *runtime) setAuthURL(authURL string) {
	authURL = strings.TrimSpace(authURL)
	if authURL == "" {
		return
	}
	r.mu.Lock()
	if r.authURL == authURL {
		r.mu.Unlock()
		return
	}
	r.authURL = authURL
	r.mu.Unlock()
	logAuthURLOnce(authURL)
	r.logStateDiagnostic("auth-url", authURL, nil)
}

func (r *runtime) markConnected(status *ipnstate.Status) bool {
	r.transitionMu.Lock()
	defer r.transitionMu.Unlock()
	if r.closed.Load() || !r.isCurrent() {
		return false
	}

	r.mu.Lock()
	r.state = StateConnected
	r.authURL = status.AuthURL
	r.tailIPs = append([]netip.Addr(nil), status.TailscaleIPs...)
	r.gatewayIdentity = newTailnetIdentity(status)
	r.mu.Unlock()

	r.connected.Store(true)
	r.connectedOnce.Do(func() {
		if r.connectedCh != nil {
			close(r.connectedCh)
		}
	})
	return true
}

func (r *runtime) startConnectedServices(tailIPs []netip.Addr) {
	r.connectedServicesOnce.Do(func() {
		if r.closed.Load() || !r.isCurrent() {
			return
		}
		if r.cfg.Mesh {
			r.startSocks5(tailIPs)
		}
		if r.closed.Load() || !r.isCurrent() {
			return
		}
		if r.cfg.ExposeController {
			r.startController()
		}
		if r.closed.Load() || !r.isCurrent() {
			return
		}
		if r.cfg.GatewaySocks5 != "" {
			r.startGateway()
		}
	})
}

func (r *runtime) watchStartupGrace(grace time.Duration) {
	timer := time.NewTimer(grace)
	defer timer.Stop()
	select {
	case <-r.connectedCh:
		return
	case <-r.cancelCtx:
		return
	case <-timer.C:
	}
	r.disableAfterStartupGrace(grace)
}

func (r *runtime) disableAfterStartupGrace(grace time.Duration) {
	r.transitionMu.Lock()
	if r.closed.Load() || r.connected.Load() {
		r.transitionMu.Unlock()
		return
	}
	if !current.CompareAndSwap(r, (*runtime)(nil)) {
		r.transitionMu.Unlock()
		return
	}
	r.transitionMu.Unlock()

	log.Warnln("[Tailscale] startup grace %s exceeded, tsnet disabled for this run; reload/restart to retry", formatStartupGrace(grace))
	r.logStateDiagnostic("startup-grace-timeout", "", nil)
	r.Close()
}

func (r *runtime) isCurrent() bool {
	return current.Load().(*runtime) == r
}

func formatStartupGrace(grace time.Duration) string {
	if grace%time.Second == 0 {
		return strconv.FormatInt(int64(grace/time.Second), 10) + "s"
	}
	return grace.String()
}

func (r *runtime) setPendingState(state State, reason string) {
	r.mu.Lock()
	if r.state == state {
		r.mu.Unlock()
		return
	}
	r.state = state
	r.mu.Unlock()

	switch state {
	case StateUnregistered:
		log.Warnln("[Tailscale] status=unregistered login-server=%s state-dir=%s node-name=%s", r.loginServerForRuntime(), r.stateDir, r.nodeName)
		log.Warnln("[Tailscale] %s", reason)
	case StateNeedsReauth:
		log.Warnln("[Tailscale] status=needs-reauth login-server=%s state-dir=%s node-name=%s reason=%s", r.loginServerForRuntime(), r.stateDir, r.nodeName, reason)
		log.Warnln("[Tailscale] local state exists, but Headscale does not currently accept this node")
	case StateRegistering:
		log.Debugln("[Tailscale] status=registering node-name=%s reason=%s", r.nodeName, reason)
	}
}

func (r *runtime) Close() {
	r.closeOnce.Do(func() {
		platformRemoveFirewallRule()
		r.logStateDiagnostic("close-begin", "", nil)
		r.closed.Store(true)
		r.cancelOnce.Do(func() { close(r.cancelCtx) })
		<-r.runDone
		r.mu.Lock()
		tcpListeners := r.tcpListeners
		udpConns := r.udpConns
		httpServers := r.httpServers
		gatewayTCPListener := r.gatewayTCPListener
		gatewayUDPConn := r.gatewayUDPConn
		r.tcpListeners = nil
		r.udpConns = nil
		r.httpServers = nil
		r.gatewayTCPListener = nil
		r.gatewayUDPConn = nil
		r.mu.Unlock()
		gatewaySessions := r.closeGatewayUDPSessions()
		for _, srv := range httpServers {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			_ = srv.Shutdown(ctx)
			cancel()
		}
		if gatewayTCPListener != nil {
			_ = gatewayTCPListener.Close()
		}
		if gatewayUDPConn != nil {
			_ = gatewayUDPConn.Close()
		}
		for _, session := range gatewaySessions {
			session.close()
		}
		for _, ln := range tcpListeners {
			_ = ln.Close()
		}
		for _, pc := range udpConns {
			_ = pc.Close()
		}
		if r.started.Load() {
			_ = r.server.Close()
		}
		if r.lock != nil {
			_ = r.lock.Close()
		}
		if r.endResolverLifecycle != nil {
			r.endResolverLifecycle()
		}
		r.logStateDiagnostic("close-end", "", nil)
	})
}

func (r *runtime) markUnregisteredIfStillWaiting() {
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-r.cancelCtx:
		return
	}
	r.mu.Lock()
	if r.state == StateRegistering {
		r.state = StateUnregistered
		log.Warnln("[Tailscale] status=unregistered login-server=%s state-dir=%s node-name=%s", r.loginServerForRuntime(), r.stateDir, r.nodeName)
		log.Warnln("[Tailscale] waiting for Headscale authorization")
	}
	r.mu.Unlock()
}

func (r *runtime) startSocks5(tailIPs []netip.Addr) {
	if !r.retryStartSocks5TCP(true) {
		return
	}
	_ = r.retryStartSocks5UDP(true, tailIPs)
}

type udpBindSet struct {
	ipv4 socks5.Addr
	ipv6 socks5.Addr
}

type retryBackoff struct {
	next  time.Time
	delay time.Duration
}

func (r *retryBackoff) shouldRetry(now time.Time) bool {
	return r.next.IsZero() || !now.Before(r.next)
}

func (r *retryBackoff) onFailure(now time.Time) time.Duration {
	if r.delay <= 0 {
		r.delay = meshRetryBaseInterval
	} else {
		r.delay *= 2
		if r.delay > meshRetryMaxInterval {
			r.delay = meshRetryMaxInterval
		}
	}
	r.next = now.Add(r.delay)
	return r.delay
}

func (r *retryBackoff) onSuccess() {
	r.next = time.Time{}
	r.delay = 0
}

func (b udpBindSet) first() socks5.Addr {
	if b.ipv4 != nil {
		return b.ipv4
	}
	return b.ipv6
}

func (b udpBindSet) forConn(conn net.Conn) socks5.Addr {
	if conn != nil {
		if addr, ok := addrPortFromNetAddr(conn.LocalAddr()); ok {
			if addr.Addr().Is4() && b.ipv4 != nil {
				return b.ipv4
			}
			if addr.Addr().Is6() && b.ipv6 != nil {
				return b.ipv6
			}
		}
	}
	return b.first()
}

func (r *runtime) onUse() {
	if r.closed.Load() {
		return
	}
	r.mu.RLock()
	connected := r.state == StateConnected
	meshEnabled := r.cfg.Mesh
	tailIPs := append([]netip.Addr(nil), r.tailIPs...)
	r.mu.RUnlock()
	if !connected || !meshEnabled {
		return
	}
	if !r.retryStartSocks5TCP(false) {
		return
	}
	_ = r.retryStartSocks5UDP(false, tailIPs)
}

func (r *runtime) retryStartSocks5TCP(force bool) bool {
	if r.closed.Load() {
		return false
	}
	now := time.Now()
	r.meshMu.Lock()
	if r.meshTCPReady {
		r.meshMu.Unlock()
		return true
	}
	if r.meshTCPRetrying {
		r.meshMu.Unlock()
		return false
	}
	if !force && !r.meshTCPRetry.shouldRetry(now) {
		r.meshMu.Unlock()
		return false
	}
	r.meshTCPRetrying = true
	r.meshMu.Unlock()

	port := r.cfg.Socks5
	ln, err := r.server.Listen("tcp", ":"+strconv.Itoa(port))
	if err != nil {
		r.meshMu.Lock()
		r.meshTCPRetrying = false
		delay := r.meshTCPRetry.onFailure(now)
		r.meshMu.Unlock()
		log.Warnln("[Tailscale] mesh socks5 tcp listen failed: %s; retry in %s on next use", err, delay.Round(time.Second))
		return false
	}

	r.mu.Lock()
	if r.closed.Load() {
		r.mu.Unlock()
		_ = ln.Close()
		return false
	}
	r.tcpListeners = append(r.tcpListeners, ln)
	r.mu.Unlock()

	r.meshMu.Lock()
	r.meshTCPRetrying = false
	r.meshTCPReady = true
	r.meshTCPRetry.onSuccess()
	r.meshMu.Unlock()

	go r.serveSocks5TCP(ln)
	log.Infoln("[Tailscale] mesh socks5 tcp listening on tailnet :%d", port)
	return true
}

func (r *runtime) retryStartSocks5UDP(force bool, tailIPs []netip.Addr) bool {
	if r.closed.Load() {
		return false
	}
	now := time.Now()
	r.meshMu.Lock()
	if r.meshUDPBinds.first() != nil {
		r.meshMu.Unlock()
		return true
	}
	if r.meshUDPRetrying {
		r.meshMu.Unlock()
		return false
	}
	if !force && !r.meshUDPRetry.shouldRetry(now) {
		r.meshMu.Unlock()
		return false
	}
	r.meshUDPRetrying = true
	r.meshMu.Unlock()

	port := r.cfg.Socks5
	var binds udpBindSet
	for _, ip := range tailIPs {
		addr := netip.AddrPortFrom(ip, uint16(port)).String()
		pc, err := r.server.ListenPacket("udp", addr)
		if err != nil {
			log.Warnln("[Tailscale] mesh socks5 udp listen failed on %s: %s", addr, err)
			continue
		}
		r.mu.Lock()
		if r.closed.Load() {
			r.mu.Unlock()
			_ = pc.Close()
			continue
		}
		r.udpConns = append(r.udpConns, pc)
		r.mu.Unlock()
		bindAddr := socks5.AddrFromStdAddrPort(netip.AddrPortFrom(ip, uint16(port)))
		if ip.Is4() && binds.ipv4 == nil {
			binds.ipv4 = bindAddr
		} else if ip.Is6() && binds.ipv6 == nil {
			binds.ipv6 = bindAddr
		}
		go sockscommon.ServePacketConn(pc, r.cfg.Tunnel, r.closed.Load,
			inbound.WithInName("TAILSCALE-SOCKS"),
			inbound.WithSpecialRules(""),
		)
		log.Infoln("[Tailscale] mesh socks5 udp listening on tailnet %s", addr)
	}

	r.meshMu.Lock()
	r.meshUDPRetrying = false
	if binds.first() != nil {
		r.meshUDPBinds = binds
		r.meshUDPRetry.onSuccess()
		r.meshMu.Unlock()
		return true
	}
	delay := r.meshUDPRetry.onFailure(now)
	r.meshMu.Unlock()
	log.Warnln("[Tailscale] mesh socks5 udp listen unavailable; retry in %s on next use", delay.Round(time.Second))
	return false
}

func (r *runtime) currentUDPSocksBind(conn net.Conn) socks5.Addr {
	r.meshMu.Lock()
	binds := r.meshUDPBinds
	r.meshMu.Unlock()
	return binds.forConn(conn)
}

func (r *runtime) serveSocksConn(conn net.Conn) {
	r.onUse()
	udpBind := r.currentUDPSocksBind(conn)
	sockscommon.ServeConn(conn, r.cfg.Tunnel, sockscommon.ServeOption{
		AuthStore:                     authStore.Default,
		UDPBindAddr:                   udpBind,
		RejectUDPAssociateWithoutBind: true,
		HandshakeTimeout:              tailnetSocksHandshakeTimeout,
		Additions: []inbound.Addition{
			inbound.WithInName("TAILSCALE-SOCKS"),
			inbound.WithSpecialRules(""),
		},
	})
}

func (r *runtime) serveSocks5TCP(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		if !r.tryAcquireTailnetSocksConn() {
			_ = conn.Close()
			if r.shouldLogTailnetSocksLimit(time.Now()) {
				log.Warnln("[Tailscale] mesh socks5 active connection limit reached: max=%d", tailnetSocksMaxActiveConns)
			}
			continue
		}
		go func() {
			defer r.releaseTailnetSocksConn()
			r.serveSocksConn(conn)
		}()
	}
}

func (r *runtime) tryAcquireTailnetSocksConn() bool {
	for {
		active := r.activeSocksConns.Load()
		if active >= tailnetSocksMaxActiveConns {
			return false
		}
		if r.activeSocksConns.CompareAndSwap(active, active+1) {
			return true
		}
	}
}

func (r *runtime) releaseTailnetSocksConn() {
	r.activeSocksConns.Add(-1)
}

func (r *runtime) shouldLogTailnetSocksLimit(now time.Time) bool {
	r.limitLogMu.Lock()
	defer r.limitLogMu.Unlock()
	if !r.lastLimitLog.IsZero() && now.Sub(r.lastLimitLog) < tailnetSocksLimitLogInterval {
		return false
	}
	r.lastLimitLog = now
	return true
}

func (r *runtime) startController() {
	addr := r.cfg.ControllerAddress
	if addr == "" {
		log.Warnln("[Tailscale] expose-controller skipped: external-controller is not configured")
		return
	}
	if r.cfg.ControllerHandler == nil {
		log.Warnln("[Tailscale] expose-controller skipped: controller handler is not available")
		return
	}
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		log.Warnln("[Tailscale] expose-controller skipped: invalid external-controller %s: %s", addr, err)
		return
	}
	ln, err := r.server.Listen("tcp", ":"+port)
	if err != nil {
		log.Warnln("[Tailscale] expose-controller disabled: %s", err)
		return
	}
	srv := &http.Server{Handler: r.cfg.ControllerHandler}
	r.mu.Lock()
	if r.closed.Load() {
		r.mu.Unlock()
		_ = ln.Close()
		return
	}
	r.tcpListeners = append(r.tcpListeners, ln)
	r.httpServers = append(r.httpServers, srv)
	r.mu.Unlock()
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Warnln("[Tailscale] expose-controller serve error: %s", err)
		}
	}()
	log.Infoln("[Tailscale] external controller listening on tailnet :%s", port)
}

func stableNodeName(stateDir string) (string, error) {
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return "", err
	}
	// Custom rename override takes precedence over the generated instance ID.
	if b, err := os.ReadFile(filepath.Join(stateDir, "node-name")); err == nil {
		if name := strings.TrimSpace(string(b)); name != "" {
			return name, nil
		}
	}
	path := filepath.Join(stateDir, "mihomo-instance-id")
	if b, err := os.ReadFile(path); err == nil {
		id := strings.TrimSpace(string(b))
		if len(id) >= 8 {
			return "mihomo-" + id[:8], nil
		}
	}
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	id := hex.EncodeToString(b[:])
	if err := os.WriteFile(path, []byte(id+"\n"), 0o600); err != nil {
		return "", err
	}
	return "mihomo-" + id[:8], nil
}

// validNodeName accepts DNS labels: 1-63 chars, [A-Za-z0-9-], no leading/trailing hyphen.
var validNodeName = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`)

// RenameNode writes a custom hostname for the Tailscale node stored in stateDir.
// The change takes effect on the next startup. name must be a valid DNS label:
// 1-63 characters, only letters, digits and hyphens, no leading/trailing hyphen.
func RenameNode(stateDir, name string) error {
	name = strings.TrimSpace(name)
	if !validNodeName.MatchString(name) {
		return fmt.Errorf("invalid node name %q: must be 1-63 chars, letters/digits/hyphens only, no leading/trailing hyphen", name)
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(stateDir, "node-name"), []byte(name), 0o600)
}

// RenameCurrentNode renames the active tsnet node. The new name takes effect
// on the next restart. Returns an error if no tsnet runtime is active.
func RenameCurrentNode(name string) error {
	rt := current.Load().(*runtime)
	if rt == nil {
		return fmt.Errorf("tsnet is not running")
	}
	return RenameNode(rt.stateDir, name)
}

func (r *runtime) userLogf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if authURL, ok := authURLFromTsnetUserLog(msg); ok {
		r.setAuthURL(authURL)
		return
	}
	r.appendLog("info", "user-log", msg)
	log.Infoln("[Tailscale] %s", msg)
}

func authURLFromTsnetUserLog(msg string) (string, bool) {
	const prefix = "To start this tsnet server, restart with TS_AUTHKEY set, or go to: "
	authURL, ok := strings.CutPrefix(msg, prefix)
	if !ok {
		return "", false
	}
	authURL = strings.TrimSpace(authURL)
	return authURL, authURL != ""
}

func authNodeKeyFromURL(authURL string) string {
	authURL = strings.TrimSpace(authURL)
	if authURL == "" {
		return "-"
	}
	u, err := url.Parse(authURL)
	if err != nil {
		return "-"
	}
	for _, part := range strings.Split(u.Path, "/") {
		if strings.HasPrefix(part, "nodekey:") {
			return part
		}
	}
	return "-"
}

func (r *runtime) logStateDiagnostic(event, authURL string, status *ipnstate.Status) {
	diag := readStateDiagnostic(r.stateDir)
	nodeKey := "-"
	if status != nil && status.Self != nil && !status.Self.PublicKey.IsZero() {
		nodeKey = status.Self.PublicKey.String()
	}
	log.Infoln(
		"[Tailscale] state-diagnostic event=%s state-dir=%s state-file=%s exists=%t size=%d mtime=%s store-readable=%t has-state=%t nodekey=%s auth-nodekey=%s",
		event,
		r.stateDir,
		diag.stateFile,
		diag.exists,
		diag.size,
		diag.mtime,
		diag.storeReadable,
		diag.hasState,
		nodeKey,
		authNodeKeyFromURL(authURL),
	)
	r.appendLog("info", event, "state diagnostic recorded")
}

type stateDiagnostic struct {
	stateFile     string
	exists        bool
	size          int64
	mtime         string
	storeReadable bool
	hasState      bool
}

func readStateDiagnostic(stateDir string) stateDiagnostic {
	diag := stateDiagnostic{
		stateFile: filepath.Join(stateDir, "tailscaled.state"),
		mtime:     "-",
	}
	info, err := os.Stat(diag.stateFile)
	if err != nil {
		return diag
	}
	diag.exists = true
	diag.size = info.Size()
	diag.mtime = info.ModTime().UTC().Format(time.RFC3339Nano)

	b, err := os.ReadFile(diag.stateFile)
	if err != nil {
		return diag
	}
	var store map[string]json.RawMessage
	if err := json.Unmarshal(b, &store); err != nil {
		return diag
	}
	diag.storeReadable = true
	for _, raw := range store {
		if isNonEmptyStateValue(raw) {
			diag.hasState = true
			break
		}
	}
	return diag
}

func isNonEmptyStateValue(raw json.RawMessage) bool {
	value := strings.TrimSpace(string(raw))
	return value != "" && value != "null" && value != `""`
}

func logAuthURLOnce(authURL string) {
	if markAuthURLLogSeen(authURL) {
		log.Warnln("[Tailscale] auth-url=%s", authURL)
	}
}

func markAuthURLLogSeen(authURL string) bool {
	authURL = strings.TrimSpace(authURL)
	if authURL == "" {
		return false
	}
	_, loaded := tailscaleAuthURLLogSeen.LoadOrStore(authURL, struct{}{})
	return !loaded
}

func formatTailIPs(ips []netip.Addr) string {
	parts := make([]string, 0, len(ips))
	for _, ip := range ips {
		parts = append(parts, ip.String())
	}
	return strings.Join(parts, ",")
}

func localTailIPForFamily(ips []netip.Addr, remote netip.Addr) (netip.Addr, bool) {
	for _, ip := range ips {
		if ip.Is4() == remote.Is4() {
			return ip, true
		}
	}
	return netip.Addr{}, false
}

func addrPortFromNetAddr(addr net.Addr) (netip.AddrPort, bool) {
	if addr == nil {
		return netip.AddrPort{}, false
	}
	switch a := addr.(type) {
	case *net.TCPAddr:
		if ip, ok := netip.AddrFromSlice(a.IP); ok {
			return netip.AddrPortFrom(ip.Unmap(), uint16(a.Port)), true
		}
	case *net.UDPAddr:
		if ip, ok := netip.AddrFromSlice(a.IP); ok {
			return netip.AddrPortFrom(ip.Unmap(), uint16(a.Port)), true
		}
	}
	ap, err := netip.ParseAddrPort(addr.String())
	return ap, err == nil
}

var _ C.Dialer = (*Snapshot)(nil)
var _ C.Dialer = (*Dialer)(nil)
