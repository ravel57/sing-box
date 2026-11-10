//go:build with_gvisor && with_wireguard

package wireguard_test

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/netip"
	"testing"
	"time"

	awgconn "github.com/amnezia-vpn/amneziawg-go/v3/conn"
	awgdevice "github.com/amnezia-vpn/amneziawg-go/v3/device"
	"github.com/amnezia-vpn/amneziawg-go/v3/tun/netstack"
	"github.com/sagernet/sing-box/option"
	"golang.org/x/crypto/curve25519"
)

func newKeyPair(t *testing.T) (private, public []byte) {
	t.Helper()
	private = make([]byte, 32)
	if _, err := rand.Read(private); err != nil {
		t.Fatal(err)
	}
	private[0] &= 248
	private[31] = (private[31] & 127) | 64
	public, err := curve25519.X25519(private, curve25519.Basepoint)
	if err != nil {
		t.Fatal(err)
	}
	return private, public
}

// startAmneziaServer runs a stock amneziawg-go device as the peer, with every AmneziaWG 3.1
// device parameter set, and an echo server inside its tunnel. The sing-box client is the
// only thing under test: it shares no obfuscation code with this server.
func startAmneziaServer(t *testing.T) awgFixture {
	t.Helper()
	serverPrivate, serverPublic := newKeyPair(t)
	clientPrivate, clientPublic := newKeyPair(t)
	tunDevice, tunNet, err := netstack.CreateNetTUN([]netip.Addr{netip.MustParseAddr("10.205.0.1")}, nil, 1420)
	if err != nil {
		t.Fatal(err)
	}
	probe, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := probe.LocalAddr().(*net.UDPAddr).Port
	probe.Close()
	device := awgdevice.NewDevice(tunDevice, awgconn.NewDefaultBind(), awgdevice.NewLogger(awgdevice.LogLevelSilent, ""))
	t.Cleanup(device.Close)
	headerKey := hex.EncodeToString(bytesOf(0x11, 32))
	config := fmt.Sprintf("private_key=%s\nlisten_port=%d\njc=4\njmin=40\njmax=70\ns1=20\ns2=30\ns3=20\ns4=20\n"+
		"h1=100000-200000\nh2=300000-400000\nh3=500000-600000\nh4=700000-800000\ni1=<r 64>\n"+
		"header_protection_key=%s\ncontent_padding_addition=8-16\nrandom_trailers=true\ndisable_cookies=false\n"+
		"public_key=%s\nallowed_ip=10.205.0.2/32\n",
		hex.EncodeToString(serverPrivate), port, headerKey, hex.EncodeToString(clientPublic))
	if err = device.IpcSet(config); err != nil {
		t.Fatal(err)
	}
	if err = device.Up(); err != nil {
		t.Fatal(err)
	}
	listener, err := tunNet.ListenTCP(&net.TCPAddr{Port: 8080})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				io.Copy(conn, conn)
			}()
		}
	}()
	fixture := awgFixture{
		Port:       uint16(port),
		ServerPub:  base64.StdEncoding.EncodeToString(serverPublic),
		ClientPriv: base64.StdEncoding.EncodeToString(clientPrivate),
		TCPEcho:    "10.205.0.1:8080",
	}
	o := &fixture.Obf
	o.Jc, o.Jmin, o.Jmax, o.S1, o.S2, o.S3, o.S4 = 4, 40, 70, 20, 30, 20, 20
	o.H1, o.H2, o.H3, o.H4 = "100000-200000", "300000-400000", "500000-600000", "700000-800000"
	o.I1 = "<r 64>"
	o.HeaderProtectionKey = base64.StdEncoding.EncodeToString(bytesOf(0x11, 32))
	o.ContentPaddingAddition = "8-16"
	o.RandomTrailers = true
	return fixture
}

func bytesOf(value byte, count int) []byte {
	b := make([]byte, count)
	for i := range b {
		b[i] = value
	}
	return b
}

func TestAmneziaWGHandshakeAndTraffic(t *testing.T) {
	dial := startAWGEndpoint(t, startAmneziaServer(t), nil)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := dial(ctx); err != nil {
		t.Fatal(err)
	}
}

// Each mismatched parameter must keep the tunnel down; with plain WireGuard (parameters
// ignored) the handshake would not care about any of them.
func TestAmneziaWGMismatchedParametersFail(t *testing.T) {
	for name, mutate := range map[string]func(*option.WireGuardEndpointOptions){
		"header values": func(o *option.WireGuardEndpointOptions) { o.H1, o.H2, o.H3, o.H4 = "11", "22", "33", "44" },
		"s1":            func(o *option.WireGuardEndpointOptions) { o.S1 = 25 },
		"header key":    func(o *option.WireGuardEndpointOptions) { o.HeaderProtectionKey = "" },
	} {
		t.Run(name, func(t *testing.T) {
			dial := startAWGEndpoint(t, startAmneziaServer(t), mutate)
			ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
			defer cancel()
			if err := dial(ctx); err == nil {
				t.Fatal("tunnel came up despite mismatched parameters")
			}
		})
	}
}
