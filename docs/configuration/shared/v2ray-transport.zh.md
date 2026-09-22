V2Ray Transport 是 v2ray 发明的一组私有协议，并污染了其他协议的名称，如 clash 中的 `trojan-grpc`。

### 结构

```json
{
  "type": ""
}
```

可用的传输协议：

* HTTP
* XHTTP
* WebSocket
* QUIC
* gRPC
* HTTPUpgrade

!!! warning "与 v2ray-core 的区别"

    * 没有 TCP 传输层, 纯 HTTP 已合并到 HTTP 传输层。
    * 没有 mKCP 传输层。
    * 没有 DomainSocket 传输层。

!!! note ""

    当内容只有一项时，可以忽略 JSON 数组 [] 标签。

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

!!! warning "与 v2ray-core 的区别"

    不强制执行 TLS。如果未配置 TLS，将使用纯 HTTP 1.1。

#### host

主机域名列表。

如果设置，客户端将随机选择，服务器将验证。

#### path

!!! warning

    V2Ray 文档称服务端和客户端的路径必须一致，但实际代码允许客户端向路径添加任何后缀。
    sing-box 使用与 V2Ray 相同的行为，但请注意，该行为在 `WebSocket` 和 `HTTPUpgrade` 传输层中不存在。

HTTP 请求路径

服务器将验证。

#### method

HTTP 请求方法

如果设置，服务器将验证。

#### headers

HTTP 请求的额外标头

如果设置，服务器将写入响应。

#### idle_timeout

在 HTTP2 服务器中：

指定闲置客户端应在多长时间内使用 GOAWAY 帧关闭。PING 帧不被视为活动。

在 HTTP2 客户端中：

如果连接上没有收到任何帧，指定一段时间后将使用 PING 帧执行健康检查。需要注意的是，PING 响应被视为已接收的帧，因此如果连接上没有其他流量，则健康检查将在每个间隔执行一次。如果值为零，则不会执行健康检查。

默认使用零。

#### ping_timeout

在 HTTP2 客户端中：

指定发送 PING 帧后，在指定的超时时间内必须接收到响应。如果在指定的超时时间内没有收到 PING 帧的响应，则连接将关闭。默认超时持续时间为 15 秒。

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

支持入站和出站，使用 Xray 默认的 XHTTP 格式：上传数据位于 POST 请求体中，
会话 ID 和数据包序号位于路径中。

#### host

客户端发送的 HTTP Host；服务端在配置后进行检查。
TLS 服务器名称需要单独通过 `tls.server_name` 配置。

#### path

请求路径，默认为 `/`。自动补充开头和结尾的 `/`。
客户端保留查询参数，服务端检查规范化的路径前缀。

#### mode

* `packet-up`：GET 下载和带序号的 POST 上传，支持 HTTP/1.1 和 HTTP/2。
* `stream-up`：独立的 GET 下载和流式 POST 上传，需要 HTTP/2。
* `stream-one`：单个双向流式 POST，需要 HTTP/2。
* `auto`（默认）：客户端在 HTTP/2 下使用 `stream-up`，HTTP/1.1 下使用 `packet-up`；服务端接受所有模式。

#### headers

客户端的额外请求头或服务端的额外响应头。协议头（包括填充）优先。

#### http_version

客户端 HTTP 版本：`1.1` 或 `2`。启用 TLS 时默认为 `2`，否则为 `1.1`。
不使用 TLS 时设置为 `2` 将使用 h2c prior knowledge。
服务端始终接受这两种版本，此字段不限制服务端。

#### no_grpc_header

不在流式上传请求中设置 `Content-Type: application/grpc`。

#### no_sse_header

不在下载响应中设置 `Content-Type: text/event-stream`。

#### max_post_size

单个上传 POST 的最大字节数，默认为 `1000000`，范围为 1–16777216。
客户端值不能超过服务端限制。

#### max_buffered_posts

每个服务端会话允许的待处理数据包序号数量，默认为 `30`，范围为 1–1024。
重复、过期或超出窗口的序号将被拒绝。没有下载请求的会话在 30 秒后过期。
最多允许 1024 个会话，所有会话缓存的上传数据总量限制为 64 MiB。

#### 兼容性

支持 Xray 默认的路径/请求体格式及 Referer 填充。
暂不支持 HTTP/3、独立下载配置、XMUX 调优、自定义填充或其他会话/序号/数据位置。
不接受 Xray 的 `extra` 对象，请使用上述 sing-box 字段。
此传输不会启用 XHTTP 上的 VLESS Vision 流控支持。

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

HTTP 请求路径

服务器将验证。

#### headers

HTTP 请求的额外标头

如果设置，服务器将写入响应。

#### max_early_data

请求中允许的最大有效负载大小。默认启用。

#### early_data_header_name

默认情况下，早期数据在路径而不是标头中发送。

要与 Xray-core 兼容，请将其设置为 `Sec-WebSocket-Protocol`。

它需要与服务器保持一致。

### QUIC

```json
{
  "type": "quic"
}
```

!!! warning "与 v2ray-core 的区别"

    没有额外的加密支持：
    它基本上是重复加密。 并且 Xray-core 在这里与 v2ray-core 不兼容。

### gRPC

!!! note ""

    默认安装不包含标准 gRPC (兼容性好，但性能较差), 参阅 [安装](/zh/installation/build-from-source/#构建标记)。

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

gRPC 服务名称。

#### idle_timeout

在标准 gRPC 服务器/客户端：

如果传输在此时间段后没有看到任何活动，它会向客户端发送 ping 请求以检查连接是否仍然活动。

在默认 gRPC 服务器/客户端：

它的行为与 HTTP 传输层中的相应设置相同。

#### ping_timeout

在标准 gRPC 服务器/客户端：

经过一段时间之后，客户端将执行 keepalive 检查并等待活动。如果没有检测到任何活动，则会关闭连接。

在默认 gRPC 服务器/客户端：

它的行为与 HTTP 传输层中的相应设置相同。

#### permit_without_stream

在标准 gRPC 客户端：

如果启用，客户端传输即使没有活动连接也会发送 keepalive ping。如果禁用，则在没有活动连接时，将忽略 `idle_timeout` 和 `ping_timeout`，并且不会发送 keepalive ping。

默认禁用。

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

主机域名。

服务器将验证。

#### path

HTTP 请求路径

服务器将验证。

#### headers

HTTP 请求的额外标头。

如果设置，服务器将写入响应。
