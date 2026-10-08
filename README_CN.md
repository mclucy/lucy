<div align="center">
  <img src="images/banner.png" alt="lucy banner" width="80%" />

# Lucy

**Minecraft 服务器的包管理器。**

[![CI](https://github.com/mclucy/lucy/actions/workflows/ci.yml/badge.svg)](https://github.com/mclucy/lucy/actions/workflows/ci.yml)
[![Coverage](https://github.com/mclucy/lucy/wiki/badge/coverage.svg)](https://raw.githack.com/wiki/mclucy/lucy/dev/coverage.html)
[![License](https://img.shields.io/github/license/mclucy/lucy)](LICENSE)

[文档](https://lucy.lc/docs) | [English](README.md) | [中文](README_CN.md)
</div>

> [!IMPORTANT]
> Lucy 处于 pre-beta 活跃开发阶段。1.0 之前，API 与模式可能发生变化。欢迎加入 [QQ 群](https://qm.qq.com/q/Sf65NVYaAi) 或提交 [Issue](https://github.com/mclucy/lucy/issues) 参与贡献。

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

# 查看解析后的核心、文件与包
lucy status

# 按照 lock 描述安装文件
lucy install
```

`lucy.yaml` 记录你的意图，`lucy-lock.yaml` 记录满足该意图的确切构件（含哈希）。两者一并提交——仅凭 lock 即可重建服务器，`--locked` 会在两者不一致时直接失败。

## 命令

| 命令                  | 用途                                                                          |
| --------------------- | ----------------------------------------------------------------------------- |
| `lucy init`           | 从已有服务器目录生成 `lucy.yaml`                                              |
| `lucy add <pkg>`      | 记录需求、解析、更新 lock 并安装                                              |
| `lucy remove <pkg>`   | 删除需求并清理无引用构件                                                      |
| `lucy install`        | 依据 `lucy-lock.yaml` 同步文件；`--locked` 在漂移时报错，`--offline` 禁止下载 |
| `lucy status`         | 显示解析后的核心、Minecraft 版本与包                                          |
| `lucy tree`           | 检查依赖图谱（支持 `--live` 探测）                                            |
| `lucy leaves`         | 列出无依赖引用的安全移除候选包                                                |
| `lucy bisect`         | 二分排查已安装模组以定位崩溃原因                                              |
| `lucy search <query>` | 跨 Modrinth、CurseForge 和 GitHub 查询                                        |

完整指南、配置与参数说明：**[lucy.lc/docs](https://lucy.lc/docs)**

---

> [!NOTE]
> 美西螈贴图的版权归 Mojang AB 所有。原创替代图正在制作中。
