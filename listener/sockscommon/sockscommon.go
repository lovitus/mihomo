package sockscommon

import (
	"io"
	"net"
	"time"

	"github.com/metacubex/mihomo/adapter/inbound"
	N "github.com/metacubex/mihomo/common/net"
	authc "github.com/metacubex/mihomo/component/auth"
	C "github.com/metacubex/mihomo/constant"
	authStore "github.com/metacubex/mihomo/listener/auth"
	"github.com/metacubex/mihomo/transport/socks4"
	"github.com/metacubex/mihomo/transport/socks5"
)

type ServeOption struct {
	AuthStore                     authc.AuthStore
	Additions                     []inbound.Addition
	UDPBindAddr                   socks5.Addr
	RejectUDPAssociateWithoutBind bool
	HandshakeTimeout              time.Duration
}

func ServeConn(conn net.Conn, tunnel C.Tunnel, option ServeOption) {
	store := option.AuthStore
	if store == nil {
		store = authStore.Default
	}
	additions := option.Additions
	if len(additions) == 0 {
		additions = []inbound.Addition{
			inbound.WithInName("DEFAULT-SOCKS"),
			inbound.WithSpecialRules(""),
		}
	}

	if option.HandshakeTimeout > 0 {
		_ = conn.SetReadDeadline(time.Now().Add(option.HandshakeTimeout))
	}
	bufConn := N.NewBufferedConn(conn)
	head, err := bufConn.Peek(1)
	if err != nil {
		conn.Close()
		return
	}

	switch head[0] {
	case socks4.Version:
		handleSocks4(bufConn, tunnel, store, option.HandshakeTimeout > 0, additions...)
	case socks5.Version:
		handleSocks5WithBindPolicy(bufConn, tunnel, store, option.UDPBindAddr, option.RejectUDPAssociateWithoutBind, option.HandshakeTimeout > 0, additions...)
	default:
		conn.Close()
	}
}

func HandleSocks4(conn net.Conn, tunnel C.Tunnel, store authc.AuthStore, additions ...inbound.Addition) {
	handleSocks4(conn, tunnel, store, false, additions...)
}

func handleSocks4(conn net.Conn, tunnel C.Tunnel, store authc.AuthStore, clearDeadline bool, additions ...inbound.Addition) {
	authenticator := store.Authenticator()
	addr, _, user, err := socks4.ServerHandshake(conn, authenticator)
	if err != nil {
		conn.Close()
		return
	}
	if clearDeadline {
		_ = conn.SetReadDeadline(time.Time{})
	}
	additions = append(additions, inbound.WithInUser(user))
	tunnel.HandleTCPConn(inbound.NewSocket(socks5.ParseAddr(addr), conn, C.SOCKS4, additions...))
}

func HandleSocks5(conn net.Conn, tunnel C.Tunnel, store authc.AuthStore, additions ...inbound.Addition) {
	HandleSocks5WithBindAddr(conn, tunnel, store, nil, additions...)
}

func HandleSocks5WithBindAddr(conn net.Conn, tunnel C.Tunnel, store authc.AuthStore, udpBindAddr socks5.Addr, additions ...inbound.Addition) {
	HandleSocks5WithBindPolicy(conn, tunnel, store, udpBindAddr, false, additions...)
}

func HandleSocks5WithBindPolicy(conn net.Conn, tunnel C.Tunnel, store authc.AuthStore, udpBindAddr socks5.Addr, rejectUDPAssociateWithoutBind bool, additions ...inbound.Addition) {
	handleSocks5WithBindPolicy(conn, tunnel, store, udpBindAddr, rejectUDPAssociateWithoutBind, false, additions...)
}

func handleSocks5WithBindPolicy(conn net.Conn, tunnel C.Tunnel, store authc.AuthStore, udpBindAddr socks5.Addr, rejectUDPAssociateWithoutBind bool, clearDeadline bool, additions ...inbound.Addition) {
	authenticator := store.Authenticator()
	allowLocalAddrFallback := !rejectUDPAssociateWithoutBind || udpBindAddr != nil
	target, command, user, err := socks5.ServerHandshakeWithReplyAddrPolicy(conn, authenticator, udpBindAddr, allowLocalAddrFallback)
	if err != nil {
		conn.Close()
		return
	}
	if clearDeadline {
		_ = conn.SetReadDeadline(time.Time{})
	}
	if command == socks5.CmdUDPAssociate {
		defer conn.Close()
		io.Copy(io.Discard, conn)
		return
	}
	additions = append(additions, inbound.WithInUser(user))
	tunnel.HandleTCPConn(inbound.NewSocket(target, conn, C.SOCKS5, additions...))
}

func ServePacketConn(pc net.PacketConn, tunnel C.Tunnel, closed func() bool, additions ...inbound.Addition) {
	if len(additions) == 0 {
		additions = []inbound.Addition{
			inbound.WithInName("DEFAULT-SOCKS"),
			inbound.WithSpecialRules(""),
		}
	}
	conn := N.NewEnhancePacketConn(pc)
	for {
		data, put, remoteAddr, err := conn.WaitReadFrom()
		if err != nil {
			if put != nil {
				put()
			}
			if closed != nil && closed() {
				break
			}
			continue
		}
		HandleSocksUDP(pc, tunnel, data, put, remoteAddr, additions...)
	}
}

func HandleSocksUDP(pc net.PacketConn, tunnel C.Tunnel, buf []byte, put func(), addr net.Addr, additions ...inbound.Addition) {
	target, payload, err := socks5.DecodeUDPPacket(buf)
	if err != nil {
		if put != nil {
			put()
		}
		return
	}
	packet := &packet{
		pc:      pc,
		rAddr:   addr,
		payload: payload,
		put:     put,
	}
	tunnel.HandleUDPPacket(inbound.NewPacket(target, packet, C.SOCKS5, additions...))
}

type packet struct {
	pc      net.PacketConn
	rAddr   net.Addr
	payload []byte
	put     func()
}

func (c *packet) Data() []byte {
	return c.payload
}

func (c *packet) WriteBack(b []byte, addr net.Addr) (n int, err error) {
	packet, err := socks5.EncodeUDPPacket(socks5.ParseAddrToSocksAddr(addr), b)
	if err != nil {
		return
	}
	return c.pc.WriteTo(packet, c.rAddr)
}

func (c *packet) LocalAddr() net.Addr {
	return c.rAddr
}

func (c *packet) Drop() {
	if c.put != nil {
		c.put()
		c.put = nil
	}
	c.payload = nil
}

func (c *packet) InAddr() net.Addr {
	return c.pc.LocalAddr()
}
