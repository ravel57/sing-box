V2Ray Transport is a set of private protocols invented by v2ray, and has contaminated the names of other protocols, such
as `trojan-grpc` in clash.

### Structure

```json
{
  "type": ""
}
```

Available transports:

* HTTP
* XHTTP
* WebSocket
* QUIC
* gRPC
* HTTPUpgrade

!!! warning "Difference from v2ray-core"

    * No TCP transport, plain HTTP is merged into the HTTP transport.
    * No mKCP transport.
    * No DomainSocket transport.

!!! note ""

    You can ignore the JSON Array [] tag when the content is only one item

### HTTP

```json
{
  "type": "http",
  "host": [],
  "path": "",
  "method": "",
  "headers": {},
  "idle_timeout": "15s",
  "ping_timeout": "15s"
}
```

!!! warning "Difference from v2ray-core"

    TLS is not enforced. If TLS is not configured, plain HTTP 1.1 is used.

#### host

List of host domain.

The client will choose randomly and the server will verify if not empty.

#### path

!!! warning

    V2Ray's documentation says that the path between the server and the client must be consistent, 
    but the actual code allows the client to add any suffix to the path.
    sing-box uses the same behavior as V2Ray, but note that the behavior does not exist in `WebSocket` and `HTTPUpgrade` transport.

Path of HTTP request.

The server will verify.

#### method

Method of HTTP request.

The server will verify if not empty.

#### headers

Extra headers of HTTP request.

The server will write in response if not empty.

#### idle_timeout

In HTTP2 server:

Specifies the time until idle clients should be closed with a GOAWAY frame. PING frames are not considered as activity.

In HTTP2 client:

Specifies the period of time after which a health check will be performed using a ping frame if no frames have been
received on the connection.Please note that a ping response is considered a received frame, so if there is no other
traffic on the connection, the health check will be executed every interval. If the value is zero, no health check will
be performed.

Zero is used by default.

#### ping_timeout

In HTTP2 client:

Specifies the timeout duration after sending a PING frame, within which a response must be received.
If a response to the PING frame is not received within the specified timeout duration, the connection will be closed.
The default timeout duration is 15 seconds.

### XHTTP

```json
{
  "type": "xhttp",
  "host": "example.com",
  "path": "/xhttp",
  "mode": "auto",
  "headers": {},
  "http_version": "2",
  "no_grpc_header": false,
  "no_sse_header": false,
  "max_post_size": 1000000,
  "max_buffered_posts": 30
}
```

Supports inbound and outbound connections using Xray's default XHTTP wire format:
POST bodies carry uploads; the session ID and packet sequence are placed in the path.

#### host

HTTP Host sent by the client. The server checks this value when configured.
It does not change the TLS server name; configure `tls.server_name` separately.

#### path

Request path, default `/`. A leading and trailing slash are added when missing.
The client preserves query parameters. The server checks the normalized path prefix.

#### mode

* `packet-up`: separate GET download and sequenced POST uploads. Supports HTTP/1.1 and HTTP/2.
* `stream-up`: separate GET download and streaming POST upload. Requires HTTP/2.
* `stream-one`: a single bidirectional streaming POST. Requires HTTP/2.
* `auto` (default): the client selects `stream-up` for HTTP/2 and `packet-up` for HTTP/1.1;
  the server accepts all three modes.

#### headers

Additional request headers on the client or response headers on the server.
Protocol headers, including padding, take precedence.

#### http_version

Client HTTP version: `1.1` or `2`. Defaults to `2` with TLS and `1.1` without TLS.
Setting `2` without TLS uses HTTP/2 prior knowledge (h2c).
The server accepts both HTTP versions regardless of this field.

#### no_grpc_header

Do not set `Content-Type: application/grpc` on streaming upload requests.

#### no_sse_header

Do not set `Content-Type: text/event-stream` on download responses.

#### max_post_size

Maximum upload POST size in bytes, default `1000000`. Valid range: 1–16777216.
The client's value must not exceed the server's limit.

#### max_buffered_posts

Server limit for outstanding packet sequences per session, default `30`.
Valid range: 1–1024. Duplicate, stale, or too-far-ahead sequences are rejected.
Sessions without a download request expire after 30 seconds; at most 1024 sessions
may exist at once. Buffered upload packets across all sessions are limited to 64 MiB.

#### Compatibility

Compatible with the default path/body framing and Referer padding used by Xray.
HTTP/3, separate download settings, XMUX tuning, custom padding, and alternate
session/sequence/data placements are not implemented. Xray `extra` objects are
not accepted; use the sing-box fields above. This transport does not enable
VLESS Vision flow support over XHTTP.

### WebSocket

```json
{
  "type": "ws",
  "path": "",
  "headers": {},
  "max_early_data": 0,
  "early_data_header_name": ""
}
```

#### path

Path of HTTP request.

The server will verify.

#### headers

Extra headers of HTTP request.

The server will write in response if not empty.

#### max_early_data

Allowed payload size is in the request. Enabled if not zero.

#### early_data_header_name

Early data is sent in path instead of header by default.

To be compatible with Xray-core, set this to `Sec-WebSocket-Protocol`.

It needs to be consistent with the server.

### QUIC

```json
{
  "type": "quic"
}
```

!!! warning "Difference from v2ray-core"

    No additional encryption support:
    It's basically duplicate encryption. And Xray-core is not compatible with v2ray-core in here.

### gRPC

!!! note ""

    standard gRPC has good compatibility but poor performance and is not included by default, see [Installation](/installation/build-from-source/#build-tags).

```json
{
  "type": "grpc",
  "service_name": "TunService",
  "idle_timeout": "15s",
  "ping_timeout": "15s",
  "permit_without_stream": false
}
```

#### service_name

Service name of gRPC.

#### idle_timeout

In standard gRPC server/client:

If the transport doesn't see any activity after a duration of this time,
it pings the client to check if the connection is still active.

In default gRPC server/client:

It has the same behavior as the corresponding setting in HTTP transport.

#### ping_timeout

In standard gRPC server/client:

The timeout that after performing a keepalive check, the client will wait for activity.
If no activity is detected, the connection will be closed.

In default gRPC server/client:

It has the same behavior as the corresponding setting in HTTP transport.

#### permit_without_stream

In standard gRPC client:

If enabled, the client transport sends keepalive pings even with no active connections.
If disabled, when there are no active connections, `idle_timeout` and `ping_timeout` will be ignored and no keepalive
pings will be sent.

Disabled by default.

### HTTPUpgrade

```json
{
  "type": "httpupgrade",
  "host": "",
  "path": "",
  "headers": {}
}
```

#### host

Host domain.

The server will verify if not empty.

#### path

Path of HTTP request.

The server will verify.

#### headers

Extra headers of HTTP request.

The server will write in response if not empty.
