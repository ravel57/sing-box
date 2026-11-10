package trusttunnel_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/netip"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/protocol/trusttunnel"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

const (
	testUser     = "alice"
	testPassword = "s3cret"
)

type testServer struct {
	addr   netip.AddrPort
	cert   string
	checks atomic.Int32
	udp    atomic.Int32
}

func newCertificate(t *testing.T) (tls.Certificate, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "tt.example.org"},
		DNSNames:     []string{"tt.example.org"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

// handler follows PROTOCOL.md: Basic auth on every CONNECT, `_check`, `_udp2`, plain TCP tunnels.
func (s *testServer) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := "Basic " + base64.StdEncoding.EncodeToString([]byte(testUser+":"+testPassword))
		if r.Method != http.MethodConnect || r.Header.Get("Proxy-Authorization") != want {
			w.WriteHeader(http.StatusProxyAuthRequired)
			return
		}
		switch r.Host {
		case "_check":
			s.checks.Add(1)
			w.WriteHeader(http.StatusOK)
		case "_udp2":
			s.udp.Add(1)
			s.serveUDP(w, r)
		default:
			upstream, err := net.Dial("tcp", r.Host)
			if err != nil {
				w.WriteHeader(http.StatusBadGateway)
				return
			}
			defer upstream.Close()
			w.WriteHeader(http.StatusOK)
			controller := http.NewResponseController(w)
			controller.Flush()
			go func() {
				io.Copy(upstream, r.Body)
				upstream.(*net.TCPConn).CloseWrite()
			}()
			buffer := make([]byte, 4096)
			for {
				n, err := upstream.Read(buffer)
				if n > 0 {
					w.Write(buffer[:n])
					controller.Flush()
				}
				if err != nil {
					return
				}
			}
		}
	})
}

// serveUDP relays each frame to its destination over a real UDP socket and frames the reply.
func (s *testServer) serveUDP(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	controller := http.NewResponseController(w)
	controller.Flush()
	var writeLock sync.Mutex
	var relays sync.WaitGroup
	defer relays.Wait()
	for {
		var prefix [4]byte
		if _, err := io.ReadFull(r.Body, prefix[:]); err != nil {
			return
		}
		body := make([]byte, binary.BigEndian.Uint32(prefix[:]))
		if _, err := io.ReadFull(r.Body, body); err != nil {
			return
		}
		sourcePort := binary.BigEndian.Uint16(body[16:18])
		destination := net.UDPAddrFromAddrPort(netip.AddrPortFrom(netip.AddrFrom4([4]byte(body[30:34])), binary.BigEndian.Uint16(body[34:36])))
		payload := body[37+int(body[36]):]
		relays.Add(1)
		go func(sourceRaw []byte) {
			defer relays.Done()
			conn, err := net.DialUDP("udp", nil, destination)
			if err != nil {
				return
			}
			defer conn.Close()
			conn.Write(payload)
			conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			reply := make([]byte, 65535)
			n, err := conn.Read(reply)
			if err != nil {
				return
			}
			frame := make([]byte, 40+n)
			binary.BigEndian.PutUint32(frame, uint32(36+n))
			copy(frame[4:20], append(make([]byte, 12), destination.IP.To4()...))
			binary.BigEndian.PutUint16(frame[20:22], uint16(destination.Port))
			copy(frame[22:38], sourceRaw)
			binary.BigEndian.PutUint16(frame[38:40], sourcePort)
			copy(frame[40:], reply[:n])
			writeLock.Lock()
			w.Write(frame)
			controller.Flush()
			writeLock.Unlock()
		}(append([]byte(nil), body[0:16]...))
	}
}

func startTCPEcho(t *testing.T) netip.AddrPort {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
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
	return listener.Addr().(*net.TCPAddr).AddrPort()
}

func startUDPEcho(t *testing.T) netip.AddrPort {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	go func() {
		buffer := make([]byte, 65535)
		for {
			n, addr, err := conn.ReadFrom(buffer)
			if err != nil {
				return
			}
			conn.WriteTo(buffer[:n], addr)
		}
	}()
	return conn.LocalAddr().(*net.UDPAddr).AddrPort()
}

func startBox(t *testing.T, server *testServer, quic bool, password string) adapter.Outbound {
	t.Helper()
	ctx, cancel := context.WithCancel(include.Context(context.Background()))
	instance, err := box.New(box.Options{
		Context: ctx,
		Options: option.Options{
			Log: &option.LogOptions{Level: "error"},
			Outbounds: []option.Outbound{{
				Type: C.TypeTrustTunnel,
				Tag:  "tt",
				Options: &option.TrustTunnelOutboundOptions{
					ServerOptions: option.ServerOptions{Server: server.addr.Addr().String(), ServerPort: server.addr.Port()},
					Username:      testUser,
					Password:      password,
					QUIC:          quic,
					OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{TLS: &option.OutboundTLSOptions{
						Enabled:     true,
						ServerName:  "tt.example.org",
						Certificate: []string{server.cert},
					}},
				},
			}},
		},
	})
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
	out, loaded := instance.Outbound().Outbound("tt")
	if !loaded {
		t.Fatal("outbound tt missing")
	}
	return out
}

func startHTTP2Server(t *testing.T) *testServer {
	t.Helper()
	certificate, certPEM := newCertificate(t)
	server := &testServer{cert: certPEM}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server.addr = listener.Addr().(*net.TCPAddr).AddrPort()
	httpServer := &http.Server{
		Handler:   server.handler(t),
		TLSConfig: &tls.Config{Certificates: []tls.Certificate{certificate}, NextProtos: []string{"h2"}},
	}
	go httpServer.ServeTLS(listener, "", "")
	t.Cleanup(func() { httpServer.Close() })
	return server
}

func testTCPAndUDP(t *testing.T, server *testServer, quic bool) {
	out := startBox(t, server, quic, testPassword)
	tcpEcho := startTCPEcho(t)
	udpEcho := startUDPEcho(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	t.Run("tcp", func(t *testing.T) {
		conn, err := out.DialContext(ctx, N.NetworkTCP, M.SocksaddrFromNetIP(tcpEcho))
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		message := bytes.Repeat([]byte("trusttunnel!"), 20000)
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
		packetConn, err := out.ListenPacket(ctx, M.SocksaddrFromNetIP(udpEcho))
		if err != nil {
			t.Fatal(err)
		}
		defer packetConn.Close()
		for _, payload := range []string{"ping-1", "ping-2"} {
			if _, err = packetConn.WriteTo([]byte(payload), net.UDPAddrFromAddrPort(udpEcho)); err != nil {
				t.Fatal(err)
			}
			packetConn.SetReadDeadline(time.Now().Add(5 * time.Second))
			buffer := make([]byte, 1500)
			n, from, err := packetConn.ReadFrom(buffer)
			if err != nil {
				t.Fatal(err)
			}
			if string(buffer[:n]) != payload || from.String() != udpEcho.String() {
				t.Fatalf("got %q from %s", buffer[:n], from)
			}
		}
		if got := server.udp.Load(); got != 1 {
			t.Fatalf("expected one shared _udp2 stream, server saw %d", got)
		}
	})

	t.Run("udp conns share the stream and stay isolated", func(t *testing.T) {
		first, err := out.ListenPacket(ctx, M.SocksaddrFromNetIP(udpEcho))
		if err != nil {
			t.Fatal(err)
		}
		defer first.Close()
		second, err := out.ListenPacket(ctx, M.SocksaddrFromNetIP(udpEcho))
		if err != nil {
			t.Fatal(err)
		}
		defer second.Close()
		second.WriteTo([]byte("for-second"), net.UDPAddrFromAddrPort(udpEcho))
		first.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
		second.SetReadDeadline(time.Now().Add(5 * time.Second))
		buffer := make([]byte, 64)
		n, _, err := second.ReadFrom(buffer)
		if err != nil || string(buffer[:n]) != "for-second" {
			t.Fatalf("second conn: %q %v", buffer[:n], err)
		}
		if _, _, err = first.ReadFrom(buffer); !errors.Is(err, os.ErrDeadlineExceeded) {
			t.Fatalf("first conn must time out, got %v", err)
		}
	})
}

func TestOutboundHTTP2(t *testing.T) {
	server := startHTTP2Server(t)
	testTCPAndUDP(t, server, false)
}

func TestOutboundWrongPasswordIs407(t *testing.T) {
	server := startHTTP2Server(t)
	out := startBox(t, server, false, "wrong")
	echo := startTCPEcho(t)
	_, err := out.DialContext(context.Background(), N.NetworkTCP, M.SocksaddrFromNetIP(echo))
	var status *trusttunnel.StatusError
	if !errors.As(err, &status) || status.Status != http.StatusProxyAuthRequired {
		t.Fatalf("want 407 StatusError, got %v", err)
	}
}

func TestOutboundHealthCheckOnIdleSession(t *testing.T) {
	defer trusttunnel.SetCheckIdleAfter(50 * time.Millisecond)()
	server := startHTTP2Server(t)
	out := startBox(t, server, false, testPassword)
	echo := startTCPEcho(t)
	dial := func() {
		conn, err := out.DialContext(context.Background(), N.NetworkTCP, M.SocksaddrFromNetIP(echo))
		if err != nil {
			t.Fatal(err)
		}
		conn.Close()
	}
	dial()
	if got := server.checks.Load(); got != 0 {
		t.Fatalf("fresh session must not be probed, saw %d _check", got)
	}
	time.Sleep(120 * time.Millisecond)
	dial()
	if got := server.checks.Load(); got != 1 {
		t.Fatalf("idle session must be probed once, saw %d _check", got)
	}
}
