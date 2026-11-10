package masque

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/sagernet/quic-go"
	"github.com/sagernet/quic-go/http3"
	"github.com/sagernet/sing-box/common/tls"
	"github.com/sagernet/sing-box/transport/masque/connectip"
	E "github.com/sagernet/sing/common/exceptions"
	"github.com/sagernet/sing/common/logger"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"

	"golang.org/x/net/http2"
)

const (
	MinPacketSize     = 1280
	initialPacketSize = 1350

	// The CONNECT-IP URI template path the panel (xray-core) MASQUE server answers on.
	DefaultPath = "/.well-known/masque/ip/*/*/"

	defaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36"
)

type Options struct {
	Logger    logger.Logger
	Dialer    N.Dialer
	Server    M.Socksaddr
	TLS       tls.Config
	HTTP2     bool
	Host      string
	Path      string
	Headers   http.Header
	KeepAlive time.Duration
}

type tunnelClient interface {
	Dial(*connectip.Request) (*connectip.Conn, *http.Response, error)
}

// Dial sets up a CONNECT-IP tunnel and returns once the server assigned an address.
func Dial(ctx context.Context, options Options) (*Conn, error) {
	if options.HTTP2 {
		return dialHTTP2(ctx, options)
	}
	return dialHTTP3(ctx, options)
}

func dialHTTP3(ctx context.Context, options Options) (*Conn, error) {
	options.TLS.SetNextProtos([]string{http3.NextProtoH3})
	stdConfig, err := options.TLS.STDConfig()
	if err != nil {
		return nil, err
	}
	packetConn, err := options.Dialer.DialContext(ctx, N.NetworkUDP, options.Server)
	if err != nil {
		return nil, E.Cause(err, "dial server")
	}
	keepAlive := options.KeepAlive
	if keepAlive == 0 {
		keepAlive = 10 * time.Second
	}
	qconn, err := quic.DialEarlyConn(ctx, packetConn, stdConfig, &quic.Config{
		MaxIdleTimeout:     30 * time.Second,
		KeepAlivePeriod:    keepAlive,
		MaxIncomingStreams: -1,
		InitialPacketSize:  initialPacketSize,
		EnableDatagrams:    true,
	})
	if err != nil {
		packetConn.Close()
		return nil, err
	}
	// quic-go does not own the dialed packet conn, so release it when the session ends.
	context.AfterFunc(qconn.Context(), func() { packetConn.Close() })
	transport := &http3.Transport{EnableDatagrams: true, DisableCompression: true}
	conn, err := establish(ctx, options, connectip.NewClientConn(transport.NewClientConn(qconn)), quicConn{qconn}, func() {
		qconn.CloseWithError(quic.ApplicationErrorCode(http3.ErrCodeRequestCanceled), "")
	}, options.serverName())
	if err != nil {
		qconn.CloseWithError(quic.ApplicationErrorCode(http3.ErrCodeNoError), "")
		return nil, err
	}
	return conn, nil
}

func dialHTTP2(ctx context.Context, options Options) (*Conn, error) {
	options.TLS.SetNextProtos([]string{http2.NextProtoTLS})
	tlsConn, err := tls.NewDialer(options.Dialer, options.TLS).DialTLSContext(ctx, options.Server)
	if err != nil {
		return nil, err
	}
	if protocol := tlsConn.ConnectionState().NegotiatedProtocol; protocol != http2.NextProtoTLS {
		tlsConn.Close()
		return nil, E.New("the server negotiated ", strconv.Quote(protocol), " instead of h2")
	}
	keepAlive := options.KeepAlive
	if keepAlive == 0 {
		keepAlive = http2KeepAlivePeriod
	}
	cc, err := newHTTP2ClientConn(tlsConn, keepAlive)
	if err != nil {
		tlsConn.Close()
		return nil, err
	}
	conn, err := establish(ctx, options, connectip.NewHTTP2ClientConn(cc), cc, func() { cc.Close() },
		options.serverName())
	if err != nil {
		cc.Close()
		return nil, err
	}
	return conn, nil
}

func (o Options) serverName() string {
	if name := o.TLS.ServerName(); name != "" {
		return name
	}
	return o.Server.AddrString()
}

func (o Options) authority(name string) string {
	if o.Host != "" {
		return o.Host
	}
	host := strings.TrimSuffix(strings.TrimPrefix(name, "["), "]")
	if o.Server.Port == 443 {
		if addr, err := netip.ParseAddr(host); err == nil && addr.Is6() {
			return "[" + host + "]"
		}
		return host
	}
	return net.JoinHostPort(host, strconv.Itoa(int(o.Server.Port)))
}

func establish(ctx context.Context, options Options, client tunnelClient, hconn httpConn, abort func(), name string) (*Conn, error) {
	stop := context.AfterFunc(ctx, abort)
	defer stop()

	path := options.Path
	if path == "" {
		path = DefaultPath
	}
	req, err := connectip.NewRequest(ctx, "https://"+options.authority(name)+path)
	if err != nil {
		return nil, err
	}
	header := req.Header()
	for key, values := range options.Headers {
		header[key] = values
	}
	if _, set := header["User-Agent"]; !set {
		header.Set("User-Agent", defaultUserAgent)
	}

	ipConn, _, err := client.Dial(req)
	if err != nil {
		if ctx.Err() != nil {
			err = context.Cause(ctx)
		}
		return nil, E.Cause(err, "CONNECT-IP request failed")
	}
	if n := ipConn.MaxPacketSize(); n < MinPacketSize {
		ipConn.Close()
		return nil, E.New("the tunnel can only carry ", n, "-byte packets, less than ", MinPacketSize)
	}
	if _, err = ipConn.RequestAddresses([]netip.Prefix{
		netip.PrefixFrom(netip.IPv4Unspecified(), 32),
		netip.PrefixFrom(netip.IPv6Unspecified(), 128),
	}); err != nil {
		ipConn.Close()
		return nil, err
	}
	var local []netip.Addr
	for len(local) == 0 {
		assigned, err := ipConn.ReceiveAddressAssignment(ctx)
		if err != nil {
			ipConn.Close()
			return nil, E.Cause(err, "no address assigned")
		}
		local = localAddrs(assigned)
	}
	if !stop() {
		ipConn.Close()
		return nil, E.Cause(context.Cause(ctx), "no address assigned")
	}
	conn := &Conn{ipConn: ipConn, httpConn: hconn, local: local, logger: options.Logger}
	go conn.serveAddressAssignments()
	go conn.serveAddressRequests()
	return conn, nil
}

func localAddrs(assigned []connectip.AssignedAddress) []netip.Addr {
	var local []netip.Addr
	var has4, has6 bool
	for _, a := range assigned {
		if a.Rejected() {
			continue
		}
		addr := a.IPPrefix.Addr()
		if a.IPPrefix.Bits() != addr.BitLen() {
			addr = a.IPPrefix.Masked().Addr().Next()
		}
		if addr.Is4() && !has4 {
			has4 = true
			local = append(local, addr)
		} else if addr.Is6() && !has6 {
			has6 = true
			local = append(local, addr)
		}
	}
	return local
}
