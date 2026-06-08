<div align="center">
  <img src="images/banner.png" alt="lucy banner" width="80%" />

#### [English](README.md) | 中文

### Lucy

<h3>
  <sup>现代的 Minecraft 服务器包管理器</sup>
</h3>

  [![Build](https://github.com/mclucy/lucy/actions/workflows/build.yml/badge.svg)](https://github.com/mclucy/lucy/actions/workflows/build.yml) [![Tests](https://github.com/mclucy/lucy/actions/workflows/tests.yml/badge.svg)](https://github.com/mclucy/lucy/actions/workflows/tests.yml) [![Coverage](https://github.com/mclucy/lucy/wiki/dev/coverage.svg)](https://raw.githack.com/wiki/mclucy/lucy/dev/coverage.html) [![Go Report Card](https://goreportcard.com/badge/github.com/mclucy/lucy)](https://goreportcard.com/report/github.com/mclucy/lucy) [![License](https://img.shields.io/github/license/mclucy/lucy)](LICENSE) [![Ask DeepWiki](https://deepwiki.com/badge.svg)](https://deepwiki.com/mclucy/lucy)
</div>

> [!IMPORTANT]
> 本项目正在开发中且尚未完成，所有内容随时可能变化。联系 <4rcadia.0@gmail.com> 或加入 [QQ 群](https://qm.qq.com/q/Sf65NVYaAi) 参与贡献或了解进展。 \
> ⭐️ Star 谢谢喵

## 简介

声明你要的模组、插件和服务端核心。Lucy 帮你解析精确版本和依赖、写入 lock 文件、同步受管目录。放到已有服务器里，它从现状接手；从空目录开始也行，随你。

```bash
cd your-server
lucy init                         # 初始化 Lucy
lucy add fabric/lithium@latest    # 解析精确版本和依赖
lucy install                      # 从 lock 文件同步受管范围
```

- 把包加入 manifest，Lucy 解析精确版本和校验和。`lucy install` 负责下载安装。
- `lucy init` 先发现你的运行时、平台和已安装的包，再问要管哪些。其余的保持原样。
- Lucy 会构建一张服务器运行时图——Fabric、Forge、MCDR、Paper、Velocity——标注角色、能力和风险等级。`lucy status`、init 探测、兼容性解析都依赖这张图。

## 快速开始

> [!WARNING]
> 首个 beta 发布前不要安装，除非你打算测试或贡献代码。数据丢失自行负责。

```bash
go install github.com/mclucy/lucy@latest
```

```bash
mkdir my-server && cd my-server
lucy init                         # 接管这个目录
lucy add fabric/fabric-api@latest # 添加模组，依赖自动解析
lucy status                       # 查看检测结果
lucy install                      # 从 lock 文件同步受管包
```

## 命令

### `lucy init`

探测目录、发现服务器环境、创建状态文件。

```bash
lucy init
lucy init --yes --game-version 1.21.4
lucy init --conflict abort
```

在项目根目录创建 `lucy.yaml` 和 `lucy-lock.yaml`。

| 参数               | 描述                                     |
| ------------------ | ---------------------------------------- |
| `-y`, `--yes`      | 跳过提示，接受默认值                     |
| `--game-version`   | 非交互模式游戏版本（默认：`1.21`）       |
| `-c`, `--conflict` | `preserve`（默认）、`abort`、`overwrite` |

### `lucy add`

将模组、插件或服务端核心加入 manifest。Lucy 解析精确版本并重写 lock 文件。

```bash
lucy add fabric-api
lucy add fabric/lithium@latest
lucy add mcdr/example-plugin@compatible
```

| 参数              | 描述                     |
| ----------------- | ------------------------ |
| `-f`, `--force`   | 跳过版本、依赖和平台警告 |
| `--with-optional` | 一同安装上游可选依赖     |
| `--no-optional`   | 跳过可选依赖（默认）     |

### `lucy remove`

从 manifest 移除包。清理 lock 中不再需要的传递依赖。

```bash
lucy remove fabric/lithium
```

### `lucy install`

将 lock 文件同步到受管目录。lock 未过期时使用精确数据，过期时根据 manifest 重新解析。

```bash
lucy install
```

### `lucy search`

跨数据源搜索，支持过滤与排序。

```bash
lucy search fabric/carpet
lucy search carpet --source modrinth --index downloads --platform fabric
```

| 参数             | 描述                                                   |
| ---------------- | ------------------------------------------------------ |
| `-i`, `--index`  | 排序：`relevance`、`downloads`、`newest`               |
| `-c`, `--client` | 包含仅客户端模组                                       |
| `-s`, `--source` | 限制数据源：`modrinth`、`curseforge`、`github`、`mcdr` |
| `--platform`     | 过滤平台：`fabric`、`forge`、`neoforge`、`bukkit`      |
| `-l`, `--long`   | 完整输出                                               |
| `--json`         | 原始 JSON                                              |

### `lucy status`

展示当前目录下 Lucy 探测到的信息：游戏版本、服务端核心、平台、拓扑、运行时状态、风险信号和已安装包。

```bash
lucy status
lucy status --json --long
```

### `lucy info`

获取包的元数据、描述、作者和版本历史。

```bash
lucy info fabric/fabric-api@latest --long
```

| 参数             | 描述       |
| ---------------- | ---------- |
| `-s`, `--source` | 指定数据源 |
| `-l`, `--long`   | 完整输出   |
| `--json`         | 原始 JSON  |

### `lucy tree`

显示依赖树。

```bash
lucy tree --live --depth 2
```

| 参数      | 描述                           |
| --------- | ------------------------------ |
| `--live`  | 探测运行中服务器而非 lock 文件 |
| `--depth` | 限制深度（0 = 不限）           |
| `--json`  | 原始 JSON                      |

### `lucy leaves`

列出没有依赖者的包——用来判断哪些可以安全移除。

```bash
lucy leaves --live
```

| 参数     | 描述                           |
| -------- | ------------------------------ |
| `--live` | 探测运行中服务器而非 lock 文件 |
| `--json` | 原始 JSON                      |

### `lucy cache`

```bash
lucy cache ls              # 列出缓存的下载
lucy cache clear           # 清除所有下载缓存
lucy cache slugs ls        # 列出 slug 到包 ID 的映射
lucy cache slugs clear     # 清除 slug 映射
```

| 子命令        | 参数     |
| ------------- | -------- |
| `ls`、`list`  | `--json` |
| `clear`、`rm` |          |
| `slugs ls`    | `--json` |
| `slugs clear` |          |

### 未实现

已注册但尚未实现的命令：

| 命令      | 计划功能               |
| --------- | ---------------------- |
| `doctor`  | 诊断服务器环境中的风险 |
| `export`  | 导出配置或生成客户端   |
| `upgrade` | 升级已安装的包         |

### 全局参数

| 参数           | 描述             |
| -------------- | ---------------- |
| `--debug`      | 显示调试日志     |
| `--log-file`   | 输出日志文件路径 |
| `--print-logs` | 在控制台打印日志 |
| `--no-style`   | 禁用彩色输出     |

## 概念

### 包标识符

```text
[平台/]名称[@版本]
```

只有名称是必需的。省略平台，Lucy 从环境推断。省略版本，默认使用 `@compatible`（匹配你服务器的最新版本）。

```text
fabric/fabric-api@1.2.3
   ↑       ↑        ↑
  平台     名称     版本
```

`@latest` 是最新可用版本。`@compatible` 是默认值——根据当前环境进行尽力匹配。

manifest 中接受的平台：`none`、`fabric`、`forge`、`neoforge`、`mcdr`

类型系统也识别 `bukkit`、`sponge`、`velocity`、`bungeecord` 用于拓扑检测，但暂不支持设为主要平台。

数据源：`modrinth`、`curseforge`、`github`、`mcdr`（`hangar` 和 `spiget` 已定义但尚未接入解析器）。

### 状态文件

意图和配置存放在 `lucy.yaml`。解析后的精确结果（版本、哈希、安装路径、来源）存放在 `lucy-lock.yaml`。

### 运行时拓扑

Lucy 为你的服务器构建一张运行时图。图中的每个节点（Fabric、Forge、Paper、MCDR、Geyser、Velocity）都有角色、能力（`fabric_mods`、`bukkit_plugins`、`mcdr_plugins`）和风险等级。边描述节点之间的关系：谁适配谁、谁桥接谁、谁代理谁。这张图是 `lucy status`、init 探测和兼容性解析的基础。

> [!NOTE]
> Logo 和美西螈像素艺术版权归 Mojang AB 所有，原创替代品正在制作中。
