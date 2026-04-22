package sockscommon

import (
	"io"
	"net"
	"testing"
	"time"

	C "github.com/metacubex/mihomo/constant"
)

type testTunnel struct {
	handleTCPConn func(net.Conn, *C.Metadata)
}

func (t testTunnel) HandleTCPConn(conn net.Conn, metadata *C.Metadata) {
	if t.handleTCPConn != nil {
		t.handleTCPConn(conn, metadata)
		return
	}
	_ = conn.Close()
}

func (t testTunnel) HandleUDPPacket(packet C.UDPPacket, metadata *C.Metadata) {}

func (t testTunnel) NatTable() C.NatTable { return nil }

func TestServeConnHandshakeTimeoutClosesIdleClient(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()

	done := make(chan struct{})
	go func() {
		ServeConn(server, testTunnel{}, ServeOption{HandshakeTimeout: 20 * time.Millisecond})
		close(done)
	}()

	_ = client.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 1)
	if _, err := client.Read(buf); err == nil {
		t.Fatal("expected idle client connection to close after handshake timeout")
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("ServeConn did not return after handshake timeout")
	}
}

func TestServeConnClearsHandshakeDeadlineAfterSocks5Connect(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	payloadErr := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			payloadErr <- err
			return
		}
		ServeConn(conn, testTunnel{handleTCPConn: func(conn net.Conn, metadata *C.Metadata) {
			defer conn.Close()
			buf := make([]byte, 4)
			if _, err := io.ReadFull(conn, buf); err != nil {
				payloadErr <- err
				return
			}
			if string(buf) != "ping" {
				payloadErr <- io.ErrUnexpectedEOF
				return
			}
			payloadErr <- nil
		}}, ServeOption{HandshakeTimeout: 50 * time.Millisecond})
	}()

	client, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	if _, err := client.Write([]byte{5, 1, 0}); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(client, make([]byte, 2)); err != nil {
		t.Fatal(err)
	}

	req := []byte{5, 1, 0, 1, 1, 2, 3, 4, 0, 80}
	if _, err := client.Write(req); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(client, make([]byte, 10)); err != nil {
		t.Fatal(err)
	}

	time.Sleep(100 * time.Millisecond)
	if _, err := client.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-payloadErr:
		if err != nil {
			t.Fatalf("payload read failed after handshake deadline should have been cleared: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for payload read")
	}
}
