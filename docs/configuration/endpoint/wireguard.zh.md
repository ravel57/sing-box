!!! quote "sing-box 1.14.0 中的更改"

    :material-plus: [udp_mapping](/zh/configuration/shared/udp-nat/#udp_mapping)  
    :material-plus: [udp_filtering](/zh/configuration/shared/udp-nat/#udp_filtering)  
    :material-plus: [udp_nat_max](/zh/configuration/shared/udp-nat/#udp_nat_max)

!!! question "自 sing-box 1.11.0 起"

### 结构

```json
{
  "type": "wireguard",
  "tag": "wg-ep",

  "system": false,
  "name": "",
  "mtu": 1408,
  "address": [],
  "private_key": "",
  "listen_port": 10000,
  "peers": [
    {
      "address": "127.0.0.1",
      "port": 10001,
      "public_key": "",
      "pre_shared_key": "",
      "allowed_ips": [],
      "persistent_keepalive_interval": 0,
      "reserved": [0, 0, 0]
    }
  ],

  ... // UDP NAT 字段

  "workers": 0,

  ... // 拨号字段
}
```

!!! note ""

    当内容只有一项时，可以忽略 JSON 数组 [] 标签

### 字段

#### system

使用系统设备。

需要特权且不能与已有系统接口冲突。

#### name

为系统接口自定义设备名称。

#### mtu

WireGuard MTU。

默认使用 1408。

#### address

==必填==

接口的 IPv4/IPv6 地址或地址段的列表。

要分配给接口的 IP（v4 或 v6）地址段列表。

#### private_key

==必填==

WireGuard 需要 base64 编码的公钥和私钥。 这些可以使用 wg(8) 实用程序生成：

```shell
wg genkey
echo "private key" || wg pubkey
```

或 `sing-box generate wg-keypair`.

#### peers

==必填==

WireGuard 对等方的列表。

#### peers.address

对等方的 IP 地址。

#### peers.port

对等方的 WireGuard 端口。

#### peers.public_key

==必填==

对等方的 WireGuard 公钥。

#### peers.pre_shared_key

对等方的预共享密钥。

#### peers.allowed_ips

==必填==

对等方的允许 IP 地址。

#### peers.persistent_keepalive_interval

对等方的持久性保持活动间隔，以秒为单位。

默认禁用。

#### peers.reserved

对等方的保留字段字节。

#### workers

WireGuard worker 数量。

默认使用 CPU 数量。

### AmneziaWG 3.1 字段

!!! question "自 sing-box 1.14.5 起"

设置以下任一字段后，端点将通过 `amneziawg-go` 使用 [AmneziaWG](https://docs.amnezia.org/documentation/amnezia-wg/) 3.1 协议，
所有取值必须与服务器一致。字段名与 amneziawg-go UAPI 一致：`jc`、`jmin`、`jmax`、`s1`-`s4`、`h1`-`h4`、`i1`-`i5`、
`header_protection_key`、`content_padding_addition`、`rekey_after_time`、`rekey_timeout`、`reject_after_time`、
`keepalive_timeout`、`max_handshake_attempts`、`random_trailers`、`disable_cookies`（详见英文文档）。

AmneziaWG 下不支持：`system`、`peers.reserved`、`workers` 和入站端口转发。域名对端仅在启动时解析一次。

### UDP NAT 字段

参阅 [UDP NAT 字段](/zh/configuration/shared/udp-nat/)。

### 拨号字段

参阅 [拨号字段](/zh/configuration/shared/dial/)。
