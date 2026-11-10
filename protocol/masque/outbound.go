package masque

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/common/dialer"
	"github.com/sagernet/sing-box/common/iponly"
	"github.com/sagernet/sing-box/common/tls"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/transport/masque"
	"github.com/sagernet/sing-box/transport/wireguard"
	"github.com/sagernet/sing-tun"
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/bufio"
	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/sagernet/sing/service"
)

var errGVisorRequired = E.New("MASQUE needs gVisor, rebuild with -tags with_gvisor")

const (
	establishTimeout = 10 * time.Second
	retryInterval    = time.Second
)

func RegisterOutbound(registry *outbound.Registry) {
	outbound.Register[option.MASQUEOutboundOptions](registry, C.TypeMASQUE, NewOutbound)
}

// Outbound is a CONNECT-IP (RFC 9484) VPN client: the server assigns the tunnel addresses and
// all TCP/UDP flows are carried as IP packets through a userspace network stack.
type Outbound struct {
	outbound.Adapter
	ctx          context.Context
	logger       log.ContextLogger
	dnsRouter    adapter.DNSRouter
	queryOptions adapter.DNSQueryOptions
	dialOptions  masque.Options

	access    sync.Mutex
	current   *tunnel
	lastErr   error
	lastErrAt time.Time
	closed    bool
}

func NewOutbound(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options option.MASQUEOutboundOptions) (adapter.Outbound, error) {
	if !tun.WithGVisor {
		return nil, errGVisorRequired
	}
	if options.TLS == nil || !options.TLS.Enabled {
		return nil, C.ErrTLSRequired
	}
	tlsConfig, err := tls.NewClient(ctx, logger, options.Server, common.PtrValueOrDefault(options.TLS))
	if err != nil {
		return nil, err
	}
	outboundDialer, err := dialer.NewWithOptions(dialer.Options{
		Context:        ctx,
		Options:        options.DialerOptions,
		RemoteIsDomain: options.ServerIsDomain(),
	})
	if err != nil {
		return nil, err
	}
	headers := options.Headers.Build()
	if options.Username != "" || options.Password != "" {
		headers.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(options.Username+":"+options.Password)))
	}
	serverAddr := options.ServerOptions.Build()
	if serverAddr.Port == 0 {
		serverAddr.Port = 443
	}
	h := &Outbound{
		Adapter:   outbound.NewAdapterWithDialerOptions(C.TypeMASQUE, tag, []string{N.NetworkTCP, N.NetworkUDP}, options.DialerOptions),
		ctx:       ctx,
		logger:    logger,
		dnsRouter: service.FromContext[adapter.DNSRouter](ctx),
		dialOptions: masque.Options{
			Logger:    logger,
			Dialer:    outboundDialer,
			Server:    serverAddr,
			TLS:       tlsConfig,
			HTTP2:     options.HTTP2,
			Host:      options.Host,
			Path:      options.Path,
			Headers:   headers,
			KeepAlive: options.KeepAlive.Build(),
		},
	}
	if resolver, isResolver := outboundDialer.(dialer.ResolveDialer); isResolver {
		h.queryOptions = resolver.QueryOptions()
	}
	return h, nil
}

func (h *Outbound) Start(stage adapter.StartStage, scope *adapter.Scope) error {
	if stage == adapter.StartStateInitialize {
		scope.Add(h.close)
	}
	return nil
}

func (h *Outbound) close() error {
	h.access.Lock()
	h.closed = true
	current := h.current
	h.current = nil
	h.access.Unlock()
	if current != nil {
		current.close()
	}
	return nil
}

func (h *Outbound) getTunnel(ctx context.Context) (*tunnel, error) {
	h.access.Lock()
	defer h.access.Unlock()
	if h.closed {
		return nil, net.ErrClosed
	}
	if h.current != nil && !h.current.isClosed() {
		return h.current, nil
	}
	if h.lastErr != nil && time.Since(h.lastErrAt) < retryInterval {
		return nil, h.lastErr
	}
	establishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), establishTimeout)
	defer cancel()
	stop := context.AfterFunc(h.ctx, cancel)
	defer stop()
	current, err := newTunnel(establishCtx, h.logger, h.dialOptions)
	if err != nil {
		h.lastErr, h.lastErrAt = err, time.Now()
		return nil, err
	}
	h.lastErr = nil
	h.current = current
	h.logger.Info("MASQUE tunnel established, local addresses ", strings.Join(common.Map(current.conn.LocalAddrs(), netip.Addr.String), ", "))
	return current, nil
}

func (h *Outbound) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	ctx, metadata := adapter.ExtendContext(ctx)
	metadata.Outbound = h.Tag()
	metadata.Destination = destination
	switch N.NetworkName(network) {
	case N.NetworkTCP:
		h.logger.InfoContext(ctx, "outbound connection to ", destination)
	case N.NetworkUDP:
		h.logger.InfoContext(ctx, "outbound packet connection to ", destination)
	default:
		return nil, E.Extend(N.ErrUnknownNetwork, network)
	}
	current, err := h.getTunnel(ctx)
	if err != nil {
		return nil, err
	}
	if destination.IsDomain() {
		addresses, err := h.lookup(ctx, destination.Fqdn)
		if err != nil {
			return nil, err
		}
		return N.DialSerial(ctx, current.device, network, destination, addresses)
	} else if !destination.Addr.IsValid() {
		return nil, E.New("invalid destination: ", destination)
	}
	return current.device.DialContext(ctx, network, destination)
}

func (h *Outbound) ListenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	ctx, metadata := adapter.ExtendContext(ctx)
	metadata.Outbound = h.Tag()
	metadata.Destination = destination
	h.logger.InfoContext(ctx, "outbound packet connection to ", destination)
	current, err := h.getTunnel(ctx)
	if err != nil {
		return nil, err
	}
	if destination.IsDomain() {
		addresses, err := h.lookup(ctx, destination.Fqdn)
		if err != nil {
			return nil, err
		}
		packetConn, address, err := N.ListenSerial(ctx, current.device, destination, addresses)
		if err != nil {
			return nil, err
		}
		return bufio.NewNATPacketConn(bufio.NewPacketConn(iponly.NewPacketConn(h.logger, packetConn)), M.SocksaddrFrom(address, destination.Port), destination), nil
	}
	packetConn, err := current.device.ListenPacket(ctx, destination)
	if err != nil {
		return nil, err
	}
	return iponly.NewPacketConn(h.logger, packetConn), nil
}

func (h *Outbound) lookup(ctx context.Context, domain string) ([]netip.Addr, error) {
	if h.dnsRouter == nil {
		return nil, E.New("DNS router not available")
	}
	return h.dnsRouter.Lookup(ctx, domain, h.queryOptions)
}

type tunnel struct {
	conn      *masque.Conn
	device    wireguard.Device
	done      chan struct{}
	closeOnce sync.Once
}

func newTunnel(ctx context.Context, logger log.ContextLogger, options masque.Options) (*tunnel, error) {
	conn, err := masque.Dial(ctx, options)
	if err != nil {
		return nil, err
	}
	prefixes := common.Map(conn.LocalAddrs(), func(addr netip.Addr) netip.Prefix {
		return netip.PrefixFrom(addr, addr.BitLen())
	})
	device, err := wireguard.NewDevice(wireguard.DeviceOptions{
		Context: ctx,
		Logger:  logger,
		MTU:     masque.MinPacketSize,
		Address: prefixes,
	})
	if err != nil {
		conn.Close()
		return nil, err
	}
	if err = device.Start(); err != nil {
		conn.Close()
		device.Close()
		return nil, err
	}
	t := &tunnel{conn: conn, device: device, done: make(chan struct{})}
	go t.readFromTunnel()
	go t.writeToTunnel()
	return t, nil
}

func (t *tunnel) isClosed() bool {
	select {
	case <-t.done:
		return true
	default:
		return false
	}
}

func (t *tunnel) close() {
	t.closeOnce.Do(func() {
		close(t.done)
		t.conn.Close()
		t.device.Close()
	})
}

func (t *tunnel) readFromTunnel() {
	defer t.close()
	buffer := make([]byte, 1<<16)
	for {
		n, err := t.conn.Read(buffer)
		if err != nil {
			if errors.Is(err, io.ErrShortBuffer) {
				continue
			}
			return
		}
		t.device.Write([][]byte{buffer[:n]}, 0)
	}
}

func (t *tunnel) writeToTunnel() {
	bufs := [][]byte{make([]byte, masque.MinPacketSize)}
	sizes := []int{0}
	for {
		if _, err := t.device.Read(bufs, sizes, 0); err != nil {
			return
		}
		if _, err := t.conn.Write(bufs[0][:sizes[0]]); err != nil {
			var tooBig *masque.PacketTooBigError
			if errors.As(err, &tooBig) {
				go t.device.Write([][]byte{tooBig.ICMP}, 0)
			}
		}
	}
}
