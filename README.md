# LeanClash

面向 **Linux 服务器裸跑 mihomo 内核**的 Web 面板：**单一 Go 二进制**（守护进程 + CLI），Vue 3 前端内嵌。通过面板一键切换 tun / socks / tproxy / redir-tproxy 四种透明代理模式，nft/ip 规则由守护进程编排并与 `mihomo@` 实例同生共死。

[![Go](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go&logoColor=white)](go.mod)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

> A single-binary mode manager for [mihomo](https://github.com/MetaCubeX/mihomo): manages tun / tproxy / redir-tproxy / socks modes, orchestrates nftables & policy-routing rules that live and die with each `mihomo@` instance, with an embedded Vue 3 web panel.

## 适用场景

- 服务器上已装 mihomo 内核（`/usr/local/bin/mihomo`），不想逐个手写 systemd 单元与 nftables/策略路由规则
- 需要在 TUN 虚拟网卡 / TPROXY / REDIR-TPROXY / SOCKS 之间频繁切换
- 想要一个轻量 Web 面板 + CLI 管理，而不引入整套代理客户端

## 特性

- **4 种模式**：tun / socks / tproxy / redir-tproxy；socks 入站由端口驱动（默认 mixed 20260，面板可改）
- **systemd 实例探测**（非容器）：查询正在运行的 `mihomo@<mode>` 单元，首页显示「正在运行 X 模式」；即便实例是 `systemctl start mihomo@tun` 拉起的也能识别。容器后端不探测宿主机单元，只反映本进程拉起的实例
- **规则生命周期**：启动 `mihomo@` 实例 → 延迟套用 nft/ip 规则 → 实例停止即清理（同生共死）；守护重启自动 reconcile 兜底；tun0 消失自动清理残留
- **回环避免双方式**：`meta skgid`（GID，优先）或 `meta mark`（路由 mark），二选一可配置
- **统一配置**：`/opt/leanclash/manager.yaml` 是规则数字/env/预定义入站的唯一事实源
- **实时状态**：dbus 订阅 + SSE 推送，面板状态秒级刷新
- **非 systemd 后端**：使用 `go build -tags container` 构建容器版本，直接由 manager 管理 `mihomo@<mode>` 子进程；默认构建使用 systemd/D-Bus
- **配置同步**：`config_general.yaml` + 各模式预定义入站（socks 由 manager.yaml 的 `env.socks_port` 生成）→ `mihomo -t` 校验后生成各模式配置
- **面板体验**：切换按真实结果反馈（已启动/已停止/失败）；守护进程不可达时明确提示并一键重试；配置编辑器未保存修改有二次确认保护

## 目录结构

```
├── main.go              # 入口：go:embed 前端产物 + CLI 子命令分发
├── internal/
│   ├── api/             # REST API + SSE 推送
│   ├── cli/             # CLI 子命令（模式启停/status/config sync）
│   ├── config/          # manager.yaml 读写与同步
│   ├── intercept/       # tun/tproxy/redir-tproxy 规则编排（exec ip/nft）
│   ├── lifecycle/       # 模式生命周期（同生共死监控、reconcile）
│   ├── netlink/         # 只读状态检测 + tun0 link 事件
│   ├── server/          # 守护进程（systemd 常驻）
│   └── systemd/         # dbus 启停/状态订阅
├── web/                 # Vue 3 + Vite 前端（构建产物内嵌进二进制）
├── deploy/              # systemd 单元 + 安装模板配置
├── container/            # 容器入口脚本与默认配置
├── Dockerfile            # 容器镜像定义
├── docker-compose.yml    # 本地容器运行配置
├── build.sh              # 二进制与容器统一构建入口
├── deploy.sh             # 原生部署：打包 / 安装 / 卸载
├── build/                # 构建产物（不提交）
├── dev/                 # 本地开发 fixture（示例配置，无真实节点）
└── LICENSE
```

## 构建

环境要求：Go 1.22+、Node.js 18+（Vite 5 前端构建）。

```bash
# 可选：构建前端（产物 web/dist/ 内嵌进二进制）
cd web && npm install && npm run build
cd ..

# 同时构建二进制和容器 tar.gz，产物全部写入 build/
./build.sh

# 只构建原生二进制
./build.sh binary

# 只构建容器二进制、镜像和 tar.gz
./build.sh container

# 查看构建信息
./build/leanclash info
```

`leanclash info` 会显示版本、提交、UTC 构建时间、目标平台和 Go 运行时版本，便于区分测试构建。目标 ARM64 时使用 `TARGET_ARCH=arm64 ./build.sh`。

构建容器前需要准备 Mihomo 运行时二进制 `build/mihomo`；它会被复制进容器镜像。

容器使用 `container` build tag 编译独立的进程管理 backend；原生二进制使用
systemd/D-Bus backend。两种二进制都由同一个 `build.sh` 生成。

编译产物的部署方法见下一节；目标服务器无需安装 Go。

## 部署到服务器（原生，不含容器）

开发机打包、服务器安装/卸载都走 `deploy.sh`。包内含 LeanClash 二进制、systemd 单元、配置模板；若本地有 `build/mihomo` 也会打进去。

```bash
# 开发机：打包（文件名带时间戳，产物在 build/）
./deploy.sh --pack
# 例如: build/leanclash-native-amd64-20260913_083415.tar.gz

# 上传安装包和脚本到服务器后直接安装（不必先解压）
scp deploy.sh build/leanclash-native-*.tar.gz 服务器:/tmp/
ssh 服务器
sudo /tmp/deploy.sh --install --file /tmp/leanclash-native-amd64-20260913_083415.tar.gz

# 卸载程序（保留配置与数据）
sudo ./deploy.sh --remove

# 连同 /opt/leanclash、/etc/mihomo、数据目录一并删除
sudo ./deploy.sh --remove --purge
```

`--install` 会：创建 `mihomo` 系统用户（已存在则跳过）、安装二进制到 `/usr/local/bin/leanclash`、写入 `leanclash.service` 与 `mihomo@.service`、创建 `/etc/mihomo` `/opt/leanclash` `/var/lib/mihomo` `/var/log/mihomo` 并设置属主，已有 `config_general.yaml` 不会覆盖，然后 `enable --now leanclash`。服务器上若还没有 mihomo 内核且包内也没有，安装会失败。

## 容器运行与部署

LeanClash 提供了专门针对容器环境构建的版本（`./build.sh container`，使用 `container` build tag 编译）。容器镜像内置了独立的轻量进程管理器，无需 systemd 与 D-Bus 依赖，直接管理 `mihomo` 子进程及其网络规则。

> [!IMPORTANT]
> **容器回环避免关键提示（使用 routing_mark）**：
> 宿主机原生部署时 Mihomo 运行在专有的 `mihomo` 系统用户组下，默认通过排除 GID（`meta skgid`）避免流量回环；但**容器精简环境中不创建专有用户**（进程直接以 root 运行），此时无法通过 GID 区分出站流量。因此在容器环境下**必须使用 routing_mark（路由标记，如 6666）避免回环**：
> 1. **Mihomo 配置**：在 `/etc/mihomo/config_general.yaml` 中添加 `routing-mark: 6666`，让内核对 Mihomo 自身的出站流量打上标记。
> 2. **LeanClash 管理配置**：在面板「系统设置」中将 TPROXY / REDIR-TPROXY 的「回环避免方式」切换为 **Mark**，或者在 `/opt/leanclash/manager.yaml` 中设置 `routing_mark: 6666` 并将 `exclude_gid: 0`。
> 
> 若未正确配置 routing_mark，透明代理流量将被自身重复捕获引发死循环导致容器断网。

### 1. 普通 Docker 容器（Host 网络模式）

适合常规 Linux 服务器裸跑容器，容器直接共享宿主机网络命名空间并操作策略路由与 nftables。

#### 方式 A：Docker Compose 启动（推荐）

```yaml
# docker-compose.yml
services:
  leanclash:
    build:
      context: .
      dockerfile: Dockerfile
    image: localhost/leanclash:latest
    container_name: leanclash
    restart: unless-stopped
    network_mode: host
    privileged: true
    volumes:
      - leanclash-config:/etc/mihomo
      - leanclash-data:/var/lib/mihomo
      - leanclash-manager:/opt/leanclash

volumes:
  leanclash-config:
    driver: local
  leanclash-data:
    driver: local
  leanclash-manager:
    driver: local
```

启动与管理：
```bash
docker compose up -d
docker compose logs -f
```

#### 方式 B：Docker Run 命令行启动

```bash
docker run -d \
  --name leanclash \
  --restart unless-stopped \
  --network host \
  --privileged \
  -v /opt/leanclash/config:/etc/mihomo \
  -v /opt/leanclash/data:/var/lib/mihomo \
  -v /opt/leanclash/manager:/opt/leanclash \
  localhost/leanclash:latest
```

---

### 2. Kata Container（独立网络命名空间与独立内核）

在需要将透明代理完全与宿主机内核隔离的场景（如旁路网关、高隔离多租户容器等），推荐使用 **Kata Containers** 运行。Kata 为容器分配轻量级虚拟机内核与独立网络命名空间（如 `macvlan` 或独立网桥 `bridge`），此时透明代理的策略路由与 nftables 仅作用于该虚拟化网络命名空间内，不干扰宿主机。

#### systemd 服务单元管理（使用 nerdctl + Kata 运行时）

创建 `/etc/systemd/system/leanclash-container.service`：

```ini
# /etc/systemd/system/leanclash-container.service
[Unit]
Description=Mihomo Kata Container
After=containerd.service network-online.target
Wants=network-online.target
Requires=containerd.service

[Service]
Type=simple
Restart=always
RestartSec=5s
TimeoutStartSec=0
TimeoutStopSec=30
# 启动前清理同名残留实例
ExecStartPre=-/usr/local/bin/nerdctl rm -f leanclash-container
ExecStart=/usr/local/bin/nerdctl run --rm \
  --name leanclash-container \
  --cgroup-manager=cgroupfs \
  --runtime io.containerd.kata.v2 \
  --network macv0 \
  --cap-add=NET_ADMIN \
  --cap-add=NET_BIND_SERVICE \
  --device /dev/net/tun:/dev/net/tun \
  -v /opt/leanclash-container/config:/etc/mihomo \
  -v /opt/leanclash-container/data:/var/lib/mihomo \
  -v /opt/leanclash-container/manager:/opt/leanclash \
  -e TZ=Asia/Shanghai \
  --sysctl net.ipv4.ip_forward=1 \
  --sysctl net.ipv6.conf.all.forwarding=1 \
  --sysctl net.ipv6.conf.all.accept_ra=2 \
  --sysctl net.ipv6.conf.eth0.accept_ra=2 \
  --sysctl net.ipv4.ip_local_port_range="1024 65535" \
  --sysctl net.ipv4.tcp_tw_reuse=1 \
  --sysctl net.ipv4.tcp_fin_timeout=15 \
  --sysctl net.core.somaxconn=32768 \
  --sysctl net.ipv4.tcp_max_syn_backlog=16384 \
  --sysctl net.core.netdev_max_backlog=16384 \
  --sysctl net.core.rmem_max=16777216 \
  --sysctl net.core.wmem_max=16777216 \
  --sysctl net.ipv4.tcp_rmem="4096 87380 16777216" \
  --sysctl net.ipv4.tcp_wmem="4096 65536 16777216" \
  --sysctl net.ipv4.tcp_congestion_control=bbr \
  --sysctl net.ipv4.neigh.default.gc_thresh1=2048 \
  --sysctl net.ipv4.neigh.default.gc_thresh2=4096 \
  --sysctl net.ipv4.neigh.default.gc_thresh3=8192 \
  --sysctl net.ipv6.neigh.default.gc_thresh1=2048 \
  --sysctl net.ipv6.neigh.default.gc_thresh2=4096 \
  --sysctl net.ipv6.neigh.default.gc_thresh3=8192 \
  localhost/leanclash-container:latest
ExecStop=/usr/local/bin/nerdctl stop -t 15 leanclash-container

[Install]
WantedBy=multi-user.target
```

#### Kata 运行要点说明：
- **`--runtime io.containerd.kata.v2`**：调用 Kata 轻量虚机运行时，提供独立的 Linux 内核环境。
- **`--network macv0`**：连接指定网段的 macvlan / 独立网桥，容器拥有独立二层 IP 和独立路由命名空间。
- **`--device /dev/net/tun:/dev/net/tun` 与 `--cap-add=NET_ADMIN`**：支持创建 TUN 虚拟网卡设备及配置策略路由。
- **独立内核网络调优（`--sysctl`）**：由于 Kata 拥有独立虚拟化内核，可以安全注入内核级网络优化参数（包括开启 IPv4/IPv6 流量转发 `ip_forward`、TCP BBR 拥塞控制、扩大连接队列与内存缓冲区 `rmem_max`/`wmem_max`、调大邻居表垃圾回收阈值等），而无需担心影响宿主机。
- **回环避免（必须使用 Mark）**：容器精简环境无专有用户，必须在 `/opt/leanclash-container/config/config_general.yaml` 中配置 `routing-mark: 6666`，并在 Web 面板或 `manager.yaml` 中将回环避免设为 Mark（`routing_mark: 6666`, `exclude_gid: 0`）。
- **持久化目录**：宿主机目录 `/opt/leanclash-container/{config,data,manager}` 分别映射至容器内 `/etc/mihomo`、`/var/lib/mihomo` 与 `/opt/leanclash`。

启用与启动服务：
```bash
sudo systemctl daemon-reload
sudo systemctl enable --now leanclash-container
```

## 常用命令

```bash
sudo leanclash tun start              # 启动模式（tun|socks|tproxy|redir-tproxy）
sudo leanclash tproxy stop            # 停止（规则自动清理）
sudo leanclash status                 # 各模式/单元/规则状态
sudo leanclash config sync            # 重新生成各模式配置（mihomo -t 校验）
leanclash info                        # 版本/编译时间/目标平台（无需 root）
```

安装后检查：`/opt/leanclash/manager.yaml` 里 tproxy/redir-tproxy 的 `exclude_gid` 与 `id -g mihomo` 一致（不一致会环路），可在面板「系统设置」修改。tun 模式的 `device: tun0` 按实际机器调整。socks 入站端口（默认 20260）在「系统设置 · SOCKS / SERVER」修改，保存后自动重新生成 `config_socks.yaml`；老版本 manager.yaml 里的 socks preset 会在守护进程启动时自动迁移为 `env.socks_port`。

## 监听地址与 IPv6（安全说明）

面板无鉴权，**默认仅监听 IPv4**（`0.0.0.0:8081`，不暴露 IPv6）。在 manager.yaml 中调整：

- 仅本机访问：`web_addr: "127.0.0.1:8081"`
- 显式启用 IPv6：`web_addr: "[::]:8081"`（仅 IPv6）或 `":8081"`（双栈，含 IPv4）

修改后 `sudo systemctl restart leanclash` 生效（也可在面板「系统设置」修改）。

## nginx 反向代理配置示例

面板（`http://<host>:8081`）无内置鉴权，公网暴露建议经 nginx 反代并加 Basic Auth：

```nginx
server {
    listen 80;
    server_name mihomo.example.com;

    # 可选：面板无鉴权，建议开启 Basic Auth（先 htpasswd -c /etc/nginx/.htpasswd 用户名）
    # auth_basic "LeanClash";
    # auth_basic_user_file /etc/nginx/.htpasswd;

    location / {
        proxy_pass http://127.0.0.1:8081;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # SSE 实时状态推送必须：关闭缓冲、放宽读超时
        proxy_buffering off;
        proxy_cache off;
        proxy_read_timeout 1h;
        proxy_send_timeout 1h;
        proxy_set_header Connection '';
    }
}
```

HTTPS 用 certbot 免费证书：

```bash
sudo apt install certbot python3-certbot-nginx
sudo certbot --nginx -d mihomo.example.com
```

## 本地开发

```bash
./dev/run.sh                # 守护进程（dev fixture，不碰系统路径），面板 :8081
cd web && npm run dev       # 前端热更新（vite 代理到 :8081）
```

## 许可证

[MIT](LICENSE) © 2026 vxzman
