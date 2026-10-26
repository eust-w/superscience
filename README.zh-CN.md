<div align="center">
  <img src="docs/readme/hero.svg" width="100%" alt="Superscience — 检索、执行、溯源">
  <h1>Superscience</h1>
  <p><strong>把文献、代码和研究证据，放进同一个本地 AI 工作台。</strong></p>
  <p>
    <a href="https://github.com/eust-w/superscience/actions/workflows/ci.yml"><img src="https://github.com/eust-w/superscience/actions/workflows/ci.yml/badge.svg" alt="CI 状态"></a>
    <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache--2.0-d99536" alt="Apache-2.0 许可证"></a>
    <a href="docs/readme/validation.md"><img src="https://img.shields.io/badge/status-research%20preview-547ac4" alt="研究预览版"></a>
  </p>
  <p><a href="#快速开始">快速开始</a> · <a href="#工作流一览">工作流</a> · <a href="docs/architecture.md">架构</a> · <a href="CONTRIBUTING.md">参与贡献</a> · <a href="https://github.com/eust-w/superscience/releases">版本发布</a></p>
  <p><a href="README.md">English</a> · <strong>简体中文</strong></p>
</div>

做研究时，我们经常在论文、终端、Notebook 和聊天窗口之间切换。Superscience 把这些工具放进同一个项目工作区，让产物与来源保持关联。项目文件和会话数据库保存在本机；模型调用和科学检索使用你配置的外部服务。

**无需 API 密钥即可开始：** 点击**打开本地工具**，使用文件、科学检索和 Notebook；AI 聊天会保持禁用，直到供应商与模型通过验证。仓库附带可复现的[入门项目](examples/research-starter/)。

**当前为研究预览版，提供源码。** 已执行的检查见[验证记录](docs/readme/validation.md)，分发要求见[发布流程](docs/releasing.md)。连接器已实现或平台能够构建，并不等于外部服务已全部实测，或桌面安装包已完成签名验收。

## 项目亮点

| 放在一起的能力 | 你可以做什么 |
| :--- | :--- |
| **研究与来源** | 从 51 个已注册科学连接器中检索文献、数据集、物理、天文、材料、机器人和生命科学资料，检查元数据与原始链接。 |
| **代码与计算** | 浏览和编辑项目文件，使用真实 PTY 终端、有状态 Jupyter Notebook 与语言服务器诊断。 |
| **模型与工具** | 配置模型供应商、自定义 Agent、工具权限、Skills 和 MCP 连接。 |
| **产物与溯源** | 保存研究产物、检查来源记录，导出或回放选定的研究结果包。 |
| **浏览器与桌面** | 通过本地 Go 服务使用 React 工作台，也可构建 Wails 桌面应用；CLI 和 ACP 入口共用后端运行时。 |
| **可选团队服务** | 独立部署 Cloud 服务，使用 OIDC、PostgreSQL 和兼容 S3 的对象存储。 |

连接器数量代表注册实现数量。外部数据源各自有可用性、凭据、限流和使用条款；请求失败时会明确报告，不会生成虚构检索结果。

## 工作流一览

![Superscience 真实 Jupyter 执行画面](docs/readme/notebook.jpg)

*真实本地工具流程：Python 内核分析 10 条明确标记的合成测量数据。模型服务当时不可用，AI 聊天保持禁用。[运行此示例](examples/research-starter/)。*

<details>
<summary>展开查看实时文献检索与本地分享回放</summary>

![带来源 DOI 的实时 Crossref 检索结果](docs/readme/search.jpg)

*Crossref 查询 robot learning 返回 20 条记录，选定文献已连同 DOI 保存为产物。*

![经过审批的本地产物分享](docs/readme/artifacts.jpg)

*选定产物发布到本地服务并验证回放；这里展示的是本地功能，不是已部署的 Cloud 服务。*

</details>

[![观看 81 秒项目介绍](docs/readme/launch-poster.png)](https://www.bilibili.com/video/BV1oCn96NEnh/)

[哔哩哔哩](https://www.bilibili.com/video/BV1oCn96NEnh/) · [YouTube](https://www.youtube.com/watch?v=l89Nk2juI5E) · [X](https://x.com/eustancewu/status/2114581844570321022) · [抖音](https://www.douyin.com/video/7700838064293186851) · [小红书主页](https://www.xiaohongshu.com/user/profile/5c6f6397000000001103bb3f) · [视频制作源码](media/launch/)

[下载横竖版视频、字幕和封面](https://github.com/eust-w/superscience/releases/tag/v0.1.0-alpha.1) · 当前为源码与媒体预览版，尚不包含签名桌面安装包。

1. **检索。** 点击**打开本地工具**并打开项目，搜索科学数据源，核对记录与来源链接，再保存为产物。需要 AI 辅助时，再验证供应商和模型。
2. **执行。** 在 Notebook 或终端中分析材料；网络、写入和执行操作出现权限请求时，先检查再批准。
3. **溯源。** 保留输出与来源记录，选择要分享的产物，导出结果包供检查或回放。

![Superscience 架构与科研工作流](docs/readme/architecture.svg)

*架构示意图；组件关系和数据边界见[架构说明](docs/architecture.md)。*

## 快速开始

准备 **Go 1.26.10 或更新版本**（[`go.mod`](go.mod) 声明的最低要求）、Node.js 24 和 pnpm 11.7.0，然后运行：

```bash
git clone https://github.com/eust-w/superscience.git
cd superscience
pnpm install --frozen-lockfile
pnpm --dir frontend build
go run ./cmd/superscience serve
```

打开 **http://127.0.0.1:4096/**，点击**打开本地工具**，将 `examples/research-starter` 打开为项目，浏览 CSV 并运行 Python 或 Notebook 示例。10 条合成测量数据的平均延迟为 **125.20 ms**；这些是人为构造的演示数值，不是性能基准。

需要 AI 聊天时，选择供应商，填写有权限使用的模型 ID 和 API 密钥，并完成验证。模型调用可能产生费用。界面目前通过 **macOS Keychain** 持久保存凭据；Linux 和 Windows 可通过[环境变量 CLI](docs/getting-started.md#environment-backed-cli)调用供应商，原生凭据存储仍属于后续工作。本地工具无需这一步凭据配置，但科学检索仍需联网。

需要 Python Notebook 时，先安装 worker 依赖和内核，再启动服务：

```bash
python3 -m venv .venv
.venv/bin/python -m pip install -r workers/python/requirements.txt
.venv/bin/python -m ipykernel install --prefix "$PWD/.venv" --name python3
export SUPERSCIENCE_PYTHON="$PWD/.venv/bin/python"
export JUPYTER_PATH="$PWD/.venv/share/jupyter"
go run ./cmd/superscience serve
```

隔离数据目录、前端开发、R/Julia 内核、桌面构建、CLI 用法与常见问题见[上手文档](docs/getting-started.md)。

## 权限与验证

Superscience 提供工作台和权限控制。获准执行的 Notebook、Shell、MCP 服务和语言服务器，仍使用当前操作系统用户的权限。需要进程隔离时请使用容器或虚拟机；处理不可信材料或部署 Cloud 前，请阅读[安全说明](SECURITY.md)。

```bash
pnpm --dir frontend exec playwright install chromium
make verify
```

验证入口覆盖 Go race 测试、vet 和漏洞扫描，前端类型检查、单测与构建，浏览器验收测试，生产依赖审计和生产占位符扫描。浏览器 E2E 使用明确命名的本地协议测试服务，用于验证集成，不代表真实模型质量。当前结果与限制见[验证记录](docs/readme/validation.md)。

本次发布验证已包含实际 Jupyter 执行和在线 Crossref 检索。尝试的供应商校验返回 HTTP 503，因此不宣称外部模型调用成功；这些结果分别记录。

## 后续方向与贡献

欢迎参与 Linux/Windows 原生凭据存储、更多连接器实测、完整工作区本地化和桌面签名分发。这些是后续工作方向，不代表已经完成，也不承诺完成日期。

可以从一个可复现问题、一份连接器文档、一项权限测试或一个清晰示例开始。请阅读[贡献指南](CONTRIBUTING.md)，[提交问题](https://github.com/eust-w/superscience/issues/new?template=bug_report.yml)或[提出功能建议](https://github.com/eust-w/superscience/issues/new?template=feature_request.yml)。如果项目对你有用，欢迎 Star，让更多研究者发现它。

## 文档导航

| 文档 | 内容 |
| :--- | :--- |
| [上手指南](docs/getting-started.md) | 安装、供应商凭据、Notebook、CLI 与常见问题 |
| [架构说明](docs/architecture.md) | 组件、请求流程、存储与扩展点 |
| [贡献指南](CONTRIBUTING.md) | 开发流程、验证方式与评审要求 |
| [安全说明](SECURITY.md) | 权限边界、私密漏洞报告和部署建议 |
| [发布流程](docs/releasing.md) | 打包、校验和、签名与发布 |
| [验证记录](docs/readme/validation.md) | 已执行的检查与验收边界 |

## 许可证与致谢

仓库原创代码与原创演示素材采用 [Apache-2.0](LICENSE)。依赖、模型、数据集和外部资源保留各自条款，详见[第三方声明](THIRD_PARTY_NOTICES.md)。

产品边界受到 OpenScience 启发，Superscience 使用 Go、Wails 和 React 实现。被 Git 忽略的 `reference/` 目录仅供本地分析，不属于分发源码。
