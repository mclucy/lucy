中文 | [English](CONTRIBUTING.md)

# 为 lucy 做贡献

## 开发环境

### 工具链

1. 在系统中安装最新版 `go`。参见 <https://go.dev/dl>。
2. 安装 `go-task`。参见 <https://taskfile.dev/docs/installation>，或通过 Go 原生安装：`go install github.com/go-task/task/v3/cmd/task@latest`。
3. 我们使用 `gofumpt` 进行代码格式化。通过 `go install mvdan.cc/gofumpt@latest` 安装，并运行 `gofumpt -w .` 格式化代码库。提交前请务必运行。

### 构建与测试

- `task build` 在 `dist/` 下构建开发产物。
- `task build:watch` 在代码库变更时自动重新构建。
- `task run -- [args]` 无需构建即可运行代码库。
- `task test` 运行代码库中的所有测试。

查看 [Taskfile](Taskfile.yml) 了解所有脚本。

CI 包含测试与构建。但本地测试能帮助我们更快合并你的贡献。

## AI 政策

我们不排斥 AI 生成的代码。但你有责任：

- 披露使用的模型和智能体客户端。
- 披露你如何使用智能体（生成代码、理解代码库等）。
- 如有可能，分享你的提示词。
- 按照[下文](#智能体配置)配置你的智能体。

你不可以：

- 在没有明确人类意图的情况下派遣高度自动化的智能体（例如“扫描代码库并修复你找到的所有 bug”）。
- 完全用 AI 生成 PR/Issue 内容。你必须能够理解并阐述你的设计/修复。
- 用 AI 生成长篇文档。
- 用智能体代替你与他人沟通。

## 智能体配置

本项目推荐的智能体是 [OpenCode](https://github.com/anomalyco/opencode) 和 [oh-my-pi](https://github.com/can1357/oh-my-pi)。这两个智能体都能读取本仓库的[智能体配置文件](opencode.json)。你也可以使用其他智能体，但需要手动复现本仓库的配置。

### MCPs

配置文件设置了以下 MCP：

- `codegraph`：代码索引工具，用于更准确、高效地向智能体展示相关源码。使用前可能需要运行 `npx -y @colbymchenry/codegraph init`。
- `github-lucy`：需要 GitHub PAT，仅内部维护者使用。
- `goland`：如果你已开启 GoLand 的 MCP 服务器，智能体可以通过该 MCP 使用 GoLand 强大的编辑功能。
- `godoc`：允许智能体查询 Go 符号的文档。
- `gopls`：Go 语言服务器。

### Skills

仓库中有一个 [`skills-lock.json`](skills-lock.json)。推荐安装以支持智能体开发：

```bash
npx -y skills experimental_install
```
