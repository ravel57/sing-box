package v2rayxhttp

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sagernet/sing-box/option"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"

	"github.com/stretchr/testify/require"
)

type testDialer struct{ net.Dialer }

func (d testDialer) DialContext(ctx context.Context, network string, addr M.Socksaddr) (net.Conn, error) {
	return d.Dialer.DialContext(ctx, network, addr.String())
}

func (d testDialer) ListenPacket(context.Context, M.Socksaddr) (net.PacketConn, error) {
	panic("unused")
}

type echoHandler struct{}

func (echoHandler) NewConnectionEx(ctx context.Context, conn net.Conn, _, _ M.Socksaddr, onClose N.CloseHandlerFunc) {
	go func() {
		_, err := io.Copy(conn, conn)
		conn.Close()
		onClose(err)
	}()
}

func TestTransport(t *testing.T) {
	for _, version := range []string{"1.1", "2"} {
		for _, mode := range []string{"auto", "packet-up", "stream-up", "stream-one"} {
			if version == "1.1" && strings.HasPrefix(mode, "stream-") {
				continue
			}
			t.Run(version+"/"+mode, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				opts := option.V2RayXHTTPOptions{Mode: mode, HTTPVersion: version, Path: "/test?key=value", Host: "example.org", MaxPostSize: 4096}
				server, err := NewServer(ctx, nil, opts, nil, echoHandler{})
				require.NoError(t, err)
				listener, err := net.Listen("tcp", "127.0.0.1:0")
				require.NoError(t, err)
				defer server.Close()
				go server.Serve(listener)
				client, err := NewClient(ctx, testDialer{}, M.ParseSocksaddr(listener.Addr().String()), opts, nil)
				require.NoError(t, err)
				defer client.Close()
				conn, err := client.DialContext(ctx)
				require.NoError(t, err)
				require.NoError(t, conn.SetDeadline(time.Now().Add(5*time.Second)))
				payload := bytes.Repeat([]byte("xhttp-test-data"), 10000)
				writeDone := make(chan error, 1)
				go func() { _, err := conn.Write(payload); writeDone <- err }()
				response := make([]byte, len(payload))
				_, err = io.ReadFull(conn, response)
				require.NoError(t, err)
				require.NoError(t, <-writeDone)
				require.Equal(t, payload, response)
				require.NoError(t, conn.Close())
				require.Eventually(t, func() bool {
					server.mu.Lock()
					defer server.mu.Unlock()
					return len(server.sessions) == 0
				}, time.Second, time.Millisecond)
			})
		}
	}
}

func TestUploadQueue(t *testing.T) {
	q := newUploadQueue(3)
	require.NoError(t, q.push(1, []byte("second")))
	require.Error(t, q.push(1, []byte("duplicate")))
	require.Error(t, q.push(3, []byte("too far")))
	require.NoError(t, q.push(0, []byte("first")))
	buffer := make([]byte, 11)
	_, err := io.ReadFull(q, buffer)
	require.NoError(t, err)
	require.Equal(t, "firstsecond", string(buffer))
	require.Error(t, q.push(0, nil))
	done := make(chan error, 1)
	go func() { _, err := q.Read(buffer); done <- err }()
	require.NoError(t, q.Close())
	select {
	case err := <-done:
		require.ErrorIs(t, err, io.ErrClosedPipe)
	case <-time.After(time.Second):
		t.Fatal("read was not unblocked")
	}
}

func TestUploadBufferBudget(t *testing.T) {
	var budget atomic.Int64
	budget.Store(maxBufferedUploadBytes - 4)
	q := newUploadQueue(3)
	q.budget = &budget
	require.NoError(t, q.push(0, []byte("1234")))
	require.Error(t, q.push(1, []byte("5")))
	require.EqualValues(t, maxBufferedUploadBytes, budget.Load())
	_, err := io.ReadFull(q, make([]byte, 4))
	require.NoError(t, err)
	require.EqualValues(t, maxBufferedUploadBytes-4, budget.Load())
	require.NoError(t, q.push(1, []byte("1234")))
	require.NoError(t, q.Close())
	require.NoError(t, q.Close())
	require.EqualValues(t, maxBufferedUploadBytes-4, budget.Load())
}

func TestInvalidRequests(t *testing.T) {
	s, err := NewServer(context.Background(), nil, option.V2RayXHTTPOptions{Path: "/test", MaxPostSize: 8}, nil, echoHandler{})
	require.NoError(t, err)
	defer s.Close()
	for _, tc := range []struct {
		method, path, body string
		status             int
	}{
		{"POST", "/wrong/", "", 404},
		{"GET", "/test/", "", 405},
		{"POST", "/test/id/0", "123456789", 413},
		{"POST", "/test/id/no", "", 400},
		{"POST", "/test/id/0/extra", "", 400},
		{"POST", "/test/id/100", "", 409},
		{"POST", "/test/id/0", "data", 200},
		{"POST", "/test/id/0", "data", 409},
	} {
		r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		require.Equal(t, tc.status, w.Code, tc.path)
	}
}

func TestClientCancellation(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer s.Close()
	ctx, cancel := context.WithCancel(context.Background())
	client, err := NewClient(ctx, testDialer{}, M.ParseSocksaddr(strings.TrimPrefix(s.URL, "http://")), option.V2RayXHTTPOptions{}, nil)
	require.NoError(t, err)
	defer client.Close()
	conn, err := client.DialContext(ctx)
	require.NoError(t, err)
	defer conn.Close()
	cancel()
	done := make(chan error, 1)
	go func() { _, err := conn.Read(make([]byte, 1)); done <- err }()
	select {
	case err := <-done:
		require.Error(t, err)
	case <-time.After(time.Second):
		t.Fatal("cancellation did not unblock read")
	}
}

func TestClientHTTPError(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusForbidden) }))
	defer s.Close()
	client, err := NewClient(context.Background(), testDialer{}, M.ParseSocksaddr(strings.TrimPrefix(s.URL, "http://")), option.V2RayXHTTPOptions{}, nil)
	require.NoError(t, err)
	defer client.Close()
	conn, err := client.DialContext(context.Background())
	require.NoError(t, err)
	defer conn.Close()
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(time.Second)))
	_, err = conn.Read(make([]byte, 1))
	require.ErrorContains(t, err, "403")
}

func TestServerClose(t *testing.T) {
	for _, mode := range []string{"packet-up", "stream-up", "stream-one"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			opts := option.V2RayXHTTPOptions{Mode: mode, HTTPVersion: "2"}
			s, err := NewServer(ctx, nil, opts, nil, echoHandler{})
			require.NoError(t, err)
			defer s.Close()
			l, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			go s.Serve(l)
			client, err := NewClient(ctx, testDialer{}, M.ParseSocksaddr(l.Addr().String()), opts, nil)
			require.NoError(t, err)
			defer client.Close()
			conn, err := client.DialContext(ctx)
			require.NoError(t, err)
			defer conn.Close()
			require.NoError(t, conn.SetDeadline(time.Now().Add(2*time.Second)))
			_, err = conn.Write([]byte("hello"))
			require.NoError(t, err)
			_, err = io.ReadFull(conn, make([]byte, 5))
			require.NoError(t, err)
			require.NoError(t, s.Close())
			_, err = conn.Read(make([]byte, 1))
			require.Error(t, err)
			s.mu.Lock()
			require.Empty(t, s.connections)
			require.Empty(t, s.sessions)
			s.mu.Unlock()
		})
	}
}

func TestInvalidOptions(t *testing.T) {
	for _, opts := range []option.V2RayXHTTPOptions{
		{Mode: "unknown"}, {HTTPVersion: "3"}, {MaxPostSize: -1},
		{MaxBufferedPosts: -1}, {Path: "/%invalid"},
	} {
		_, err := NewClient(context.Background(), testDialer{}, M.Socksaddr{}, opts, nil)
		require.Error(t, err)
		_, err = NewServer(context.Background(), nil, opts, nil, echoHandler{})
		require.Error(t, err)
	}
	_, err := NewClient(context.Background(), testDialer{}, M.Socksaddr{}, option.V2RayXHTTPOptions{Mode: "stream-one", HTTPVersion: "1.1"}, nil)
	require.Error(t, err)
}
