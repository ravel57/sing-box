---
icon: material/new-box
---

!!! question "自 sing-box 1.14.5 起"

### 结构

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

  ... // 拨号字段
}
```

TrustTunnel 客户端：基于 TLS 的 HTTP CONNECT 隧道，使用 Basic 认证
（[协议规范](https://github.com/TrustTunnel/TrustTunnel/blob/master/PROTOCOL.md)）。

* TCP：每个连接使用一个 `CONNECT host:port` 流。
* UDP：所有数据包共享一个 `CONNECT _udp2` 流。域名目标在本地解析。
* 空闲超过 7 秒的会话会在打开新流前通过 `CONNECT _check` 探测；返回 `407` 时连接以认证错误失败。
* 不支持 ICMP（`_icmp`）。

### 字段

#### server

==必填==

服务器地址。

#### server_port

==必填==

服务器端口。

#### username

==必填==

TrustTunnel 用户名。

#### password

==必填==

TrustTunnel 密码。

#### quic

使用基于 QUIC 的 HTTP/3（ALPN `h3`，UDP 端口）代替基于 TLS 的 HTTP/2（ALPN `h2`）。

需要 `with_quic` 构建标签；服务器须在相同端口号上监听 HTTP/3。

#### tls

==必填==

TLS 配置, 参阅 [TLS](/zh/configuration/shared/tls/#出站)。

### 拨号字段

参阅 [拨号字段](/zh/configuration/shared/dial/)。
