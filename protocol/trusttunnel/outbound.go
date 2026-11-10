package trusttunnel

import (
	"context"
	"net"
	"net/netip"
	"os"
	"sync/atomic"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/common/dialer"
	"github.com/sagernet/sing-box/common/tls"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/bufio"
	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/sagernet/sing/service"
)

// A session idle for this long is probed with `_check` before the next tunnel is opened.
var checkIdleAfter = 7 * time.Second

func RegisterOutbound(registry *outbound.Registry) {
	outbound.Register[option.TrustTunnelOutboundOptions](registry, C.TypeTrustTunnel, NewOutbound)
}

var _ adapter.OutboundWithMultiplex = (*Outbound)(nil)

type Outbound struct {
	outbound.Adapter
	ctx          context.Context
	logger       log.ContextLogger
	opener       streamOpener
	dnsRouter    adapter.DNSRouter
	queryOptions adapter.DNSQueryOptions
	udp          *udpMux
	activeConns  atomic.Int32
	lastActive   atomic.Int64
}

func NewOutbound(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options option.TrustTunnelOutboundOptions) (adapter.Outbound, error) {
	if options.TLS == nil || !options.TLS.Enabled {
		return nil, C.ErrTLSRequired
	}
	if options.Username == "" || options.Password == "" {
		return nil, E.New("missing username or password")
	}
	tlsConfig, err := tls.NewClient(ctx, logger, options.Server, common.PtrValueOrDefault(options.TLS))
	if err != nil {
		return nil, err
	}
	outboundDialer, err := dialer.NewWithOptions(dialer.Options{
		Context:        ctx,
		Options:        options.DialerOptions,
		RemoteIsDomain: true,
	})
	if err != nil {
		return nil, err
	}
	serverAddr := options.ServerOptions.Build()
	h := &Outbound{
		Adapter:   outbound.NewAdapterWithDialerOptions(C.TypeTrustTunnel, tag, []string{N.NetworkTCP, N.NetworkUDP}, options.DialerOptions),
		ctx:       ctx,
		logger:    logger,
		dnsRouter: service.FromContext[adapter.DNSRouter](ctx),
	}
	if resolver, isResolver := outboundDialer.(dialer.ResolveDialer); isResolver {
		h.queryOptions = resolver.QueryOptions()
	}
	if options.QUIC {
		h.opener, err = newH3Opener(outboundDialer, serverAddr, tlsConfig, options.Username, options.Password)
		if err != nil {
			return nil, err
		}
	} else {
		h.opener = newH2Opener(outboundDialer, serverAddr, tlsConfig, options.Username, options.Password)
	}
	h.udp = &udpMux{
		open: func(ctx context.Context) (net.Conn, error) {
			return h.openStream(ctx, udpAuthority)
		},
		resolve: h.lookup,
	}
	return h, nil
}

func (h *Outbound) Start(stage adapter.StartStage, scope *adapter.Scope) error {
	if stage == adapter.StartStateInitialize {
		scope.Add(h.opener.Close)
	}
	return nil
}

func (h *Outbound) MultiplexEnabled() bool {
	return true
}

func (h *Outbound) lookup(ctx context.Context, domain string) ([]netip.Addr, error) {
	if h.dnsRouter == nil {
		return nil, E.New("DNS router not available")
	}
	return h.dnsRouter.Lookup(ctx, domain, h.queryOptions)
}

func (h *Outbound) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	ctx, metadata := adapter.ExtendContext(ctx)
	metadata.Outbound = h.Tag()
	metadata.Destination = destination
	switch N.NetworkName(network) {
	case N.NetworkTCP:
		h.logger.InfoContext(ctx, "outbound connection to ", destination)
		return h.openStream(ctx, destination.String())
	case N.NetworkUDP:
		packetConn, err := h.ListenPacket(ctx, destination)
		if err != nil {
			return nil, err
		}
		return bufio.NewBindPacketConn(packetConn, destination), nil
	}
	return nil, os.ErrInvalid
}

func (h *Outbound) ListenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	ctx, metadata := adapter.ExtendContext(ctx)
	metadata.Outbound = h.Tag()
	metadata.Destination = destination
	h.logger.InfoContext(ctx, "outbound packet connection to ", destination)
	if err := h.checkIfIdle(ctx); err != nil {
		return nil, err
	}
	conn, err := h.udp.listen(ctx)
	if err != nil {
		return nil, err
	}
	h.touch()
	return conn, nil
}

func (h *Outbound) touch() {
	h.lastActive.Store(time.Now().UnixNano())
}

func (h *Outbound) openStream(ctx context.Context, authority string) (net.Conn, error) {
	if authority != udpAuthority {
		err := h.checkIfIdle(ctx)
		if err != nil {
			return nil, err
		}
	}
	conn, err := h.opener.OpenStream(ctx, authority)
	if err != nil {
		return nil, err
	}
	h.touch()
	if authority == udpAuthority {
		return conn, nil
	}
	h.activeConns.Add(1)
	return &trackedConn{Conn: conn, outbound: h}, nil
}

// checkIfIdle probes a session nothing has used recently. A 407 is fatal; any other failure
// drops idle sessions so the real request dials a fresh one.
func (h *Outbound) checkIfIdle(ctx context.Context) error {
	last := h.lastActive.Load()
	if last == 0 || h.activeConns.Load() > 0 || time.Since(time.Unix(0, last)) < checkIdleAfter {
		return nil
	}
	checkCtx, cancel := context.WithTimeout(ctx, 7*time.Second)
	defer cancel()
	conn, err := h.opener.OpenStream(checkCtx, checkAuthority)
	if err == nil {
		conn.Close()
		h.touch()
		return nil
	}
	if status, isStatus := err.(*StatusError); isStatus && status.Status == 407 {
		return err
	}
	h.logger.DebugContext(ctx, "health check failed, resetting session: ", err)
	h.opener.CloseIdleSessions()
	return nil
}

type trackedConn struct {
	net.Conn
	outbound *Outbound
	closed   atomic.Bool
}

func (c *trackedConn) Close() error {
	if c.closed.CompareAndSwap(false, true) {
		c.outbound.activeConns.Add(-1)
		c.outbound.touch()
	}
	return c.Conn.Close()
}

func (c *trackedConn) CloseWrite() error {
	if closer, isCloser := c.Conn.(interface{ CloseWrite() error }); isCloser {
		return closer.CloseWrite()
	}
	return os.ErrInvalid
}

func (c *trackedConn) NeedAdditionalReadDeadline() bool {
	needs, isNeeds := c.Conn.(interface{ NeedAdditionalReadDeadline() bool })
	return isNeeds && needs.NeedAdditionalReadDeadline()
}

func (c *trackedConn) Upstream() any {
	return c.Conn
}
