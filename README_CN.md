<div align="center">
  <img src="images/banner.png" alt="lucy banner" width="80%" />

#### [English](README.md) | 中文

### Lucy

<h3>
  <sup>现代的 Minecraft 服务器包管理器</sup>
</h3>

  [![CI](https://github.com/mclucy/lucy/actions/workflows/ci.yml/badge.svg)](https://github.com/mclucy/lucy/actions/workflows/ci.yml) [![Coverage](https://github.com/mclucy/lucy/wiki/dev/coverage.svg)](https://raw.githack.com/wiki/mclucy/lucy/dev/coverage.html) [![Go Report Card](https://goreportcard.com/badge/github.com/mclucy/lucy)](https://goreportcard.com/report/github.com/mclucy/lucy) [![License](https://img.shields.io/github/license/mclucy/lucy)](LICENSE) [![Ask DeepWiki](https://deepwiki.com/badge.svg)](https://deepwiki.com/mclucy/lucy)
</div>

> [!WARNING]
> DeepWiki 内容目前可能过时

> [!IMPORTANT]
> 项目还在开发，功能未定型，随时可能改。想参与或跟进度：联系 <4rcadia.0@gmail.com>，或进 [QQ 群](https://qm.qq.com/q/Sf65NVYaAi)。 \
> ⭐️ 觉得有用的话，给仓库点个 Star

## 简介

在 manifest 里写清楚要的模组、插件和服务端核心就行。Lucy 会解析出具体版本和依赖，写 lock，并把受管范围同步到磁盘。指向已有目录会从现状接着管；空目录从零开始也可以。

```bash
cd your-server
lucy init                         # 在本目录启用 Lucy
lucy add fabric/lithium@latest    # 解析具体版本与依赖
lucy install                      # 按 lock 同步受管范围
```

- manifest 里声明包，Lucy 解析版本和校验和；`lucy install` 负责下载和落盘。
- `lucy init` 会先摸清运行时、平台和已装内容，再问你哪些要纳入管理，其余不动。
- Lucy 会画一张运行时图（Fabric、Forge、MCDR、Paper、Velocity 等），标角色、能力（`fabric_mods`、`bukkit_plugins`、`mcdr_plugins`）和风险。`lucy status`、init 探测和兼容性解析都靠这张图。

## 快速开始

> [!WARNING]
> 首个 beta 之前别当生产环境用，除非你就是要测或写代码。数据丢了自负。

```bash
go install github.com/mclucy/lucy@latest
```

```bash
mkdir my-server && cd my-server
lucy init                         # 接管这个目录
lucy add fabric/fabric-api@latest # 加模组，依赖会自动解析
lucy status                       # 看探测结果
lucy install                      # 按 lock 同步受管包
```

## 命令

### `lucy init`

探测目录、发现服务器环境、创建状态文件。

```bash
lucy init
lucy init --yes --game-version 1.21.4
lucy init --conflict abort
```

在项目根目录生成 `lucy.yaml` 和 `lucy-lock.yaml`。

| 参数               | 说明                                     |
| ------------------ | ---------------------------------------- |
| `-y`, `--yes`      | 跳过交互，用默认值                       |
| `--game-version`   | 非交互时的游戏版本（默认 `1.21`）        |
| `-c`, `--conflict` | `preserve`（默认）、`abort`、`overwrite` |

### `lucy add`

把模组、插件或服务端核心写进 manifest；Lucy 解析具体版本并重写 lock。

```bash
lucy add fabric-api
lucy add fabric/lithium@latest
lucy add mcdr/example-plugin@compatible
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

把 lock 应用到受管运行时。lock 仍有效就用里面的精确数据；过期则回退到 manifest 意图再解析。

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
| `-l`, `--long`   | 完整输出                                               |
| `--json`         | 原始 JSON                                              |

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

| 参数           | 说明                                           |
| -------------- | ---------------------------------------------- |
| `-l`, `--long` | 节点内显示角色、能力、风险等级                 |
| `--json`       | 输出原始拓扑数据及生成的 Mermaid 源码          |
| `--no-style`   | 用普通 ASCII，不用制表符框线字符               |

### `lucy info`

查包的元数据、描述、作者和版本历史。

```bash
lucy info fabric/fabric-api@latest --long
```

| 参数           | 说明      |
| -------------- | --------- |
| `-l`, `--long` | 完整输出  |
| `--json`       | 原始 JSON |

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

## 概念

### 包标识符

```text
[platform/]name[@version]
```

必填只有名称。不写 platform 时由环境推断；不写 version 时默认 `@compatible`（按当前服务器尽力匹配最新可用）。

```text
fabric/fabric-api@1.2.3
   ↑       ↑        ↑
  平台     名称     版本
```

`@latest` 表示最新可用；`@compatible` 是默认，按探测到的环境做尽力匹配。

manifest 里可写的主平台：`none`、`fabric`、`forge`、`neoforge`、`mcdr`

类型系统还识别 `bukkit`、`sponge`、`velocity`、`bungeecord`，用于拓扑探测，但暂时不能设成主平台。

数据源：`modrinth`、`curseforge`、`github`、`mcdr`（`hangar`、`spiget` 已定义，解析器里还没接上）。

### 状态文件

意图和配置在 `lucy.yaml`；解析结果（版本、哈希、安装路径、来源）在 `lucy-lock.yaml`。

### 运行时拓扑

Lucy 为服务器建一张运行时图。每个节点（Fabric、Forge、Paper、MCDR、Geyser、Velocity 等）带角色、能力集合和风险等级；边表示关系，例如谁适配谁、谁桥接谁。`lucy status`、init 探测和兼容性解析都依赖这张图。

> [!NOTE]
> Logo 与美西螈像素艺术版权归 Mojang AB；原创替代素材制作中。
