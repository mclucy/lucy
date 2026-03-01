# Lucy

<div align="center">

<!-- ![lucy](https://socialify.git.ci/litetech-dev/lucy/image?description=1&font=Jost&forks=1&issues=1&language=1&name=1&owner=1&pattern=Brick%20Wall&pulls=1&stargazers=1&theme=Auto) -->

![banner](https://raw.githubusercontent.com/minecraft-lucy/lucy/main/images/banner.png)

[English](./README.md) | [中文](./README_CN.md)

</div>

---

> 🚧 \
> 本项目正在开发中且尚未完成。 \
> 我们需要您的帮助！ \
> 如果你有兴趣贡献代码或想了解最新进展，请联系 <4rcadia.0@gmail.com>，或者加 [QQ 群](https://qm.qq.com/q/Sf65NVYaAi)。

## 🚀 项目简介

`lucy` 是一个强大的、统一的命令行工具，旨在简化 Minecraft 服务器端的管理。包括安装插件、模组、管理依赖项，甚至是协调复杂的整合包。仅通过几行简单的命令解决各种复杂的服务器管理需求。

许多设计理念模仿了你可能熟悉的其他包管理器，比如 `apt`、`brew` 或 `npm`。如果你曾经使用过这些工具，那么`lucy`的学习成本对你几乎为0。我们希望通过借鉴现代包管理器的可靠性、易用性和便捷性，让新手和经验丰富的管理员都能以信心和效率管理他们的服务器内容。

## ⭐ 核心功能

- **依赖管理** - 自动解析依赖项，处理冲突，无缝管理升级。
- **来源集成** - 从各种来源下载。
- **非侵入式设计** - 独立于服务器运行，确保零干扰。
- **现代 CLI** - 用户友好的命令行界面，命令和选项清晰。
- **脚本和自动化** - 轻松将 `lucy` 集成到脚本和自动化工作流中进行持续部署。

## 🚀 快速开始

### 安装

当我们发布第一个beta时，我们将尝试支持尽可能多的安装方式。

### 基本命令

> 🚧 \
> 所有示例均可能会随着开发进展而更改。

```bash
# 在服务器目录初始化 Lucy
lucy init

# 搜索包
lucy search <keyword>

# 获取特定包的详细信息
lucy info <package-id>

# 安装包到你的服务器
lucy install <package-id>

# 检查服务器状态和已安装的包列表
lucy status <server-path>

# 更新配置和设置
lucy config
```

### 使用示例

#### 在服务器上安装 Fabric

```bash
# 获取 Fabric 的详细信息
lucy info fabric/fabric

# 安装 Fabric
lucy install fabric/fabric@latest
```

#### 安装带有依赖项的模组

```bash
# 搜索并查看机械动力
lucy search create
lucy info create

# 安装机械动力
lucy install create

# Lucy 会自动处理机械动力的依赖项
```

## 📖 语法和概念

### Platform（平台）

平台是一个通过向其传入第三方文件来修改（例如 JVM 注入）Minecraft 原版游戏的程序。

从逻辑角度来看，平台是大量包的通用且异构的依赖。

根据给定的定义，范围将包括 NeoForge、Fabric、Iris Mod 等。

### Project（项目）

项目是依赖于一个或多个平台的软件。你可以认为这是一个 GitHub 仓库或者 Modrinth 主页。这个概念用于区分程序和其特定版本。

*Package Name* 是 Project 的同义词（如果不是完全可互换的话）。

### Package（包）

包是项目的实例，具有特定的平台和版本。这些是 `lucy` 中唯一直接可管理和安装的实体。

**示例**：`fabric/fabric-api@1.2.3`、`neoforge/create@0.5.1`

### Package Identifier（包标识符）

包使用以下格式标识：`platform/project@version`

```
fabric/fabric-api@1.2.3
   ↑        ↑        ↑
平台      名称    版本
```

- 平台和版本都可以省略，在可能的情况下从上下文推断
- 示例：`fabric-api@latest`、`neoforge/create`

## 🛠️ 使用场景

### 服务器管理员

跨多个服务器和平台的集中包管理。管理依赖项、跟踪版本并通过单一工具自动化更新。

### 模组包开发者

高效组织、版本管理和分发你的模组包。维护依赖树并确保版本间的兼容性。

### 托管服务

跨多个服务器实例自动化部署和更新。集成到 CI/CD 流水线中以实现流畅的服务器配置。

### 开发团队

将包管理集成到 CI/CD 流水线和自动化工作流中。将服务器配置作为代码进行管理。

## ⚖️ 许可证

本项目以 Apache 2.0 许可证授权。

Logo和其他图片中出现的蝾螈形象的著作权完全属于Mojang AB。
