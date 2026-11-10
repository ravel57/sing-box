package trusttunnel

import (
	"context"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/tls"
	"github.com/sagernet/sing/common/baderror"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"

	"golang.org/x/net/http2"
)

const (
	checkAuthority = "_check"
	udpAuthority   = "_udp2"
	userAgent      = "sing-box"
)

// streamOpener opens one CONNECT stream; the returned conn is the tunnel body after a 200.
type streamOpener interface {
	OpenStream(ctx context.Context, authority string) (net.Conn, error)
	CloseIdleSessions()
	Close() error
}

type StatusError struct {
	Status int
}

func (e *StatusError) Error() string {
	if e.Status == http.StatusProxyAuthRequired {
		return "trusttunnel: authentication failed (407)"
	}
	return "trusttunnel: unexpected status " + strconv.Itoa(e.Status)
}

func connectHeader(username, password, agent string) http.Header {
	header := make(http.Header)
	header.Set("User-Agent", agent)
	header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(username+":"+password)))
	return header
}

// agentFor mirrors the reference client: "<platform> <app>" for tunnels, "<platform> _udp2" for UDP.
func agentFor(authority string) string {
	switch authority {
	case udpAuthority:
		return userAgent + " " + udpAuthority
	case checkAuthority:
		return userAgent
	default:
		return userAgent + " " + userAgent
	}
}

type h2Opener struct {
	serverAddr M.Socksaddr
	username   string
	password   string
	transport  *http2.Transport
}

func newH2Opener(dialer N.Dialer, serverAddr M.Socksaddr, tlsConfig tls.Config, username, password string) *h2Opener {
	tlsConfig.SetNextProtos([]string{http2.NextProtoTLS})
	tlsDialer := tls.NewDialer(dialer, tlsConfig)
	return &h2Opener{
		serverAddr: serverAddr,
		username:   username,
		password:   password,
		transport: &http2.Transport{
			DialTLSContext: func(ctx context.Context, network, addr string, cfg *tls.STDConfig) (net.Conn, error) {
				return tlsDialer.DialTLSContext(adapter.ContextForMultiplexSession(ctx), serverAddr)
			},
			ReadIdleTimeout: 30 * time.Second,
			PingTimeout:     15 * time.Second,
		},
	}
}

func (o *h2Opener) OpenStream(ctx context.Context, authority string) (net.Conn, error) {
	pipeReader, pipeWriter := io.Pipe()
	requestCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	request := (&http.Request{
		Method: http.MethodConnect,
		// The URL host selects the pooled connection; the target goes in :authority.
		URL:    &url.URL{Scheme: "https", Host: o.serverAddr.String()},
		Host:   authority,
		Header: connectHeader(o.username, o.password, agentFor(authority)),
		Body:   pipeReader,
	}).WithContext(requestCtx)
	stopCancel := context.AfterFunc(ctx, cancel)
	response, err := o.transport.RoundTrip(request)
	if !stopCancel() && err == nil {
		response.Body.Close()
		err = ctx.Err()
	}
	if err != nil {
		cancel()
		pipeWriter.Close()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		cancel()
		pipeWriter.Close()
		return nil, &StatusError{Status: response.StatusCode}
	}
	return &h2Conn{body: response.Body, writer: pipeWriter, cancel: cancel, remote: o.serverAddr}, nil
}

func (o *h2Opener) CloseIdleSessions() {
	o.transport.CloseIdleConnections()
}

func (o *h2Opener) Close() error {
	o.transport.CloseIdleConnections()
	return nil
}

type h2Conn struct {
	body      io.ReadCloser
	writer    *io.PipeWriter
	cancel    context.CancelFunc
	remote    net.Addr
	closeOnce sync.Once
}

func (c *h2Conn) Read(p []byte) (int, error) {
	n, err := c.body.Read(p)
	return n, baderror.WrapH2(err)
}

func (c *h2Conn) Write(p []byte) (int, error) {
	n, err := c.writer.Write(p)
	return n, baderror.WrapH2(err)
}

func (c *h2Conn) CloseWrite() error {
	return c.writer.Close()
}

func (c *h2Conn) Close() error {
	c.closeOnce.Do(func() {
		c.writer.Close()
		c.body.Close()
		c.cancel()
	})
	return nil
}

func (c *h2Conn) LocalAddr() net.Addr              { return M.Socksaddr{} }
func (c *h2Conn) RemoteAddr() net.Addr             { return c.remote }
func (c *h2Conn) SetDeadline(time.Time) error      { return os.ErrInvalid }
func (c *h2Conn) SetReadDeadline(time.Time) error  { return os.ErrInvalid }
func (c *h2Conn) SetWriteDeadline(time.Time) error { return os.ErrInvalid }
func (c *h2Conn) NeedAdditionalReadDeadline() bool { return true }
