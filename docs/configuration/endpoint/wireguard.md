!!! quote "Changes in sing-box 1.14.0"

    :material-plus: [udp_mapping](/configuration/shared/udp-nat/#udp_mapping)  
    :material-plus: [udp_filtering](/configuration/shared/udp-nat/#udp_filtering)  
    :material-plus: [udp_nat_max](/configuration/shared/udp-nat/#udp_nat_max)

!!! question "Since sing-box 1.11.0"

### Structure

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

  ... // UDP NAT Fields

  "workers": 0,
 
  ... // Dial Fields
}
```

!!! note ""

    You can ignore the JSON Array [] tag when the content is only one item

### Fields

#### system

Use system interface.

Requires privilege and cannot conflict with exists system interfaces.

#### name

Custom interface name for system interface.

#### mtu

WireGuard MTU.

`1408` will be used by default.

#### address

==Required==

List of IP (v4 or v6) address prefixes to be assigned to the interface.

#### private_key

==Required==

WireGuard requires base64-encoded public and private keys. These can be generated using the wg(8) utility:

```shell
wg genkey
echo "private key" || wg pubkey
```

or `sing-box generate wg-keypair`.

#### peers

==Required==

List of WireGuard peers.

#### peers.address

WireGuard peer address.

#### peers.port

WireGuard peer port.

#### peers.public_key

==Required==

WireGuard peer public key.

#### peers.pre_shared_key

WireGuard peer pre-shared key.

#### peers.allowed_ips

==Required==

WireGuard allowed IPs.

#### peers.persistent_keepalive_interval

WireGuard persistent keepalive interval, in seconds.

Disabled by default.

#### peers.reserved

WireGuard reserved field bytes.

#### workers

WireGuard worker count.

CPU count is used by default.

### AmneziaWG 3.1 Fields

!!! question "Since sing-box 1.14.5"

Setting any of these fields makes the endpoint speak [AmneziaWG](https://docs.amnezia.org/documentation/amnezia-wg/) 3.1
(through `amneziawg-go`). Every value must equal the server's. It is compatible with the AmneziaWG
server of ultunnel-server-panel, whose parameters can be pasted 1:1 (`headerProtectionKey` becomes
`header_protection_key`, and so on).

Not supported with AmneziaWG: `system`, `peers.reserved`, `workers`, and inbound port forwarding.
Domain peer addresses are resolved once at start.

```json
{
  "jc": 4,
  "jmin": 40,
  "jmax": 70,
  "s1": 20,
  "s2": 30,
  "s3": 20,
  "s4": 20,
  "h1": "100000-200000",
  "h2": "300000-400000",
  "h3": "500000-600000",
  "h4": "700000-800000",
  "i1": "<r 64>",
  "i2": "",
  "i3": "",
  "i4": "",
  "i5": "",
  "header_protection_key": "",
  "content_padding_addition": "",
  "rekey_after_time": "",
  "rekey_timeout": "",
  "reject_after_time": "",
  "keepalive_timeout": "",
  "max_handshake_attempts": "",
  "random_trailers": false,
  "disable_cookies": false
}
```

| Field | Meaning |
|-------|---------|
| `jc`, `jmin`, `jmax` | Junk packet count (0-128) and their size range in bytes; `jmin` must not exceed `jmax` |
| `s1`, `s2`, `s3` | Random padding in bytes on the handshake initiation, response and cookie reply (0-65535); `s1 + 56` must not equal `s2` |
| `s4` | Padding on transport packets (0-32); the default `mtu` becomes `1420 - s4` (at least 1280) |
| `h1`-`h4` | Message type headers: an integer or a `low-high` range |
| `i1`-`i5` | CPS signature packets sent before the handshake, e.g. `<r 64>` |
| `header_protection_key` | Base64 32-byte key; requires every `s1`-`s4` to be at least 12 |
| `content_padding_addition` | `low-high` range of extra padding on transport packets |
| `rekey_after_time`, `rekey_timeout`, `reject_after_time`, `keepalive_timeout`, `max_handshake_attempts` | `low-high` ranges (or an integer) overriding the timing defaults |
| `random_trailers`, `disable_cookies` | 3.1 device toggles |

### Example

```json
{
  "endpoints": [
    {
      "type": "wireguard",
      "tag": "awg",
      "address": ["10.8.0.2/32"],
      "private_key": "<client private key>",
      "peers": [
        {
          "address": "awg.example.org",
          "port": 51820,
          "public_key": "<server public key>",
          "allowed_ips": ["0.0.0.0/0", "::/0"]
        }
      ],
      "jc": 4, "jmin": 40, "jmax": 70,
      "s1": 20, "s2": 30, "s3": 20, "s4": 20,
      "h1": "100000-200000", "h2": "300000-400000", "h3": "500000-600000", "h4": "700000-800000"
    }
  ]
}
```

### UDP NAT Fields

See [UDP NAT Fields](/configuration/shared/udp-nat/) for details.

### Dial Fields

See [Dial Fields](/configuration/shared/dial/) for details.
