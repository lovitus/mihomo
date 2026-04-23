package outbound

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strconv"

	N "github.com/metacubex/mihomo/common/net"
	"github.com/metacubex/mihomo/component/ca"
	"github.com/metacubex/mihomo/component/tsnet"
	C "github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/log"
	"github.com/metacubex/mihomo/transport/socks5"

	"github.com/metacubex/tls"
)

type Socks5 struct {
	*Base
	option         *Socks5Option
	user           string
	pass           string
	tls            bool
	skipCertVerify bool
	tlsConfig      *tls.Config
}

type Socks5Option struct {
	BasicOption
	Name           string `proxy:"name"`
	Server         string `proxy:"server"`
	Port           int    `proxy:"port"`
	UserName       string `proxy:"username,omitempty"`
	Password       string `proxy:"password,omitempty"`
	TLS            bool   `proxy:"tls,omitempty"`
	UDP            bool   `proxy:"udp,omitempty"`
	SkipCertVerify bool   `proxy:"skip-cert-verify,omitempty"`
	Fingerprint    string `proxy:"fingerprint,omitempty"`
	Certificate    string `proxy:"certificate,omitempty"`
	PrivateKey     string `proxy:"private-key,omitempty"`
}

// StreamConnContext implements C.ProxyAdapter
func (ss *Socks5) StreamConnContext(ctx context.Context, c net.Conn, metadata *C.Metadata) (net.Conn, error) {
	if ss.tls {
		cc := tls.Client(c, ss.tlsConfig)
		err := cc.HandshakeContext(ctx)
		c = cc
		if err != nil {
			return nil, fmt.Errorf("%s connect error: %w", ss.addr, err)
		}
	}

	var user *socks5.User
	if ss.user != "" {
		user = &socks5.User{
			Username: ss.user,
			Password: ss.pass,
		}
	}
	if _, err := ss.clientHandshakeContext(ctx, c, serializesSocksAddr(metadata), socks5.CmdConnect, user); err != nil {
		return nil, err
	}
	return c, nil
}

// DialContext implements C.ProxyAdapter
func (ss *Socks5) DialContext(ctx context.Context, metadata *C.Metadata) (_ C.Conn, err error) {
	c, _, err := ss.dialSocksServer(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s connect error: %w", ss.addr, err)
	}

	defer func(c net.Conn) {
		safeConnClose(c, err)
	}(c)

	c, err = ss.StreamConnContext(ctx, c, metadata)
	if err != nil {
		return nil, err
	}

	return NewConn(c, ss), nil
}

// ListenPacketContext implements C.ProxyAdapter
func (ss *Socks5) ListenPacketContext(ctx context.Context, metadata *C.Metadata) (_ C.PacketConn, err error) {
	if err = ss.ResolveUDP(ctx, metadata); err != nil {
		return nil, err
	}
	pc, err := ss.listenPacketContext(ctx, metadata, true)
	if err == nil {
		return pc, nil
	}
	return nil, err
}

func (ss *Socks5) listenPacketContext(ctx context.Context, metadata *C.Metadata, allowTsnet bool) (_ C.PacketConn, err error) {
	c, usedTsnet, err := ss.dialSocksServerWithOption(ctx, allowTsnet)
	if err != nil {
		err = fmt.Errorf("%s connect error: %w", ss.addr, err)
		return
	}

	if ss.tls {
		cc := tls.Client(c, ss.tlsConfig)
		err = cc.HandshakeContext(ctx)
		if err != nil && usedTsnet {
			_ = c.Close()
			return ss.listenPacketContext(ctx, metadata, false)
		}
		if err != nil {
			return nil, fmt.Errorf("%s connect error: %w", ss.addr, err)
		}
		c = cc
	}

	defer func(c net.Conn) {
		safeConnClose(c, err)
	}(c)

	var user *socks5.User
	if ss.user != "" {
		user = &socks5.User{
			Username: ss.user,
			Password: ss.pass,
		}
	}

	udpAssocateAddr := socks5.AddrFromStdAddrPort(netip.AddrPortFrom(netip.IPv4Unspecified(), 0))
	bindAddr, err := ss.clientHandshakeContext(ctx, c, udpAssocateAddr, socks5.CmdUDPAssociate, user)
	if err != nil {
		err = fmt.Errorf("client hanshake error: %w", err)
		if usedTsnet {
			_ = c.Close()
			return ss.listenPacketContext(ctx, metadata, false)
		}
		return
	}

	// Support unspecified UDP bind address.
	bindUDPAddr := bindAddr.UDPAddr()
	if bindUDPAddr == nil {
		err = errors.New("invalid UDP bind address")
		if usedTsnet {
			_ = c.Close()
			return ss.listenPacketContext(ctx, metadata, false)
		}
		return
	} else if bindUDPAddr.IP.IsUnspecified() {
		// Keep the tsnet path intentionally small: do not add tailnet identity
		// or MagicDNS relay-address resolution here. Tailnet addresses are
		// private; if this resolution misses, the tsnet UDP attempt simply
		// fails and falls back to the existing default path below.
		serverAddr, err := resolveUDPAddr(ctx, "udp", ss.Addr(), C.IPv4Prefer)
		if err != nil {
			if usedTsnet {
				_ = c.Close()
				return ss.listenPacketContext(ctx, metadata, false)
			}
			return nil, err
		}

		bindUDPAddr.IP = serverAddr.IP
	}

	dialer := ss.dialer
	if usedTsnet {
		snap := tsnet.CurrentSnapshot()
		dialer = &snap
	}
	pc, err := dialer.ListenPacket(ctx, "udp", "", bindUDPAddr.AddrPort())
	if err != nil {
		if usedTsnet {
			_ = c.Close()
			return ss.listenPacketContext(ctx, metadata, false)
		}
		return
	}

	go func() {
		io.Copy(io.Discard, c)
		c.Close()
		// A UDP association terminates when the TCP connection that the UDP
		// ASSOCIATE request arrived on terminates. RFC1928
		pc.Close()
	}()

	return newPacketConn(&socksPacketConn{PacketConn: pc, rAddr: bindUDPAddr, tcpConn: c}, ss), nil
}

func (ss *Socks5) dialSocksServer(ctx context.Context) (net.Conn, bool, error) {
	return ss.dialSocksServerWithOption(ctx, true)
}

func (ss *Socks5) dialSocksServerWithOption(ctx context.Context, allowTsnet bool) (net.Conn, bool, error) {
	if allowTsnet {
		tsnet.NotifyUse()
		snap := tsnet.CurrentSnapshot()
		if snap.Ready && ss.option.Port == snap.Socks5Port {
			// Intentional: any SOCKS5 node using the configured tailnet SOCKS5
			// port first tries tsnet, then falls back to the original dialer.
			c, err := snap.DialContext(ctx, "tcp", ss.addr)
			if err == nil {
				return c, true, nil
			}
			log.Debugln("[Tailscale] tsnet dial failed for socks5 server %s: %v; fallback to default dialer", ss.addr, err)
		}
	}
	c, err := ss.dialer.DialContext(ctx, "tcp", ss.addr)
	return c, false, err
}

// ProxyInfo implements C.ProxyAdapter
func (ss *Socks5) ProxyInfo() C.ProxyInfo {
	info := ss.Base.ProxyInfo()
	info.DialerProxy = ss.option.DialerProxy
	return info
}

func (ss *Socks5) clientHandshakeContext(ctx context.Context, c net.Conn, addr socks5.Addr, command socks5.Command, user *socks5.User) (_ socks5.Addr, err error) {
	if ctx.Done() != nil {
		done := N.SetupContextForConn(ctx, c)
		defer done(&err)
	}
	return socks5.ClientHandshake(c, addr, command, user)
}

func NewSocks5(option Socks5Option) (*Socks5, error) {
	var tlsConfig *tls.Config
	if option.TLS {
		var err error
		tlsConfig, err = ca.GetTLSConfig(ca.Option{
			TLSConfig: &tls.Config{
				InsecureSkipVerify: option.SkipCertVerify,
				ServerName:         option.Server,
			},
			Fingerprint: option.Fingerprint,
			Certificate: option.Certificate,
			PrivateKey:  option.PrivateKey,
		})
		if err != nil {
			return nil, err
		}
	}

	outbound := &Socks5{
		Base: NewBase(BaseOption{
			Name:         option.Name,
			Addr:         net.JoinHostPort(option.Server, strconv.Itoa(option.Port)),
			Type:         C.Socks5,
			ProviderName: option.ProviderName,
			UDP:          option.UDP,
			TFO:          option.TFO,
			MPTCP:        option.MPTCP,
			Interface:    option.Interface,
			RoutingMark:  option.RoutingMark,
			Prefer:       option.IPVersion,
		}),
		option:         &option,
		user:           option.UserName,
		pass:           option.Password,
		tls:            option.TLS,
		skipCertVerify: option.SkipCertVerify,
		tlsConfig:      tlsConfig,
	}
	outbound.dialer = option.NewDialer(outbound.DialOptions())
	return outbound, nil
}

type socksPacketConn struct {
	net.PacketConn
	rAddr   net.Addr
	tcpConn net.Conn
}

func (uc *socksPacketConn) WriteTo(b []byte, addr net.Addr) (n int, err error) {
	packet, err := socks5.EncodeUDPPacket(socks5.ParseAddrToSocksAddr(addr), b)
	if err != nil {
		return
	}
	return uc.PacketConn.WriteTo(packet, uc.rAddr)
}

func (uc *socksPacketConn) ReadFrom(b []byte) (int, net.Addr, error) {
	n, _, e := uc.PacketConn.ReadFrom(b)
	if e != nil {
		return 0, nil, e
	}
	addr, payload, err := socks5.DecodeUDPPacket(b)
	if err != nil {
		return 0, nil, err
	}

	udpAddr := addr.UDPAddr()
	if udpAddr == nil {
		return 0, nil, errors.New("parse udp addr error")
	}

	// due to DecodeUDPPacket is mutable, record addr length
	copy(b, payload)
	return n - len(addr) - 3, udpAddr, nil
}

func (uc *socksPacketConn) Close() error {
	uc.tcpConn.Close()
	return uc.PacketConn.Close()
}
