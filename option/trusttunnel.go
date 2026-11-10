package option

type TrustTunnelOutboundOptions struct {
	DialerOptions
	ServerOptions
	OutboundTLSOptionsContainer
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	QUIC     bool   `json:"quic,omitempty"`
}
