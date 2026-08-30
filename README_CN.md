<div align="center">
  <img src="images/banner.png" alt="lucy banner" width="80%" />

#### [English](README.md) | 中文

### Lucy

Minecraft 服务器包管理器。

[![CI](https://github.com/mclucy/lucy/actions/workflows/ci.yml/badge.svg)](https://github.com/mclucy/lucy/actions/workflows/ci.yml) [![Coverage](https://github.com/mclucy/lucy/wiki/badge/coverage.svg)](https://raw.githack.com/wiki/mclucy/lucy/dev/coverage.html) [![License](https://img.shields.io/github/license/mclucy/lucy)](LICENSE) [![Ask DeepWiki](https://deepwiki.com/badge.svg)](https://deepwiki.com/mclucy/lucy)
</div>

> [!WARNING]
> DeepWiki 的内容可能过时。

> [!IMPORTANT]
> 本项目正在开发中，功能尚不完整。API 和行为可能发生变化。如需参与贡献或关注进展，请联系 <4rcadiaaa@gmail.com> 或加入 [QQ 群](https://qm.qq.com/q/Sf65NVYaAi)。

## 概述

以类似 `npm` 的体验管理任何生态的服务器组件。

- Fabric
- Forge
- Neoforge
- Spigot/Paper Plugins
- MCDReforged

## 安装

> [!WARNING]
> 不建议在生产环境中使用 beta 之前的版本。

使用 Go 安装：

```bash
go install github.com/mclucy/lucy@latest
```

使用 Homebrew 安装：

```bash
brew install --HEAD mclucy/tap/lucy
```

## 快速开始

```bash
mkdir my-server && cd my-server
lucy init                         # 在当前目录初始化 Lucy
lucy add fabric/fabric-api@stable # 添加一个模组；依赖会自动解析
lucy status                       # 显示 Lucy 检测到的信息
lucy install                      # 从 lock 文件安装包
```

## 命令

### 管理包

#### `lucy init`

在当前目录创建清单文件和 lock 文件。已有的服务器会被保留。

```bash
lucy init
```

| 标志 | 说明 |
| ---------------- | -------------------------------------- |
| `-y`, `--yes` | 跳过提示并接受默认值 |
| `--game-version` | 非交互式初始化时使用的游戏版本（默认：`1.21`） |

#### `lucy add`

向清单中添加一个包。

```bash
lucy add fabric-api
lucy add fabric/lithium@stable
lucy add folia
lucy add mcdr/example-plugin@beta
```

| 标志 | 说明 |
| ----------------- | -------------------------------- |
| `-f`, `--force` | 跳过版本、依赖和平台警告 |
| `--with-optional` | 包含上游的可选依赖 |
| `--no-optional` | 跳过可选依赖（默认） |

#### `lucy remove`

从清单中移除一个包，并从 lock 文件中清理不再使用的间接依赖。

```bash
lucy remove fabric/lithium
```

#### `lucy install`

从 lock 文件安装包。lock 文件是最新时，Lucy 使用其中的确切数据；lock 文件过期时，Lucy 回退到清单重新解析。

```bash
lucy install
```

### 检查工作区

#### `lucy status`

显示 Lucy 在当前目录检测到的信息：游戏版本、服务端核心、平台、运行时活动和已安装的包。

```bash
lucy status
lucy status --json --long
```

#### `lucy search`

在各个数据源中搜索。

```bash
lucy search carpet
lucy search carpet --source modrinth --index downloads --platform fabric
```

| 标志 | 说明 |
| ---------------- | ----------------------------------------------- |
| `-i`, `--index` | 按 `relevance`、`downloads` 或 `newest` 排序 |
| `-c`, `--client` | 包含纯客户端模组 |
| `-s`, `--source` | 限定为 `modrinth`、`curseforge`、`hangar`、`spiget` 或 `mcdr` |
| `--platform` | 按 `fabric`、`forge`、`neoforge`、`bukkit` 过滤 |
| `-l`, `--long` | 显示完整输出 |
| `--json` | 输出原始 JSON |

#### `lucy info`

显示一个包的元数据、简介、作者和版本历史。

```bash
lucy info fabric/fabric-api@stable --long
```

| 标志 | 说明 |
| -------------- | -------- |
| `-l`, `--long` | 完整输出 |

#### `lucy tree`

显示依赖树。

```bash
lucy tree --live --depth 2
```

| 标志 | 说明 |
| --------- | -------------------------------- |
| `--live` | 探测正在运行的服务器而非 lock 文件 |
| `--depth` | 限制深度（0 = 不限制） |
| `--json` | 输出原始 JSON |

#### `lucy leaves`

列出没有其他包依赖的包。可以用这个命令找出可以安全移除的包。

```bash
lucy leaves --live
```

| 标志 | 说明 |
| -------- | ---------------------------------- |
| `--live` | 探测正在运行的服务器而非 lock 文件 |
| `--json` | 输出原始 JSON |

### 缓存

#### `lucy cache`

管理本地下载缓存。

```bash
lucy cache ls              # 列出缓存的下载
lucy cache clear           # 清空所有缓存的下载
lucy cache slugs ls        # 列出 slug 到包 ID 的映射
lucy cache slugs clear     # 清空 slug 映射
```

| 子命令 | 标志 |
| ------------- | -------- |
| `ls`, `list` | `--json` |
| `clear`, `rm` | |
| `slugs ls` | `--json` |
| `slugs clear` | |

### 故障排查

#### `lucy bisect`

对已安装的模组进行二分查找，定位导致故障的模组。

```bash
lucy bisect start          # 开始一次二分查找会话
lucy bisect good           # 将当前中点标记为正常（故障模组在右半部分）
lucy bisect bad            # 将当前中点标记为故障（故障模组在左半部分）
lucy bisect status         # 显示当前的二分查找会话
lucy bisect reset          # 中止会话并重新启用模组
```

### 计划中的命令

以下命令已注册但尚未实现。

| 命令 | 计划功能 |
| --------- | ---------------------- |
| `doctor` | 诊断服务器环境风险 |
| `export` | 导出配置或生成客户端 |
| `upgrade` | 升级已安装的包 |

### 全局标志

| 标志 | 说明 |
| --------------- | ----------------------- |
| `--debug` | 显示调试日志 |
| `--log-file` | 输出日志文件路径 |
| `--print-logs` | 在控制台输出日志 |
| `--no-style` | 禁用彩色输出 |
| `--json-compact` | 输出不带缩进的 JSON |

> [!NOTE]
> 美西螈贴图的版权归 Mojang AB 所有。原创替代图正在制作中。
