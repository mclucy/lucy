<div align="center">
  <img src="images/banner.png" alt="lucy banner" width="80%" />

# Lucy

**Minecraft 服务器的包管理器。**

[![CI](https://github.com/mclucy/lucy/actions/workflows/ci.yml/badge.svg)](https://github.com/mclucy/lucy/actions/workflows/ci.yml)
[![Coverage](https://github.com/mclucy/lucy/wiki/badge/coverage.svg)](https://raw.githack.com/wiki/mclucy/lucy/badge/coverage.html)
[![License](https://img.shields.io/github/license/mclucy/lucy)](LICENSE)

[文档](https://lucy.lc/docs) | [English](README.md) | [中文](README.zh.md)
</div>

> [!IMPORTANT]
> Lucy 正处于 pre-beta 活跃开发阶段。API 与模式可能在 `v0.1.0` 之前发生变化。欢迎加入 [QQ 群](https://qm.qq.com/q/Sf65NVYaAi) 或 [Discord 服务器](https://discord.gg/9zxNYvGx4Y) 讨论与获取帮助。

Lucy 为 Minecraft 服务器带来现代化的包管理体验。通过简单直观的声明式工作流定义和管理你的服务器。

我们（正在努力）支持多种生态及其组合：

- Fabric
- Quilt
- Forge
- NeoForge
- MCDReforged
- Bukkit/Spigot/Paper

Lucy 从多个来源获取元数据与文件：

- Modrinth
- CurseForge
- 在 GitHub 上托管你自己的源！

## 安装

macOS 与 Linux：

```sh
curl -fsSL https://lucy.lc/sh | sh
```

安装脚本会把 Lucy 放到 `~/.local/bin`；如有需要，请将该目录加入 `PATH`。使用脚本与压缩包安装的版本可通过 `lucy self-update` 原地更新；Homebrew、Go、Nix 与 Docker 安装的版本则通过各自的工具更新。

Windows（PowerShell）：

```powershell
irm https://lucy.lc/ps1 | iex
```

安装脚本会将 `%USERPROFILE%\.local\bin` 加入你的用户 PATH。请打开新的终端窗口后使用。

其他安装方式：

```bash
# Homebrew (macOS / Linux)
brew tap mclucy/tap
brew trust --tap mclucy/tap
brew install --cask lucy

# Go
go install github.com/mclucy/lucy@latest

# Nix（flake）
nix run github:mclucy/lucy

# Docker（仅 CLI）
docker pull ghcr.io/mclucy/lucy
```

也可在 [Releases](https://github.com/mclucy/lucy/releases) 下载预编译二进制文件。

该镜像只提供 CLI，用于处理挂载进来的服务器目录，不会启动服务器。详见 [容器](https://lucy.lc/docs/containers)。

在 NixOS 或 home-manager 中，把该 flake 作为输入引入，导入 `nixosModules.default` 或 `homeManagerModules.default`，并设置 `programs.lucy.enable = true;`。

## 快速上手

```bash
# 创建新服务器
lucy create

# 也可以不用 create，接管已有的服务器目录：
# lucy init

# 添加包并自动解析依赖
lucy add modrinth:fabric-api modrinth:lithium

# 查看实时服务器状态
lucy status

# 按 manifest 文件的定义同步服务器
lucy install
```

`lucy.yaml` 记录你的服务器配置，`lucy-lock.yaml` 记录满足该配置的精确制品。凭借 lock，lucy 可以重建任何服务器，无论其多么复杂。

## Agent 支持

我们认为 AI Agent 与人类用户同等重要。Agent 可以流畅、高效地使用 lucy。你甚至不需要配置 MCP 服务器（它们只会用工具定义占满你的上下文），也不需要 skills。安装 lucy，然后告诉你的 Agent 使用它。

试试这个提示词：

```text
Use `lucy` to host a Technical Minecraft server with Minecraft 26.2 for me.
```

## 命令

| 命令                   | 用途                                                                     |
| ---------------------- | ------------------------------------------------------------------------ |
| `lucy create`          | 在目录中创建新服务器并安装；`--no-install` 只写入 manifest 与 lock       |
| `lucy init`            | 从已有服务器目录生成 `lucy.yaml` 与 `lucy-lock.yaml`                     |
| `lucy add <pkg>...`    | 记录需求、解析依赖闭包、更新 lock 并同步                                 |
| `lucy remove <pkg>...` | 移除需求、重新解析，并清理不再需要的制品                                 |
| `lucy deinit`          | 从服务器目录移除 lucy 的 manifest、lock 与本地状态                       |
| `lucy install`         | 依据 lock 收敛服务器状态；lock 过期时重新解析，可通过 `--locked` 与 `--offline` 控制 |
| `lucy status`          | 显示实时服务器的身份、文件与包；`--from-lock` 渲染 lock                  |
| `lucy search <query>`  | 搜索 Modrinth、CurseForge、Hangar、Spiget 与 MCDReforged 目录            |

完整指南、配置与参数说明： **[lucy.lc/docs](https://lucy.lc/docs)**

---

> [!NOTE]
> Logo 与美西螈像素画的版权归 Mojang AB 所有。原创替代作品正在制作中。
