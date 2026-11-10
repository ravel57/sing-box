---
icon: material/new-box
---

!!! question "Since sing-box 1.14.5"

### Structure

```json
{
  "type": "trusttunnel",
  "tag": "trusttunnel-out",

  "server": "127.0.0.1",
  "server_port": 443,
  "username": "sekai",
  "password": "password",
  "quic": false,
  "tls": {},

  ... // Dial Fields
}
```

TrustTunnel client: HTTP CONNECT tunnels over TLS with Basic authentication
([protocol specification](https://github.com/TrustTunnel/TrustTunnel/blob/master/PROTOCOL.md)).

* TCP: one `CONNECT host:port` stream per connection.
* UDP: all packets of the outbound share one `CONNECT _udp2` stream, framed as in the specification. Domain destinations are resolved locally.
* A session idle for more than 7 seconds is probed with `CONNECT _check` before the next stream is opened; a `407` answer fails the connection with an authentication error.
* ICMP (`_icmp`) is not supported.

### Example

A client for an HTTP/2 TrustTunnel endpoint (for `tt://` deep links: `hostname` is `tls.server_name`,
`addresses` is `server:server_port`, `upstream_protocol` `http3` is `quic: true`):

```json
{
  "outbounds": [
    {
      "type": "trusttunnel",
      "tag": "tt",
      "server": "tt.example.org",
      "server_port": 443,
      "username": "sekai",
      "password": "password",
      "tls": {
        "enabled": true,
        "server_name": "tt.example.org"
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

==Required==

The server port.

#### username

==Required==

The TrustTunnel username.

#### password

==Required==

The TrustTunnel password.

#### quic

Use HTTP/3 over QUIC (ALPN `h3`, UDP port) instead of HTTP/2 over TLS (ALPN `h2`).

Requires the `with_quic` build tag; the server must listen for HTTP/3 on the same port number.

#### tls

==Required==

TLS configuration, see [TLS](/configuration/shared/tls/#outbound).

With `quic` enabled only plain TLS fields (server name, certificates, `insecure`) are honoured.

### Dial Fields

See [Dial Fields](/configuration/shared/dial/) for details.
