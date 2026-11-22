# Установка кастомной сборки sing-box (ravel57/sing-box) на OpenWrt (aarch64)

Собранный бинарник: `dist-openwrt/sing-box-arm64` для `linux/arm64`,
**полностью статический** (`CGO_ENABLED=0`), версия `v1.14.4-6-g5a7fdb1bf`.

Теги сборки: `with_gvisor,with_quic,with_dhcp,with_wireguard,with_utls,with_acme,
with_clash_api,with_tailscale,with_ccm,with_ocm,with_cloudflared,with_usbip,
with_openvpn,with_openconnect`.

Работают все протоколы из конфигов ultunnel **кроме `naive`**:
hysteria2, tuic, wireguard, anytls, masque, shadowsocks(+shadowtls),
trusttunnel, vless, vmess. `naive` требует линковки с cronet (Chromium
network stack) — сборка с ним описана в конце этого файла, в разделе
«Как добавить naive позже».

SHA-256: `d7448a95b166baa0d61766bd0b2dc12b04853ce7f0e7a5f9689a83b0fe622d8f`

## 1. Скопировать бинарник на роутер

```bash
scp dist-openwrt/sing-box-arm64 root@<router-ip>:/usr/bin/sing-box
```

Если на роутере уже стоит штатный sing-box (из репозитория OpenWrt) —
сначала сделайте бэкап:

```bash
ssh root@<router-ip> 'cp /usr/bin/sing-box /usr/bin/sing-box.bak 2>/dev/null; true'
```

## 2. Права на исполнение

```bash
ssh root@<router-ip> 'chmod 755 /usr/bin/sing-box'
```

## 3. Проверить, что бинарник живой

```bash
ssh root@<router-ip> 'sing-box version'
```

## 4. init.d / UCI (если ещё не настроено)

Если на роутере ещё нет `/etc/init.d/sing-box`, возьмите из этого репозитория:

- `release/config/openwrt.init` → `/etc/init.d/sing-box` (chmod 755)
- `release/config/openwrt.conf` → `/etc/config/sing-box`

```bash
scp release/config/openwrt.init root@<router-ip>:/etc/init.d/sing-box
scp release/config/openwrt.conf root@<router-ip>:/etc/config/sing-box
ssh root@<router-ip> 'chmod 755 /etc/init.d/sing-box; mkdir -p /usr/share/sing-box /etc/sing-box'
ssh root@<router-ip> '/etc/init.d/sing-box enable'
```

UCI-конфиг включает сервис (`option enabled '1'`) и указывает путь к конфигу
`/etc/sing-box/config.json` — это тот же путь, что ждёт `openwrt-parser-go`
(`APP_SINGBOX_CONFIG_PATH`, по умолчанию `/etc/sing-box/config.json`).

**Если на роутере уже есть свой механизм запуска** (например кастомный
`/usr/libexec/lan-mode-vpn-setup`, который использует `openwrt-parser-go` —
`APP_SINGBOX_APPLY_COMMAND`) — ничего менять не нужно, он продолжит работать
с новым бинарником, просто проверьте, что он не перетирает установленный
только что `/usr/bin/sing-box`.

## 5. Зависимости (пакеты OpenWrt)

Эта сборка требует (как и официальный пакет sing-box для OpenWrt):

```bash
ssh root@<router-ip> 'opkg update && opkg install ca-bundle kmod-inet-diag kmod-tun firewall4 kmod-nft-queue'
```

(или `apk add ...` на OpenWrt 24.10+ с apk).

## 6. Применить конфиг и перезапустить

Через UI/бэкенд `openwrt-parser-go` (вкладка sing-box) — выберите сервер и
протокол, бэкенд сам положит `/etc/sing-box/config.json`, проверит его
(`sing-box check`) и перезапустит сервис.

Вручную:

```bash
ssh root@<router-ip> 'sing-box check -c /etc/sing-box/config.json && /etc/init.d/sing-box restart'
```

## 7. Проверка, что процесс работает

```bash
ssh root@<router-ip> 'pidof sing-box && logread | grep sing-box | tail -30'
```

## Откат

```bash
ssh root@<router-ip> 'mv /usr/bin/sing-box.bak /usr/bin/sing-box && /etc/init.d/sing-box restart'
```

## Как была собрана эта версия (без naive)

```bash
cd sing-box   # этот репозиторий
BUILD_TAGS=$(cat release/DEFAULT_BUILD_TAGS_OTHERS)
LDFLAGS_SHARED=$(cat release/LDFLAGS)
VERSION=$(git describe --tags --always)
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 \
go build -v -trimpath -o dist-openwrt/sing-box-arm64 \
  -tags "$BUILD_TAGS" \
  -ldflags "-X 'github.com/sagernet/sing-box/constant.Version=${VERSION}' ${LDFLAGS_SHARED} -s -w -buildid=" \
  ./cmd/sing-box
```

Чисто Go (CGO отключен), кросс-компилируется прямо на Mac без Docker/toolchain —
занимает пару минут.

## Как добавить naive позже

Для `naive` нужна линковка с cronet (Chromium network stack) через
`github.com/sagernet/cronet-go` — официальный CI (`.github/workflows/build.yml`,
джоба `build`, `variant: musl, naive: true`) делает это так:

1. Клонировать `sagernet/cronet-go` на коммит из `.github/CRONET_GO_VERSION`,
   с submodule `naiveproxy` (большой, тянет Chromium-инфраструктуру сборки).
2. `go run ./cmd/build-naive --target=linux/arm64 --libc=musl download-toolchain`
   — скачивает clang + sysroot (toolchain всегда **x86_64-хостовый**, даже
   под arm64-таргет).
3. `go run ./cmd/build-naive --target=linux/arm64 --libc=musl env --export`
   — даёт `CC`/`CXX`/`CGO_LDFLAGS` для кросс-линковки.
4. `CGO_ENABLED=1 GOOS=linux GOARCH=arm64 go build -tags "$(cat release/DEFAULT_BUILD_TAGS),with_musl" ...`

Мы пробовали это в Docker (`--platform=linux/amd64`, т.к. toolchain x86_64-only):
сборка стабильно падала без явной ошибки на последнем шаге — похоже, не
хватало памяти VM Docker Desktop (7.65 ГБ). Чтобы повторить:

- Увеличьте память Docker Desktop (Settings → Resources) хотя бы до 12 ГБ
  и перезапустите Docker Desktop (это остановит ваши текущие контейнеры —
  minio/elasticsearch/redis/postgres, если они ещё работают).
- Либо соберите через GitHub Actions в вашем форке — `build.yml` уже всё
  умеет (`workflow_dispatch`, build type `Binary`), просто нужен `gh` CLI
  или ручной запуск через веб-интерфейс GitHub, и скачать артефакт
  `sing-box_<version>_linux_aarch64_cortex-a53` (или `_generic`) из релиза/рана.
