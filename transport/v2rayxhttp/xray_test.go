package v2rayxhttp

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"io"
	"net"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/sagernet/sing-box/common/tls"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-vmess/vless"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"

	"github.com/stretchr/testify/require"
)

// Set XRAY_BINARY to an official Xray executable to run wire interoperability
// tests in both directions. No Xray Go dependency is added to sing-box.
func TestXrayInterop(t *testing.T) {
	binary := os.Getenv("XRAY_BINARY")
	if binary == "" {
		t.Skip("set XRAY_BINARY to run Xray interoperability tests")
	}
	certPEM, keyPEM := testCertificate(t)
	const userID = "bff4389a-8707-46c4-938e-aabedcdc245d"
	for _, tc := range []struct{ mode, version string }{{"packet-up", "1.1"}, {"packet-up", "2"}, {"stream-up", "2"}, {"stream-one", "2"}} {
		mode := tc.mode
		alpn := "h2"
		if tc.version == "1.1" {
			alpn = "http/1.1"
		}
		for _, direction := range []string{"client", "server"} {
			t.Run(mode+"/"+tc.version+"/"+direction, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				dir := t.TempDir()
				certPath, keyPath := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
				require.NoError(t, os.WriteFile(certPath, certPEM, 0o600))
				require.NoError(t, os.WriteFile(keyPath, keyPEM, 0o600))
				port := freePort(t)
				stream := map[string]any{
					"network": "xhttp", "security": "tls",
					"xhttpSettings": map[string]any{"path": "/interop", "mode": mode},
					"tlsSettings":   map[string]any{"alpn": []string{alpn}, "certificates": []any{map[string]any{"certificateFile": certPath, "keyFile": keyPath}}},
				}
				var conn net.Conn
				var err error
				if direction == "client" {
					echo, err := net.Listen("tcp", "127.0.0.1:0")
					require.NoError(t, err)
					defer echo.Close()
					go func() {
						for {
							c, err := echo.Accept()
							if err != nil {
								return
							}
							go func() { defer c.Close(); io.Copy(c, c) }()
						}
					}()
					startXray(t, binary, port, map[string]any{
						"inbounds":  []any{map[string]any{"listen": "127.0.0.1", "port": port, "protocol": "dokodemo-door", "settings": map[string]any{"address": "127.0.0.1", "port": echo.Addr().(*net.TCPAddr).Port, "network": "tcp"}, "streamSettings": stream}},
						"outbounds": []any{map[string]any{"protocol": "freedom"}},
					})
					tlsConfig, err := tls.NewSTDClient(ctx, nil, "localhost", option.OutboundTLSOptions{Enabled: true, Insecure: true})
					require.NoError(t, err)
					client, err := NewClient(ctx, testDialer{}, M.ParseSocksaddr(net.JoinHostPort("127.0.0.1", strconv.Itoa(port))), option.V2RayXHTTPOptions{Mode: mode, HTTPVersion: tc.version, Path: "/interop"}, tlsConfig)
					require.NoError(t, err)
					defer client.Close()
					conn, err = client.DialContext(ctx)
					require.NoError(t, err)
				} else {
					tlsConfig, err := tls.NewSTDServer(ctx, nil, option.InboundTLSOptions{Enabled: true, Certificate: []string{string(certPEM)}, Key: []string{string(keyPEM)}})
					require.NoError(t, err)
					defer tlsConfig.Close()
					service := vless.NewService[string](nil, vlessEchoHandler{})
					service.UpdateUsers([]string{"test"}, []string{userID}, []string{""})
					server, err := NewServer(ctx, nil, option.V2RayXHTTPOptions{Mode: mode, Path: "/interop"}, tlsConfig, vlessTransportHandler{service})
					require.NoError(t, err)
					defer server.Close()
					listener, err := net.Listen("tcp", "127.0.0.1:0")
					require.NoError(t, err)
					go server.Serve(listener)
					certBlock, _ := pem.Decode(certPEM)
					hash := sha256.Sum256(certBlock.Bytes)
					stream["tlsSettings"] = map[string]any{"pinnedPeerCertSha256": hex.EncodeToString(hash[:]), "serverName": "localhost", "alpn": []string{alpn}}
					startXray(t, binary, port, map[string]any{
						"inbounds":  []any{map[string]any{"listen": "127.0.0.1", "port": port, "protocol": "dokodemo-door", "settings": map[string]any{"address": "127.0.0.1", "port": 80, "network": "tcp"}}},
						"outbounds": []any{map[string]any{"protocol": "vless", "settings": map[string]any{"vnext": []any{map[string]any{"address": "127.0.0.1", "port": listener.Addr().(*net.TCPAddr).Port, "users": []any{map[string]any{"id": userID, "encryption": "none"}}}}}, "streamSettings": stream}},
					})
					conn, err = net.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
					require.NoError(t, err)
				}
				defer conn.Close()
				require.NoError(t, conn.SetDeadline(time.Now().Add(10*time.Second)))
				for i := 0; i < 3; i++ {
					payload := make([]byte, 128*1024)
					for j := range payload {
						payload[j] = byte(i + j)
					}
					done := make(chan error, 1)
					go func() { _, err := conn.Write(payload); done <- err }()
					response := make([]byte, len(payload))
					_, err = io.ReadFull(conn, response)
					require.NoError(t, err)
					require.NoError(t, <-done)
					require.Equal(t, payload, response)
				}
			})
		}
	}
}

func testCertificate(t *testing.T) ([]byte, []byte) {
	s := httptest.NewTLSServer(nil)
	defer s.Close()
	cert := s.TLS.Certificates[0]
	key, err := x509.MarshalPKCS8PrivateKey(cert.PrivateKey)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[0]}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key})
}

func freePort(t *testing.T) int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func startXray(t *testing.T, binary string, port int, config map[string]any) {
	config["log"] = map[string]any{"loglevel": "warning"}
	data, err := json.Marshal(config)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "xray.json")
	require.NoError(t, os.WriteFile(path, data, 0o600))
	command := exec.Command(binary, "run", "-c", path)
	command.Stdout, command.Stderr = os.Stdout, os.Stderr
	require.NoError(t, command.Start())
	t.Cleanup(func() { command.Process.Kill(); command.Wait() })
	require.Eventually(t, func() bool {
		c, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 50*time.Millisecond)
		if err != nil {
			return false
		}
		c.Close()
		return true
	}, 5*time.Second, 20*time.Millisecond)
}

type vlessEchoHandler struct{ echoHandler }

func (vlessEchoHandler) NewPacketConnectionEx(context.Context, N.PacketConn, M.Socksaddr, M.Socksaddr, N.CloseHandlerFunc) {
	panic("unused")
}

type vlessTransportHandler struct{ service *vless.Service[string] }

func (h vlessTransportHandler) NewConnectionEx(ctx context.Context, c net.Conn, source, _ M.Socksaddr, onClose N.CloseHandlerFunc) {
	go func() {
		if err := h.service.NewConnection(ctx, c, source, onClose); err != nil {
			c.Close()
			onClose(err)
		}
	}()
}
