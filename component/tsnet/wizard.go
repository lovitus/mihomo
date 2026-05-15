package tsnet

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	C "github.com/metacubex/mihomo/constant"
	"gopkg.in/yaml.v3"
	"tailscale.com/ipn"
	tsnetlib "tailscale.com/tsnet"
)

const wizardTimeout = 2 * time.Minute

// RunWizard is an interactive CLI for Tailscale node registration and diagnostics.
// It runs independently of the full mihomo stack (no proxy, no geo-site), making
// it suitable for platforms like OpenWrt where the auth URL is hard to find in logs.
//
// Invoked via: mihomo -d <homedir> -f <config> -tailscale-wizard
// Returns 0 on success, 1 on failure.
func RunWizard(homeDir, configFile string) int {
	fmt.Println("─────────────────────────────────────────────────")
	fmt.Println("  Mihomo Tailscale Wizard")
	fmt.Println("─────────────────────────────────────────────────")

	if homeDir != "" && !filepath.IsAbs(homeDir) {
		if cwd, err := os.Getwd(); err == nil {
			homeDir = filepath.Join(cwd, homeDir)
		}
	}
	if configFile != "" && !filepath.IsAbs(configFile) {
		if cwd, err := os.Getwd(); err == nil {
			configFile = filepath.Join(cwd, configFile)
		}
	}

	cfg := wizardLoadConfig(homeDir, configFile)

	if cfg.ConfigError != nil {
		fmt.Println()
		fmt.Printf("  ERROR: %v\n", cfg.ConfigError)
		return 1
	}
	if cfg.LoginServer == "" {
		fmt.Println()
		fmt.Println("  ERROR: no login-server configured.")
		fmt.Println("  Set tailscale.login-server in config.yaml.")
		return 1
	}
	if cfg.StateDir == "" {
		fmt.Println()
		fmt.Println("  ERROR: no state-dir configured.")
		fmt.Println("  Set tailscale.state-dir in config.yaml.")
		return 1
	}

	fmt.Printf("  Server:   %s\n", cfg.LoginServer)
	if len(cfg.LoginServerIPFallbacks) > 0 {
		fmt.Printf("  IP fallback candidates: %d\n", len(cfg.LoginServerIPFallbacks))
	}
	fmt.Printf("  StateDir: %s\n", cfg.StateDir)
	fmt.Println()

	wizardShowStateInfo(cfg.StateDir)

	lock, err := lockStateDir(cfg.StateDir)
	if err != nil {
		fmt.Printf("  ERROR: state-dir is locked: %v\n", err)
		fmt.Println("  Another mihomo instance may already be using this state-dir.")
		return 1
	}
	defer lock.Close()

	nodeName, err := stableNodeName(cfg.StateDir)
	if err != nil {
		fmt.Printf("  ERROR generating node name: %v\n", err)
		return 1
	}
	fmt.Printf("Starting node: %s\n", nodeName)

	disableTailscaleBackgroundLogUploads()
	selection := selectLoginServer(context.Background(), cfg.LoginServer, cfg.LoginServerIPFallbacks, probeLoginServerKey)
	wizardPrintLoginServerSelection(selection)
	endResolverLifecycle := beginDefaultResolverLifecycle()
	defer endResolverLifecycle()

	server := &tsnetlib.Server{
		Dir:        cfg.StateDir,
		Hostname:   nodeName,
		ControlURL: selection.ActiveLoginServer,
		Port:       0,
		UserLogf:   func(string, ...any) {},
	}

	if err := server.Start(); err != nil {
		fmt.Printf("  ERROR: failed to start tsnet server: %v\n", err)
		return 1
	}
	defer server.Close()

	lc, err := server.LocalClient()
	if err != nil {
		fmt.Printf("  ERROR: local client: %v\n", err)
		return 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), wizardTimeout)
	defer cancel()

	watcher, err := lc.WatchIPNBus(ctx, ipn.NotifyInitialState|ipn.NotifyNoPrivateKeys)
	if err != nil {
		fmt.Printf("  ERROR: watch state: %v\n", err)
		return 1
	}
	defer watcher.Close()

	fmt.Printf("Waiting for connection (timeout: %s) ...\n", wizardTimeout)

	authShown := false
	var lastPrintedState ipn.State
	for {
		n, err := watcher.Next()
		if err != nil {
			if ctx.Err() != nil {
				fmt.Println()
				fmt.Println("  TIMEOUT: node did not connect within the allotted time.")
				fmt.Println("  Hints:")
				fmt.Println("    - Verify the Headscale server is reachable from this device")
				fmt.Println("    - Check firewall/iptables rules allow outbound TCP 443")
				fmt.Printf("    - Try: curl -v --max-time 10 %s/key\n", selection.ActiveLoginServer)
				return 1
			}
			fmt.Printf("  ERROR: %v\n", err)
			return 1
		}

		if n.BrowseToURL != nil && *n.BrowseToURL != "" && !authShown {
			authShown = true
			fmt.Println()
			fmt.Println("┌─ ACTION REQUIRED ─────────────────────────────────────────┐")
			fmt.Println("│ Open this URL in a browser to authorize this node:         │")
			fmt.Println("│                                                            │")
			fmt.Printf("│  %s\n", *n.BrowseToURL)
			fmt.Println("│                                                            │")
			fmt.Println("└────────────────────────────────────────────────────────────┘")
			fmt.Println()
			fmt.Println("Waiting for authorization ...")
		}

		if n.State == nil {
			continue
		}

		switch *n.State {
		case ipn.Running:
			status, err := lc.Status(ctx)
			if err != nil {
				fmt.Printf("  ERROR: get status: %v\n", err)
				return 1
			}
			ips := addrsToStrings(status.TailscaleIPs)
			fmt.Println()
			fmt.Println("─────────────────────────────────────────────────")
			fmt.Println("  SUCCESS: Tailscale node is connected!")
			if status.Self != nil {
				fmt.Printf("  Node:  %s\n", status.Self.HostName)
			}
			fmt.Printf("  IPs:   %v\n", ips)
			fmt.Println()
			fmt.Println("  You can now start mihomo normally.")
			fmt.Println("─────────────────────────────────────────────────")
			return 0

		case ipn.NeedsMachineAuth:
			if lastPrintedState != ipn.NeedsMachineAuth {
				fmt.Println("  Status: waiting for machine authorization in Headscale admin panel ...")
				lastPrintedState = ipn.NeedsMachineAuth
			}

		case ipn.NeedsLogin, ipn.NoState:
			if !authShown && lastPrintedState != *n.State {
				fmt.Println("  Status: waiting for Headscale authorization ...")
				lastPrintedState = *n.State
			}

		default:
			if lastPrintedState != *n.State {
				fmt.Printf("  Status: %s ...\n", *n.State)
				lastPrintedState = *n.State
			}
		}
	}
}

// wizardFileCfg is a minimal YAML struct that reads only the tailscale section,
// avoiding any dependency on geo-site, rules, or other mihomo subsystems.
type wizardFileCfg struct {
	Tailscale struct {
		LoginServer            string   `yaml:"login-server"`
		LoginServerIPFallbacks []string `yaml:"login-server-ip-fallbacks"`
		StateDir               string   `yaml:"state-dir"`
	} `yaml:"tailscale"`
}

type wizardRunCfg struct {
	LoginServer            string
	LoginServerIPFallbacks []string
	StateDir               string
	ConfigError            error
}

func wizardLoadConfig(homeDir, configFile string) wizardRunCfg {
	var cfg wizardRunCfg
	// Mirror main.go / C.Path.Resolve: when -d is omitted fall back to the
	// same default home directory that normal mihomo startup would use.
	effectiveHome := homeDir
	if effectiveHome == "" {
		effectiveHome = C.Path.HomeDir()
	}
	if configFile == "" {
		configFile = filepath.Join(effectiveHome, "config.yaml")
	}
	data, err := os.ReadFile(configFile)
	if err != nil {
		fmt.Printf("  Config: cannot read %s: %v\n", configFile, err)
		return cfg
	}
	fmt.Printf("  Config: %s\n", configFile)
	var raw wizardFileCfg
	if err := yaml.Unmarshal(data, &raw); err != nil {
		fmt.Printf("  Config: parse error: %v\n", err)
		return cfg
	}
	cfg.LoginServer = raw.Tailscale.LoginServer
	if cfg.LoginServerIPFallbacks, err = normalizeLoginServerIPFallbacks(raw.Tailscale.LoginServerIPFallbacks); err != nil {
		cfg.ConfigError = err
		return cfg
	}
	stateDir := raw.Tailscale.StateDir
	if stateDir == "" {
		// Match config.DefaultRawConfig default so the wizard accepts the
		// same configurations as normal mihomo startup.
		stateDir = "tailscale"
	}
	if !filepath.IsAbs(stateDir) {
		cfg.StateDir = filepath.Join(effectiveHome, stateDir)
	} else {
		cfg.StateDir = stateDir
	}
	return cfg
}

func wizardShowStateInfo(stateDir string) {
	stateFile := filepath.Join(stateDir, "tailscaled.state")
	info, err := os.Stat(stateFile)
	if err != nil {
		fmt.Printf("State file: %s\n  NOT FOUND — node will register fresh.\n\n", stateFile)
		return
	}
	fmt.Printf("State file: %s\n", stateFile)
	fmt.Printf("  Size: %d bytes | Modified: %s\n", info.Size(), info.ModTime().Format("2006-01-02 15:04:05"))
	if info.Size() < 200 {
		fmt.Println("  WARNING: Very small — node key likely missing, registration required.")
	} else {
		fmt.Println("  Node key present — will attempt reconnect.")
	}
	fmt.Println()
}

func wizardPrintLoginServerSelection(selection loginServerSelection) {
	for _, result := range selection.Results {
		if result.Success {
			fmt.Printf("  Login server selected: %s (%s)\n", result.URL, result.Source)
			continue
		}
		fmt.Printf("  Login server probe failed: %s (%s): %v\n", result.URL, result.Source, result.Err)
	}
	if selection.ActiveLoginServer != selection.LoginServer {
		fmt.Printf("  Login server configured: %s\n", selection.LoginServer)
		fmt.Printf("  Login server active:     %s\n", selection.ActiveLoginServer)
		return
	}
	if len(selection.Results) > 0 && !loginServerSelectionSucceeded(selection) {
		fmt.Printf("  Login server active:     %s (primary; all IP probes failed)\n", selection.ActiveLoginServer)
	}
}
