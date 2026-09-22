package v2rayxhttp

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/common/tls"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/transport/v2rayhttp"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"

	"github.com/gofrs/uuid/v5"
	"golang.org/x/net/http2"
)

var _ adapter.V2RayClientTransport = (*Client)(nil)

type Client struct {
	options   option.V2RayXHTTPOptions
	url       url.URL
	transport http.RoundTripper
}

func NewClient(ctx context.Context, dialer N.Dialer, serverAddr M.Socksaddr, options option.V2RayXHTTPOptions, tlsConfig tls.Config) (adapter.V2RayClientTransport, error) {
	options, u, err := normalize(options)
	if err != nil {
		return nil, err
	}
	version := options.HTTPVersion
	if version == "" {
		version = "1.1"
		if tlsConfig != nil {
			version = "2"
		}
	}
	if options.Mode == "auto" {
		options.Mode = "packet-up"
		if version == "2" {
			options.Mode = "stream-up"
		}
	}
	if version == "1.1" && options.Mode != "packet-up" {
		return nil, fmt.Errorf("xhttp: %s requires HTTP/2", options.Mode)
	}
	u.Host = serverAddr.String()
	u.Scheme = "http"
	if tlsConfig != nil {
		u.Scheme = "https"
		tlsConfig = tlsConfig.Clone()
		proto := "http/1.1"
		if version == "2" {
			proto = http2.NextProtoTLS
		}
		tlsConfig.SetNextProtos([]string{proto})
	}
	dial := func(ctx context.Context, _, _ string) (net.Conn, error) {
		if tlsConfig != nil {
			return tls.NewDialer(dialer, tlsConfig).DialTLSContext(ctx, serverAddr)
		}
		return dialer.DialContext(ctx, N.NetworkTCP, serverAddr)
	}
	var transport http.RoundTripper
	if version == "2" {
		transport = &http2.Transport{
			AllowHTTP: tlsConfig == nil,
			DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.STDConfig) (net.Conn, error) {
				return dial(ctx, network, addr)
			},
		}
	} else {
		transport = &http.Transport{DialContext: dial, DialTLSContext: dial, MaxIdleConnsPerHost: 16}
	}
	return &Client{options: options, url: *u, transport: transport}, nil
}

func (c *Client) request(ctx context.Context, method, suffix string, body io.Reader) (*http.Response, error) {
	u := c.url
	u.Path += suffix
	if u.RawPath != "" {
		u.RawPath += suffix
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, err
	}
	req.Header = c.options.Headers.Build()
	if c.options.Host != "" {
		req.Host = c.options.Host
	}
	// Xray's default padding placement is a query parameter inside Referer.
	paddingURL := u
	query := paddingURL.Query()
	query.Set("x_padding", strings.Repeat("X", 100+rand.IntN(901)))
	paddingURL.RawQuery = query.Encode()
	req.Header.Set("Referer", paddingURL.String())
	if body != nil && c.options.Mode != "packet-up" && !c.options.NoGRPCHeader {
		req.Header.Set("Content-Type", "application/grpc")
	}
	response, err := c.transport.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, fmt.Errorf("xhttp: unexpected HTTP status: %s", response.Status)
	}
	return response, nil
}

func (c *Client) DialContext(ctx context.Context) (net.Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	id, err := uuid.NewV4()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	local, remote := net.Pipe()
	conn := &clientConn{Conn: local, cancel: cancel}
	stop := context.AfterFunc(ctx, func() { conn.setError(ctx.Err()); remote.Close() })
	finish := func(err error) { conn.setError(err); cancel(); stop(); remote.Close() }
	session := id.String()
	go func() {
		var streamErr error
		defer func() { finish(streamErr) }()
		method, suffix := http.MethodGet, session
		var body io.Reader
		if c.options.Mode == "stream-one" {
			method, suffix, body = http.MethodPost, "", remote
		}
		response, err := c.request(ctx, method, suffix, body)
		if err != nil {
			streamErr = err
			return
		}
		defer response.Body.Close()
		_, streamErr = io.Copy(remote, response.Body)
	}()
	if c.options.Mode != "stream-one" {
		go func() {
			var streamErr error
			defer func() { finish(streamErr) }()
			if c.options.Mode == "stream-up" {
				response, err := c.request(ctx, http.MethodPost, session, remote)
				if err != nil {
					streamErr = err
					return
				}
				defer response.Body.Close()
				_, streamErr = io.Copy(io.Discard, response.Body)
				return
			}
			buffer := make([]byte, c.options.MaxPostSize)
			for seq := uint64(0); ; seq++ {
				n, err := remote.Read(buffer)
				if err != nil {
					streamErr = err
					return
				}
				response, err := c.request(ctx, http.MethodPost, session+"/"+strconv.FormatUint(seq, 10), bytes.NewReader(buffer[:n]))
				if err != nil {
					streamErr = err
					return
				}
				_, err = io.Copy(io.Discard, response.Body)
				response.Body.Close()
				if err != nil {
					streamErr = err
					return
				}
			}
		}()
	}
	return conn, nil
}

func (c *Client) Close() error {
	v2rayhttp.CloseIdleConnections(c.transport)
	return nil
}

type clientConn struct {
	net.Conn
	cancel context.CancelFunc
	mu     sync.Mutex
	err    error
}

func (c *clientConn) Close() error {
	c.cancel()
	return c.Conn.Close()
}

func (c *clientConn) setError(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err == nil {
		if err == nil {
			err = io.EOF
		}
		c.err = err
	}
}

func (c *clientConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if err == io.EOF || err == io.ErrClosedPipe {
		c.mu.Lock()
		if c.err != nil {
			err = c.err
		}
		c.mu.Unlock()
	}
	return n, err
}

func (c *clientConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	if err == io.ErrClosedPipe {
		c.mu.Lock()
		if c.err != nil {
			err = c.err
		}
		c.mu.Unlock()
	}
	return n, err
}
