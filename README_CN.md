# lucy

<div align="center">

  <img src="images/banner.png" alt="banner" width="80%" />

  <a href="README.md">English</a> | <a href="README_CN.md">中文</a>

  <h2>
    <div>描述你的 Minecraft 服务器，而不是从头开始构建</div>
    <sup>……只需要一行命令</sup>
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

`lucy` 是一个面向 Minecraft 服务器的环境管理器。它帮助你检查一台已经存在（当然，也可用于新建）的服务器、弄清楚当前环境里到底装了什么，再决定哪些部分交给 `lucy` 管。它通过统一的命令行界面处理依赖解析、版本追踪、环境探测与风险告警。

如果你用过 `apt`、`brew` 或 `npm`，`lucy` 的一部分操作会很熟悉。但它不是建立在“服务器一定是干净新建的”这个前提上，也不是要你把所有手工维护都交出去。它更适合这样一种场景：你已经有一台在跑的服务器，现在想先看清环境，再逐步把真正关心的那部分整理到可控状态。

### 核心特性

<!-- TODO: Replace this section with .gif demo -->

- 自动解析依赖、处理冲突
- 支持 Modrinth、CurseForge、MCDR 插件目录等多个源
- 支持以发现事实为先的服务器环境探测与环境推断
- 支持先接管已有服务器，再逐步整理和同步环境
- 允许 `lucy` 只管理你关心的那部分环境，而不是强制接管整台服务器
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
lucy init
lucy add fabric@latest
lucy add fabric/lithium@compatible
lucy status
java -jar fabric-server.jar
```

`lucy init` 的会先检查当前目录。如果这里已经是一台在运行或曾经运行过的服务器，`lucy` 会向你展示现状，再决定哪些部分交给它管理。

## 🛠️ 命令

`lucy` 提供一系列命令来管理服务器包并审计服务器环境。*所有示例在开发期间可能发生变化。*

### `init` - 初始化并接管 lucy 状态

检查当前目录、聚合已有服务器信息，并创建 `lucy` 的项目本地状态文件。

```bash
lucy init
lucy init --yes --game-version 1.21.4
lucy init --conflict abort
```

`lucy init` 会创建 `.lucy/config.toml`、`.lucy/manifest.toml` 和 `.lucy/lock.json`。

对于已有服务器，`lucy init` 更像一个“接管流程”。先发现 runtime、平台、目录与包，再让你决定哪些内容应该保持同步，哪些内容仍然留给人工管理。

- `-y`, `--yes`：跳过提示并接受默认值
- `--game-version`：在非交互初始化中设置游戏版本
- `-c`, `--conflict`：为已有 `.lucy` 文件选择 `preserve`、`abort` 或 `overwrite`

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

添加模组、插件或服务端核心。`lucy` 会自动解析依赖、校验平台兼容性，并以尽量非侵入的方式更新本地环境。`add` 同时会把包纳入服务器的 manifest 和 lock 管理，保持环境的可追踪和可重现。

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

**平台（platform）** 是一个包面向的兼容面或运行面，例如 Fabric、Forge、NeoForge、MCDR 或原版 Minecraft。**项目（project）** 是一段可安装的软件，比如模组、插件或服务端扩展。**包（package）** 是项目在特定平台和版本下的编译实例，也是你真正安装的实体。这些包共同构成了 `lucy` 会探测、接管、审计并管理的本地服务器环境。

不同平台扮演的角色并不完全相同。例如，MCDR 是一个独立的插件框架/控制层，它从服务端进程外部管理 Minecraft 服务器，并不是 Bukkit 的派生插件层。

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
