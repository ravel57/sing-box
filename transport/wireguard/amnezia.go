package wireguard

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"

	awgconn "github.com/amnezia-vpn/amneziawg-go/v3/conn"
	awgdevice "github.com/amnezia-vpn/amneziawg-go/v3/device"
	awgtun "github.com/amnezia-vpn/amneziawg-go/v3/tun"
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/bufio"
	E "github.com/sagernet/sing/common/exceptions"
	"github.com/sagernet/sing/common/logger"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/sagernet/sing/service"
	"github.com/sagernet/sing/service/pause"
)

// AmneziaOptions are the AmneziaWG 3.1 settings; they must match the server's on every field.
type AmneziaOptions struct {
	Jc                     int
	Jmin                   int
	Jmax                   int
	S1, S2, S3, S4         int
	H1, H2, H3, H4         string
	I1, I2, I3, I4, I5     string
	HeaderProtectionKey    string
	ContentPaddingAddition string
	RekeyAfterTime         string
	RekeyTimeout           string
	RejectAfterTime        string
	KeepaliveTimeout       string
	MaxHandshakeAttempts   string
	RandomTrailers         bool
	DisableCookies         bool
}

const (
	maxJunkPackets    = 128
	maxPreamblePacket = 65535
)

func (o *AmneziaOptions) validate() error {
	if o.Jc < 0 || o.Jc > maxJunkPackets {
		return E.New("invalid jc ", o.Jc, ", must be 0..", maxJunkPackets)
	}
	if o.Jmin < 0 || o.Jmax < 0 || o.Jmin > maxPreamblePacket || o.Jmax > maxPreamblePacket {
		return E.New("invalid jmin/jmax, must be 0..", maxPreamblePacket)
	}
	if o.Jmin > o.Jmax {
		return E.New("invalid jmin/jmax: ", o.Jmin, " must not exceed ", o.Jmax)
	}
	for name, value := range map[string]int{"s1": o.S1, "s2": o.S2, "s3": o.S3} {
		if value < 0 || value > 65535 {
			return E.New("invalid ", name, " ", value, ", must be 0..65535")
		}
	}
	if o.S4 < 0 || o.S4 > 32 {
		return E.New("invalid s4 ", o.S4, ", must be 0..32")
	}
	if o.S1+56 == o.S2 {
		return E.New("invalid s1/s2: s1+56 must not equal s2")
	}
	return nil
}

// ipcLines renders the device-level UAPI lines. An omitted header_protection_key line would
// mean "unchanged", so a cleared key is sent as the all-zero key that disables the feature.
func (o *AmneziaOptions) ipcLines() (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "\njc=%d\njmin=%d\njmax=%d\ns1=%d\ns2=%d\ns3=%d\ns4=%d", o.Jc, o.Jmin, o.Jmax, o.S1, o.S2, o.S3, o.S4)
	for _, line := range [][2]string{
		{"h1", o.H1}, {"h2", o.H2}, {"h3", o.H3}, {"h4", o.H4},
		{"i1", o.I1}, {"i2", o.I2}, {"i3", o.I3}, {"i4", o.I4}, {"i5", o.I5},
		{"content_padding_addition", o.ContentPaddingAddition},
		{"rekey_after_time", o.RekeyAfterTime}, {"rekey_timeout", o.RekeyTimeout},
		{"reject_after_time", o.RejectAfterTime}, {"keepalive_timeout", o.KeepaliveTimeout},
		{"max_handshake_attempts", o.MaxHandshakeAttempts},
	} {
		if line[1] != "" {
			b.WriteString("\n" + line[0] + "=" + line[1])
		}
	}
	protectionKey := strings.Repeat("0", 64)
	if o.HeaderProtectionKey != "" {
		raw, err := base64.StdEncoding.DecodeString(o.HeaderProtectionKey)
		if err != nil || len(raw) != 32 {
			return "", E.New("invalid header_protection_key, expected a base64 32-byte key")
		}
		protectionKey = hex.EncodeToString(raw)
	}
	fmt.Fprintf(&b, "\nheader_protection_key=%s\nrandom_trailers=%t\ndisable_cookies=%t", protectionKey, o.RandomTrailers, o.DisableCookies)
	return b.String(), nil
}

func (e *Endpoint) startAmnezia(postStart bool) error {
	hasDomainPeer := common.Any(e.peers, func(peer peerConfig) bool {
		return peer.destination.IsDomain()
	})
	if postStart != hasDomainPeer {
		return nil
	}
	peers := make([]peerConfig, len(e.peers))
	copy(peers, e.peers)
	for index := range peers {
		if !peers[index].destination.IsDomain() {
			continue
		}
		addresses, err := e.options.ResolvePeer(peers[index].destination.Fqdn)
		if err != nil {
			return E.Cause(err, "resolve peer ", peers[index].destination.Fqdn)
		}
		if len(addresses) == 0 {
			return E.New("no address found for peer ", peers[index].destination.Fqdn)
		}
		peers[index].endpoint = netip.AddrPortFrom(addresses[0], peers[index].destination.Port)
	}
	isConnect := len(peers) == 1 && peers[0].endpoint.IsValid()
	var connectAddr netip.AddrPort
	if isConnect {
		connectAddr = peers[0].endpoint
	}
	bind := newAmneziaBind(e.options.Context, e.options.Logger, e.options.Dialer, isConnect, connectAddr)
	if err := e.tunDevice.Start(); err != nil {
		return err
	}
	logger := &awgdevice.Logger{
		Verbosef: func(format string, args ...any) {
			e.options.Logger.Debug(fmt.Sprintf(strings.ToLower(format), args...))
		},
		Errorf: func(format string, args ...any) {
			e.options.Logger.Error(fmt.Sprintf(strings.ToLower(format), args...))
		},
	}
	awgDevice := awgdevice.NewDevice(newAmneziaTun(e.tunDevice), bind, logger)
	obfuscation, err := e.options.Amnezia.ipcLines()
	if err != nil {
		awgDevice.Close()
		return err
	}
	var ipcConf strings.Builder
	ipcConf.WriteString(e.ipcConf)
	ipcConf.WriteString(obfuscation)
	for _, peer := range peers {
		ipcConf.WriteString(peer.GenerateIpcLines())
	}
	if err = awgDevice.IpcSet(ipcConf.String()); err != nil {
		awgDevice.Close()
		return E.Cause(err, "setup AmneziaWG")
	}
	if err = awgDevice.Up(); err != nil {
		awgDevice.Close()
		return E.Cause(err, "start AmneziaWG")
	}
	e.awgDevice = awgDevice
	e.pause = service.FromContext[pause.Manager](e.options.Context)
	if e.pause != nil {
		e.pauseCallback = e.pause.RegisterCallback(e.onPauseUpdated)
	}
	return nil
}

// amneziaTun adapts the shared packet device to amneziawg-go's own tun.Device types.
type amneziaTun struct {
	Device
	events chan awgtun.Event
	once   sync.Once
}

func newAmneziaTun(device Device) *amneziaTun {
	return &amneziaTun{Device: device, events: make(chan awgtun.Event, 1)}
}

func (t *amneziaTun) Events() <-chan awgtun.Event {
	t.once.Do(func() {
		go func() {
			defer close(t.events)
			for event := range t.Device.Events() {
				t.events <- awgtun.Event(event)
			}
		}()
	})
	return t.events
}

type amneziaBind struct {
	ctx         context.Context
	logger      logger.Logger
	pause       pause.Manager
	dialer      N.Dialer
	isConnect   bool
	connectAddr netip.AddrPort

	bindCtx  context.Context
	bindDone context.CancelFunc
	access   sync.Mutex
	conn     *wireConn
	done     chan struct{}
}

var _ awgconn.Bind = (*amneziaBind)(nil)

func newAmneziaBind(ctx context.Context, logger logger.Logger, dialer N.Dialer, isConnect bool, connectAddr netip.AddrPort) *amneziaBind {
	return &amneziaBind{
		ctx:         ctx,
		logger:      logger,
		pause:       service.FromContext[pause.Manager](ctx),
		dialer:      dialer,
		isConnect:   isConnect,
		connectAddr: connectAddr,
		done:        make(chan struct{}),
	}
}

func (b *amneziaBind) connect() (*wireConn, error) {
	b.access.Lock()
	defer b.access.Unlock()
	select {
	case <-b.done:
		return nil, net.ErrClosed
	default:
	}
	if b.conn != nil {
		select {
		case <-b.conn.done:
		default:
			return b.conn, nil
		}
	}
	if b.isConnect {
		udpConn, err := b.dialer.DialContext(b.bindCtx, N.NetworkUDP, M.SocksaddrFromNetIP(b.connectAddr))
		if err != nil {
			return nil, err
		}
		b.conn = &wireConn{PacketConn: bufio.NewUnbindPacketConn(udpConn), done: make(chan struct{})}
	} else {
		udpConn, err := b.dialer.ListenPacket(b.bindCtx, M.Socksaddr{Addr: netip.IPv4Unspecified()})
		if err != nil {
			return nil, err
		}
		b.conn = &wireConn{PacketConn: bufio.NewPacketConn(udpConn), done: make(chan struct{})}
	}
	return b.conn, nil
}

func (b *amneziaBind) Open(port uint16) ([]awgconn.ReceiveFunc, uint16, error) {
	b.access.Lock()
	select {
	case <-b.done:
		b.done = make(chan struct{})
	default:
	}
	b.bindCtx, b.bindDone = context.WithCancel(b.ctx)
	b.access.Unlock()
	return []awgconn.ReceiveFunc{b.receive}, 0, nil
}

func (b *amneziaBind) receive(packets [][]byte, sizes []int, eps []awgconn.Endpoint) (int, error) {
	udpConn, err := b.connect()
	if err != nil {
		select {
		case <-b.done:
			return 0, net.ErrClosed
		default:
		}
		b.logger.Error(E.Cause(err, "connect to server"))
		if b.pause != nil {
			b.pause.WaitActiveContext(b.bindCtx)
		}
		select {
		case <-b.done:
			return 0, net.ErrClosed
		case <-time.After(time.Second):
		}
		return 0, nil
	}
	n, addr, err := udpConn.ReadFrom(packets[0])
	if err != nil {
		udpConn.Close()
		select {
		case <-b.done:
			return 0, net.ErrClosed
		default:
		}
		b.logger.Error(E.Cause(err, "read packet"))
		return 0, nil
	}
	sizes[0] = n
	eps[0] = remoteEndpoint(M.SocksaddrFromNet(addr).Unwrap().AddrPort())
	return 1, nil
}

func (b *amneziaBind) Close() error {
	b.access.Lock()
	defer b.access.Unlock()
	select {
	case <-b.done:
	default:
		close(b.done)
	}
	if b.bindDone != nil {
		b.bindDone()
	}
	common.Close(common.PtrOrNil(b.conn))
	return nil
}

func (b *amneziaBind) SetMark(uint32) error { return nil }

func (b *amneziaBind) Send(bufs [][]byte, ep awgconn.Endpoint) error {
	udpConn, err := b.connect()
	if err != nil {
		time.Sleep(time.Second)
		return err
	}
	destination := netip.AddrPort(ep.(remoteEndpoint))
	for _, buffer := range bufs {
		if _, err = udpConn.WriteToUDPAddrPort(buffer, destination); err != nil {
			udpConn.Close()
			return err
		}
	}
	return nil
}

func (b *amneziaBind) ParseEndpoint(s string) (awgconn.Endpoint, error) {
	addrPort, err := netip.ParseAddrPort(s)
	if err != nil {
		return nil, err
	}
	return remoteEndpoint(addrPort), nil
}

func (b *amneziaBind) BatchSize() int { return 1 }
