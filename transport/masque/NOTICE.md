# Third-party code

* `connectip/` is adapted from `github.com/quic-go/connect-ip-go` (MIT, Marten Seemann), via
  the copy in `github.com/xtls/xray-core/transport/internet/masque/connectip`. See `connectip/LICENSE`.
* `conn.go`, `http2.go` and the client flow in `dialer.go` are derived from
  `github.com/xtls/xray-core/transport/internet/masque` (MPL-2.0, https://mozilla.org/MPL/2.0/).
  Changes: logging, TLS/dialer plumbing and the QUIC fork were replaced; WARP support was removed.
