package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/metacubex/mihomo/transport/socks5"
)

func main() {
	gateway := flag.String("gateway", "127.0.0.1:1667", "SOCKS5 gateway address")
	tcpTarget := flag.String("tcp", "", "TCP target to connect through the gateway")
	udpTarget := flag.String("udp", "", "UDP target to send through the gateway")
	udpPayload := flag.String("udp-payload", "", "UDP payload, optionally prefixed with hex:")
	count := flag.Int("count", 1, "number of UDP sends")
	timeout := flag.Duration("timeout", 5*time.Second, "operation timeout")
	flag.Parse()

	var failed bool
	if *tcpTarget != "" {
		if err := checkTCP(*gateway, *tcpTarget, *timeout); err != nil {
			fmt.Fprintf(os.Stderr, "tcp check failed: %v\n", err)
			failed = true
		}
	}
	if *udpTarget != "" {
		payload, err := parsePayload(*udpPayload)
		if err != nil {
			fmt.Fprintf(os.Stderr, "udp payload failed: %v\n", err)
			failed = true
		} else if err := checkUDP(*gateway, *udpTarget, payload, *count, *timeout); err != nil {
			fmt.Fprintf(os.Stderr, "udp check failed: %v\n", err)
			failed = true
		}
	}
	if *tcpTarget == "" && *udpTarget == "" {
		fmt.Fprintln(os.Stderr, "at least one of -tcp or -udp is required")
		failed = true
	}
	if failed {
		os.Exit(1)
	}
}

func checkTCP(gateway, target string, timeout time.Duration) error {
	start := time.Now()
	conn, err := net.DialTimeout("tcp", gateway, timeout)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	targetAddr := socks5.ParseAddr(target)
	if targetAddr == nil {
		return fmt.Errorf("invalid TCP target: %s", target)
	}
	if _, err := socks5.ClientHandshake(conn, targetAddr, socks5.CmdConnect, nil); err != nil {
		return err
	}
	fmt.Printf("tcp connected target=%s latency=%s\n", target, time.Since(start).Round(time.Millisecond))
	return nil
}

func checkUDP(gateway, target string, payload []byte, count int, timeout time.Duration) error {
	tcpConn, err := net.DialTimeout("tcp", gateway, timeout)
	if err != nil {
		return err
	}
	defer tcpConn.Close()
	_ = tcpConn.SetDeadline(time.Now().Add(timeout))

	relayAddr, err := socks5.ClientHandshake(tcpConn, socks5.ParseAddr("0.0.0.0:0"), socks5.CmdUDPAssociate, nil)
	if err != nil {
		return err
	}
	relayUDPAddr := relayAddr.UDPAddr()
	if relayUDPAddr == nil {
		return fmt.Errorf("invalid UDP relay address: %s", relayAddr.String())
	}
	relayUDPAddr, listenNetwork, listenAddr, err := resolveRelayUDPAddr(relayUDPAddr)
	if err != nil {
		return err
	}

	udpConn, err := net.ListenPacket(listenNetwork, listenAddr)
	if err != nil {
		return err
	}
	defer udpConn.Close()
	_ = udpConn.SetDeadline(time.Now().Add(timeout))

	targetAddr := socks5.ParseAddr(target)
	if targetAddr == nil {
		return fmt.Errorf("invalid UDP target: %s", target)
	}
	for i := 0; i < count; i++ {
		packet, err := socks5.EncodeUDPPacket(targetAddr, payload)
		if err != nil {
			return err
		}
		start := time.Now()
		if _, err := udpConn.WriteTo(packet, relayUDPAddr); err != nil {
			return err
		}
		buf := make([]byte, 64*1024)
		n, from, err := udpConn.ReadFrom(buf)
		if err != nil {
			return err
		}
		source, body, err := socks5.DecodeUDPPacket(buf[:n])
		if err != nil {
			return err
		}
		fmt.Printf("udp response target=%s relay=%s from=%s source=%s bytes=%d latency=%s\n",
			target, relayUDPAddr, from, source.String(), len(body), time.Since(start).Round(time.Millisecond))
	}
	return nil
}

func resolveRelayUDPAddr(relayUDPAddr *net.UDPAddr) (*net.UDPAddr, string, string, error) {
	resolved := *relayUDPAddr
	listenNetwork := "udp4"
	listenAddr := "127.0.0.1:0"
	if resolved.IP.To4() == nil {
		listenNetwork = "udp6"
		listenAddr = "[::1]:0"
	}
	if !resolved.IP.IsUnspecified() {
		return &resolved, listenNetwork, listenAddr, nil
	}
	// The gateway is expected to translate wildcard UDP listeners into a
	// concrete relay address via the accepted TCP control connection. If an
	// unspecified relay still appears here, surface it as a real failure rather
	// than guessing an address locally and masking the server-side bug.
	return nil, "", "", fmt.Errorf("unsupported unspecified UDP relay address: %s", relayUDPAddr.String())
}

func parsePayload(value string) ([]byte, error) {
	if value == "" {
		return []byte("mihomo-tsnet-gateway-check"), nil
	}
	if raw, ok := strings.CutPrefix(value, "hex:"); ok {
		return hex.DecodeString(raw)
	}
	if n, err := strconv.ParseUint(value, 10, 16); err == nil {
		return []byte{byte(n >> 8), byte(n)}, nil
	}
	return []byte(value), nil
}
