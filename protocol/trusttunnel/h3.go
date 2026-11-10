//go:build with_quic

package trusttunnel

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/sagernet/quic-go"
	"github.com/sagernet/quic-go/http3"
	"github.com/sagernet/sing-box/common/tls"
	"github.com/sagernet/sing/common/baderror"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

// Matches the reference client: 2 * (connection timeout + health check timeout).
const h3IdleTimeout = 2 * (30*time.Second + 7*time.Second)

type h3Opener struct {
	dialer     N.Dialer
	serverAddr M.Socksaddr
	tlsConfig  *tls.STDConfig
	username   string
	password   string
	transport  *http3.Transport

	access sync.Mutex
	conn   *quic.Conn
	client *http3.ClientConn
}

func newH3Opener(dialer N.Dialer, serverAddr M.Socksaddr, tlsConfig tls.Config, username, password string) (streamOpener, error) {
	tlsConfig.SetNextProtos([]string{http3.NextProtoH3})
	stdConfig, err := tlsConfig.STDConfig()
	if err != nil {
		return nil, err
	}
	return &h3Opener{
		dialer:     dialer,
		serverAddr: serverAddr,
		tlsConfig:  stdConfig,
		username:   username,
		password:   password,
		transport:  &http3.Transport{},
	}, nil
}

func (o *h3Opener) session(ctx context.Context) (*http3.ClientConn, error) {
	o.access.Lock()
	defer o.access.Unlock()
	if o.client != nil && o.conn.Context().Err() == nil {
		return o.client, nil
	}
	packetConn, err := o.dialer.DialContext(ctx, N.NetworkUDP, o.serverAddr)
	if err != nil {
		return nil, err
	}
	conn, err := quic.DialEarlyConn(ctx, packetConn, o.tlsConfig, &quic.Config{
		MaxIdleTimeout:  h3IdleTimeout,
		KeepAlivePeriod: 15 * time.Second,
	})
	if err != nil {
		packetConn.Close()
		return nil, err
	}
	// quic-go does not own the dialed packet conn, so release it when the session ends.
	go func() {
		<-conn.Context().Done()
		packetConn.Close()
	}()
	o.conn = conn
	o.client = o.transport.NewClientConn(conn)
	return o.client, nil
}

func (o *h3Opener) dropSession(client *http3.ClientConn) {
	o.access.Lock()
	defer o.access.Unlock()
	if o.client == client {
		o.conn.CloseWithError(0, "")
		o.client = nil
		o.conn = nil
	}
}

func (o *h3Opener) OpenStream(ctx context.Context, authority string) (net.Conn, error) {
	for attempt := 0; ; attempt++ {
		client, err := o.session(ctx)
		if err != nil {
			return nil, err
		}
		stream, err := client.OpenRequestStream(ctx)
		if err != nil {
			o.dropSession(client)
			if attempt == 0 && ctx.Err() == nil {
				continue
			}
			return nil, err
		}
		return o.connect(ctx, stream, authority)
	}
}

func (o *h3Opener) connect(ctx context.Context, stream *http3.RequestStream, authority string) (net.Conn, error) {
	request := &http.Request{
		Method: http.MethodConnect,
		URL:    &url.URL{Host: authority},
		Host:   authority,
		Header: connectHeader(o.username, o.password, agentFor(authority)),
	}
	stopCancel := context.AfterFunc(ctx, func() {
		stream.CancelRead(quic.StreamErrorCode(http3.ErrCodeRequestCanceled))
		stream.CancelWrite(quic.StreamErrorCode(http3.ErrCodeRequestCanceled))
	})
	err := stream.SendRequestHeader(request)
	var response *http.Response
	if err == nil {
		response, err = stream.ReadResponse()
	}
	if !stopCancel() {
		if err == nil {
			err = ctx.Err()
		}
	}
	if err != nil {
		stream.CancelRead(quic.StreamErrorCode(http3.ErrCodeRequestCanceled))
		stream.CancelWrite(quic.StreamErrorCode(http3.ErrCodeRequestCanceled))
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		stream.CancelRead(quic.StreamErrorCode(http3.ErrCodeRequestCanceled))
		stream.CancelWrite(quic.StreamErrorCode(http3.ErrCodeRequestCanceled))
		return nil, &StatusError{Status: response.StatusCode}
	}
	return &h3Conn{stream: stream, local: o.localAddr(), remote: o.serverAddr}, nil
}

func (o *h3Opener) localAddr() net.Addr {
	o.access.Lock()
	defer o.access.Unlock()
	if o.conn == nil {
		return M.Socksaddr{}
	}
	return o.conn.LocalAddr()
}

func (o *h3Opener) CloseIdleSessions() {}

func (o *h3Opener) Close() error {
	o.access.Lock()
	defer o.access.Unlock()
	if o.conn != nil {
		o.conn.CloseWithError(0, "")
		o.client = nil
		o.conn = nil
	}
	return o.transport.Close()
}

type h3Conn struct {
	stream *http3.RequestStream
	local  net.Addr
	remote net.Addr
}

func (c *h3Conn) Read(p []byte) (int, error) {
	n, err := c.stream.Read(p)
	return n, baderror.WrapQUIC(err)
}

func (c *h3Conn) Write(p []byte) (int, error) {
	n, err := c.stream.Write(p)
	return n, baderror.WrapQUIC(err)
}

func (c *h3Conn) CloseWrite() error {
	return c.stream.Close()
}

func (c *h3Conn) Close() error {
	c.stream.CancelRead(quic.StreamErrorCode(http3.ErrCodeNoError))
	return c.stream.Close()
}

func (c *h3Conn) LocalAddr() net.Addr                { return c.local }
func (c *h3Conn) RemoteAddr() net.Addr               { return c.remote }
func (c *h3Conn) SetDeadline(t time.Time) error      { return c.stream.SetDeadline(t) }
func (c *h3Conn) SetReadDeadline(t time.Time) error  { return c.stream.SetReadDeadline(t) }
func (c *h3Conn) SetWriteDeadline(t time.Time) error { return c.stream.SetWriteDeadline(t) }
