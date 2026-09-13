#!/usr/bin/env bash
# LeanClash 原生部署（不含容器镜像）。
#   开发机:  ./deploy.sh --pack
#   服务器:  sudo ./deploy.sh --install
#            sudo ./deploy.sh --remove
#            sudo ./deploy.sh --remove --purge
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BUILD_DIR="${BUILD_DIR:-$SCRIPT_DIR/build}"

BIN_DST=/usr/local/bin/leanclash
MIHOMO_DST=/usr/local/bin/mihomo
SYSTEMD_DIR=/etc/systemd/system
ETC_MIHOMO=/etc/mihomo
OPT_DIR=/opt/leanclash
DATA_DIR=/var/lib/mihomo
LOG_DIR=/var/log/mihomo

usage() {
    cat <<EOF
Usage: $0 --pack | --install | --remove [--purge]

  --pack              在开发机打包原生部署所需文件（带时间戳）
  --install           在服务器安装二进制、systemd 单元、用户与目录
  --remove            停止服务并移除程序文件（保留配置与数据）
  --remove --purge    同时删除 /opt/leanclash、/etc/mihomo、数据目录

Environment:
  BUILD_DIR     打包输出目录（默认 <repo>/build）
  TARGET_ARCH   包名中的架构（默认由 uname -m 推断）
EOF
}

log() { printf '[INFO] %s\n' "$*"; }
die() { printf '[ERROR] %s\n' "$*" >&2; exit 1; }

need_cmd() { command -v "$1" >/dev/null 2>&1 || die "$1 is required"; }

require_root() {
    if [ "$(id -u)" -ne 0 ]; then
        die "需要 root 权限（请使用 sudo $0 ${1:-}）"
    fi
}

arch_name() {
    local arch="${TARGET_ARCH:-$(uname -m)}"
    case "$arch" in
        x86_64|amd64) printf 'amd64' ;;
        aarch64|arm64) printf 'arm64' ;;
        *) printf '%s' "$arch" ;;
    esac
}

# 打包目录布局，或仓库源码布局。
resolve_payload() {
    if [ -x "$SCRIPT_DIR/bin/leanclash" ]; then
        PAYLOAD_BIN="$SCRIPT_DIR/bin/leanclash"
        PAYLOAD_UNIT_LC="$SCRIPT_DIR/systemd/leanclash.service"
        PAYLOAD_UNIT_MH="$SCRIPT_DIR/systemd/mihomo@.service"
        PAYLOAD_GENERAL="$SCRIPT_DIR/etc-mihomo/config_general.yaml"
        PAYLOAD_MIHOMO="$SCRIPT_DIR/bin/mihomo"
        return
    fi
    PAYLOAD_BIN="$SCRIPT_DIR/build/leanclash"
    PAYLOAD_UNIT_LC="$SCRIPT_DIR/deploy/leanclash.service"
    PAYLOAD_UNIT_MH="$SCRIPT_DIR/deploy/mihomo@.service"
    PAYLOAD_GENERAL="$SCRIPT_DIR/deploy/etc-mihomo/config_general.yaml"
    PAYLOAD_MIHOMO="$SCRIPT_DIR/build/mihomo"
}

# ─── pack ───────────────────────────────────────────────────

do_pack() {
    need_cmd tar
    local bin="$SCRIPT_DIR/build/leanclash"
    if [ ! -x "$bin" ]; then
        [ -x "$SCRIPT_DIR/build.sh" ] || die "找不到二进制 $bin，且没有 build.sh"
        log "二进制不存在，先执行 ./build.sh binary"
        "$SCRIPT_DIR/build.sh" binary
    fi
    [ -x "$bin" ] || die "找不到可执行文件: $bin"

    local unit_lc="$SCRIPT_DIR/deploy/leanclash.service"
    local unit_mh="$SCRIPT_DIR/deploy/mihomo@.service"
    local general="$SCRIPT_DIR/deploy/etc-mihomo/config_general.yaml"
    [ -f "$unit_lc" ] || die "缺少 $unit_lc"
    [ -f "$unit_mh" ] || die "缺少 $unit_mh"
    [ -f "$general" ] || die "缺少 $general"

    local stamp arch name stage archive
    stamp="$(date +%Y%m%d_%H%M%S)"
    arch="$(arch_name)"
    name="leanclash-native-${arch}-${stamp}"
    stage="$BUILD_DIR/$name"
    archive="$BUILD_DIR/${name}.tar.gz"

    mkdir -p "$BUILD_DIR" "$stage/bin" "$stage/systemd" "$stage/etc-mihomo"
    install -m 0755 "$SCRIPT_DIR/deploy.sh" "$stage/deploy.sh"
    install -m 0755 "$bin" "$stage/bin/leanclash"
    install -m 0644 "$unit_lc" "$stage/systemd/leanclash.service"
    install -m 0644 "$unit_mh" "$stage/systemd/mihomo@.service"
    install -m 0644 "$general" "$stage/etc-mihomo/config_general.yaml"
    if [ -x "$SCRIPT_DIR/build/mihomo" ]; then
        install -m 0755 "$SCRIPT_DIR/build/mihomo" "$stage/bin/mihomo"
        log "已打入 mihomo 内核"
    else
        log "未找到 build/mihomo，包内不含内核（服务器需已安装 /usr/local/bin/mihomo）"
    fi

    {
        echo "name=$name"
        echo "packed_at=$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
        echo
        "$bin" info
    } >"$stage/MANIFEST.txt"

    tar -C "$BUILD_DIR" -czf "$archive" "$name"
    rm -rf "$stage"
    log "打包完成: $archive ($(du -h "$archive" | cut -f1))"
}

# ─── install ────────────────────────────────────────────────

ensure_user() {
    if id mihomo >/dev/null 2>&1; then
        log "系统用户 mihomo 已存在 (uid=$(id -u mihomo) gid=$(id -g mihomo))"
        return
    fi
    useradd --system --shell /usr/sbin/nologin --home-dir "$DATA_DIR" --no-create-home mihomo
    log "已创建系统用户 mihomo (uid=$(id -u mihomo) gid=$(id -g mihomo))"
}

do_install() {
    require_root --install
    need_cmd systemctl
    resolve_payload

    [ -x "$PAYLOAD_BIN" ] || die "找不到 leanclash 二进制（请先在开发机 ./deploy.sh --pack 并解压）"
    [ -f "$PAYLOAD_UNIT_LC" ] || die "找不到 $PAYLOAD_UNIT_LC"
    [ -f "$PAYLOAD_UNIT_MH" ] || die "找不到 $PAYLOAD_UNIT_MH"
    [ -f "$PAYLOAD_GENERAL" ] || die "找不到 $PAYLOAD_GENERAL"

    ensure_user

    log "安装二进制 $BIN_DST"
    install -m 0755 -o root -g root "$PAYLOAD_BIN" "$BIN_DST"

    if [ -x "$PAYLOAD_MIHOMO" ]; then
        if [ -x "$MIHOMO_DST" ]; then
            log "保留已有 $MIHOMO_DST"
        else
            log "安装 mihomo 内核 $MIHOMO_DST"
            install -m 0755 -o root -g root "$PAYLOAD_MIHOMO" "$MIHOMO_DST"
        fi
    elif [ ! -x "$MIHOMO_DST" ]; then
        die "服务器没有 $MIHOMO_DST，且安装包内不含 mihomo。请先安装 mihomo 内核。"
    fi

    log "安装 systemd 单元"
    install -m 0644 -o root -g root "$PAYLOAD_UNIT_LC" "$SYSTEMD_DIR/leanclash.service"
    install -m 0644 -o root -g root "$PAYLOAD_UNIT_MH" "$SYSTEMD_DIR/mihomo@.service"

    install -d -m 0755 -o root -g root "$ETC_MIHOMO" "$OPT_DIR"
    install -d -m 0750 -o mihomo -g mihomo "$DATA_DIR" "$LOG_DIR"

    if [ -f "$ETC_MIHOMO/config_general.yaml" ]; then
        log "保留已有 $ETC_MIHOMO/config_general.yaml"
    else
        install -m 0644 -o root -g root "$PAYLOAD_GENERAL" "$ETC_MIHOMO/config_general.yaml"
        log "已写入模板 $ETC_MIHOMO/config_general.yaml"
    fi

    systemctl daemon-reload
    systemctl enable --now leanclash
    log "已启动 leanclash。面板默认 http://<host>:8081"
}

# ─── remove ─────────────────────────────────────────────────

stop_units() {
    systemctl disable --now leanclash 2>/dev/null || true
    local units
    units="$(systemctl list-units --all --no-legend 'mihomo@*.service' 2>/dev/null | awk '{print $1}' || true)"
    if [ -n "$units" ]; then
        # shellcheck disable=SC2086
        systemctl stop $units 2>/dev/null || true
    fi
}

do_remove() {
    require_root --remove
    need_cmd systemctl

    local purge=false
    for arg in "$@"; do
        case "$arg" in
            --purge) purge=true ;;
            --remove) ;;
            *) die "未知参数: $arg" ;;
        esac
    done

    log "停止 LeanClash 与 mihomo@ 实例"
    stop_units

    rm -f "$BIN_DST"
    rm -f "$SYSTEMD_DIR/leanclash.service" "$SYSTEMD_DIR/mihomo@.service"
    rm -rf /run/leanclash
    systemctl daemon-reload
    log "已移除 $BIN_DST 与 systemd 单元（未删除 mihomo 内核）"

    if [ "$purge" = true ]; then
        rm -rf "$OPT_DIR" "$DATA_DIR" "$LOG_DIR"
        rm -f "$ETC_MIHOMO"/config_general.yaml "$ETC_MIHOMO"/config_*.yaml
        rmdir "$ETC_MIHOMO" 2>/dev/null || true
        log "已清除配置与数据目录"
    else
        log "配置与数据已保留: $OPT_DIR $ETC_MIHOMO $DATA_DIR"
        log "若需一并删除，使用: sudo $0 --remove --purge"
    fi
}

# ─── dispatch ───────────────────────────────────────────────

case "${1:-}" in
    --pack) do_pack ;;
    --install) do_install ;;
    --remove) shift; do_remove "$@" ;;
    -h|--help|help) usage ;;
    "") usage >&2; exit 2 ;;
    *) usage >&2; die "未知参数: $1" ;;
esac
