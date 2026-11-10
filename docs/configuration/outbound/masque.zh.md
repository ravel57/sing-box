---
icon: material/new-box
---

!!! question "自 sing-box 1.14.5 起"

### 结构

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

  ... // 拨号字段
}
```

使用 CONNECT-IP（[RFC 9484](https://www.rfc-editor.org/rfc/rfc9484)）的 MASQUE 客户端，基于 HTTP/3 数据报，
启用 `http2` 时基于 HTTP/2 capsule。服务器分配隧道地址，TCP 和 UDP 流量经用户态网络栈以 IP 包形式传输，
因此需要 `with_gvisor` 和 `with_quic` 构建标签。兼容 xray-core 的 MASQUE 入站。

不支持 CONNECT-UDP（[RFC 9298](https://www.rfc-editor.org/rfc/rfc9298)）和 Cloudflare WARP。
域名目标由 sing-box DNS 路由在隧道外解析。

### 字段

#### server

==必填==

服务器地址。

#### server_port

服务器端口。默认 443。

#### username

HTTP Basic 认证用户名。

#### password

HTTP Basic 认证密码。

#### http2

使用基于 TCP 的 HTTP/2（ALPN `h2`）代替基于 QUIC 的 HTTP/3（ALPN `h3`）。

#### host

覆盖 CONNECT-IP 请求的 authority。默认 `tls.server_name`（端口不是 443 时附带端口）。

#### path

请求路径。默认 `/.well-known/masque/ip/*/*/`。

#### headers

CONNECT-IP 请求的额外 HTTP 头。

#### keep_alive

QUIC 保活周期或 HTTP/2 PING 间隔。默认 10s。

#### tls

==必填==

TLS 配置, 参阅 [TLS](/zh/configuration/shared/tls/#出站)。

### 拨号字段

参阅 [拨号字段](/zh/configuration/shared/dial/)。
