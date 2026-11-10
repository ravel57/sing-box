package option

import "github.com/sagernet/sing/common/json/badoption"

type MASQUEOutboundOptions struct {
	DialerOptions
	ServerOptions
	OutboundTLSOptionsContainer
	Username  string               `json:"username,omitempty"`
	Password  string               `json:"password,omitempty"`
	HTTP2     bool                 `json:"http2,omitempty"`
	Host      string               `json:"host,omitempty"`
	Path      string               `json:"path,omitempty"`
	Headers   badoption.HTTPHeader `json:"headers,omitempty"`
	KeepAlive badoption.Duration   `json:"keep_alive,omitempty"`
}
