package tsnet

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	stdhttp "net/http"
	"net/url"
	"strings"
	"time"

	"github.com/metacubex/mihomo/log"
)

const loginServerProbeTimeout = 2 * time.Second

type loginServerSelection struct {
	LoginServer       string
	ActiveLoginServer string
	Source            string
	Results           []loginServerProbeResult
}

type loginServerProbeResult struct {
	URL     string
	Source  string
	Success bool
	Err     error
}

type loginServerProbeFunc func(context.Context, string) error

func normalizeLoginServerIPFallbacks(values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	fallbacks := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		normalized, err := normalizeLoginServerIPFallback(value)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		fallbacks = append(fallbacks, normalized)
	}
	return fallbacks, nil
}

func normalizeLoginServerIPFallback(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("tailscale.login-server-ip-fallbacks must not contain empty entries")
	}
	u, err := url.Parse(value)
	if err != nil {
		return "", fmt.Errorf("invalid tailscale.login-server-ip-fallbacks entry %q: %w", value, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("tailscale.login-server-ip-fallbacks only supports http/https URLs: %s", value)
	}
	host := u.Hostname()
	if host == "" {
		return "", fmt.Errorf("tailscale.login-server-ip-fallbacks host is required: %s", value)
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return "", fmt.Errorf("tailscale.login-server-ip-fallbacks only accepts IP literal hosts, got %s", host)
	}
	if ip.To4() == nil && !strings.HasPrefix(u.Host, "[") {
		return "", fmt.Errorf("tailscale.login-server-ip-fallbacks IPv6 URLs must use brackets: %s", value)
	}
	return value, nil
}

func selectLoginServer(ctx context.Context, loginServer string, ipFallbacks []string, probe loginServerProbeFunc) loginServerSelection {
	if probe == nil {
		probe = probeLoginServerKey
	}
	selection := loginServerSelection{
		LoginServer:       loginServer,
		ActiveLoginServer: loginServer,
		Source:            "primary",
	}
	candidates := loginServerProbeCandidates(loginServer, ipFallbacks)
	for _, candidate := range candidates {
		err := probe(ctx, candidate.url)
		result := loginServerProbeResult{URL: candidate.url, Source: candidate.source, Err: err}
		if err == nil {
			result.Success = true
			selection.ActiveLoginServer = candidate.url
			selection.Source = candidate.source
			selection.Results = append(selection.Results, result)
			return selection
		}
		selection.Results = append(selection.Results, result)
	}
	return selection
}

type loginServerProbeCandidate struct {
	url    string
	source string
}

func loginServerProbeCandidates(loginServer string, ipFallbacks []string) []loginServerProbeCandidate {
	candidates := make([]loginServerProbeCandidate, 0, len(ipFallbacks)+1)
	seen := make(map[string]struct{}, len(ipFallbacks)+1)
	if isLoginServerIPLiteral(loginServer) {
		candidates = append(candidates, loginServerProbeCandidate{url: loginServer, source: "primary-ip"})
		seen[loginServer] = struct{}{}
	}
	for _, fallback := range ipFallbacks {
		if _, ok := seen[fallback]; ok {
			continue
		}
		candidates = append(candidates, loginServerProbeCandidate{url: fallback, source: "ip-fallback"})
		seen[fallback] = struct{}{}
	}
	return candidates
}

func isLoginServerIPLiteral(rawURL string) bool {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return false
	}
	return net.ParseIP(u.Hostname()) != nil
}

func probeLoginServerKey(ctx context.Context, rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("unsupported login-server scheme: %s", u.Scheme)
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil {
		return fmt.Errorf("login-server probe requires IP literal host: %s", u.Hostname())
	}
	port := u.Port()
	if port == "" {
		switch u.Scheme {
		case "http":
			port = "80"
		case "https":
			port = "443"
		}
	}
	probeURL := *u
	probeURL.Path = strings.TrimRight(probeURL.Path, "/") + "/key"
	probeURL.RawQuery = ""
	probeURL.Fragment = ""

	dialAddr := net.JoinHostPort(ip.String(), port)
	transport := &stdhttp.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, gotPort, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			gotIP := net.ParseIP(host)
			if gotIP == nil || !gotIP.Equal(ip) || gotPort != port {
				return nil, fmt.Errorf("login-server probe refused non-candidate dial target: %s", addr)
			}
			var d net.Dialer
			return d.DialContext(ctx, network, dialAddr)
		},
	}
	client := &stdhttp.Client{
		Transport: transport,
		Timeout:   loginServerProbeTimeout,
		CheckRedirect: func(*stdhttp.Request, []*stdhttp.Request) error {
			return stdhttp.ErrUseLastResponse
		},
	}
	req, err := stdhttp.NewRequestWithContext(ctx, stdhttp.MethodGet, probeURL.String(), nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != stdhttp.StatusOK {
		return fmt.Errorf("login-server probe returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func logLoginServerSelection(selection loginServerSelection) {
	for _, result := range selection.Results {
		if result.Success {
			log.Infoln("[Tailscale] login-server selected url=%s source=%s", result.URL, result.Source)
			continue
		}
		log.Warnln("[Tailscale] login-server probe failed url=%s source=%s err=%v", result.URL, result.Source, result.Err)
	}
	if selection.ActiveLoginServer == selection.LoginServer {
		if len(selection.Results) == 0 {
			log.Infoln("[Tailscale] login-server selected url=%s source=primary", selection.LoginServer)
		} else if !loginServerSelectionSucceeded(selection) {
			log.Warnln("[Tailscale] login-server ip fallback probes failed; using primary login-server=%s", selection.LoginServer)
		}
		return
	}
	log.Infoln("[Tailscale] login-server configured=%s active=%s source=%s", selection.LoginServer, selection.ActiveLoginServer, selection.Source)
}

func loginServerSelectionSucceeded(selection loginServerSelection) bool {
	for _, result := range selection.Results {
		if result.Success {
			return true
		}
	}
	return false
}
