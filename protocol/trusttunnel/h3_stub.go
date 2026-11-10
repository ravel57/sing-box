//go:build !with_quic

package trusttunnel

import (
	"github.com/sagernet/sing-box/common/tls"
	C "github.com/sagernet/sing-box/constant"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

func newH3Opener(dialer N.Dialer, serverAddr M.Socksaddr, tlsConfig tls.Config, username, password string) (streamOpener, error) {
	return nil, C.ErrQUICNotIncluded
}
