# 便捷游戏加速器服务端 (Easy Game Accelerator Server)

基于 [Xray-core](https://github.com/XTLS/Xray-core) 实现的 VLESS + REALITY 一键服务端启动工具。运行后自动生成密钥对、UUID、ShortID，并自动探测本机公网 IP，直接打印出可导入 Clash / Mihomo 等客户端的节点配置，无需手动拼接繁琐参数。

> A one-click VLESS + REALITY server bootstrap tool built on Xray-core. Automatically generates key pairs, UUID, and ShortID, detects the machine's public IP, and prints ready-to-use Clash/Mihomo node configs.

## ✨ 功能特性

- 🔑 自动生成 REALITY 密钥对（Curve25519）与 ShortID，无需手动执行 `xray x25519`
- 🌐 自动探测本机公网 IP，避免手动查询、复制粘贴出错
- 🧾 一键输出 Clash/Mihomo 风格 YAML 节点配置，复制即用
- 🛡️ 内置 VLESS + REALITY + `xtls-rprx-vision`，以正常网站（默认 `www.bing.com`）作为伪装目标，抗探测能力强
- ⚙️ 命令行参数自定义伪装域名（SNI）与节点名称，无需改代码

## 📦 环境要求

- Go 1.20+
- 依赖模块：
  - `github.com/google/uuid`
  - `golang.org/x/crypto/curve25519`
  - `github.com/xtls/xray-core`

## 🚀 快速开始

```bash
# 克隆项目
git clone https://github.com/<your-name>/game-accelerator-server.git
cd game-accelerator-server

# 安装依赖
go mod tidy

# 编译
go build -o game-accelerator-server main.go

# 运行（默认伪装域名 www.bing.com，节点名 Game-VLESS）
./game-accelerator-server

# 自定义伪装域名与节点名
./game-accelerator-server www.microsoft.com MyNode
```

运行后终端会依次输出：

1. 客户端连接信息（公网 IP / UUID / 公钥 / SNI / ShortID / 端口 / Flow）
2. 可直接粘贴进 Clash/Mihomo 配置文件的 YAML 节点片段
3. 服务端启动日志，监听 `443` 端口等待客户端连接

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

- **端口占用**：程序固定监听 `443` 端口，需要 root/管理员权限，并确保该端口未被其他服务（如 Nginx）占用。
- **公网 IP 获取依赖外网**：程序通过 `api.ipify.org` 获取公网 IP，若服务器无法访问外网或该服务不可用，需要自行替换探测方式或手动指定 IP。
- **单独的 JSON 配置文件**：Xray 服务端配置目前以字符串形式硬编码在代码中生成，实际部署建议拆分为独立的 `config.json` 文件单独维护，便于修改、复用与版本管理，避免每次调整配置都要重新编译。
- **Windows / Linux 文本格式差异**：如果将配置文件在 Windows 上编辑后再上传到 Linux 服务器运行，需要注意换行符差异（Windows 为 `CRLF`，Linux 为 `LF`），否则 Xray 解析 JSON 配置时可能报错。建议：
  - 使用 VSCode 等编辑器统一将文件保存格式设置为 `LF`；
  - 或上传后在 Linux 服务器执行 `dos2unix config.json` 进行转换；
  - Git 用户可在 `.gitattributes` 中添加 `*.json text eol=lf` 强制统一换行符。
- 请遵守所在地区法律法规，仅将本工具用于合法合规的游戏网络加速与测试场景。

## 🧩 项目分工

| 角色 | 内容 |
| --- | --- |
| 代码实现 | Claude（AI 辅助生成核心代码逻辑） |
| 问题发现 | 作者（发现 JSON 配置文件繁琐、Windows 与 Linux 换行符差异等实际部署问题） |
| 问题解决 | 作者 |
| 框架设计 | 作者 |
| 部署测试 | 作者 |
| Bug 反馈 | 作者 |

欢迎通过 Issue 反馈问题或提交 PR 改进项目。

## 📄 License

MIT
