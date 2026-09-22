package v2rayxhttp

import (
	"context"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/tls"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/transport/v2rayhttp"
	"github.com/sagernet/sing/common/logger"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	aTLS "github.com/sagernet/sing/common/tls"
	sHTTP "github.com/sagernet/sing/protocol/http"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c" //nolint:staticcheck
)

var _ adapter.V2RayServerTransport = (*Server)(nil)

type session struct {
	upload   *uploadQueue
	done     chan struct{}
	once     sync.Once
	attached bool // guarded by Server.mu
	timer    *time.Timer
}

func (s *session) close() {
	s.once.Do(func() { s.upload.Close(); close(s.done) })
}

type Server struct {
	options     option.V2RayXHTTPOptions
	path        string
	tlsConfig   tls.ServerConfig
	handler     adapter.V2RayServerTransportHandler
	httpServer  *http.Server
	ctx         context.Context
	cancel      context.CancelFunc
	mu          sync.Mutex
	sessions    map[string]*session
	connections map[*trackedConn]struct{}
	buffered    atomic.Int64
	closed      bool
}

func NewServer(ctx context.Context, _ logger.ContextLogger, options option.V2RayXHTTPOptions, tlsConfig tls.ServerConfig, handler adapter.V2RayServerTransportHandler) (*Server, error) {
	options, u, err := normalize(options)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	s := &Server{ctx: ctx, cancel: cancel, options: options, path: u.Path, tlsConfig: tlsConfig, handler: handler, sessions: make(map[string]*session), connections: make(map[*trackedConn]struct{})}
	s.httpServer = &http.Server{
		// Also dispatches HTTP/2 with sing-box's custom TLS implementations.
		Handler:           h2c.NewHandler(s, &http2.Server{}), //nolint:staticcheck
		ReadHeaderTimeout: C.TCPTimeout,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	return s, nil
}

func (s *Server) getSession(id string, attach bool) *session {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	entry := s.sessions[id]
	if entry == nil {
		if len(s.sessions) >= 1024 {
			return nil
		}
		entry = &session{upload: newUploadQueue(s.options.MaxBufferedPosts), done: make(chan struct{})}
		entry.upload.budget = &s.buffered
		s.sessions[id] = entry
		entry.timer = time.AfterFunc(30*time.Second, func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			if !entry.attached && s.sessions[id] == entry {
				delete(s.sessions, id)
				entry.close()
			}
		})
	}
	if attach {
		if entry.attached {
			return nil
		}
		entry.attached = true
		entry.timer.Stop()
	}
	return entry
}

func (s *Server) removeSession(id string, entry *session) {
	s.mu.Lock()
	if s.sessions[id] == entry {
		delete(s.sessions, id)
	}
	s.mu.Unlock()
	entry.timer.Stop()
	entry.close()
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if (s.options.Host != "" && r.Host != s.options.Host) || !strings.HasPrefix(r.URL.Path, s.path) {
		http.NotFound(w, r)
		return
	}
	suffix := strings.TrimPrefix(r.URL.Path, s.path)
	parts := strings.Split(suffix, "/")
	if len(parts) > 2 || len(parts[0]) > 128 || (len(parts) == 2 && (parts[0] == "" || parts[1] == "")) {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	mode := "stream-one"
	if suffix != "" {
		mode = "stream-up"
		if len(parts) == 2 {
			mode = "packet-up"
		}
	}
	isDownload := r.Method == http.MethodGet && suffix != "" && len(parts) == 1
	if !isDownload && r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !isDownload && s.options.Mode != "auto" && s.options.Mode != mode {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if isDownload && s.options.Mode == "stream-one" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if !isDownload && mode != "packet-up" && r.ProtoMajor != 2 {
		w.WriteHeader(http.StatusHTTPVersionNotSupported)
		return
	}
	for key, values := range s.options.Headers.Build() {
		w.Header()[key] = values
	}
	w.Header().Set("X-Padding", strings.Repeat("X", 100+rand.IntN(901)))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	var entry *session
	if suffix != "" {
		entry = s.getSession(parts[0], isDownload)
		if entry == nil {
			w.WriteHeader(http.StatusConflict)
			return
		}
	}
	if !isDownload && mode == "packet-up" {
		seq, err := strconv.ParseUint(parts[1], 10, 64)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		payload, err := io.ReadAll(io.LimitReader(r.Body, int64(s.options.MaxPostSize)+1))
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if len(payload) > s.options.MaxPostSize {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		if err = entry.upload.push(seq, payload); err != nil {
			w.WriteHeader(http.StatusConflict)
			return
		}
		w.WriteHeader(http.StatusOK)
		return
	}
	if !isDownload && mode == "stream-up" {
		if err := entry.upload.setStream(r.Body); err != nil {
			w.WriteHeader(http.StatusConflict)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		select {
		case <-entry.done:
		case <-r.Context().Done():
			entry.close()
		case <-s.ctx.Done():
			entry.close()
		}
		return
	}
	var reader io.Reader = r.Body
	if entry != nil {
		defer s.removeSession(parts[0], entry)
		reader = entry.upload
	}
	if !s.options.NoSSEHeader {
		w.Header().Set("Content-Type", "text/event-stream")
	}
	w.WriteHeader(http.StatusOK)
	w.(http.Flusher).Flush()
	done := make(chan struct{})
	var once sync.Once
	closeDone := func() { once.Do(func() { close(done) }) }
	base := &serverConn{
		ServerHTTPConn: v2rayhttp.ServerHTTPConn{HTTP2Conn: v2rayhttp.NewHTTPConn(reader, w), Flusher: w.(http.Flusher)},
		closeDone:      closeDone,
	}
	conn := v2rayhttp.NewHTTP2Wrapper(base)
	s.handler.NewConnectionEx(r.Context(), conn, sHTTP.SourceAddress(r), M.Socksaddr{}, func(error) { closeDone() })
	select {
	case <-done:
	case <-r.Context().Done():
	case <-s.ctx.Done():
	}
	conn.CloseWrapper()
}

type serverConn struct {
	v2rayhttp.ServerHTTPConn
	closeDone func()
}

func (c *serverConn) Close() error {
	c.closeDone()
	return nil
}

func (s *Server) Network() []string { return []string{N.NetworkTCP} }

func (s *Server) Serve(listener net.Listener) error {
	listener = &trackedListener{Listener: listener, server: s}
	if s.tlsConfig != nil {
		s.tlsConfig.SetNextProtos([]string{http2.NextProtoTLS, "http/1.1"})
		listener = aTLS.NewListener(listener, s.tlsConfig)
	}
	return s.httpServer.Serve(listener)
}

func (s *Server) ServePacket(net.PacketConn) error { return os.ErrInvalid }

func (s *Server) Close() error {
	s.cancel()
	s.mu.Lock()
	s.closed = true
	for id, entry := range s.sessions {
		entry.timer.Stop()
		entry.close()
		delete(s.sessions, id)
	}
	connections := make([]*trackedConn, 0, len(s.connections))
	for conn := range s.connections {
		connections = append(connections, conn)
	}
	s.mu.Unlock()
	// h2c takes ownership of HTTP/2 connections, so http.Server.Close alone
	// cannot close them.
	for _, conn := range connections {
		conn.Close()
	}
	return s.httpServer.Close()
}

type trackedListener struct {
	net.Listener
	server *Server
}

func (l *trackedListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	conn := &trackedConn{Conn: c, server: l.server}
	l.server.mu.Lock()
	defer l.server.mu.Unlock()
	if l.server.closed {
		c.Close()
		return nil, net.ErrClosed
	}
	l.server.connections[conn] = struct{}{}
	return conn, nil
}

type trackedConn struct {
	net.Conn
	server *Server
}

func (c *trackedConn) Close() error {
	c.server.mu.Lock()
	delete(c.server.connections, c)
	c.server.mu.Unlock()
	return c.Conn.Close()
}
