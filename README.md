# 便捷游戏加速器服务端 V2不支持.ini配置文件版本 (Easy Game Accelerator Server V2)
## 额外提示：基于个人隐私与法律法规方面的考虑，所以只能请各位自行编译。
基于 [Xray-core](https://github.com/XTLS/Xray-core) 实现的 VLESS + REALITY 一键服务端启动工具。运行后自动生成密钥对、UUID、ShortID，并自动探测本机公网 IP，直接输出可导入 Clash / Mihomo 等客户端的节点配置，无需手动拼接繁琐参数。

V2 版本在 V1 基础上引入了**多用户支持、SQLite 持久化、编译期配置注入、健康检查 API** 等生产级特性，同时修复了 V1 中 JSON 配置硬编码、换行符差异等实际部署痛点。

> A one-click VLESS + REALITY server bootstrap tool built on Xray-core. Automatically generates key pairs, UUID, and ShortID, detects the machine's public IP, and prints ready-to-use Clash/Mihomo node configs. V2 adds multi-user support, SQLite persistence, compile-time config injection, and health-check APIs.

## 🤖 AI 生成声明

> **本项目 V2 版本代码的 90% 以上由 DeepSeek 生成**，少量错误逻辑和边界问题由作者发现并反馈修正。
>
> 作者角色：
> - 🏗️ 项目框架设计
> - ⚙️ 编译参数与部署配置设计
> - 🧪 运行测试与 Bug 反馈
> - 🔄 人机协作流程协调
>
> 这是一个典型的 **AI 辅助编程实践项目**。V2 相对 V1 的演进过程（从单用户到多用户、从内存到 SQLite、从硬编码到编译期注入）展示了当前大语言模型在**渐进式重构**和**生产化改造**中的能力与局限性。通过人机协作，大幅提升了开发效率，同时保证了代码质量。

## ✨ 功能特性

### V2 新增特性

- 👥 **多用户支持**：单进程支持 1~3 个用户，每个用户拥有独立 UUID 与 ShortID
- 🗄️ **SQLite 持久化**：重启不丢用户配置，支持自动扩容（用户数增加时增量生成）
- 🧩 **编译期配置注入**：通过 `-ldflags -X` 注入用户数、端口、存储模式等参数，无需改代码重新编译
- 📄 **YAML 自动落盘**：为每个用户生成独立的 Clash/Mihomo 节点文件，方便批量分发
- 🩺 **健康检查 API**：内置 `/healthz`（存活探针）与 `/hxray`（就绪探针），适配 K8s / Docker Compose 健康检查
- 🔑 **共享 REALITY 密钥对**：所有用户共享同一对 Curve25519 密钥，仅 UUID/ShortID 独立，符合 REALITY 协议设计
- 🛑 **优雅退出**：响应 SIGINT/SIGTERM，关闭 Xray 实例后再退出进程

### 沿用 V1 的核心能力

- 🔑 自动生成 REALITY 密钥对（Curve25519）与 ShortID
- 🌐 自动探测本机公网 IP，支持 `ip` / `domain` 两种连接地址模式
- 🧾 输出 Clash/Mihomo 风格 YAML 节点配置，复制即用
- 🛡️ 内置 VLESS + REALITY + `xtls-rprx-vision`，以正常网站（默认 `www.bing.com`）作为伪装目标，抗探测能力强
- ⚙️ 伪装域名（SNI）与节点名前缀可通过编译参数自定义

## 📦 环境要求

- Go 1.20+（V2 使用 `modernc.org/sqlite` 纯 Go 实现，无需 CGO）
- 依赖模块：
  - `github.com/google/uuid`
  - `golang.org/x/crypto/curve25519`
  - `github.com/xtls/xray-core`
  - `modernc.org/sqlite`（纯 Go SQLite 驱动，CGO_ENABLED=0 也可编译）

## 🚀 快速开始

### 1. 编译

V2 采用**编译期配置注入**，所有运行参数通过 `-ldflags -X` 传入，无需在代码中硬编码修改：

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath   -ldflags "-s -w    -X main.cfgUsers=3    -X main.cfgPort=8206    -X main.cfgMode=ip    -X main.cfgDomain=    -X main.cfgStorage=sql    -X main.cfgServerName=www.bing.com    -X main.cfgProxyName=ZhiLiaoYa-Game-VLESS    -X main.cfgAPIEnabled=true    -X main.cfgAPIListen=127.0.0.1    -X main.cfgAPIPort=8207"   -o my-xray-server .
```

> 💡 `-trimpath -s -w` 去除路径信息与调试符号，减小二进制体积；`CGO_ENABLED=0` 保证在 Alpine 等无 glibc 环境也能运行。

### 2. 运行

```bash
# 需要 root 权限（监听 443 端口）
sudo ./my-xray-server
```

运行后终端依次输出：

1. 运行模式摘要（用户数 / 连接模式 / 存储模式 / 端口）
2. 连接地址（自动探测的公网 IP 或配置的域名）
3. 每个用户的客户端连接信息（UUID / ShortID / PublicKey 等）
4. 每个用户的 YAML 配置文件路径（`zhiliaoya/yaml/client-N.yaml`）
5. Xray 启动日志，监听指定端口等待客户端连接

### 3. 配置参数一览

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `cfgUsers` | `1` | 用户数量（1~3） |
| `cfgPort` | `8206` | 监听端口 |
| `cfgMode` | `ip` | 连接地址模式：`ip`（自动探测）或 `domain`（手动指定） |
| `cfgDomain` | 空 | `domain` 模式下的域名，`ip` 模式留空即可 |
| `cfgStorage` | `sql` | 存储模式：`none`（无存储）或 `sql`（SQLite 持久化） |
| `cfgServerName` | `www.bing.com` | REALITY 伪装域名（SNI） |
| `cfgProxyName` | `Game-VLESS` | 节点名称前缀，自动追加 `-1/-2/-3` |
| `cfgAPIEnabled` | `true` | 是否启用健康检查 API |
| `cfgAPIListen` | `127.0.0.1` | 健康检查 API 监听地址 |
| `cfgAPIPort` | `8207` | 健康检查 API 监听端口 |

### 4. 目录结构

运行后自动生成以下目录：

```
zhiliaoya/
├── mydata/
│   └── xray.db          # SQLite 用户数据库
└── yaml/
    ├── client-1.yaml    # 用户 1 的 Clash/Mihomo 配置
    ├── client-2.yaml
    └── client-3.yaml
```

## 🩺 健康检查 API

V2 内置两个健康检查端点，适配 Kubernetes / Docker Compose 健康检查：

| 端点 | 用途 | 说明 |
|------|------|------|
| `GET /healthz` | 存活探针（Liveness） | 进程能响应即 200，不依赖 Xray 状态 |
| `GET /hxray` | 就绪探针（Readiness） | Xray 未启动时返回 503，就绪后返回 200 + JSON 状态 |

`/hxray` 返回示例：

```json
{
  "ok": true,
  "xray_up": true,
  "uptime_sec": 3600,
  "listen_port": 8206,
  "user_count": 3,
  "last_error": "",
  "last_err_at": "0001-01-01T00:00:00Z"
}
```

**Kubernetes 探针配置示例：**

```yaml
livenessProbe:
  httpGet:
    path: /healthz
    port: 8207
  initialDelaySeconds: 5
  periodSeconds: 10

readinessProbe:
  httpGet:
    path: /hxray
    port: 8207
  initialDelaySeconds: 10
  periodSeconds: 10
```

**Docker Compose 健康检查示例：**

```yaml
services:
  xray-server:
    image: my-xray-server:latest
    ports:
      - "8206:8206"
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://127.0.0.1:8207/healthz"]
      interval: 30s
      timeout: 5s
      retries: 3
```

## 🖥️ 服务器推荐

节点体验很大程度取决于服务器线路质量，选购 VPS 时建议关注以下几点：

- **三网加速（BGP 多线 / CN2 GIA 优先）**：优先选择支持电信、联通、移动三网回程优化的机房，避免出现「联通/移动能连，电信卡顿」的情况。国内访问优先选 CN2 GIA、CU2/CMIN2 等优化线路。
- **全球加速覆盖**：如果需要多地区访问，优先选择自带 Anycast / 全球 CDN 加速网络的机房，或者干脆在日本、新加坡、美西、香港等主流地区各部署一个节点，客户端按延迟自动切换。
- **带宽 ≥ 5Mbps（约 700~600kb/s 视具体计费单位而定）**：注意区分服务商标注的是 `Mbps`（兆比特/秒，需除以 8 才是实际下载速度）还是 `MB/s`（兆字节/秒），游戏加速对稳定低延迟带宽比"跑分带宽"更重要，建议选择独享带宽或有明确保底带宽承诺的套餐，避免共享带宽超卖导致高峰期掉速。
- **低延迟优先于高带宽**：游戏场景对延迟（ping）和抖动更敏感，选购前建议先用官方测试 IP（多数服务商提供）实测到目标游戏服务器区域的延迟和丢包率，再决定购买。
- **可选参考方向**（非商业推荐，仅供选型参考，具体请以实际测速和最新评测为准）：
  - 国际主流云厂商：Vultr、DigitalOcean、AWS Lightsail、Oracle Cloud、Linode
  - 面向国内优化线路的服务商：搜索关键词「CN2 GIA」「三网回程」「BGP 多线」进行比价
  - 国内云厂商海外节点（都需要企业认证）：阿里云国际版、腾讯云国际版

> 💡 建议部署前先用小时/按量计费的套餐做实测，确认延迟、带宽、丢包表现满足需求后再转包月/包年，避免长期锁定到线路质量不佳的机房。

## ⚠️ 使用注意事项

- **端口占用**：程序监听 `443`（或 `cfgPort` 指定端口），需要 root/管理员权限，并确保该端口未被其他服务（如 Nginx）占用。
- **公网 IP 获取依赖外网**：`ip` 模式下程序通过 `api.ipify.org` 获取公网 IP，若服务器无法访问外网或该服务不可用，请改用 `cfgMode=domain` 并手动指定 `cfgDomain`。
- **SQLite 持久化与扩容**：`cfgStorage=sql` 时用户配置持久化到 `zhiliaoya/mydata/xray.db`。重启时若 `cfgUsers` 增大，会基于已有用户的密钥对增量生成新用户；若减小，则多余用户仍保留在库中但不会注册到 Xray 入站。
- **共享 REALITY 密钥对**：所有用户共享同一对 Curve25519 密钥（方案 B），仅 UUID 与 ShortID 独立。这是 REALITY 协议的设计约束——`privateKey` / `dest` / `serverNames` 是入站级别的，无法按用户区分。
- **Windows / Linux 文本格式差异**：V2 已通过编译期注入规避硬编码 JSON 配置文件，若需手动维护配置文件，仍建议在 VSCode 中将换行符统一为 `LF`，或在 Linux 上执行 `dos2unix` 转换。
- **无存储模式的易失性**：`cfgStorage=none` 时所有用户配置仅本次运行有效，重启后 UUID、ShortID、密钥对全部重新生成，仅适合临时测试。
- **API 安全**：健康检查 API 默认仅监听 `127.0.0.1`，请勿直接暴露到公网；如需外部访问，建议通过反向代理 + 认证保护。
- 请遵守所在地区法律法规，仅将本工具用于合法合规的游戏网络加速与测试场景。

## 🧩 项目分工

| 角色 | 内容 |
| --- | --- |
| 代码实现 | DeepSeek（AI 辅助生成 V2 核心代码逻辑，含多用户、SQLite、健康检查 API） |
| 问题发现 | 作者（发现 JSON 配置文件繁琐、Windows 与 Linux 换行符差异等实际部署问题） |
| 问题解决 | 作者（提出编译期注入、共享密钥对、优雅退出等设计方案） |
| 框架设计 | 作者（V1 → V2 演进路线、多用户方案、API 探针设计） |
| 部署测试 | 作者（编译、部署、健康检查验证） |
| Bug 反馈 | 作者（反馈运行时问题并协助定位） |

### V1 → V2 演进说明

| 维度 | V1 | V2 |
|------|----|----|
| 用户数量 | 单用户 | 1~3 用户 |
| 配置方式 | 命令行参数 / 代码硬编码 | 编译期 `-ldflags -X` 注入 |
| 存储 | 无（每次重启重新生成） | SQLite 持久化 + 自动扩容 |
| YAML 输出 | 仅打印到终端 | 落盘到 `zhiliaoya/yaml/` |
| 健康检查 | 无 | `/healthz` + `/hxray` 双探针 |
| 退出方式 | 强制终止 | 优雅退出（SIGINT/SIGTERM） |
| 密钥管理 | 单对密钥 | 共享密钥对 + 独立 UUID/ShortID |

> V2 的演进过程由作者提出设计方向，DeepSeek 完成主体代码生成，作者负责编译部署、测试验证与问题反馈。这体现了 AI 辅助编程在**渐进式重构**场景下的典型工作流。

## 📄 License

MIT

## 🤝 贡献

欢迎通过 Issue 反馈问题或提交 PR 改进项目。

### 特别欢迎以下贡献：
- 🌟 更多用户的并发支持（> 3 用户场景优化）
- 🌟 用户管理 API（增删用户、查询流量统计）
- 🌟 多端口 / 多入站支持
- 🌟 配置热重载（无需重启更新用户）
- 🌟 性能优化与 Bug 修复

---

**Easy Game Accelerator Server V2** - 从能用到好用的一步之遥 🚀

> *编译期注入配置，一次构建，多环境部署*
>
> *SQLite 持久化 + 自动扩容，重启不丢配置*
>
> *健康检查双探针，适配 K8s / Docker Compose*
>
> *多用户共享密钥，符合 REALITY 协议设计*
>
> *本项目是 AI 辅助编程的实践成果，展示了人机协作在渐进式重构中的高效开发模式*
