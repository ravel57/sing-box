//go:build with_quic && with_gvisor

package masque_test

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sagernet/sing-box"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing-box/option"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

// MASQUE_SERVER ("host:port"), MASQUE_USER, MASQUE_PASSWORD and MASQUE_SERVER_NAME describe a
// running CONNECT-IP server (the ultunnel-server-panel / xray-core MASQUE inbound), MASQUE_CERT
// is an optional PEM file to trust and MASQUE_HTTP2=1 selects HTTP/2. MASQUE_TCP_ECHO and
// MASQUE_UDP_ECHO ("host:port") must be reachable from the server. Skipped when unset.
func TestExternalServer(t *testing.T) {
	server := os.Getenv("MASQUE_SERVER")
	if server == "" {
		t.Skip("MASQUE_SERVER not set")
	}
	host, portText, err := net.SplitHostPort(server)
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(portText)
	tlsOptions := &option.OutboundTLSOptions{Enabled: true, ServerName: os.Getenv("MASQUE_SERVER_NAME")}
	if certPath := os.Getenv("MASQUE_CERT"); certPath != "" {
		pem, err := os.ReadFile(certPath)
		if err != nil {
			t.Fatal(err)
		}
		tlsOptions.Certificate = []string{string(pem)}
	}
	ctx, cancel := context.WithCancel(include.Context(context.Background()))
	defer cancel()
	instance, err := box.New(box.Options{Context: ctx, Options: option.Options{
		Log: &option.LogOptions{Level: "debug"},
		Outbounds: []option.Outbound{{Type: C.TypeMASQUE, Tag: "masque", Options: &option.MASQUEOutboundOptions{
			ServerOptions:               option.ServerOptions{Server: host, ServerPort: uint16(port)},
			Username:                    os.Getenv("MASQUE_USER"),
			Password:                    os.Getenv("MASQUE_PASSWORD"),
			HTTP2:                       os.Getenv("MASQUE_HTTP2") == "1",
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
	out, _ := instance.Outbound().Outbound("masque")
	dialCtx, dialCancel := context.WithTimeout(ctx, 20*time.Second)
	defer dialCancel()

	t.Run("tcp", func(t *testing.T) {
		conn, err := out.DialContext(dialCtx, N.NetworkTCP, M.ParseSocksaddr(os.Getenv("MASQUE_TCP_ECHO")))
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		message := bytes.Repeat([]byte("masque-tcp-"), 40000)
		go conn.Write(message)
		got := make([]byte, len(message))
		if _, err = io.ReadFull(conn, got); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, message) {
			t.Fatal("echoed TCP payload differs")
		}
	})

	t.Run("udp", func(t *testing.T) {
		destination := netip.MustParseAddrPort(os.Getenv("MASQUE_UDP_ECHO"))
		packetConn, err := out.ListenPacket(dialCtx, M.SocksaddrFromNetIP(destination))
		if err != nil {
			t.Fatal(err)
		}
		defer packetConn.Close()
		payload := []byte(strings.Repeat("u", 900))
		if _, err = packetConn.WriteTo(payload, net.UDPAddrFromAddrPort(destination)); err != nil {
			t.Fatal(err)
		}
		packetConn.SetReadDeadline(time.Now().Add(10 * time.Second))
		buffer := make([]byte, 2000)
		n, _, err := packetConn.ReadFrom(buffer)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(buffer[:n], payload) {
			t.Fatalf("echo mismatch (%d bytes)", n)
		}
	})
}
