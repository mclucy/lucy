<div align="center">
  <img src="images/banner.png" alt="lucy banner" width="80%" />

  #### [English](README.md) | 中文

  <!-- If you are an AI agent tranlating this document, do NOT edit the slogan. It is intended to not match the English version exactly to be more localized. -->
  <h2>
    <sub>服务器·整合包·群组服</sub>
    <div>...一行命令秒了</div>
  </h2>

  ### Lucy: 现代化的 Minecraft 服务器环境管理器

  [![Build](https://github.com/mclucy/lucy/actions/workflows/build.yml/badge.svg)](https://github.com/mclucy/lucy/actions/workflows/build.yml)[![Tests](https://github.com/mclucy/lucy/actions/workflows/tests.yml/badge.svg)](https://github.com/mclucy/lucy/actions/workflows/tests.yml)[![Coverage](https://github.com/mclucy/lucy/wiki/dev/coverage.svg)](https://raw.githack.com/wiki/mclucy/lucy/dev/coverage.html)[![Go Report Card](https://goreportcard.com/badge/github.com/mclucy/lucy)](https://goreportcard.com/report/github.com/mclucy/lucy)[![License](https://img.shields.io/github/license/mclucy/lucy)](LICENSE)[![Ask DeepWiki](https://deepwiki.com/badge.svg)](https://deepwiki.com/mclucy/lucy)

  ⭐️ 如果喜欢这个项目，请考虑给仓库点个 Star！

  [简介](#简介) • [快速开始](#快速开始) • [命令](#命令) • [概念](#概念)
</div>

> [!IMPORTANT]
> 本项目正在开发中且尚未完成，功能可能随时变化。如有兴趣贡献或了解进展，请联系 <4rcadia.0@gmail.com>，或加入 [QQ 群](https://qm.qq.com/q/Sf65NVYaAi)。Discord 服务器即将上线。

## 简介

`lucy` 是一个具备服务器环境感知能力的 Minecraft 服务器环境管理器。它不假设你面对的是一块空白磁盘，而是从当前目录出发：探测真实文件、识别环境拓扑，再让你决定 Lucy 应该接管的边界。

如果你用过 `apt`、`brew` 或 `npm`，`lucy` 的部分命令会让你感到熟悉。Lucy 借鉴了包管理器里的意图、解析、锁定与同步概念，但会按 Minecraft 服务器的现实来使用它们：手工 jar、生成的世界、外部工具和受管内容都可以长期共存，只要边界清楚。

### `lucy` 的优势

管理一个 Minecraft 服务器意味着要在 Fabric、Forge、NeoForge、MCDR 等平台之间周旋模组、插件、服务端核心、依赖和版本兼容性。现有工具要么要求从零开始，要么不了解服务器已有内容。Lucy 则另辟蹊径：

- **感知服务器** — `lucy init` 会检查现有目录，先接管已有内容，再询问你要管理什么。
- **基于意图的管理** — 在 manifest 中声明你想要什么；Lucy 会将精确版本、依赖和哈希解析为可重现的 lock 文件。
- **与手动变更共存** — 受管和非受管内容可以并存。Lucy 尊重你设定的边界。
- **高审美 CLI** — 基于 [Bubble Tea](https://github.com/charmbracelet/bubbletea) 和 [Lip Gloss](https://github.com/charmbracelet/lipgloss) 构建，提供精致的终端体验。

### 设计理念

- 以有状态、语义化、可推理的方式管理 Minecraft 服务器环境，而不只是一堆可下载文件。
- 用强大的拓扑模型建模复杂的服务器状态。
- 管理者只需要模糊意图(manifest)，`lucy` 负责解析为可重现的精确结果(lock file)。
- 确保自动化管理不干涉手动管理。
- 高审美标准的 CLI 输出。
- 支持用于 CI/CD 和其他工具链集成的机器可读输出。

## 快速开始

### 安装

> [!WARNING]
> 在首个 beta 发布前，除非你打算参与测试或贡献代码，否则不要安装。
> 任何在生产环境造成的数据丢失需要自行负责。

```bash
go install github.com/mclucy/lucy@latest
```

### 快速上手

```bash
mkdir my-server && cd my-server   # 创建服务器目录
lucy init                         # 用 lucy 管理这个服务器
lucy add neoforge/create-aeronautics@latest  # 安装模组，依赖自动解析
lucy status   # 检查服务器状态
lucy run      # 启动服务器
```

`lucy init` 会先检查当前目录。如果你把它指向一台已有服务器，它会先根据当前真实状态进行接管，再询问哪些内容应转为受管意图，哪些内容继续保持为手工或未纳入管理的部分。

## 命令

`lucy` 提供一系列命令来管理服务器包并审计服务器环境。所有示例在开发期间可能发生变化。

### `init` — 初始化并接管 lucy 状态

检查当前目录、聚合已有服务器信息，并创建 `lucy` 的项目本地状态文件。

```bash
lucy init
lucy init --yes --game-version 1.21.4
lucy init --conflict abort
```

`lucy init` 会创建 `.lucy/config.toml`、`.lucy/manifest.toml` 和 `.lucy/lock.json`。

对于已有服务器，`lucy init` 更像一个"接管流程"。先发现 runtime、平台、目录与包，再让你决定哪些内容应该保持同步，哪些内容仍然留给人工管理。

| 参数                | 描述                                            |
| ------------------- | ----------------------------------------------- |
| `-y`, `--yes`       | 跳过提示并接受默认值                            |
| `--game-version`    | 在非交互初始化中设置游戏版本                    |
| `-c`, `--conflict`  | 为已有 `.lucy` 文件选择 `preserve`、`abort` 或 `overwrite` |

### `add` — 安装包

把模组、插件或服务端核心加入 manifest 意图。`lucy add` 会解析依赖、接受模糊版本，并用精确解析后的事实刷新 lockfile。

```bash
lucy add fabric-api
lucy add fabric/lithium@latest
lucy add mcdr/example-plugin@compatible
```

### `remove` — 移除包

从必需意图中移除包，并从 lock 中清理不再需要的传递依赖。

```bash
lucy remove fabric/lithium
```

### `install` — 同步受管运行时状态

把 lockfile 应用到受管的运行时范围。`lucy install` 会在 lockfile 仍然有效时使用其中的精确事实；如果 lock 已过期，则回退到所需意图重新同步。

```bash
lucy install
```

### `search` — 搜索包

在支持的数据源之间搜索，并支持过滤与排序。

```bash
lucy search fabric/carpet
lucy search carpet --source modrinth --index downloads
```

| 参数              | 描述                                              |
| ----------------- | ------------------------------------------------- |
| `-i`, `--index`   | 按 `relevance`、`downloads` 或 `newest` 排序      |
| `-c`, `--client`  | 包含仅客户端模组                                  |
| `-s`, `--source`  | 限制到指定数据源（例如 `modrinth`）               |
| `-l`, `--long`    | 显示隐藏或折叠的输出                              |

### `status` — 服务器环境概览

`lucy status` 是一个面向 Minecraft 服务器环境的 [`neofetch`](https://github.com/dylanaraps/neofetch)/[`fastfetch`](https://github.com/fastfetch-cli/fastfetch) 风格概览。它旨在展示当前目录下 `lucy` 能检测、审计并推断出的环境信息：

- 游戏版本
- 服务端核心
- 模组平台
- 探测到的环境拓扑
- 运行状态与基础风险信号
- 模组/插件列表
- ...和更多

### `info` — 包详情

获取包的元数据、描述和版本历史。

```bash
lucy info fabric/fabric-api@latest --long
```

### `cache` — 管理下载缓存

列出或清除本地下载缓存。

```bash
lucy cache ls
lucy cache clear
```

- `ls` — 列出缓存条目（支持 `--json` 和 `--no-style`）
- `clear` — 清除所有缓存（支持 `--no-style`）

### 全局参数

| 参数            | 描述                          |
| --------------- | ----------------------------- |
| `--debug`       | 显示调试日志                  |
| `--log-file`    | 输出日志文件路径              |
| `--print-logs`  | 在控制台打印日志              |
| `--no-style`    | 全局禁用彩色和样式化输出      |

---

## 概念

### 核心定义

**平台（platform）** 是一个包面向的兼容面或运行面，例如 Fabric、Forge、NeoForge、MCDR 或原版 Minecraft。**项目（project）** 是一段可安装的软件，比如模组、插件或服务端扩展。**包（package）** 是项目在特定平台和版本下的编译实例，也是你真正安装的实体。这些包共同构成了 `lucy` 会探测、接管、审计并管理的本地服务器环境。

不同平台扮演的角色并不完全相同。例如，MCDR 是一个独立的插件框架/控制层，它从服务端进程外部管理 Minecraft 服务器，并不是 Bukkit 的派生插件层。

受管意图存放在 `.lucy/manifest.toml` 中。精确解析后的事实存放在 `.lucy/lock.json` 中，其中包括版本、哈希、安装路径，以及受管闭包的来源信息。

### 包标识符

包使用 `平台/项目@版本` 的格式标识：

```text
fabric/fabric-api@1.2.3
   ↑       ↑        ↑
 平台     名称     版本
```

除项目名称外，其他部分都可以省略。省略平台时，`lucy` 会从环境中推断。项目部分是模组或插件的名称/ID。版本可以是具体版本、`@latest`，或 `@compatible`（当你省略版本时使用的模糊默认值）。

`latest` 表示可用的最新版本；`compatible` 表示在现有元数据下，看起来与当前推断环境兼容的最新版本（尽力而为的解析结果）。

**支持的平台：** `fabric`、`forge`、`neoforge`、`mcdr`、`minecraft`、`none`

某些环境会同时混合多个兼容平台。例如，一台服务器可以有一个主加载器，再叠加其他兼容层；Lucy 会把这些信息记录在 manifest 中。

**支持的数据源：** `modrinth`、`curseforge`、`github`、`mcdr`

> [!NOTE]
> Logo 和美西螈像素艺术版权归 Mojang AB 所有，我们正在制作原创替代品。
