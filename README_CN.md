<div align="center">
  <img src="images/banner.png" alt="lucy banner" width="80%" />

#### [English](README.md) | 中文

### Lucy

<h3>
  <sup>Minecraft 服务器包管理器</sup>
</h3>

[![CI](https://github.com/mclucy/lucy/actions/workflows/ci.yml/badge.svg)](https://github.com/mclucy/lucy/actions/workflows/ci.yml) [![Coverage](https://github.com/mclucy/lucy/wiki/badge/coverage.svg)](https://raw.githack.com/wiki/mclucy/lucy/dev/coverage.html) [![Go Report Card](https://goreportcard.com/badge/github.com/mclucy/lucy)](https://goreportcard.com/report/github.com/mclucy/lucy) [![License](https://img.shields.io/github/license/mclucy/lucy)](LICENSE) [![Ask DeepWiki](https://deepwiki.com/badge.svg)](https://deepwiki.com/mclucy/lucy)
</div>

> [!WARNING]
> DeepWiki 内容目前可能过时

> [!IMPORTANT]
> 项目仍在开发，功能未定型，随时可能改。想参与或跟进度：联系 <4rcadia.0@gmail.com>，或进 [QQ 群](https://qm.qq.com/q/Sf65NVYaAi)。 \
> ⭐️ 觉得有用的话，给仓库点个 Star

## 简介

把服务器目录变成稳定、可复现的工作区。一条命令管模组和插件，批量部署会省事很多。

```bash
cd your-server
lucy init                  # 在本目录启用 Lucy
lucy add fabric            # 安装 Fabric
lucy add lithium@latest    # 安装模组
```

## 快速开始

> [!WARNING]
> 不建议在正式环境使用 beta 之前的版本。

```bash
go install github.com/mclucy/lucy@latest   # 原生安装
brew install --HEAD mclucy/tap/lucy        # Homebrew
```

## 命令

### `lucy init`

创建 manifest 和 lock 文件。已有服务器内容会保留。

```bash
lucy init
```

| 参数             | 说明                              |
| ---------------- | --------------------------------- |
| `-y`, `--yes`    | 跳过交互，用默认值                |
| `--game-version` | 非交互时的游戏版本（默认 `1.21`） |

### `lucy add`

往服务器里加任意包。

```bash
lucy add fabric-api
lucy add fabric/lithium@latest
lucy add folia
```

| 参数              | 说明                         |
| ----------------- | ---------------------------- |
| `-f`, `--force`   | 跳过版本、依赖、平台相关警告 |
| `--with-optional` | 一并装上上游可选依赖         |
| `--no-optional`   | 不装可选依赖（默认）         |

### `lucy remove`

从 manifest 删掉包，并在 lock 里剪掉不再需要的传递依赖。

```bash
lucy remove fabric/lithium
```

### `lucy install`

把 lock 应用到受管运行时。lock 仍有效就用精确数据；过期则按 manifest 意图重新解析。

```bash
lucy install
```

### `lucy search`

跨数据源搜索，可过滤、排序。

```bash
lucy search fabric/carpet
lucy search modrinth:carpet --index downloads --platform fabric
```

| 参数             | 说明                                              |
| ---------------- | ------------------------------------------------- |
| `-i`, `--index`  | 排序：`relevance`、`downloads`、`newest`          |
| `-c`, `--client` | 包含仅客户端模组                                  |
| `--platform`     | 平台过滤：`fabric`、`forge`、`neoforge`、`bukkit` |
| `-l`, `--long`   | 完整输出          |
| `--json`         | 原始 JSON         |

### `lucy status`

显示当前目录下 Lucy 探测到的内容：游戏版本、服务端核心、平台、拓扑、运行时活动、风险信号、已安装包。

```bash
lucy status
lucy status --json --long
```

### `lucy topology`

把探测到的服务端运行时拓扑画成 ASCII 图。

```bash
lucy topology
lucy topology --long
lucy topology --json
```

| 参数           | 说明                           |
| -------------- | ------------------------------ |
| `-l`, `--long` | 节点内显示角色、能力、风险等级 |

### `lucy info`

查包的元数据、描述、作者和版本历史。

```bash
lucy info fabric/fabric-api@latest --long
```

| 参数           | 说明     |
| -------------- | -------- |
| `-l`, `--long` | 完整输出 |

### `lucy tree`

显示依赖树。

```bash
lucy tree --live --depth 2
```

| 参数      | 说明                        |
| --------- | --------------------------- |
| `--live`  | 探测运行中服务器，不用 lock |
| `--depth` | 限制深度（0 表示不限制）    |
| `--json`  | 原始 JSON                   |

### `lucy leaves`

列出没有依赖者的包，用来判断哪些能安全删。

```bash
lucy leaves --live
```

| 参数     | 说明                        |
| -------- | --------------------------- |
| `--live` | 探测运行中服务器，不用 lock |
| `--json` | 原始 JSON                   |

### `lucy cache`

```bash
lucy cache ls              # 列出缓存的下载
lucy cache clear           # 清空下载缓存
lucy cache slugs ls        # 列出 slug → 包 ID 映射
lucy cache slugs clear     # 清空 slug 映射
```

| 子命令        | 参数     |
| ------------- | -------- |
| `ls`、`list`  | `--json` |
| `clear`、`rm` |          |
| `slugs ls`    | `--json` |
| `slugs clear` |          |

### `lucy bisect`

```bash
lucy bisect start          # 开始二分排查会话
lucy bisect good           # 当前中点正常（问题在右半段）
lucy bisect bad            # 当前中点异常（问题在左半段）
lucy bisect status         # 查看当前会话
lucy bisect reset          # 中止会话并重新启用模组
```

### 占位命令

已注册，尚未实现：

| 命令      | 计划用途           |
| --------- | ------------------ |
| `doctor`  | 诊断服务器环境风险 |
| `export`  | 导出配置或生成客户端 |
| `upgrade` | 升级已安装的包     |

### 全局参数

| 参数           | 说明             |
| -------------- | ---------------- |
| `--debug`      | 输出调试日志     |
| `--log-file`   | 打印日志文件路径 |
| `--print-logs` | 日志打到控制台   |
| `--no-style`   | 关闭彩色输出     |
