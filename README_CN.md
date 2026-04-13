# lucy

<div align="center">

  <img src="images/banner.png" alt="banner" width="80%" />

  <a href="README.md">English</a> | <a href="README_CN.md">中文</a>

  <h2>
    <div>服务器 · 群组服 · 整合包</div>
    <sup>……一行命令秒了</sup>
  </h2>

  <h3>现代化的 Minecraft 服务端环境管理器</h3>

  <img
    src="https://goreportcard.com/badge/github.com/mclucy/lucy"
    alt="Go Report Card"
  />
  <img
    src="https://github.com/mclucy/lucy/actions/workflows/github-code-scanning/codeql/badge.svg"
    alt="CodeQL"
  />
  <img
    src="https://img.shields.io/github/last-commit/mclucy/lucy"
    alt="Last Commit"
  />
  <img
    src="https://img.shields.io/github/languages/code-size/mclucy/lucy"
    alt="Code Size"
  />
  <img
    src="https://img.shields.io/github/license/mclucy/lucy"
    alt="License"
  />

</div>

> [!IMPORTANT]
> 本项目正在开发中且尚未完成。功能可能随时变化。
> 如有兴趣贡献或了解进展，请联系 <4rcadia.0@gmail.com>，或加入 [QQ 群](https://qm.qq.com/q/Sf65NVYaAi)。Discord 服务器即将上线。

## 简介

`lucy` 是一个面向 Minecraft 服务器的、具备环境感知能力的包管理与环境工具。它通过统一的命令行界面处理依赖解析、版本追踪、环境探测与风险告警。如果你用过 `apt`、`brew` 或 `npm`，`lucy` 对你来说会非常熟悉。

### 核心特性

<!-- TODO: Replace this section with .gif demo -->

- 自动解析依赖、处理冲突
- 支持 Modrinth、CurseForge、MCDR 插件目录等多个源
- 支持基于服务器环境的探测与环境推断
- 支持基于环境拓扑的状态展示与风险提示
- 非侵入式设计，所有操作独立于服务器运行时
- 支持 bash、zsh、fish、pwsh 的命令补全
- 美观的命令行输出
- 支持机器可读的输出格式，便于集成到 CI/CD 和脚本

## 🚀 快速开始

### 安装

> [!WARNING]
> 在首个 beta 发布前，除非你打算参与测试或贡献代码，否则不要安装。
> 任何在生产环境造成的数据丢失需要自行负责。

```bash
go install github.com/mclucy/lucy@latest
```

### 快速上手

```bash
mkdir my-server && cd my-server
lucy add fabric@latest
lucy add fabric/lithium@compatible
lucy status
java -jar fabric-server.jar
```

## 🛠️ 命令

`lucy` 提供一系列命令来管理服务器包并审计服务器环境。所有示例在开发期间可能发生变化。

### `search` - 搜索包

跨支持的数据源搜索，支持过滤和排序。

```bash
lucy search fabric/carpet
lucy search carpet --source modrinth --index downloads
```

- `-i`, `--index` — 按 `relevance`（相关性）、`downloads`（下载量）或 `newest`（最新）排序
- `-c`, `--client` — 包含客户端模组
- `-s`, `--source` — 限定数据源（如 `modrinth`）
- `-l`, `--long` — 显示隐藏或折叠的内容

### `add` - 安装包

添加模组、插件或服务端核心。`lucy` 会自动解析依赖、校验平台兼容性，并以尽量非侵入的方式更新本地环境。这是当前最主要的直接动作工作流。

```bash
lucy add fabric/fabric-api@latest
lucy add neoforge/create --force
```

<!-- TODO: Add screenshot -->

### `status` - 服务器环境概览

`lucy status` 是一个面向 Minecraft 服务器环境的 `neofetch` 风格概览。它的目标是展示当前目录下 lucy 能检测、审计和理解到的环境信息：

- 游戏版本
- 服务端核心
- 模组平台
- 探测到的环境拓扑
- 运行状态与基础风险信号
- 模组/插件列表
- ...和更多

<!-- TODO: Add screenshot -->

### `info` - 包详情

获取包的元数据、描述和版本历史。

```bash
lucy info fabric/fabric-api@latest --long
```

<!-- TODO: Add screenshot -->

### `cache` - 管理下载缓存

列出或清除本地下载缓存。

```bash
lucy cache ls
lucy cache clear
```

`ls` - 列出缓存条目（支持 `--json` 和 `--no-style`）
`clear` - 清除所有缓存（支持 `--no-style`）

### 全局参数

- `--debug` — 显示调试日志
- `--log-file` — 输出日志文件路径
- `--print-logs` — 在控制台打印日志
- `--no-style` — 全局禁用彩色和样式化输出

---

## 📖 语法与概念

### 核心定义

**平台（platform）** 是修改 Minecraft 原版游戏的程序（如 NeoForge、Fabric、MCDR），作为一组包的共同依赖。**项目（project）** 是依赖一个或多个平台的软件，比如模组或插件。**包（package）** 是项目在特定平台和版本下的编译实例，也是你实际安装的实体。这些包共同构成了 `lucy` 会探测、审计、理解并管理的本地服务器环境。

### 包标识符

包使用 `平台/项目@版本` 的格式标识：

```text
fabric/fabric-api@1.2.3
    ↑        ↑        ↑
  平台     名称     版本
```

除项目名称外，其他部分均可省略。省略平台时，`lucy` 会从环境推断。版本可以是具体版本号、`@latest`（最新）或 `@compatible`（兼容，默认）。

支持的平台：`fabric`、`forge`、`neoforge`、`mcdr`、`minecraft`、`none`

支持的源：`modrinth`、`curseforge`、`github`、`mcdr`

---

## ⚖️ 许可证

本项目采用 [Apache 2.0 License](LICENSE) 许可。

*Logo 和美西螈像素艺术版权归 Mojang AB 所有，我们正在制作原创替代品。*
