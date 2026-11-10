package trusttunnel_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/netip"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/sagernet/sing-box"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing-box/option"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

// TRUSTTUNNEL_FIXTURE points at a JSON file describing a running TrustTunnel server, e.g. the
// ultunnel-server-panel tlsproxy server. Without it the tests below are skipped.
//
//	{"addr":"host:port","user":"..","password":"..","server_name":"..","cert":"<PEM, optional>",
//	 "tcp_echo":"host:port (optional)","udp_echo":"host:port (optional)"}
//
// TRUSTTUNNEL_QUIC=1 selects HTTP/3. Without echo endpoints the test uses example.com:80 over
// TCP and a DNS query to 1.1.1.1:53 over UDP.
type externalFixture struct {
	Addr       string `json:"addr"`
	User       string `json:"user"`
	Password   string `json:"password"`
	ServerName string `json:"server_name"`
	Cert       string `json:"cert"`
	TCPEcho    string `json:"tcp_echo"`
	UDPEcho    string `json:"udp_echo"`
}

func TestExternalServer(t *testing.T) {
	path := os.Getenv("TRUSTTUNNEL_FIXTURE")
	if path == "" {
		t.Skip("TRUSTTUNNEL_FIXTURE not set")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fixture externalFixture
	if err = json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	host, portText, err := net.SplitHostPort(fixture.Addr)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	serverName := fixture.ServerName
	if serverName == "" {
		serverName = "localhost"
	}
	tlsOptions := &option.OutboundTLSOptions{Enabled: true, ServerName: serverName}
	if fixture.Cert != "" {
		tlsOptions.Certificate = []string{fixture.Cert}
	}
	ctx, cancel := context.WithCancel(include.Context(context.Background()))
	defer cancel()
	instance, err := box.New(box.Options{Context: ctx, Options: option.Options{
		Log: &option.LogOptions{Level: "error"},
		Outbounds: []option.Outbound{{Type: C.TypeTrustTunnel, Tag: "tt", Options: &option.TrustTunnelOutboundOptions{
			ServerOptions:               option.ServerOptions{Server: host, ServerPort: uint16(port)},
			Username:                    fixture.User,
			Password:                    fixture.Password,
			QUIC:                        os.Getenv("TRUSTTUNNEL_QUIC") == "1",
			OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{TLS: tlsOptions},
		}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err = instance.Start(); err != nil {
		t.Fatal(err)
	}
	defer instance.Close()
	out, _ := instance.Outbound().Outbound("tt")
	dialCtx, dialCancel := context.WithTimeout(ctx, 20*time.Second)
	defer dialCancel()

	t.Run("tcp", func(t *testing.T) {
		destination := M.ParseSocksaddr("example.com:80")
		request := []byte("GET / HTTP/1.0\r\nHost: example.com\r\n\r\n")
		expect := "HTTP/1."
		if fixture.TCPEcho != "" {
			destination = M.ParseSocksaddr(fixture.TCPEcho)
			request = bytes.Repeat([]byte("panel-tcp-"), 30000)
			expect = string(request)
		}
		conn, err := out.DialContext(dialCtx, N.NetworkTCP, destination)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		go conn.Write(request)
		got := make([]byte, len(expect))
		if _, err = io.ReadFull(conn, got); err != nil {
			t.Fatal(err)
		}
		if string(got) != expect {
			t.Fatalf("unexpected TCP reply %q", got[:min(len(got), 64)])
		}
	})

	t.Run("udp", func(t *testing.T) {
		destination := netip.MustParseAddrPort("1.1.1.1:53")
		payload := []byte{0x12, 0x34, 0x01, 0, 0, 1, 0, 0, 0, 0, 0, 0, 7, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 3, 'c', 'o', 'm', 0, 0, 1, 0, 1}
		if fixture.UDPEcho != "" {
			destination = netip.MustParseAddrPort(fixture.UDPEcho)
			payload = []byte("panel-udp")
		}
		packetConn, err := out.ListenPacket(dialCtx, M.SocksaddrFromNetIP(destination))
		if err != nil {
			t.Fatal(err)
		}
		defer packetConn.Close()
		if _, err = packetConn.WriteTo(payload, net.UDPAddrFromAddrPort(destination)); err != nil {
			t.Fatal(err)
		}
		packetConn.SetReadDeadline(time.Now().Add(10 * time.Second))
		buffer := make([]byte, 1500)
		n, from, err := packetConn.ReadFrom(buffer)
		if err != nil {
			t.Fatal(err)
		}
		if from.String() != destination.String() {
			t.Fatalf("reply from %s, want %s", from, destination)
		}
		if fixture.UDPEcho != "" && !bytes.Equal(buffer[:n], payload) {
			t.Fatalf("echo mismatch %q", buffer[:n])
		}
		if fixture.UDPEcho == "" && (n < 2 || buffer[0] != 0x12 || buffer[1] != 0x34) {
			t.Fatalf("not a DNS reply to our query: %x", buffer[:n])
		}
	})
}
