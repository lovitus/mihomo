package config

import (
	"strings"
	"testing"
)

func TestParseTailscaleDisabledDefaults(t *testing.T) {
	cfg := DefaultRawConfig()
	ts, err := parseTailscale(cfg)
	if err != nil {
		t.Fatalf("parseTailscale() error = %v", err)
	}
	if ts.Enable {
		t.Fatal("tailscale should be disabled by default")
	}
	if ts.StateDir != "tailscale" {
		t.Fatalf("state-dir = %q, want tailscale", ts.StateDir)
	}
	if ts.Socks5 != 1666 {
		t.Fatalf("socks5 = %d, want 1666", ts.Socks5)
	}
	if ts.GatewaySocks5 != "" {
		t.Fatalf("gateway-socks5 = %q, want empty", ts.GatewaySocks5)
	}
}

func TestParseTailscaleRequiresLoginServerWhenEnabled(t *testing.T) {
	cfg := DefaultRawConfig()
	cfg.Tailscale.Enable = true
	_, err := parseTailscale(cfg)
	if err == nil || !strings.Contains(err.Error(), "login-server") {
		t.Fatalf("parseTailscale() error = %v, want login-server error", err)
	}
}

func TestParseTailscaleUsesDefaultStateDirWhenEnabled(t *testing.T) {
	cfg := DefaultRawConfig()
	cfg.Tailscale.Enable = true
	cfg.Tailscale.LoginServer = "https://hs.example.com"
	ts, err := parseTailscale(cfg)
	if err != nil {
		t.Fatalf("parseTailscale() error = %v", err)
	}
	if ts.StateDir != "tailscale" {
		t.Fatalf("state-dir = %q, want tailscale", ts.StateDir)
	}
}

func TestParseTailscaleRejectsEmptyStateDirAfterDefaults(t *testing.T) {
	cfg := DefaultRawConfig()
	cfg.Tailscale.Enable = true
	cfg.Tailscale.LoginServer = "https://hs.example.com"
	cfg.Tailscale.StateDir = ""
	_, err := parseTailscale(cfg)
	if err == nil || !strings.Contains(err.Error(), "state-dir") {
		t.Fatalf("parseTailscale() error = %v, want state-dir error", err)
	}
}

func TestParseTailscaleRejectsInvalidSocks5Port(t *testing.T) {
	for _, port := range []int{0, -1, 65536} {
		cfg := DefaultRawConfig()
		cfg.Tailscale.Enable = true
		cfg.Tailscale.LoginServer = "https://hs.example.com"
		cfg.Tailscale.Socks5 = port
		_, err := parseTailscale(cfg)
		if err == nil || !strings.Contains(err.Error(), "tailscale.socks5") {
			t.Fatalf("parseTailscale(port=%d) error = %v, want socks5 error", port, err)
		}
	}
}

func TestParseTailscaleNormalizesGatewaySocks5Port(t *testing.T) {
	cfg := DefaultRawConfig()
	cfg.Tailscale.Enable = true
	cfg.Tailscale.LoginServer = "https://hs.example.com"
	cfg.Tailscale.GatewaySocks5 = "1667"
	ts, err := parseTailscale(cfg)
	if err != nil {
		t.Fatalf("parseTailscale() error = %v", err)
	}
	if ts.GatewaySocks5 != "127.0.0.1:1667" {
		t.Fatalf("gateway-socks5 = %q, want 127.0.0.1:1667", ts.GatewaySocks5)
	}
}

func TestParseTailscaleAcceptsGatewaySocks5Addresses(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:1667", "[::1]:1667", "[::]:1667", ":1667", "0.0.0.0:1667"} {
		cfg := DefaultRawConfig()
		cfg.Tailscale.Enable = true
		cfg.Tailscale.LoginServer = "https://hs.example.com"
		cfg.Tailscale.GatewaySocks5 = addr
		ts, err := parseTailscale(cfg)
		if err != nil {
			t.Fatalf("parseTailscale(gateway=%q) error = %v", addr, err)
		}
		if ts.GatewaySocks5 == "" {
			t.Fatalf("gateway-socks5(%q) normalized to empty", addr)
		}
	}
}

func TestParseTailscalePreservesGatewaySocks5IPv6Wildcard(t *testing.T) {
	cfg := DefaultRawConfig()
	cfg.Tailscale.Enable = true
	cfg.Tailscale.LoginServer = "https://hs.example.com"
	cfg.Tailscale.GatewaySocks5 = "[::]:1667"
	ts, err := parseTailscale(cfg)
	if err != nil {
		t.Fatalf("parseTailscale() error = %v", err)
	}
	if ts.GatewaySocks5 != "[::]:1667" {
		t.Fatalf("gateway-socks5 = %q, want [::]:1667", ts.GatewaySocks5)
	}
}

func TestParseTailscaleRejectsInvalidGatewaySocks5(t *testing.T) {
	for _, addr := range []string{"0", ":0", "bad", "127.0.0.1:bad", "[fe80::1%lo0]:1667"} {
		cfg := DefaultRawConfig()
		cfg.Tailscale.Enable = true
		cfg.Tailscale.LoginServer = "https://hs.example.com"
		cfg.Tailscale.GatewaySocks5 = addr
		_, err := parseTailscale(cfg)
		if err == nil || !strings.Contains(err.Error(), "tailscale.gateway-socks5") {
			t.Fatalf("parseTailscale(gateway=%q) error = %v, want gateway-socks5 error", addr, err)
		}
	}
}
