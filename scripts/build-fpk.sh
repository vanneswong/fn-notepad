#!/usr/bin/env bash
# Build a .fpk package for fnOS (飞牛 OS).
#
#   ./scripts/build-fpk.sh
#
# Produce a single architecture-independent package: the app payload is packed
# into app.tgz (amd64 + arm64 + armv7 binaries, chosen at runtime by cmd/main),
# so the manifest can declare platform = all.
#
# Package layout (matches what fnOS installs into /var/apps/<appname>):
#
#   manifest            key = value 清单（appname / version / platform 必填）
#   ICON.PNG            64x64  图标
#   ICON_256.PNG        256x256 图标
#   cmd/main            生命周期脚本: main start|stop|status
#   cmd/install_init    install_callback / upgrade_init / upgrade_callback /
#   cmd/uninstall_init  uninstall_callback —— 占位脚本
#   config/privilege    运行身份
#   config/resource     资源声明
#   wizard/             安装向导目录（可为空）
#   app.tgz             应用载荷，解压后落到 ${TRIM_APPDEST}
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)

manifest_value() { sed -n "s/^ *$1 *= *//p" "$ROOT/packaging/manifest" | head -1 | tr -d '\r'; }

APPNAME=$(manifest_value appname)
VERSION=$(manifest_value version)

export CGO_ENABLED=0 GOOS=linux GOFLAGS=-mod=mod
export GOCACHE=${GOCACHE:-/tmp/go-cache}

DIST="$ROOT/dist"
STAGE="$DIST/stage"
PAYLOAD="$STAGE/payload"
OUT="$DIST/$APPNAME-$VERSION.fpk"

rm -rf "$STAGE"
mkdir -p "$PAYLOAD/ui/images" "$STAGE/cmd" "$STAGE/config" "$STAGE/wizard"

echo "==> building binaries ($APPNAME $VERSION)"
build() {
  local name=$1 goarch=$2 goarm=${3:-}
  GOARCH="$goarch" GOARM="$goarm" go build -buildvcs=false -trimpath \
    -ldflags "-s -w -X main.version=$VERSION" \
    -o "$PAYLOAD/notepad-$name" "$ROOT/cmd/notepad"
  echo "    notepad-$name  ($(du -h "$PAYLOAD/notepad-$name" | cut -f1))"
}

build amd64 amd64
build arm64 arm64
build armv7 arm 7

echo "==> generating icons"
env -u GOOS -u GOARCH -u GOARM \
  go run -buildvcs=false "$ROOT/tools/icongen" -out "$DIST/icons" >/dev/null
cp "$DIST/icons/icon_64.png"  "$STAGE/ICON.PNG"
cp "$DIST/icons/icon_256.png" "$STAGE/ICON_256.PNG"
# ui/config 的 icon 字段是 images/icon_{0}.png（官方约定，{0} 由飞牛替换成
# 64 / 256），所以必须提供下划线命名的 icon_64.png 与 icon_256.png；
# 连字符版本一并保留，兼容旧写法。
cp "$DIST/icons/icon_64.png"  "$PAYLOAD/ui/images/icon_64.png"
cp "$DIST/icons/icon_256.png" "$PAYLOAD/ui/images/icon_256.png"
cp "$DIST/icons/icon_64.png"  "$PAYLOAD/ui/images/icon-64.png"
cp "$DIST/icons/icon_256.png" "$PAYLOAD/ui/images/icon-256.png"

echo "==> assembling package"
cp "$ROOT/packaging/manifest" "$STAGE/manifest"
cp "$ROOT"/packaging/cmd/* "$STAGE/cmd/"
cp "$ROOT"/packaging/config/* "$STAGE/config/"
cp "$ROOT"/packaging/app/ui/* "$PAYLOAD/ui/"
chmod 0755 "$STAGE"/cmd/* "$PAYLOAD"/notepad-*

# 桌面入口是官方文档里最容易写错的地方，打包时就卡住，别等装上才发现
# 没有「打开」按钮。参考 developer.fnnas.com/docs/core-concepts/app-entry/。
echo "==> verifying desktop entry"
UI_CONFIG="$PAYLOAD/ui/config"
grep -q '"\.url"' "$UI_CONFIG" || {
  echo "ui/config: 缺少 .url 包装，飞牛不会生成桌面入口" >&2; exit 1; }
grep -q '"notepad\.main"' "$UI_CONFIG" || {
  echo "ui/config: 缺少 notepad.main 入口" >&2; exit 1; }
if command -v python3 >/dev/null 2>&1; then
  python3 - "$UI_CONFIG" <<'PY' || exit 1
import json, sys
cfg = json.load(open(sys.argv[1], encoding="utf-8"))
entry = cfg[".url"]["notepad.main"]
for key in ("title", "icon", "type", "url", "allUsers"):
    assert key in entry, "notepad.main 缺少字段: " + key
assert entry["icon"] == "images/icon_{0}.png", "icon 应为 images/icon_{0}.png"
assert entry["type"] in ("iframe", "url"), "type 只能是 iframe 或 url"
if "gatewayPrefix" in entry:
    # 网关入口（HTTPS 桌面必需）：protocol/port 会被忽略，靠这两个字段路由。
    assert entry.get("gatewaySocket", "").endswith(".sock"), \
        "gatewaySocket 应形如 app.sock"
    assert entry["url"].startswith(entry["gatewayPrefix"]), \
        "url 必须在 gatewayPrefix 下"
else:
    for key in ("protocol", "port"):
        assert key in entry, "端口入口缺少字段: " + key
PY
fi

png_dim() {
  od -An -tu1 -j16 -N8 "$1" |
    awk '{printf "%dx%d", $1*16777216+$2*65536+$3*256+$4, $5*16777216+$6*65536+$7*256+$8}'
}
[ "$(png_dim "$STAGE/ICON.PNG")" = "64x64" ] || {
  echo "ICON.PNG 必须是严格 64x64" >&2; exit 1; }
[ "$(png_dim "$STAGE/ICON_256.PNG")" = "256x256" ] || {
  echo "ICON_256.PNG 必须是严格 256x256" >&2; exit 1; }

# The payload is shipped as app.tgz; fnOS unpacks it into ${TRIM_APPDEST}.
payload_files=$(cd "$PAYLOAD" && ls)
( cd "$PAYLOAD" && tar -czf "$STAGE/app.tgz" $payload_files )

# List entries explicitly: some validators look for an entry named exactly
# "manifest" and choke on the "./" prefix that "." produces.
FILES="manifest cmd config wizard app.tgz ICON.PNG ICON_256.PNG"
( cd "$STAGE" && tar -czf "$OUT" $FILES )

echo "==> $OUT"
echo
echo "    package:"
tar -tzf "$OUT" | sed 's/^/        /'
echo "    desktop entry:"
sed 's/^/        /' "$PAYLOAD/ui/config"
echo "    app.tgz:"
tar -tzf "$STAGE/app.tgz" | sed 's/^/        /'
