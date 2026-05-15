package tsnet

import (
	"context"
	"net/netip"
	"sort"
	"sync"
	"time"

	"tailscale.com/ipn/ipnstate"
	"tailscale.com/types/key"
)

type APIStatus struct {
	Enable            bool              `json:"enable"`
	Ready             bool              `json:"ready"`
	State             State             `json:"state"`
	StatusError       string            `json:"statusError,omitempty"`
	LoginServer       string            `json:"loginServer,omitempty"`
	ActiveLoginServer string            `json:"activeLoginServer,omitempty"`
	NodeName          string            `json:"nodeName,omitempty"`
	AuthURL           string            `json:"authURL,omitempty"`
	TailIPs           []string          `json:"tailIPs,omitempty"`
	BackendState      string            `json:"backendState,omitempty"`
	DaemonVersion     string            `json:"daemonVersion,omitempty"`
	HaveNodeKey       bool              `json:"haveNodeKey,omitempty"`
	Health            []string          `json:"health,omitempty"`
	Tailnet           *APITailnetStatus `json:"tailnet,omitempty"`
	Self              *APIPeerStatus    `json:"self,omitempty"`
	Peers             []APIPeerStatus   `json:"peers,omitempty"`
	Services          *APIServices      `json:"services,omitempty"`
	Diagnostic        *APIDiagnostic    `json:"diagnostic,omitempty"`
}

type APITailnetStatus struct {
	Name            string `json:"name,omitempty"`
	MagicDNSSuffix  string `json:"magicDNSSuffix,omitempty"`
	MagicDNSEnabled bool   `json:"magicDNSEnabled"`
}

type APIServices struct {
	Mesh       APIMeshService       `json:"mesh"`
	Gateway    APIGatewayService    `json:"gateway"`
	Controller APIControllerService `json:"controller"`
}

type APIMeshService struct {
	Enabled           bool `json:"enabled"`
	TCPReady          bool `json:"tcpReady"`
	UDPReady          bool `json:"udpReady"`
	Socks5Port        int  `json:"socks5Port,omitempty"`
	ActiveConnections int  `json:"activeConnections"`
}

type APIGatewayService struct {
	Enabled     bool   `json:"enabled"`
	TCPReady    bool   `json:"tcpReady"`
	UDPReady    bool   `json:"udpReady"`
	Address     string `json:"address,omitempty"`
	UDPSessions int    `json:"udpSessions"`
}

type APIControllerService struct {
	Enabled bool   `json:"enabled"`
	Ready   bool   `json:"ready"`
	Address string `json:"address,omitempty"`
}

type APIDiagnostic struct {
	StateDir      string `json:"stateDir,omitempty"`
	StateFile     string `json:"stateFile,omitempty"`
	Exists        bool   `json:"exists"`
	Size          int64  `json:"size,omitempty"`
	MTime         string `json:"mtime,omitempty"`
	StoreReadable bool   `json:"storeReadable"`
	HasState      bool   `json:"hasState"`
}

type APIPeerStatus struct {
	ID             string     `json:"id,omitempty"`
	PublicKey      string     `json:"publicKey,omitempty"`
	HostName       string     `json:"hostName,omitempty"`
	DNSName        string     `json:"dnsName,omitempty"`
	OS             string     `json:"os,omitempty"`
	TailscaleIPs   []string   `json:"tailscaleIPs,omitempty"`
	AllowedIPs     []string   `json:"allowedIPs,omitempty"`
	Tags           []string   `json:"tags,omitempty"`
	PrimaryRoutes  []string   `json:"primaryRoutes,omitempty"`
	Addrs          []string   `json:"addrs,omitempty"`
	CurAddr        string     `json:"curAddr,omitempty"`
	Relay          string     `json:"relay,omitempty"`
	RxBytes        int64      `json:"rxBytes,omitempty"`
	TxBytes        int64      `json:"txBytes,omitempty"`
	Created        *time.Time `json:"created,omitempty"`
	LastWrite      *time.Time `json:"lastWrite,omitempty"`
	LastSeen       *time.Time `json:"lastSeen,omitempty"`
	LastHandshake  *time.Time `json:"lastHandshake,omitempty"`
	Online         bool       `json:"online"`
	ExitNode       bool       `json:"exitNode"`
	ExitNodeOption bool       `json:"exitNodeOption"`
	Active         bool       `json:"active"`
	PeerAPIURL     []string   `json:"peerAPIURL,omitempty"`
	ShareeNode     bool       `json:"shareeNode,omitempty"`
	Expired        bool       `json:"expired,omitempty"`
	KeyExpiry      *time.Time `json:"keyExpiry,omitempty"`
}

type APILogEntry struct {
	Time    time.Time `json:"time"`
	Level   string    `json:"level"`
	Event   string    `json:"event"`
	Message string    `json:"message,omitempty"`
}

type APILogs struct {
	Logs []APILogEntry `json:"logs"`
}

func Status(ctx context.Context) APIStatus {
	rt := current.Load().(*runtime)
	if rt == nil {
		return APIStatus{
			Enable: false,
			Ready:  false,
			State:  StateDisabled,
		}
	}
	return rt.apiStatus(ctx)
}

func Logs() APILogs {
	rt := current.Load().(*runtime)
	if rt == nil || rt.logs == nil {
		return APILogs{Logs: []APILogEntry{}}
	}
	return APILogs{Logs: rt.logs.entries()}
}

func (r *runtime) apiStatus(ctx context.Context) APIStatus {
	st := r.runtimeAPIStatus()
	status, err := r.localStatus(ctx)
	if err != nil {
		st.StatusError = err.Error()
		return st
	}
	mergeLocalStatus(&st, status)
	return st
}

func (r *runtime) runtimeAPIStatus() APIStatus {
	r.mu.RLock()
	state := r.state
	nodeName := r.nodeName
	loginServer := r.cfg.LoginServer
	activeLoginServer := r.loginServerForRuntime()
	authURL := r.authURL
	tailIPs := addrsToStrings(r.tailIPs)
	meshEnabled := r.cfg.Mesh
	meshPort := r.cfg.Socks5
	gatewayAddress := r.cfg.GatewaySocks5
	controllerEnabled := r.cfg.ExposeController
	controllerAddress := r.cfg.ControllerAddress
	tcpListeners := len(r.tcpListeners)
	httpServers := len(r.httpServers)
	gatewayTCPReady := r.gatewayTCPListener != nil
	gatewayUDPReady := r.gatewayUDPConn != nil
	r.mu.RUnlock()

	r.meshMu.Lock()
	meshTCPReady := r.meshTCPReady
	meshUDPReady := r.meshUDPBinds.first() != nil
	r.meshMu.Unlock()

	r.gatewayUDPMu.Lock()
	gatewayUDPSessions := len(r.gatewayUDPSessions)
	r.gatewayUDPMu.Unlock()

	diag := readStateDiagnostic(r.stateDir)
	return APIStatus{
		Enable:            true,
		Ready:             state == StateConnected,
		State:             state,
		LoginServer:       loginServer,
		ActiveLoginServer: activeLoginServer,
		NodeName:          nodeName,
		AuthURL:           authURL,
		TailIPs:           tailIPs,
		Services: &APIServices{
			Mesh: APIMeshService{
				Enabled:           meshEnabled,
				TCPReady:          meshTCPReady,
				UDPReady:          meshUDPReady,
				Socks5Port:        meshPort,
				ActiveConnections: int(r.activeSocksConns.Load()),
			},
			Gateway: APIGatewayService{
				Enabled:     gatewayAddress != "",
				TCPReady:    gatewayTCPReady,
				UDPReady:    gatewayUDPReady,
				Address:     gatewayAddress,
				UDPSessions: gatewayUDPSessions,
			},
			Controller: APIControllerService{
				Enabled: controllerEnabled,
				Ready:   controllerEnabled && tcpListeners > 0 && httpServers > 0,
				Address: controllerAddress,
			},
		},
		Diagnostic: &APIDiagnostic{
			StateDir:      r.stateDir,
			StateFile:     diag.stateFile,
			Exists:        diag.exists,
			Size:          diag.size,
			MTime:         diag.mtime,
			StoreReadable: diag.storeReadable,
			HasState:      diag.hasState,
		},
	}
}

func (r *runtime) localStatus(ctx context.Context) (*ipnstate.Status, error) {
	if r.statusFn != nil {
		return r.statusFn(ctx)
	}
	if r.server == nil {
		return nil, errLocalStatusUnavailable
	}
	lc, err := r.server.LocalClient()
	if err != nil {
		return nil, err
	}
	return lc.Status(ctx)
}

var errLocalStatusUnavailable = statusError("local status unavailable")

type statusError string

func (e statusError) Error() string { return string(e) }

func mergeLocalStatus(st *APIStatus, status *ipnstate.Status) {
	if status == nil {
		return
	}
	st.BackendState = status.BackendState
	st.DaemonVersion = status.Version
	st.HaveNodeKey = status.HaveNodeKey
	st.AuthURL = firstNonEmpty(status.AuthURL, st.AuthURL)
	st.TailIPs = addrsToStrings(status.TailscaleIPs)
	st.Health = append([]string(nil), status.Health...)
	if status.CurrentTailnet != nil {
		st.Tailnet = &APITailnetStatus{
			Name:            status.CurrentTailnet.Name,
			MagicDNSSuffix:  status.CurrentTailnet.MagicDNSSuffix,
			MagicDNSEnabled: status.CurrentTailnet.MagicDNSEnabled,
		}
	} else if status.MagicDNSSuffix != "" {
		st.Tailnet = &APITailnetStatus{MagicDNSSuffix: status.MagicDNSSuffix}
	}
	st.Self = apiPeerStatus(status.Self)
	st.Peers = apiPeerStatuses(status.Peer)
}

func apiPeerStatuses(peers map[key.NodePublic]*ipnstate.PeerStatus) []APIPeerStatus {
	if len(peers) == 0 {
		return nil
	}
	out := make([]APIPeerStatus, 0, len(peers))
	for _, peer := range peers {
		if converted := apiPeerStatus(peer); converted != nil {
			out = append(out, *converted)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].DNSName != out[j].DNSName {
			return out[i].DNSName < out[j].DNSName
		}
		return out[i].HostName < out[j].HostName
	})
	return out
}

func apiPeerStatus(peer *ipnstate.PeerStatus) *APIPeerStatus {
	if peer == nil {
		return nil
	}
	out := &APIPeerStatus{
		ID:             string(peer.ID),
		PublicKey:      peer.PublicKey.String(),
		HostName:       peer.HostName,
		DNSName:        peer.DNSName,
		OS:             peer.OS,
		TailscaleIPs:   peerTailscaleIPs(peer),
		Addrs:          append([]string(nil), peer.Addrs...),
		CurAddr:        peer.CurAddr,
		Relay:          peer.Relay,
		RxBytes:        peer.RxBytes,
		TxBytes:        peer.TxBytes,
		Created:        nonZeroTime(peer.Created),
		LastWrite:      nonZeroTime(peer.LastWrite),
		LastSeen:       nonZeroTime(peer.LastSeen),
		LastHandshake:  nonZeroTime(peer.LastHandshake),
		Online:         peer.Online,
		ExitNode:       peer.ExitNode,
		ExitNodeOption: peer.ExitNodeOption,
		Active:         peer.Active,
		PeerAPIURL:     append([]string(nil), peer.PeerAPIURL...),
		ShareeNode:     peer.ShareeNode,
		Expired:        peer.Expired,
		KeyExpiry:      peer.KeyExpiry,
	}
	if peer.AllowedIPs != nil {
		out.AllowedIPs = prefixesToStrings(peer.AllowedIPs.AsSlice())
	}
	if peer.Tags != nil {
		out.Tags = peer.Tags.AsSlice()
	}
	if peer.PrimaryRoutes != nil {
		out.PrimaryRoutes = prefixesToStrings(peer.PrimaryRoutes.AsSlice())
	}
	return out
}

// peerTailscaleIPs returns the peer's Tailscale IPs, augmenting
// peer.TailscaleIPs with any single-host prefixes found in peer.AllowedIPs
// that are missing. This handles Headscale deployments using non-standard
// IPv4 ranges (e.g. 10.x.x.x) which the Tailscale client filters out of
// PeerStatus.TailscaleIPs via tsaddr.IsTailscaleIP.
func peerTailscaleIPs(peer *ipnstate.PeerStatus) []string {
	out := addrsToStrings(peer.TailscaleIPs)
	if peer.AllowedIPs == nil {
		return out
	}
	seen := make(map[netip.Addr]bool, len(peer.TailscaleIPs))
	for _, a := range peer.TailscaleIPs {
		seen[a] = true
	}
	// PrimaryRoutes contains advertised subnet routes (possibly single-host),
	// explicitly excluding node addresses. Skip these so we don't treat a
	// routed host prefix (e.g. 192.168.1.10/32) as the peer's own address.
	skipPfx := make(map[netip.Prefix]bool)
	if peer.PrimaryRoutes != nil {
		for _, pfx := range peer.PrimaryRoutes.AsSlice() {
			skipPfx[pfx] = true
		}
	}
	var extra4 []string
	for _, pfx := range peer.AllowedIPs.AsSlice() {
		if !pfx.IsSingleIP() || skipPfx[pfx] {
			continue
		}
		addr := pfx.Addr()
		if seen[addr] {
			continue
		}
		seen[addr] = true
		if addr.Is4() {
			extra4 = append(extra4, addr.String())
		} else {
			out = append(out, addr.String())
		}
	}
	if len(extra4) > 0 {
		out = append(extra4, out...) // IPv4 first
	}
	return out
}

func addrsToStrings(addrs []netip.Addr) []string {
	if len(addrs) == 0 {
		return nil
	}
	out := make([]string, 0, len(addrs))
	for _, addr := range addrs {
		out = append(out, addr.String())
	}
	return out
}

func prefixesToStrings(prefixes []netip.Prefix) []string {
	if len(prefixes) == 0 {
		return nil
	}
	out := make([]string, 0, len(prefixes))
	for _, prefix := range prefixes {
		out = append(out, prefix.String())
	}
	return out
}

func nonZeroTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

type logRing struct {
	mu    sync.Mutex
	items []APILogEntry
	next  int
	full  bool
}

func newLogRing(size int) *logRing {
	return &logRing{items: make([]APILogEntry, 0, size)}
}

func (r *runtime) appendLog(level, event, message string) {
	if r == nil || r.logs == nil {
		return
	}
	r.logs.append(APILogEntry{
		Time:    time.Now().UTC(),
		Level:   level,
		Event:   event,
		Message: message,
	})
}

func (b *logRing) append(entry APILogEntry) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if cap(b.items) == 0 {
		return
	}
	if len(b.items) < cap(b.items) {
		b.items = append(b.items, entry)
		return
	}
	b.items[b.next] = entry
	b.next = (b.next + 1) % cap(b.items)
	b.full = true
}

func (b *logRing) entries() []APILogEntry {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.items) == 0 {
		return []APILogEntry{}
	}
	if !b.full {
		return append([]APILogEntry(nil), b.items...)
	}
	out := make([]APILogEntry, 0, len(b.items))
	out = append(out, b.items[b.next:]...)
	out = append(out, b.items[:b.next]...)
	return out
}
