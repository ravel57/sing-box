---
icon: material/new-box
---

!!! question "Since sing-box 1.14.5"

### Structure

```json
{
  "type": "masque",
  "tag": "masque-out",

  "server": "127.0.0.1",
  "server_port": 443,
  "username": "sekai",
  "password": "password",
  "http2": false,
  "host": "",
  "path": "",
  "headers": {},
  "keep_alive": "",
  "tls": {},

  ... // Dial Fields
}
```

MASQUE client using CONNECT-IP ([RFC 9484](https://www.rfc-editor.org/rfc/rfc9484)) over HTTP/3
datagrams, or over HTTP/2 capsules with `http2`. The server assigns the tunnel addresses; TCP and UDP
flows are carried as IP packets through a userspace network stack, so this outbound needs the
`with_gvisor` and `with_quic` build tags. It is compatible with the MASQUE inbound of xray-core
(as used by ultunnel-server-panel).

CONNECT-UDP ([RFC 9298](https://www.rfc-editor.org/rfc/rfc9298)) and Cloudflare WARP are not supported.

Domain destinations are resolved locally, outside the tunnel, with the sing-box DNS router.

### Example

```json
{
  "outbounds": [
    {
      "type": "masque",
      "tag": "masque",
      "server": "masque.example.org",
      "server_port": 443,
      "username": "sekai",
      "password": "password",
      "tls": {
        "enabled": true,
        "server_name": "masque.example.org"
      }
    }
  ]
}
```

### Fields

#### server

==Required==

The server address.

#### server_port

The server port. Default: 443.

#### username

The user name sent with HTTP Basic authentication (the client's email on xray-core / panel servers).

#### password

The password sent with HTTP Basic authentication.

#### http2

Use HTTP/2 over TCP (ALPN `h2`) instead of HTTP/3 over QUIC (ALPN `h3`).

The server must offer the same transport: xray-core listens for HTTP/2 when its TLS ALPN contains `h2`
and for HTTP/3 when it contains `h3` or does not contain `h2`.

#### host

Overrides the authority of the CONNECT-IP request. Default: `tls.server_name` (plus the port when it is not 443).

#### path

The request path. Default: `/.well-known/masque/ip/*/*/`.

#### headers

Extra HTTP headers of the CONNECT-IP request. `User-Agent` defaults to a Chrome one.

#### keep_alive

Keep-alive period of the QUIC connection or HTTP/2 PING interval. Default: 10s.

#### tls

==Required==

TLS configuration, see [TLS](/configuration/shared/tls/#outbound).

### Dial Fields

See [Dial Fields](/configuration/shared/dial/) for details.
