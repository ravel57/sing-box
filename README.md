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

`linux arm 64`
```bash
TARGET_ARCH=arm64
CGO_ENABLED=0 GOOS=linux GOARCH="$TARGET_ARCH" \
    go build -trimpath \
    -tags "$(cat release/DEFAULT_BUILD_TAGS),with_purego" \
    -ldflags "-X github.com/sagernet/sing-box/constant.Version=$(git describe --tags --always --dirty)" \
    -o sing-box \
    ./cmd/sing-box

CRONET_DIR="$(go list -m -f '{{.Dir}}' "github.com/sagernet/cronet-go/lib/linux_${TARGET_ARCH}")"
cp "$CRONET_DIR/libcronet.so" ./
```

`macOS build (arm64 и x86_64)`

Ядро собирается без CGO, поэтому обе архитектуры можно собирать на любом Mac (Apple Silicon или Intel) кросс-компиляцией.
Для macOS используются теги `release/DEFAULT_BUILD_TAGS_OTHERS` (без `with_naive_outbound`).

```bash
VERSION="$(git describe --tags --always --dirty)"
TAGS="$(cat release/DEFAULT_BUILD_TAGS_OTHERS)"
LDFLAGS="$(cat release/LDFLAGS) -X github.com/sagernet/sing-box/constant.Version=${VERSION}"

# Apple Silicon (arm64)
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 \
    go build -trimpath -tags "$TAGS" -ldflags "$LDFLAGS" \
    -o sing-box-darwin-arm64 ./cmd/sing-box

# Intel (x86_64 / amd64)
CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 \
    go build -trimpath -tags "$TAGS" -ldflags "$LDFLAGS" \
    -o sing-box-darwin-amd64 ./cmd/sing-box
```

Универсальный бинарник (arm64 + x86_64 в одном файле):

```bash
lipo -create -output sing-box-darwin-universal \
    sing-box-darwin-arm64 sing-box-darwin-amd64
```

Проверка:

```bash
file sing-box-darwin-arm64 sing-box-darwin-amd64 sing-box-darwin-universal
./sing-box-darwin-arm64 version   # на Intel: ./sing-box-darwin-amd64 version
```

Если нужен `with_naive_outbound` — используйте теги `release/DEFAULT_BUILD_TAGS` + `with_purego` (как в сборке для Linux)
и положите `libcronet.dylib` рядом с бинарником:

```bash
TARGET_ARCH=arm64   # или amd64
CRONET_DIR="$(go list -m -f '{{.Dir}}' "github.com/sagernet/cronet-go/lib/darwin_${TARGET_ARCH}")"
cp "$CRONET_DIR/libcronet.dylib" ./
```
