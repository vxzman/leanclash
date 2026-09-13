# LeanClash

<p align="center">
  <b>专为 Linux 服务器设计的 Mihomo 代理内核透明代理模式编排器与 Web 控制台</b>
</p>

<p align="center">
  <a href="#-本项目特色裸核代理透明编排">项目特色</a> •
  <a href="#-前端设计美学winui-3--mica-材质">设计美学</a> •
  <a href="#-从源码构建测试预览与编译">构建与预览</a> •
  <a href="#-普通二进制部署宿主机原生环境">二进制部署</a> •
  <a href="#-容器化部署docker--compose--kata">容器部署</a> •
  <a href="#-安全与反向代理">反代与安全</a>
</p>

---

## 🌟 本项目特色：裸核代理透明编排

在 Linux 服务器、软路由或云主机上直接运行 `mihomo` 等纯代理内核（无桌面客户端、无原生透明代理编排面板）时，传统运维往往面临三大痛点：
1. **策略路由与防火墙规则繁琐易错**：手动编写复杂的 `nftables` 表、`ip rule` 优先级及专用路由表，稍有不慎即造成网络死锁甚至整机失联；
2. **进程与规则生命周期脱节**：内核意外退出或停止后规则仍然残留，导致整机网络中断；系统重启后无法自动审计与清理残留规则；
3. **流量回环难以防范**：本机代理出站流量若未做精细绕行，会再度被拦截并重新打回代理端口，瞬间引发死循环打爆系统。

**LeanClash 为彻底解决上述痛点而生：**

* **单一二进制，极简交付**：整个项目编译为单一 Go 二进制程序（常驻守护进程 + 命令行 CLI），内嵌现代化 Web 前端，无任何外部环境依赖。
* **4 种运行模式一键切换**：开箱支持 `tun`、`tproxy`、`redir-tproxy` 和 `socks`（SOCKS5/HTTP 混合代理，默认 20260 端口），与宿主机 `systemd` 或容器进程管理深度集成。
* **规则与内核“同生共死”**：守护进程实时监听服务生命周期，内核实例启动时自动下发配套 nft/ip 规则，内核停止或异常崩溃时自动清理回收规则，守护进程重启时自动 reconcile 兜底，`tun0` 网卡消失时自动清理残留，彻底杜绝网络锁死。
* **双重回环避免机制**：原生支持 `meta skgid`（系统专有用户组 GID 绕行，宿主机环境推荐）与 `meta mark` / `routing_mark`（Fwmark 标记绕行，容器环境推荐），二选一可灵活配置。
* **配置单一事实源**：以 `config_general.yaml`（通用节点与分流规则）与 `manager.yaml`（规则端口、网络参数与入站预设）为中心，自动调用内核校验并原子合并生成各模式配置。
* **系统状态实时响应**：宿主机原生通过 D-Bus 监听 systemd 状态，前端通过 SSE（Server-Sent Events）秒级推送，手动执行 `systemctl` 也能即时同步到面板。

### 四大运行模式对比

| 模式 | 运行实例 | 核心机制 | 适用场景与系统要求 |
|---|---|---|---|
| **TUN** | `mihomo@tun` | 创建虚拟网卡 `tun0`，由内核通过 `auto_route` 自动接管三层流量 | 通用性最高，不依赖特定 iptables/nftables 模块 |
| **TPROXY** | `mihomo@tproxy` | 基于 `nftables` + `ip rule` 实现纯四层透明代理（默认端口 22016） | 性能优异，保留真实源 IP，适合现代 Linux 内核 |
| **REDIR-TPROXY** | `mihomo@redir-tproxy` | TCP 使用 REDIRECT（端口 22017），UDP 使用 TPROXY（端口 22016） | 兼容老旧内核或特定需要 TCP REDIRECT 的软路由环境 |
| **SOCKS** | `mihomo@socks` | 启动本地 Mixed (SOCKS5/HTTP) 代理端口（默认 20260） | 本地或局域网客户端显式代理，不修改系统路由 |

---

## 🎨 前端设计美学：WinUI 3 + Mica 材质

前端基于 Vue 3 + Vite 构建，深度融合 Windows 11 Fluent Design 与 WinUI 3 视觉设计语言：

* **Mica（云母）材质分层**：
  * **动态采样本底**：窗口背景由柔光壁纸采样生成底色，并铺设细致双尺度微噪点层理（Micro-noise）；
  * **通透半透明卡片**：内容卡片采用轻量柔和的半透明实色分层，去除二次模糊，让底层云母色自然透出；
  * **Acrylic（亚克力）控件**：顶部导航胶囊与浮动 Toast 弹窗呈现高级通透感。
* **深蓝渐变猫咪 Logo**：
  * 专为 Mihomo 打造的精致猫咪形象，搭配深蓝至青蓝渐变，统一网站图标（Favicon）与控制台 Header。
* **状态芯片与可视化交互**：
  * **运行状态**：Hero 摘要卡片实时反馈当前生效模式，统计芯片直观展示单元活跃态与内核规则状态（正常 / 缺失 / 残留）；
  * **配置管理**：双栏编辑器，支持主配置文件实时校验与模式配置只读比对，未保存修改具备二次确认保护；
  * **系统设置**：可视化分段调整 `manager.yaml` 参数（端口、排除 GID、路由 Mark、入站预设模板）。

---

## 🛠️ 从源码构建、测试预览与编译

### 前置要求
* **Go**：`>= 1.22`
* **Node.js**：`>= 18` 与 `npm`（构建前端所需；二进制自带占位页，无 node 环境亦可编译基本后端）
* **mihomo 内核**（可选）：放置于 `build/mihomo`（测试与打包时会自动探测）

### 1. 克隆项目
```bash
git clone git@github.com:vxzman/leanclash.git
cd leanclash
```

### 2. Web 前端开发与测试预览
开发阶段可享受 Vite 带来的毫秒级热更新，同时使用 `deploy/` 下的 Linux 映射配置启动本地模拟后端：

```bash
# 终端 1：启动本地模拟后端（使用 deploy 映射配置，监听 0.0.0.0:8081）
LEANCLASH_CONFIG=$PWD/deploy/opt/leanclash/manager.yaml go run . serve

# 终端 2：启动前端开发热更新服务器（代理 API 请求到 :8081）
cd web
npm install
npm run dev
```
浏览器打开 `http://localhost:5173` 即可进行前端界面实时调试与预览。

### 3. 编译普通二进制（Host 环境）
使用根目录构建脚本一键完成前端打包与后端编译注入：
```bash
./build.sh binary
```
编译产物输出至 **`build/leanclash`**，前端静态资源已完全内嵌进单一二进制中。可以通过如下命令查看构建元数据：
```bash
./build/leanclash info
```

### 4. 编译容器版本与 Docker 镜像
在具备容器构建环境（Docker 或 Podman）的机器上，可一键完成双二进制、Debian 基础镜像与离线包构建：
```bash
# 准备目标架构的 mihomo 内核（若已有）
mkdir -p build && cp /path/to/mihomo build/mihomo

# 编译容器版独立进程二进制、构建镜像并生成 tar 归档
./build.sh container
```
构建产物包括：
* `build/leanclash`（Host systemd 版）
* `build/leanclash-container`（带 `-tags container` 的独立进程版）
* Docker 镜像 `localhost/leanclash:latest`
* 离线归档包 `build/leanclash-<version>-<arch>-<timestamp>.tar.gz`

---

## 🚀 普通二进制部署（宿主机原生环境）

LeanClash 采用类似标准 Linux 根文件系统的映射目录结构（`deploy/`），并提供统一的一键部署脚本 [deploy.sh](deploy.sh)。

### 方案 A：一键打包与安装部署（推荐，免目标机编译环境）

#### 1. 在开发机上一键打包
在本地开发机执行打包命令，脚本会自动将编译产物复制至 Linux 映射结构并打成带有时间戳的压缩包：
```bash
./deploy.sh --pack
```
产物统一生成于 **`build/leanclash-deploy-<version>-<timestamp>.tar.gz`**（包内含二进制、服务单元、配置文件模板与部署脚本）。

#### 2. 上传安装包至目标服务器
```bash
scp build/leanclash-deploy-*.tar.gz root@<server_ip>:/tmp/
```

#### 3. 在目标服务器上一键安装部署
登录服务器并直接指定部署包进行一键安装：
```bash
ssh root@<server_ip>
sudo ./deploy.sh --install --file /tmp/leanclash-deploy-*.tar.gz
```
> **部署脚本全自动完成**：
> 1. 检查并创建专用系统用户与组 `mihomo`；
> 2. 初始化 `/etc/mihomo`、`/var/lib/mihomo`、`/var/log/mihomo`、`/opt/leanclash` 等目录并正确设置属主与权限；
> 3. 安装 `leanclash` 与 `mihomo` 到 `/usr/local/bin/` 并赋予必要网络能力（Capability）；
> 4. 安装配置模板，并自动匹配检测到的 `mihomo` 用户组 GID 到 `manager.yaml`；
> 5. 安装 `leanclash.service` 与 `mihomo@.service` 服务单元；
> 6. 重载 systemd 并启动 `leanclash.service` 开机自启。

#### 4. 卸载与清理（如需）
```bash
sudo ./deploy.sh --remove
# 如需连同 /opt/leanclash、配置与数据目录一并删除：
# sudo ./deploy.sh --remove --purge
```

---

### 方案 B：手动文件部署说明（Linux 目录映射参考）

若习惯纯手工放置文件，可参考 `deploy/` 的映射关系进行安装：

| 本地映射路径 | 目标系统绝对路径 | 权限/所有者 | 描述 |
|---|---|---|---|
| `deploy/usr/local/bin/leanclash` | `/usr/local/bin/leanclash` | `0755 root:root` | LeanClash 控制平面主程序 |
| `deploy/usr/local/bin/mihomo` | `/usr/local/bin/mihomo` | `0755 root:root` | mihomo 代理内核 |
| `deploy/etc/systemd/system/leanclash.service` | `/etc/systemd/system/leanclash.service` | `0644 root:root` | Web 控制台与编排守护单元 |
| `deploy/etc/systemd/system/mihomo@.service` | `/etc/systemd/system/mihomo@.service` | `0644 root:root` | 内核实例运行模板单元 |
| `deploy/etc/mihomo/config_general.yaml` | `/etc/mihomo/config_general.yaml` | `0644 root:root` | 用户代理配置单一事实源 |
| `deploy/opt/leanclash/manager.yaml` | `/opt/leanclash/manager.yaml` | `0644 root:root` | 系统编排与网络环境变量配置 |
| 运行时数据目录 | `/var/lib/mihomo/` | `0750 mihomo:mihomo` | 内核缓存与数据目录 |
| 运行时日志目录 | `/var/log/mihomo/` | `0750 mihomo:mihomo` | 日志目录 |

手动启动服务命令：
```bash
sudo systemctl daemon-reload
sudo systemctl enable --now leanclash.service
```

### 日常热更新
二进制内置前端，日常迭代更新只需上传单一文件并重启服务：
```bash
scp build/leanclash root@<server_ip>:/usr/local/bin/leanclash
ssh root@<server_ip> 'systemctl restart leanclash'
```

### 命令行常用操作
除了 Web 面板，亦可直接通过命令行管理：
```bash
sudo leanclash info                 # 查看当前版本、Git提交与运行平台
sudo leanclash status               # 查看各模式实例及网络规则活跃状态
sudo leanclash tun start            # 启动 TUN 模式（支持 tun|tproxy|redir-tproxy|socks）
sudo leanclash tun stop             # 停止模式并自动清理对应规则
sudo leanclash config sync          # 基于通用配置重新校验并生成各模式配置
```

---

## 🐳 容器化部署（Docker / Compose / Kata）

容器版本使用 `leanclash-container` 独立进程后端（使用 `container` build tag 编译），无需依赖宿主机 systemd 或 D-Bus，由 LeanClash 直接接管 `mihomo` 子进程生命周期与 iptables/nftables 规则。

> ⚠️ **容器环境关键注意事项（回环避免必须使用 routing_mark）**：
> 1. 宿主机原生部署时 Mihomo 运行在专有 `mihomo` 用户下，通过 `meta skgid` 排除 GID 避免流量回环；
> 2. 但**容器精简环境中进程以 root 运行**，没有专有的宿主机 GID，因此**严禁使用 `meta skgid`**，必须使用 `routing_mark`（如 6666）避免回环：
>    * 在 `/etc/mihomo/config_general.yaml` 中添加 `routing-mark: 6666`；
>    * 在 Web 面板「系统设置」或 `/opt/leanclash/manager.yaml` 中将回环避免设为 Mark，设置 `routing_mark: 6666` 且 `exclude_gid: 0`。

### 1. 使用 Docker Compose（标准 Host 网络模式）

直接使用项目根目录的 [docker-compose.yml](docker-compose.yml)：

```yaml
services:
  leanclash:
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

启动与日志监控：
```bash
docker compose up -d
docker compose logs -f
```

### 2. 使用 `docker run` 直接运行

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

容器启动后，即可在浏览器访问 `http://<服务器IP>:8081`。

### 3. Kata Containers 强隔离部署（硬件级微虚机沙箱）

在多租户服务器、公共云计算实例或对安全性要求极高的生产环境中，代理内核（处理复杂外网流量、解密 TLS 等）以及网络特权操作往往具有潜在的安全暴露面。

**传统 runc 容器 vs Kata Containers 隔离对比**：

* **传统 runc 容器**：容器与宿主机共享同一个 Linux 内核。透明代理所需的 `--privileged` 特权与 `CAP_NET_ADMIN` 使得容器进程能够直接触碰宿主机内核底层；一旦代理内核或 netfilter 驱动存在漏洞，存在攻击者逃逸至宿主机的风险。
* **Kata Containers（微虚机架构）**：每个容器均运行在独立的轻量级虚拟机（MicroVM，基于 QEMU / Cloud Hypervisor）之中，拥有**完全专享且独立的 Guest Linux 内核**。
  * **特权安全封锁**：容器内赋予的 `--privileged` 仅作用于该微虚机内部的 Guest 内核，无法突破硬件虚拟化层侵入物理宿主机；
  * **网络零污染**：在微虚机中建立的全部 `nftables` 规则与 `tun0` 路由表均在微虚机内部闭环，物理宿主机的全局网络与防火墙保持绝对纯净。

#### 前置环境准备
确保宿主机 CPU 支持硬件虚拟化（`egrep -c '(vmx|svm)' /proc/cpuinfo`），并在 Docker 中配置了 Kata 运行时（如 `/etc/docker/daemon.json`）：
```json
{
  "runtimes": {
    "kata-qemu": {
      "path": "/usr/bin/kata-runtime"
    },
    "kata-clh": {
      "path": "/usr/bin/kata-runtime"
    }
  }
}
```

#### 部署方式一：使用 Docker Compose（微虚机独立端口映射）
项目提供开箱即用的 [docker-compose.kata.yml](docker-compose.kata.yml)：

```bash
docker compose -f docker-compose.kata.yml up -d
docker compose -f docker-compose.kata.yml logs -f
```

配置将 Web 面板端口 `8081` 与 Mixed 代理端口 `20260` 暴露，所有内核操作在 Kata 独立的 Guest Linux 内核中安全沙箱化运行。

#### 部署方式二：独立旁路网关模式（Macvlan / 局域网物理直通，强烈推荐）
通过 Docker Macvlan 驱动将物理局域网网段直接接入 Kata 容器微虚机，让其作为一个独立的物理“网络硬件设备”运行：

```bash
# 1. 创建直通局域网的 Macvlan 网络（以 eth0 为父网卡为例）
docker network create -d macvlan \
  --subnet=192.168.1.0/24 \
  --gateway=192.168.1.1 \
  -o parent=eth0 kata-lan

# 2. 启动 Kata 独立微虚机容器，并赋予专属内网 IP（如 192.168.1.88）
docker run -d \
  --runtime kata-qemu \
  --name leanclash-kata \
  --restart unless-stopped \
  --network kata-lan \
  --ip 192.168.1.88 \
  --privileged \
  --device /dev/net/tun:/dev/net/tun \
  -v /opt/leanclash-kata/config:/etc/mihomo \
  -v /opt/leanclash-kata/data:/var/lib/mihomo \
  -v /opt/leanclash-kata/manager:/opt/leanclash \
  localhost/leanclash:latest
```

* **使用效果**：局域网中其他设备或客户端只需将**默认网关**与 **DNS** 设定为 `192.168.1.88`，即可透明享受高速科学代理；即使该代理实例遭遇高压甚至未知攻击，物理宿主机与物理局域网其他服务依然安然无恙。

#### 部署方式三：nerdctl + containerd systemd 单元（带高级 sysctl 调优）
在以 containerd 为容器运行时的生产主机上，可直接使用 nerdctl 启动并注入专有 Guest 内核调优参数（如开启 BBR 拥塞控制、扩大连接队列与内存缓冲区）：

```bash
# 服务单元模板参考：使用 --runtime io.containerd.kata.v2 与 --network macv0
sudo systemctl enable --now leanclash-container
```

---

## 🔒 安全与反向代理

### 1. 监听安全说明
Web 控制面板默认**不设强制鉴权**，出于安全考虑：
* **默认仅监听 IPv4**（`0.0.0.0:8081`），不暴露外部未保护的 IPv6；
* 如需仅限本机回环访问，可在面板「系统设置」或 `/opt/leanclash/manager.yaml` 中修改：
  ```yaml
  daemon:
    web_addr: "127.0.0.1:8081"
  ```
* 修改后执行 `sudo systemctl restart leanclash`（或容器重启）生效。

### 2. Nginx 反向代理配置（带 Basic Auth 与 SSE 优化）

若需将面板暴露于公网，强烈建议通过 Nginx 进行反向代理并开启密码认证（Basic Auth）。由于面板使用了 **SSE（Server-Sent Events）** 实时推送状态，需对反代缓冲区与超时进行配置：

```nginx
server {
    listen 80;
    server_name mihomo.example.com;

    # 启用 HTTP 基本认证（通过 htpasswd -c /etc/nginx/.htpasswd 用户名 生成）
    auth_basic "LeanClash Control Panel";
    auth_basic_user_file /etc/nginx/.htpasswd;

    location / {
        proxy_pass http://127.0.0.1:8081;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # SSE 实时状态推送关键配置：关闭缓冲、放宽读写超时
        proxy_buffering off;
        proxy_cache off;
        proxy_read_timeout 24h;
        proxy_send_timeout 24h;
        proxy_set_header Connection '';
    }
}
```

---

## 📂 项目结构全景

```
.
├── main.go                     # CLI 命令行与守护进程主入口
├── build.sh                    # 原生双二进制与 Debian 容器构建脚本
├── deploy.sh                   # 一键打包 (--pack)、部署 (--install) 与卸载 (--remove) 脚本
├── docker-compose.yml          # 容器编排部署配置 (Host 模式)
├── docker-compose.kata.yml     # Kata Containers 微虚机强隔离部署配置
├── Dockerfile                  # 容器镜像定义
├── deploy/                     # Linux 标准系统目录映射（用于测试、模拟与一键打包）
│   ├── etc/
│   │   ├── mihomo/             # 配置文件模板 (config_general.yaml, 各模式参考配置)
│   │   └── systemd/system/     # 服务单元模板 (leanclash.service, mihomo@.service)
│   ├── opt/leanclash/          # 系统设置模板 (manager.yaml)
│   ├── usr/local/bin/          # 二进制文件落位与打包目录
│   └── var/lib/mihomo/         # 运行数据目录占位
├── internal/                   # 核心实现逻辑
│   ├── api/                    # RESTful 控制接口与 SSE 事件流
│   ├── cli/                    # 命令行控制逻辑
│   ├── config/                 # 配置模型加载、合并、校验
│   ├── intercept/              # nftables / 策略路由编排与回环避免核心
│   ├── lifecycle/              # 模式同生共死生命周期状态机
│   ├── netlink/                # Linux Netlink 路由与网络事件通信
│   ├── server/                 # HTTP/SSE 服务端装配
│   └── systemd/                # systemd D-Bus 实例交互后端
├── web/                        # Vue 3 前端工程
│   ├── src/                    # 前端源码（WinUI 3 + Mica 材质样式）
│   └── dist/                   # 前端编译产物（由 Go 二进制内嵌）
└── container/                  # 容器专用初始化与默认配置
```

---

## 📄 License

[MIT](LICENSE) © 2026 vxzman
