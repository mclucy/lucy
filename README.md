# Lucy

<div align="center">

<!-- ![lucy](https://socialify.git.ci/litetech-dev/lucy/image?description=1&font=Jost&forks=1&issues=1&language=1&name=1&owner=1&pattern=Brick%20Wall&pulls=1&stargazers=1&theme=Auto) -->

![banner](https://raw.githubusercontent.com/minecraft-lucy/lucy/main/images/banner.png)

[English](./README.md) | [中文](./README_CN.md)

</div>

---

> 🚧 \
> This project is currently INCOMPLETE and under active development. Features and functionalities are subject to change. \
> The project is large and we really need assistance! \
> If you're interested in contributing or want to stay updated, please contact <4rcadia.0@gmail.com>, or join the [QQ groupchat](https://qm.qq.com/q/Sf65NVYaAi). A Discord server will be up soon!

## 🚀 Overview

`lucy` is a powerful, unified command-line tool to simplify the management of Minecraft server-side content. Whether you're installing plugins, mods, managing dependencies, or coordinating complex modpacks, `lucy` provides an intuitive command-line interface to handle all your package management needs.

We want to fully mimic the experience of other package managers your might be familiar with, such as `apt`, `brew`, or `npm`. If you've used any of these tools, you'll feel right at home with `lucy`'s command syntax and workflow. We want to bring the reliability, ease of use, and convenience of modern package management to Minecraft server administration, allowing both newcomers and experienced admins to manage their server content with confidence and efficiency.

## ⭐ Functionalities

- **Dependency Management** - Automatically resolve dependencies, handle conflicts, and manage upgrades seamlessly.
- **Multi-Source Integration** - Access packages from various sources.
- **Non-Intrusive Design** - Runs independently from your server, ensuring zero interference with server runtime.
- **Modern CLI** - User-friendly command-line interface with clear commands and options.
- **Scripting & Automation** - Easily integrate `lucy` into scripts and automation workflows for continuous deployment.

## 🚀 Quick Start

### Installation

We know you server owners might be using some niche Linux distros, so we will be available via as many package managers as possible when we release the first beta.

### Basic Commands

> 🚧 \
> All examples are subject to change as we are still in development.

```bash
# Initialize Lucy in your server directory
lucy init

# Search for packages across all sources
lucy search <keyword>

# Get detailed information about a specific package
lucy info <package-id>

# Add a package to your server
lucy add <package-id>

# Check server status and list installed packages
lucy status
```

### Real-World Examples

#### Example 1: Set Up Fabric Server

```bash
# Search for Fabric
lucy search fabric

# Get details about Fabric
lucy info fabric/fabric

# Install Fabric
lucy add fabric/fabric@latest
```

#### Example 2: Install Mods with Dependencies

```bash
# Search for Create mod
lucy search create

# Install Create (dependencies auto-resolved)
lucy add create

# Lucy automatically handles Fabric API and other dependencies
```

## 📖 Syntax & Concepts

### Platform

A platform is a program that modifies (e.g., JVM injection) the Minecraft vanilla game in a way further specified by a third-party file passed into it.

From a logical perspective, platforms are common and heterogeneous dependencies for a large group of packages.

According to the given definition, the scope would cover NeoForge, Fabric, Iris Mod, etc.

### Project

A project is a piece of software that relies on one or more platforms.

A project usually reflects as a GitHub Repository, a Modrinth Homepage, etc.

This is defined to differ between a program and a specific version of it.

*Package Name* is a synonym (if not fully interchangeable) with Project.

### Package

A compiled, ready-to-use instance of a project with a specific platform and version. These are the only directly manageable entities in `lucy` and what you actually install.

**Examples**: `fabric/fabric-api@1.2.3`, `neoforge/create@0.5.1`

### Package Identifier

Packages are identified using the format: `platform/project@version`

```
fabric/fabric-api@1.2.3
   ↑        ↑        ↑
platform   name   version
```

- Both `platform` and `version` can be omitted and inferred from context when possible
- Examples: `fabric-api@latest`, `neoforge/create`

## 🛠️ Use Cases

### Server Administrators

Centralized package management across multiple servers and platforms. Manage dependencies, track versions, and automate updates with a single tool.

### Modpack Developers

Organize, version, and distribute your modpacks efficiently. Maintain dependency trees and ensure compatibility across versions.

### Hosting Services

Automate deployment and updates across multiple server instances. Integrate with CI/CD pipelines for streamlined server provisioning.

### Development Teams
Integrate package management into CI/CD pipelines and automation workflows. Manage server configurations as code.

## ⚖️ License

This project is licensed under the Apache 2.0 License.

Logo and other images featuring the axolotl pixel art are the copyright of Mojang AB.
