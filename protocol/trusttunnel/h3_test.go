//go:build with_quic

package trusttunnel_test

import (
	"crypto/tls"
	"net"
	"testing"

	"github.com/sagernet/quic-go/http3"
)

func TestOutboundHTTP3(t *testing.T) {
	certificate, certPEM := newCertificate(t)
	server := &testServer{cert: certPEM}
	packetConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server.addr = packetConn.LocalAddr().(*net.UDPAddr).AddrPort()
	h3Server := &http3.Server{
		Handler:   server.handler(t),
		TLSConfig: http3.ConfigureTLSConfig(&tls.Config{Certificates: []tls.Certificate{certificate}}),
	}
	go h3Server.Serve(packetConn)
	t.Cleanup(func() {
		h3Server.Close()
		packetConn.Close()
	})
	testTCPAndUDP(t, server, true)
}
