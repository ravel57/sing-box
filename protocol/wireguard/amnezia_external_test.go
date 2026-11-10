//go:build with_gvisor && with_wireguard

package wireguard_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/netip"
	"os"
	"testing"
	"time"

	"github.com/sagernet/sing-box"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/json/badoption"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

// AWG_FIXTURE points at a JSON file describing a running AmneziaWG 3.1 server (the
// ultunnel-server-panel amneziawgnet device):
//
//	{"port":51820,"server_pub":"<b64>","client_priv":"<b64>","tcp_echo":"host:port",
//	 "obf":{"jc":..,"jmin":..,"jmax":..,"s1":..,"s2":..,"s3":..,"s4":..,"h1":"..","h2":"..","h3":"..","h4":"..",
//	        "i1":"..","headerProtectionKey":"..","contentPaddingAddition":"..", ...}}
//
// The client address is AWG_CLIENT_ADDRESS (default 10.205.0.2/32); the server is 127.0.0.1.
type awgFixture struct {
	Port       uint16 `json:"port"`
	ServerPub  string `json:"server_pub"`
	ClientPriv string `json:"client_priv"`
	TCPEcho    string `json:"tcp_echo"`
	Obf        struct {
		Jc                     int    `json:"jc"`
		Jmin                   int    `json:"jmin"`
		Jmax                   int    `json:"jmax"`
		S1                     int    `json:"s1"`
		S2                     int    `json:"s2"`
		S3                     int    `json:"s3"`
		S4                     int    `json:"s4"`
		H1                     string `json:"h1"`
		H2                     string `json:"h2"`
		H3                     string `json:"h3"`
		H4                     string `json:"h4"`
		I1                     string `json:"i1"`
		I2                     string `json:"i2"`
		I3                     string `json:"i3"`
		I4                     string `json:"i4"`
		I5                     string `json:"i5"`
		HeaderProtectionKey    string `json:"headerProtectionKey"`
		ContentPaddingAddition string `json:"contentPaddingAddition"`
		RekeyAfterTime         string `json:"rekeyAfterTime"`
		RekeyTimeout           string `json:"rekeyTimeout"`
		RejectAfterTime        string `json:"rejectAfterTime"`
		KeepaliveTimeout       string `json:"keepaliveTimeout"`
		MaxHandshakeAttempts   string `json:"maxHandshakeAttempts"`
		RandomTrailers         bool   `json:"randomTrailers"`
		DisableCookies         bool   `json:"disableCookies"`
	} `json:"obf"`
}

func loadFixture(t *testing.T) awgFixture {
	path := os.Getenv("AWG_FIXTURE")
	if path == "" {
		t.Skip("AWG_FIXTURE not set")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fixture awgFixture
	if err = json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func startAWGEndpoint(t *testing.T, fixture awgFixture, mutate func(*option.WireGuardEndpointOptions)) (dial func(ctx context.Context) error) {
	address := os.Getenv("AWG_CLIENT_ADDRESS")
	if address == "" {
		address = "10.205.0.2/32"
	}
	o := fixture.Obf
	options := &option.WireGuardEndpointOptions{
		Address:    badoption.Listable[netip.Prefix]{netip.MustParsePrefix(address)},
		PrivateKey: fixture.ClientPriv,
		Peers: []option.WireGuardPeer{{
			Address:    "127.0.0.1",
			Port:       fixture.Port,
			PublicKey:  fixture.ServerPub,
			AllowedIPs: badoption.Listable[netip.Prefix]{netip.MustParsePrefix("0.0.0.0/0")},
		}},
		WireGuardAmneziaOptions: option.WireGuardAmneziaOptions{
			Jc: o.Jc, Jmin: o.Jmin, Jmax: o.Jmax, S1: o.S1, S2: o.S2, S3: o.S3, S4: o.S4,
			H1: o.H1, H2: o.H2, H3: o.H3, H4: o.H4, I1: o.I1, I2: o.I2, I3: o.I3, I4: o.I4, I5: o.I5,
			HeaderProtectionKey: o.HeaderProtectionKey, ContentPaddingAddition: o.ContentPaddingAddition,
			RekeyAfterTime: o.RekeyAfterTime, RekeyTimeout: o.RekeyTimeout, RejectAfterTime: o.RejectAfterTime,
			KeepaliveTimeout: o.KeepaliveTimeout, MaxHandshakeAttempts: o.MaxHandshakeAttempts,
			RandomTrailers: o.RandomTrailers, DisableCookies: o.DisableCookies,
		},
	}
	if mutate != nil {
		mutate(options)
	}
	ctx, cancel := context.WithCancel(include.Context(context.Background()))
	instance, err := box.New(box.Options{Context: ctx, Options: option.Options{
		Log:       &option.LogOptions{Level: "error"},
		Endpoints: []option.Endpoint{{Type: C.TypeWireGuard, Tag: "awg", Options: options}},
	}})
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if err = instance.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		instance.Close()
		cancel()
	})
	endpoint, _ := instance.Endpoint().Get("awg")
	return func(ctx context.Context) error {
		conn, err := endpoint.DialContext(ctx, N.NetworkTCP, M.ParseSocksaddr(fixture.TCPEcho))
		if err != nil {
			return err
		}
		defer conn.Close()
		message := bytes.Repeat([]byte("awg-"), 50000)
		go conn.Write(message)
		got := make([]byte, len(message))
		if _, err = io.ReadFull(conn, got); err != nil {
			return err
		}
		if !bytes.Equal(got, message) {
			return io.ErrUnexpectedEOF
		}
		return nil
	}
}

func TestAmneziaWGExternalServer(t *testing.T) {
	dial := startAWGEndpoint(t, loadFixture(t), nil)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := dial(ctx); err != nil {
		t.Fatal(err)
	}
}
