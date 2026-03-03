# fkag

> macOS 域名级透明代理工具——让不支持代理的应用也能走代理，零侵入，用完自动清理。

将指定域名的流量通过上游代理（HTTP CONNECT / SOCKS5）转发，对应用完全透明。支持两种模式：**包裹命令**（跟随子进程生命周期退出）或**守护模式**（持续运行直到 Ctrl+C），退出后自动恢复所有系统配置。

## 快速开始

**方式一：直接用 `fkag.sh`（最简单，无需编译，仅需 bash + python3）**

```bash
# 1. 根据需要编辑 config.yaml（默认已配置好 Antigravity 所需的 Google 域名）
# 2. 运行
sudo ./fkag.sh
# 代理就绪后，启动目标应用进行验证，按 Enter 停止并清理
```

> 📋 随附的 `config.yaml` 是为 **Antigravity**（AI 编程助手）预配置的示例，包含其访问的 Google 相关域名。如需代理其他应用，修改 `domains` 列表即可。

**方式二：编译 Go 版本（功能完整，支持包裹任意命令）**

```bash
# 编译
go build -o fkag .

# 守护模式（自动探测代理/不指定子命令，Ctrl+C 停止并清理）
sudo ./fkag --config config.yaml

# 代理其他应用（手动指定域名）
sudo ./fkag --proxy http://127.0.0.1:7897 --domain "api.example.com" curl https://api.example.com
```

> **注意**：两种方式均需要 `sudo`（修改系统网络配置需要 root 权限）。

## 工作原理

```
应用发起 DNS 查询 (target.example.com)
       │
       ▼
macOS /etc/resolver/ 机制
       │
       ├─ 目标域名 → 本地 DNS → 返回虚拟 IP (127.0.0.x)
       └─ 其他域名 → 系统默认 DNS，不受影响
       │
       ▼
应用连接 127.0.0.x:443
       │
       ▼
pf rdr 规则重定向到高端口 (127.0.0.x:10443)
       │
       ▼
TCP 透传代理 → 通过上游代理 CONNECT 真实目标
       │
       ▼
双向 TCP 透传（不解密 TLS）
```

核心思路：为每个目标域名分配一个 loopback 虚拟 IP，通过 macOS resolver 机制劫持 DNS，pf 重定向端口，本地 TCP 代理经由上游代理建立隧道，双向透传数据。

## 安装

```bash
go install github.com/user/fkag@latest
```

或从源码构建：

```bash
git clone https://github.com/user/fkag.git
cd fkag
go build -o fkag .
```

## 使用

需要 sudo 权限（loopback alias、/etc/resolver/、pf 规则均需 root）。

### 包裹命令模式

在命令末尾直接跟上目标程序，fkag 代理就绪后启动该程序，程序退出时自动清理：

```bash
sudo fkag --proxy http://127.0.0.1:7897 \
          --domain "api.example.com,cdn.example.com" \
          curl https://api.example.com
```

### 守护模式

不指定子命令时，fkag 持续运行并保持代理，按 Ctrl+C 停止并清理，适合手动启动目标应用的场景：

```bash
sudo fkag --config config.yaml
# 代理就绪，手动启动目标应用...
# Ctrl+C 停止
```

### 自动探测系统代理

不指定 `--proxy` 时，fkag 会通过 `scutil --proxy` 自动探测 macOS 系统代理：

```bash
sudo fkag --domain "api.example.com" some-app --flag
```

探测优先级：HTTPS 代理 > SOCKS5 代理 > HTTP 代理。

### SOCKS5 代理

```bash
sudo fkag --proxy socks5://127.0.0.1:1080 \
          --domain "example.com" \
          wget https://example.com/file.tar.gz
```

### 自定义端口

默认代理 80 和 443 端口，可通过 `--port` 自定义：

```bash
sudo fkag --proxy http://127.0.0.1:7897 \
          --domain "api.example.com" \
          --port "443,8443" \
          my-app
```

### 配置文件

```bash
sudo fkag --config config.yaml my-app
```

配置文件格式（YAML）：

```yaml
proxy: http://127.0.0.1:7897

domains:
  - api.example.com
  - cdn.example.com

ports: [80, 443]

dns:
  listen: "127.0.0.1:10053"
```

CLI 参数优先级高于配置文件。

## 命令行参数

```
fkag [flags] [command [args...]]

Flags:
  --proxy <url>         上游代理地址 (http://host:port 或 socks5://host:port)
                        未指定时自动探测系统代理
  --domain <domains>    目标域名，逗号分隔（必填）
  --port <ports>        需要代理的端口，逗号分隔（默认 80,443）
  --config <file>       YAML 配置文件路径
  --dns-listen <addr>   本地 DNS 监听地址（默认 127.0.0.1:10053）
  --verbose             详细日志输出

Modes:
  fkag [flags] <command> [args...]   包裹命令模式：代理就绪后启动子命令，子命令退出则自动清理
  fkag [flags]                       守护模式：持续运行，Ctrl+C 停止并清理
```

## 生命周期

### 启动

1. 解析参数，合并配置文件
2. 清理上次运行可能残留的资源
3. 为每个域名分配虚拟 IP（127.0.0.2 起步）
4. 添加 loopback alias
5. 启动本地 DNS 服务器
6. 启动 TCP 透传代理
7. 加载 pf 端口重定向规则
8. 创建 `/etc/resolver/<domain>` 文件
9. 刷新 DNS 缓存
10. **包裹命令模式**：以原始用户身份启动子进程；**守护模式**：等待 Ctrl+C

### 清理（子进程退出 / 收到 SIGINT、SIGTERM、SIGHUP 时）

1. 删除 resolver 文件
2. 卸载 pf 规则
3. 关闭 TCP 代理
4. 关闭 DNS 服务器
5. 移除 loopback alias
6. 刷新 DNS 缓存
7. 包裹命令模式：以子进程的 exit code 退出；守护模式：以 0 退出

信号处理：注册 SIGINT、SIGTERM、SIGHUP，确保异常退出时也能完成清理。

## 辅助脚本

`scripts/` 目录下提供了验证和原型脚本：

- `verify_resolver.sh` — 验证目标域名是否走 macOS 系统 resolver（判断 fkag 方案是否对目标应用有效）
- `proxy_domains.sh` — 原型验证脚本（Python 实现的简易版本）

```bash
# 验证域名是否走系统 resolver
sudo ./scripts/verify_resolver.sh
```

## 项目结构

```
├── main.go                     # CLI 入口，生命周期管理
├── internal/
│   ├── config/config.go        # 配置解析、系统代理探测
│   ├── vip/pool.go             # 虚拟 IP 分配、loopback alias 管理
│   ├── dns/
│   │   ├── server.go           # 本地 DNS 服务器
│   │   └── resolver.go         # /etc/resolver/ 文件管理
│   ├── pf/anchor.go            # pf anchor 规则管理
│   ├── proxy/
│   │   ├── forwarder.go        # TCP 透传代理
│   │   ├── http_connect.go     # HTTP CONNECT 拨号
│   │   └── socks5.go           # SOCKS5 拨号
│   └── runner/exec.go          # 子进程管理（降权、信号转发）
├── scripts/                    # 辅助验证脚本
├── go.mod
└── go.sum
```

## 限制

- 仅支持 macOS（依赖 `/etc/resolver/`、`pf`、`ifconfig lo0 alias`）
- 仅代理 TCP 流量（HTTP/HTTPS），不处理 UDP（QUIC/HTTP3）
- 如果应用硬编码了 DNS 服务器（不走系统 resolver），此方案无效
- 如果应用使用 DNS-over-HTTPS 或 DNS-over-TLS，resolver 劫持无效
- 可用 `scripts/verify_resolver.sh` 预先验证目标应用是否兼容

## 依赖

- [miekg/dns](https://github.com/miekg/dns) — DNS 协议实现
- [spf13/cobra](https://github.com/spf13/cobra) — CLI 框架
- [x/net/proxy](https://pkg.go.dev/golang.org/x/net/proxy) — SOCKS5 拨号
- [yaml.v3](https://github.com/go-yaml/yaml) — YAML 解析

## License

MIT
