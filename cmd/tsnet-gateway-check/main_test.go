package main

import (
	"net"
	"strings"
	"testing"
)

func TestResolveRelayUDPAddrUsesIPv4ListenSocket(t *testing.T) {
	relay, network, listenAddr, err := resolveRelayUDPAddr(&net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1667})
	if err != nil {
		t.Fatalf("resolveRelayUDPAddr() error = %v", err)
	}
	if got := relay.String(); got != "127.0.0.1:1667" {
		t.Fatalf("relay = %s, want 127.0.0.1:1667", got)
	}
	if network != "udp4" || listenAddr != "127.0.0.1:0" {
		t.Fatalf("listen = %s %s, want udp4 127.0.0.1:0", network, listenAddr)
	}
}

func TestResolveRelayUDPAddrUsesIPv6ListenSocket(t *testing.T) {
	relay, network, listenAddr, err := resolveRelayUDPAddr(&net.UDPAddr{IP: net.ParseIP("::1"), Port: 1667})
	if err != nil {
		t.Fatalf("resolveRelayUDPAddr() error = %v", err)
	}
	if got := relay.String(); got != "[::1]:1667" {
		t.Fatalf("relay = %s, want [::1]:1667", got)
	}
	if network != "udp6" || listenAddr != "[::1]:0" {
		t.Fatalf("listen = %s %s, want udp6 [::1]:0", network, listenAddr)
	}
}

func TestResolveRelayUDPAddrRejectsUnspecifiedRelay(t *testing.T) {
	_, _, _, err := resolveRelayUDPAddr(&net.UDPAddr{IP: net.IPv4zero, Port: 1667})
	if err == nil || !strings.Contains(err.Error(), "unspecified UDP relay address") {
		t.Fatalf("resolveRelayUDPAddr() error = %v, want unspecified relay error", err)
	}
}
