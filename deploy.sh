#!/usr/bin/env bash
# ==============================================================================
# LeanClash 部署脚本
#
# 用法:
#   ./deploy.sh --pack [输出路径]      在开发机打包所有需要的文件（自动带时间戳）
#   ./deploy.sh --install [部署包]     在目标服务器上部署安装（需 root）
#   ./deploy.sh --remove [选项]        在目标服务器上卸载并移除文件（需 root）
# ==============================================================================
set -euo pipefail

# ---------------- 常量与路径探测 ----------------

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# 自动判断根目录（兼容根目录执行或 deploy/ 子目录执行）
if [ -f "$SCRIPT_DIR/main.go" ]; then
    ROOT_DIR="$SCRIPT_DIR"
elif [ -f "$SCRIPT_DIR/../main.go" ]; then
    ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
else
    ROOT_DIR="$SCRIPT_DIR"
fi

DESTDIR="${DESTDIR:-}"
SYSTEM_BIN_DIR="${DESTDIR}/usr/local/bin"
SYSTEM_SYSTEMD_DIR="${DESTDIR}/etc/systemd/system"
ETC_MIHOMO_DIR="${DESTDIR}/etc/mihomo"
DATA_MIHOMO_DIR="${DESTDIR}/var/lib/mihomo"
LOG_MIHOMO_DIR="${DESTDIR}/var/log/mihomo"
OPT_LEANCLASH_DIR="${DESTDIR}/opt/leanclash"
RUN_LEANCLASH_DIR="${DESTDIR}/run/leanclash"

# ---------------- 日志与提示 ----------------

log_info()    { printf '\033[34m[INFO]\033[0m %s\n' "$*"; }
log_success() { printf '\033[32m[OK]\033[0m %s\n' "$*"; }
log_warn()    { printf '\033[33m[WARN]\033[0m %s\n' "$*"; }
log_error()   { printf '\033[31m[ERROR]\033[0m %s\n' "$*" >&2; }
die()         { log_error "$*"; exit 1; }

require_root() {
    if [ -n "$DESTDIR" ]; then
        return 0
    fi
    if [ "$(id -u)" -ne 0 ]; then
        die "此操作需要 root 权限，请使用 sudo 执行: sudo $0 ${ACTION:-}"
    fi
}

usage() {
    cat <<EOF
LeanClash 部署与维护工具

用法:
  $0 --pack [--file 输出路径.tar.gz]
      在本地开发机器上打包所有需要的文件（二进制、systemd 单元、配置模板与本脚本），
      默认产物统一存放于 build/ 目录，文件名自动附加时间戳。

  sudo $0 --install [--file 部署包.tar.gz]
      在目标服务器上部署运行环境：
      - 创建 mihomo 系统用户与用户组
      - 创建并赋权必要目录 (/etc/mihomo, /var/lib/mihomo, /var/log/mihomo, /opt/leanclash)
      - 安装 leanclash 与 mihomo 二进制到 /usr/local/bin
      - 安装并初始化模板配置 /etc/mihomo/config_general.yaml（保留既有配置）
      - 安装 systemd 单元并开机自启 leanclash.service

  sudo $0 --remove [--keep-mihomo] [--keep-config] [--purge]
      在目标服务器上移除 LeanClash 服务与相关文件：
      - 停止并禁用所有相关 systemd 单元
      - 清理残留策略路由 (ip rule/route) 与 nftables 表
      - 移除服务单元与二进制文件
      选项:
        --keep-mihomo  保留 /usr/local/bin/mihomo 内核二进制
        --keep-config  保留 /etc/mihomo 配置目录
        --purge        同时删除 /opt/leanclash、数据与日志目录及 mihomo 系统用户

选项别名:
  -p, --pack       打包模式（支持 --file / -f 指定输出）
  -i, --install    安装部署模式（支持 --file / -f 指定安装包）
  -r, --remove     卸载移除模式
  -h, --help       显示帮助信息
EOF
}

# ---------------- 辅助函数 ----------------

get_timestamp() {
    date '+%Y%m%d_%H%M%S'
}

cleanup_firewall() {
    log_info "清理策略路由与 nftables 残留..."
    local pref table

    # 清除策略路由规则（TUN 与 TPROXY）
    for pref in 8999 9000 9001 9002 9010; do
        while ip rule del pref "$pref" 2>/dev/null; do
            :
        done
    done
    while ip rule del fwmark 1 table 100 2>/dev/null; do
        :
    done

    # 清除 nftables 表
    for table in "inet mihomo" "ip mihomo_tproxy4" "ip mihomo_redir_tproxy4"; do
        # shellcheck disable=SC2086
        nft delete table $table 2>/dev/null || true
    done

    # 清除专用路由表
    for table in 100 2022; do
        ip route flush table "$table" 2>/dev/null || true
    done
}

# ---------------- 打包 (--pack) ----------------

do_pack() {
    local target_out=""
    while [ $# -gt 0 ]; do
        case "$1" in
            --file|-f)
                shift
                [ $# -gt 0 ] || die "--file 缺少输出路径参数"
                target_out="$1"
                ;;
            --file=*)
                target_out="${1#*=}"
                ;;
            -*)
                log_warn "忽略未知选项: $1"
                ;;
            *)
                if [ -z "$target_out" ]; then
                    target_out="$1"
                fi
                ;;
        esac
        shift || true
    done

    local timestamp
    timestamp="$(get_timestamp)"

    log_info "开始为 LeanClash 项目准备打包..."

    # 1. 查找或构建 leanclash 二进制（严格存放于 build/ 目录）
    local leanclash_bin=""
    if [ -f "$ROOT_DIR/build/leanclash" ] && [ -x "$ROOT_DIR/build/leanclash" ]; then
        leanclash_bin="$ROOT_DIR/build/leanclash"
    fi

    if [ -z "$leanclash_bin" ]; then
        log_info "未检测到 build/leanclash 二进制，尝试自动构建..."
        if command -v go >/dev/null 2>&1; then
            mkdir -p "$ROOT_DIR/build"
            if [ -x "$ROOT_DIR/build.sh" ]; then
                "$ROOT_DIR/build.sh" binary
                leanclash_bin="$ROOT_DIR/build/leanclash"
            else
                local ver
                ver="$(git -C "$ROOT_DIR" describe --tags --exact-match 2>/dev/null || echo dev)"
                (cd "$ROOT_DIR" && go build -ldflags="-s -w -X main.version=$ver" -o "$ROOT_DIR/build/leanclash" .)
                leanclash_bin="$ROOT_DIR/build/leanclash"
            fi
        else
            die "未找到 build/leanclash 且当前环境无 Go 编译器，无法打包！请先编译出 leanclash 二进制。"
        fi
    fi

    # 检测版本号辅助命名
    local version=""
    if [ -x "$leanclash_bin" ]; then
        version="$("$leanclash_bin" info 2>/dev/null | awk '/版本:/{print $2; exit}' || true)"
    fi
    if [ -z "$version" ]; then
        version="$(git -C "$ROOT_DIR" describe --tags --exact-match 2>/dev/null || true)"
    fi

    # 默认输出包名（严格统一存放于 build/ 目录，不污染项目根目录）
    local out_file="$target_out"
    if [ -z "$out_file" ]; then
        mkdir -p "$ROOT_DIR/build"
        if [ -n "$version" ]; then
            out_file="$ROOT_DIR/build/leanclash-deploy-${version}-${timestamp}.tar.gz"
        else
            out_file="$ROOT_DIR/build/leanclash-deploy-${timestamp}.tar.gz"
        fi
    fi

    # 2. 复制编译好的二进制到 deploy/usr/local/bin
    mkdir -p "$ROOT_DIR/deploy/usr/local/bin"
    cp "$leanclash_bin" "$ROOT_DIR/deploy/usr/local/bin/leanclash"
    chmod 0755 "$ROOT_DIR/deploy/usr/local/bin/leanclash"
    log_info "  + 复制二进制: leanclash ($(du -h "$leanclash_bin" | awk '{print $1}')) -> deploy/usr/local/bin/"

    # 查找并复制 mihomo 内核（若存在）
    local mihomo_bin=""
    for candidate in "$ROOT_DIR/build/mihomo" "$ROOT_DIR/bin/mihomo"; do
        if [ -f "$candidate" ] && [ -x "$candidate" ]; then
            mihomo_bin="$candidate"
            break
        fi
    done
    if [ -z "$mihomo_bin" ] && command -v mihomo >/dev/null 2>&1; then
        mihomo_bin="$(command -v mihomo)"
    fi

    if [ -n "$mihomo_bin" ]; then
        cp "$mihomo_bin" "$ROOT_DIR/deploy/usr/local/bin/mihomo"
        chmod 0755 "$ROOT_DIR/deploy/usr/local/bin/mihomo"
        log_info "  + 复制内核:   mihomo ($(du -h "$mihomo_bin" | awk '{print $1}')) -> deploy/usr/local/bin/"
    else
        log_warn "  ! 未找到 mihomo 内核二进制，打包中将不包含 mihomo。"
        log_warn "    在目标服务器部署时需自行提供 /usr/local/bin/mihomo。"
    fi

    # 3. 校验映射目录结构完整性
    [ -f "$ROOT_DIR/deploy/etc/systemd/system/leanclash.service" ] || die "缺少必要文件: deploy/etc/systemd/system/leanclash.service"
    [ -f "$ROOT_DIR/deploy/etc/systemd/system/mihomo@.service" ] || die "缺少必要文件: deploy/etc/systemd/system/mihomo@.service"
    [ -f "$ROOT_DIR/deploy/etc/mihomo/config_general.yaml" ] || die "缺少必要文件: deploy/etc/mihomo/config_general.yaml"
    [ -f "$ROOT_DIR/deploy/opt/leanclash/manager.yaml" ] || die "缺少必要文件: deploy/opt/leanclash/manager.yaml"

    # 4. 直接打包 Linux 映射目录与部署脚本
    log_info "打包 Linux 映射目录 (etc, opt, usr, var) 与部署脚本..."
    mkdir -p "$(dirname "$out_file")"
    tar -czf "$out_file" -C "$ROOT_DIR" deploy.sh -C "$ROOT_DIR/deploy" etc opt usr var

    local file_size
    file_size="$(du -h "$out_file" | awk '{print $1}')"

    log_success "打包完成！产物路径: $out_file (大小: $file_size)"
    echo ""
    log_info "部署到远程服务器示例:"
    log_info "  1. 上传安装包:"
    log_info "     scp \"$out_file\" root@<server_ip>:/tmp/"
    log_info "  2. 在服务器上一键部署:"
    log_info "     ssh root@<server_ip> \"./deploy.sh --install --file /tmp/$(basename "$out_file")\""
}

# ---------------- 部署安装 (--install) ----------------

do_install() {
    require_root "$@"

    local pkg_arg=""
    while [ $# -gt 0 ]; do
        case "$1" in
            --file|-f)
                shift
                [ $# -gt 0 ] || die "--file 缺少部署包路径参数"
                pkg_arg="$1"
                ;;
            --file=*)
                pkg_arg="${1#*=}"
                ;;
            -*)
                log_warn "忽略未知选项: $1"
                ;;
            *)
                if [ -z "$pkg_arg" ]; then
                    pkg_arg="$1"
                fi
                ;;
        esac
        shift || true
    done

    local work_dir="$SCRIPT_DIR"
    local cleanup_work_dir=false

    # 如果传了 tar.gz 包路径，先解压到临时目录
    if [ -n "$pkg_arg" ]; then
        if [ -f "$pkg_arg" ]; then
            work_dir="$(mktemp -d)"
            cleanup_work_dir=true
            log_info "解压部署归档包: $pkg_arg ..."
            tar -xzf "$pkg_arg" -C "$work_dir"
        else
            die "指定的部署包不存在: $pkg_arg"
        fi
    fi

    if [ "$cleanup_work_dir" = true ]; then
        trap 'rm -rf "${work_dir:-}"' EXIT
    fi

    # 定位各物料文件（优先匹配 Linux 标准映射目录）
    local leanclash_src=""
    for f in "$work_dir/usr/local/bin/leanclash" "$work_dir/leanclash" "$ROOT_DIR/deploy/usr/local/bin/leanclash" "$ROOT_DIR/build/leanclash"; do
        if [ -f "$f" ] && [ -x "$f" ]; then
            leanclash_src="$f"
            break
        fi
    done
    [ -n "$leanclash_src" ] || die "未找到 leanclash 二进制文件，无法进行安装！"

    local mihomo_src=""
    for f in "$work_dir/usr/local/bin/mihomo" "$work_dir/mihomo" "$ROOT_DIR/deploy/usr/local/bin/mihomo" "$ROOT_DIR/build/mihomo"; do
        if [ -f "$f" ] && [ -x "$f" ]; then
            mihomo_src="$f"
            break
        fi
    done

    local lc_service_src=""
    for f in "$work_dir/etc/systemd/system/leanclash.service" "$work_dir/leanclash.service" "$ROOT_DIR/deploy/etc/systemd/system/leanclash.service"; do
        if [ -f "$f" ]; then
            lc_service_src="$f"
            break
        fi
    done
    [ -n "$lc_service_src" ] || die "未找到 leanclash.service 服务单元文件！"

    local mh_service_src=""
    for f in "$work_dir/etc/systemd/system/mihomo@.service" "$work_dir/mihomo@.service" "$ROOT_DIR/deploy/etc/systemd/system/mihomo@.service"; do
        if [ -f "$f" ]; then
            mh_service_src="$f"
            break
        fi
    done
    [ -n "$mh_service_src" ] || die "未找到 mihomo@.service 服务单元文件！"

    local cfg_general_src=""
    for f in "$work_dir/etc/mihomo/config_general.yaml" "$work_dir/config_general.yaml" "$ROOT_DIR/deploy/etc/mihomo/config_general.yaml"; do
        if [ -f "$f" ]; then
            cfg_general_src="$f"
            break
        fi
    done
    [ -n "$cfg_general_src" ] || die "未找到 config_general.yaml 模板配置文件！"

    local manager_yaml_src=""
    for f in "$work_dir/opt/leanclash/manager.yaml" "$work_dir/manager.yaml" "$ROOT_DIR/deploy/opt/leanclash/manager.yaml"; do
        if [ -f "$f" ]; then
            manager_yaml_src="$f"
            break
        fi
    done

    # 开始安装流程
    log_info "=================================================="
    log_info "开始在当前系统部署 LeanClash"
    log_info "=================================================="

    # 1. 用户创建
    log_info "[1/6] 创建/检查 mihomo 系统用户与用户组..."
    if [ -z "$DESTDIR" ]; then
        if ! id mihomo >/dev/null 2>&1; then
            useradd --system --shell /usr/sbin/nologin --home-dir "$DATA_MIHOMO_DIR" --no-create-home mihomo
            log_success "已创建系统用户 mihomo (UID=$(id -u mihomo), GID=$(id -g mihomo))"
        else
            log_info "系统用户 mihomo 已存在 (UID=$(id -u mihomo), GID=$(id -g mihomo))"
            usermod -d "$DATA_MIHOMO_DIR" mihomo 2>/dev/null || true
        fi
    else
        log_info "[DESTDIR 模式] 跳过真实系统用户创建"
    fi

    # 2. 文件夹创建与权限
    log_info "[2/6] 创建对应文件夹并设置权限..."
    if [ -z "$DESTDIR" ]; then
        # 配置目录: 0755 root:root
        install -d -m 0755 -o root -g root "$ETC_MIHOMO_DIR"
        # 管理器主目录: 0755 root:root
        install -d -m 0755 -o root -g root "$OPT_LEANCLASH_DIR"
        # 数据运行目录: 0750 mihomo:mihomo
        install -d -m 0750 -o mihomo -g mihomo "$DATA_MIHOMO_DIR"
        chown -R mihomo:mihomo "$DATA_MIHOMO_DIR"
        chmod 0750 "$DATA_MIHOMO_DIR"
        # 日志目录: 0750 mihomo:mihomo
        install -d -m 0750 -o mihomo -g mihomo "$LOG_MIHOMO_DIR"
        chown -R mihomo:mihomo "$LOG_MIHOMO_DIR"
        chmod 0750 "$LOG_MIHOMO_DIR"
        # 运行时目录: 0750 root:root
        install -d -m 0750 -o root -g root "$RUN_LEANCLASH_DIR"
    else
        install -d -m 0755 "$ETC_MIHOMO_DIR"
        install -d -m 0755 "$OPT_LEANCLASH_DIR"
        install -d -m 0750 "$DATA_MIHOMO_DIR"
        install -d -m 0750 "$LOG_MIHOMO_DIR"
        install -d -m 0750 "$RUN_LEANCLASH_DIR"
    fi
    log_success "配置目录:     $ETC_MIHOMO_DIR (0755)"
    log_success "管理器目录:   $OPT_LEANCLASH_DIR (0755)"
    log_success "数据运行目录: $DATA_MIHOMO_DIR (0750 mihomo:mihomo)"
    log_success "日志目录:     $LOG_MIHOMO_DIR (0750 mihomo:mihomo)"

    # 3. 安装二进制文件
    log_info "[3/6] 安装二进制文件到 $SYSTEM_BIN_DIR ..."
    install -d "$SYSTEM_BIN_DIR"
    if [ -z "$DESTDIR" ]; then
        install -m 0755 -o root -g root "$leanclash_src" "$SYSTEM_BIN_DIR/leanclash"
    else
        install -m 0755 "$leanclash_src" "$SYSTEM_BIN_DIR/leanclash"
    fi
    log_success "已安装: $SYSTEM_BIN_DIR/leanclash"

    if [ -n "$mihomo_src" ]; then
        if [ -z "$DESTDIR" ]; then
            install -m 0755 -o root -g root "$mihomo_src" "$SYSTEM_BIN_DIR/mihomo"
        else
            install -m 0755 "$mihomo_src" "$SYSTEM_BIN_DIR/mihomo"
        fi
        log_success "已安装: $SYSTEM_BIN_DIR/mihomo"
    elif [ -x "$SYSTEM_BIN_DIR/mihomo" ]; then
        log_info "系统中已存在 $SYSTEM_BIN_DIR/mihomo，保留当前版本"
    else
        log_warn "未提供 mihomo 内核且 $SYSTEM_BIN_DIR/mihomo 不存在！"
        log_warn "mihomo 服务启动需要该内核。请自行下载 mihomo 并拷贝到 $SYSTEM_BIN_DIR/mihomo"
    fi

    # 赋予内核必要 capability（如系统支持 setcap）
    if [ -z "$DESTDIR" ] && command -v setcap >/dev/null 2>&1 && [ -x "$SYSTEM_BIN_DIR/mihomo" ]; then
        setcap 'cap_net_bind_service,cap_net_admin,cap_net_raw+ep' "$SYSTEM_BIN_DIR/mihomo" 2>/dev/null || true
    fi

    # 4. 配置初始化
    log_info "[4/6] 检查配置模板..."
    if [ ! -f "$ETC_MIHOMO_DIR/config_general.yaml" ]; then
        if [ -z "$DESTDIR" ]; then
            install -m 0644 -o root -g root "$cfg_general_src" "$ETC_MIHOMO_DIR/config_general.yaml"
        else
            install -m 0644 "$cfg_general_src" "$ETC_MIHOMO_DIR/config_general.yaml"
        fi
        log_success "已初始化模板配置: $ETC_MIHOMO_DIR/config_general.yaml"
    else
        log_info "检测到已有 $ETC_MIHOMO_DIR/config_general.yaml，保留现有配置不覆盖"
    fi

    # 复制各模式初始参考配置（如不存在）
    for cfg_dir in "$work_dir/etc/mihomo" "$work_dir/deploy/etc/mihomo" "$ROOT_DIR/deploy/etc/mihomo"; do
        if [ -d "$cfg_dir" ]; then
            for mode_cfg in "$cfg_dir"/config_*.yaml; do
                if [ -f "$mode_cfg" ]; then
                    local fname
                    fname="$(basename "$mode_cfg")"
                    if [ ! -f "$ETC_MIHOMO_DIR/$fname" ]; then
                        if [ -z "$DESTDIR" ]; then
                            install -m 0644 -o root -g root "$mode_cfg" "$ETC_MIHOMO_DIR/$fname"
                        else
                            install -m 0644 "$mode_cfg" "$ETC_MIHOMO_DIR/$fname"
                        fi
                        log_info "  + 初始化模式配置: $fname"
                    fi
                fi
            done
            break
        fi
    done

    # 初始化 manager.yaml（若不存在）
    if [ -n "$manager_yaml_src" ] && [ ! -f "$OPT_LEANCLASH_DIR/manager.yaml" ]; then
        if [ -z "$DESTDIR" ]; then
            install -m 0644 -o root -g root "$manager_yaml_src" "$OPT_LEANCLASH_DIR/manager.yaml"
            if id mihomo >/dev/null 2>&1; then
                local mhgid
                mhgid="$(id -g mihomo)"
                sed -i "s/exclude_gid: [0-9]\+/exclude_gid: $mhgid/g" "$OPT_LEANCLASH_DIR/manager.yaml"
            fi
        else
            install -m 0644 "$manager_yaml_src" "$OPT_LEANCLASH_DIR/manager.yaml"
        fi
        log_success "已初始化系统设置: $OPT_LEANCLASH_DIR/manager.yaml"
    elif [ -f "$OPT_LEANCLASH_DIR/manager.yaml" ]; then
        log_info "已有系统设置 $OPT_LEANCLASH_DIR/manager.yaml，保留现有配置"
    fi

    # 5. 安装 systemd 单元
    log_info "[5/6] 安装 systemd 单元文件..."
    install -d "$SYSTEM_SYSTEMD_DIR"
    if [ -z "$DESTDIR" ]; then
        install -m 0644 -o root -g root "$lc_service_src" "$SYSTEM_SYSTEMD_DIR/leanclash.service"
        install -m 0644 -o root -g root "$mh_service_src" "$SYSTEM_SYSTEMD_DIR/mihomo@.service"
    else
        install -m 0644 "$lc_service_src" "$SYSTEM_SYSTEMD_DIR/leanclash.service"
        install -m 0644 "$mh_service_src" "$SYSTEM_SYSTEMD_DIR/mihomo@.service"
    fi

    log_success "已安装: $SYSTEM_SYSTEMD_DIR/leanclash.service"
    log_success "已安装: $SYSTEM_SYSTEMD_DIR/mihomo@.service"

    # 6. 重新加载并启动
    if [ -z "$DESTDIR" ]; then
        log_info "[6/6] 重载 systemd 并启动服务..."
        systemctl daemon-reload
        systemctl enable leanclash.service >/dev/null 2>&1 || true
        systemctl restart leanclash.service

        sleep 2

        if systemctl is-active --quiet leanclash.service; then
            local host_ip
            host_ip="$(ip route get 1.1.1.1 2>/dev/null | awk '{print $7; exit}' || true)"
            [ -n "$host_ip" ] || host_ip="127.0.0.1"

            echo ""
            log_success "=================================================="
            log_success "LeanClash 部署完成并已成功启动！"
            log_success "  - Web 控制面板:  http://${host_ip}:8081"
            log_success "  - 服务状态查询:  systemctl status leanclash"
            log_success "  - CLI 命令行工具: leanclash status / leanclash info"
            log_success "=================================================="
        else
            log_warn "leanclash.service 状态非 active，请查看日志排查:"
            journalctl -u leanclash.service -n 25 --no-pager || true
        fi
    else
        log_success "[6/6] [DESTDIR 模式] 文件部署完成，跳过 systemd 守护进程启停"
    fi

    if [ "$cleanup_work_dir" = true ]; then
        rm -rf "$work_dir"
        trap - EXIT
    fi
}

# ---------------- 移除卸载 (--remove) ----------------

do_remove() {
    require_root "$@"

    local keep_mihomo=false
    local keep_config=false
    local purge=false

    for arg in "$@"; do
        case "$arg" in
            --keep-mihomo) keep_mihomo=true ;;
            --keep-config) keep_config=true ;;
            --purge)       purge=true ;;
        esac
    done

    log_info "=================================================="
    log_info "开始在当前系统移除 LeanClash 相关文件与服务"
    log_info "=================================================="

    # 1. 停止并禁用所有相关服务
    if [ -z "$DESTDIR" ]; then
        log_info "[1/4] 停止并禁用 systemd 服务..."
        systemctl disable --now leanclash 2>/dev/null || true
        local units
        units="$(systemctl list-units --all --no-legend 'mihomo@*.service' 2>/dev/null | awk '{print $1}' || true)"
        if [ -n "$units" ]; then
            # shellcheck disable=SC2086
            systemctl stop $units 2>/dev/null || true
        fi
    else
        log_info "[1/4] [DESTDIR 模式] 跳过 systemd 服务停止"
    fi

    # 2. 清理策略路由与防火墙表残留
    if [ -z "$DESTDIR" ]; then
        log_info "[2/4] 清理策略路由与网络规则残留..."
        cleanup_firewall
    else
        log_info "[2/4] [DESTDIR 模式] 跳过策略路由与防火墙清理"
    fi

    # 3. 移除服务单元、二进制与目录
    log_info "[3/4] 移除服务单元与文件..."
    rm -f "$SYSTEM_SYSTEMD_DIR/leanclash.service" \
          "$SYSTEM_SYSTEMD_DIR/mihomo@.service"

    rm -f "$SYSTEM_BIN_DIR/leanclash"

    if [ "$keep_mihomo" = true ]; then
        log_info "已保留内核二进制: $SYSTEM_BIN_DIR/mihomo"
    else
        rm -f "$SYSTEM_BIN_DIR/mihomo"
        log_info "已删除内核二进制: $SYSTEM_BIN_DIR/mihomo"
    fi

    rm -rf "$RUN_LEANCLASH_DIR"

    if [ "$purge" = true ]; then
        rm -rf "$OPT_LEANCLASH_DIR" "$DATA_MIHOMO_DIR" "$LOG_MIHOMO_DIR"
        if [ "$keep_config" = true ]; then
            log_info "已保留配置目录: $ETC_MIHOMO_DIR"
        else
            rm -rf "$ETC_MIHOMO_DIR"
            log_info "已删除配置目录: $ETC_MIHOMO_DIR"
        fi
    else
        log_info "配置与数据已保留: $OPT_LEANCLASH_DIR $ETC_MIHOMO_DIR $DATA_MIHOMO_DIR"
        log_info "若需一并彻底删除，使用: sudo $0 --remove --purge"
    fi

    # 4. 删除用户（仅在 purge 时）
    if [ -z "$DESTDIR" ] && [ "$purge" = true ]; then
        log_info "[4/4] 移除 mihomo 系统用户与用户组..."
        pkill -u mihomo 2>/dev/null || true
        userdel mihomo 2>/dev/null || true
        groupdel mihomo 2>/dev/null || true

        systemctl daemon-reload
        systemctl reset-failed 2>/dev/null || true
    else
        log_info "[4/4] 跳过系统用户删除"
    fi

    log_success "=================================================="
    log_success "LeanClash 相关清理完成。"
    log_success "=================================================="
}

# ---------------- 主入口分发 ----------------

ACTION="${1:-}"
case "$ACTION" in
    --pack|-p|pack)
        shift || true
        do_pack "$@"
        ;;
    --install|-i|install|copy|upgrade)
        shift || true
        do_install "$@"
        ;;
    --remove|-r|remove)
        shift || true
        do_remove "$@"
        ;;
    --help|-h|help|"")
        usage
        ;;
    *)
        log_error "未知指令: $ACTION"
        usage >&2
        exit 1
        ;;
esac
