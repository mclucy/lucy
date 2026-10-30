<div align="center">
  <img src="images/banner.png" alt="lucy banner" width="80%" />

# Lucy

**Minecraft 服务器的包管理器。**

[![CI](https://github.com/mclucy/lucy/actions/workflows/ci.yml/badge.svg)](https://github.com/mclucy/lucy/actions/workflows/ci.yml)
[![Coverage](https://github.com/mclucy/lucy/wiki/badge/coverage.svg)](https://raw.githack.com/wiki/mclucy/lucy/badge/coverage.html)
[![License](https://img.shields.io/github/license/mclucy/lucy)](LICENSE)

[文档](https://lucy.lc/docs) | [English](README.md) | [中文](README_CN.md)
</div>

> [!IMPORTANT]
> Lucy 处于 pre-beta 活跃开发阶段。1.0 之前，API 与模式可能发生变化。欢迎加入 [QQ 群](https://qm.qq.com/q/Sf65NVYaAi)
> 或提交 [Issue](https://github.com/mclucy/lucy/issues) 参与贡献。

Lucy 为 Minecraft 服务器带来现代化的包管理体验。通过简单直观的声明式工作流定义和管理你的服务器。

我们（正在努力）支持多种生态及其组合：

- Fabric
- Forge
- NeoForge
- MCDReforged
- Bukkit/Spigot/Paper

Lucy 从多个来源获取元数据与文件：

- Modrinth
- CurseForge
- 在 GitHub 上托管你自己的源！

## 安装

```bash
# Homebrew (macOS / Linux)
brew install --HEAD mclucy/tap/lucy

# Go
go install github.com/mclucy/lucy@latest
```

也可在 [Releases](https://github.com/mclucy/lucy/releases) 下载预编译二进制文件。

## 快速上手

```bash
# 在一个已有服务器中初始化 lucy.yaml
lucy init

# 添加包并自动解析依赖
lucy add modrinth:fabric-api modrinth:lithium

# 查看实时服务器状态
lucy status

# 将服务器同步到 lock 描述的状态
lucy install
```

`lucy.yaml` 记录你的服务器配置，`lucy-lock.yaml` 记录对配置的完整解析。仅凭 lock 即可重建任何复杂的服务器。

## Agent 支持

我们认为 AI Agents 是与人类同等重要的用户。Agents 可以高效的使用 lucy。你不需要配置任何 MCP（它们只会用大量的 tools 占满你的上下文），甚至不需要 skills。安装 lucy，然后告诉你的 agent 使用它。

试试这个提示词：

```text
Use `lucy` to host a Technical Minecraft server with Minecraft 26.2 for me.
```

## 命令

| 命令                   | 用途                                                          |
| ---------------------- | ------------------------------------------------------------- |
| `lucy init`            | 从已有服务器目录生成 `lucy.yaml`                              |
| `lucy add <pkg>...`    | 记录包、解析依赖、更新 lock 并同步                            |
| `lucy remove <pkg>...` | 删除包、重新解析，并清理不再需要的文件                        |
| `lucy deinit`          | 从服务器目录移除 lucy 的 manifest、lock 与本地状态            |
| `lucy install`         | 将服务器同步到 manifest 与 lock；lock 过期时重新解析          |
| `lucy status`          | 显示实时服务器与包；`--from-lock` 渲染 lock                   |
| `lucy search <query>`  | 搜索 Modrinth、CurseForge、Hangar、Spiget 和 MCDReforged 目录 |

完整指南、配置与参数说明： **[lucy.lc/docs](https://lucy.lc/docs)**

---

> [!NOTE]
> 美西螈贴图的版权归 Mojang AB 所有。原创替代图正在制作中。
