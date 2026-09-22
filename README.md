# sing-box

The universal proxy platform.

[![Packaging status](https://repology.org/badge/vertical-allrepos/sing-box.svg)](https://repology.org/project/sing-box/versions)

## Documentation

https://sing-box.sagernet.org

## License

```
Copyright (C) 2022 by nekohasekai <contact-sagernet@sekai.icu>

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with this program. If not, see <http://www.gnu.org/licenses/>.

In addition, no derivative work may use the name or imply association
with this application without prior consent.
```

`iOS build`
```bash
gomobile bind \                                            
-target=ios \
-tags "with_gvisor,with_naive_outbound,with_quic,with_wireguard,with_utls,with_dhcp,with_clash_api" \
-o Libbox.xcframework \
./experimental/libbox
```


`tvOS build`
```bash
gomobile bind \
-target=tvos,tvossimulator \
-tags "with_gvisor,with_naive_outbound,with_quic,with_wireguard,with_utls,with_dhcp,with_clash_api" \
-o Libbox.xcframework \
./experimental/libbox
```


`android`
```bash
gomobile bind \
  -target=android \
  -androidapi=21 \
  -ldflags="-checklinkname=0" \
  -o libbox.aar \
  ./experimental/libbox
```
