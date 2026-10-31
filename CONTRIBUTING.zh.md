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
- `task test` 运行默认的 Go 测试套件，其中包括使用临时目录、本地 HTTP 服务与自执行辅助进程的 CAS、HTTP 缓存、文件事务和 CLI 文件追踪守卫场景，无需从外部下载服务器文件。

查看 [Taskfile](Taskfile.yml) 了解所有脚本。

CI 包含测试与构建。但本地测试能帮助我们更快合并你的贡献。

## AI 政策

我们不排斥 AI 生成的代码。但你有责任：

- 披露使用的模型和智能体客户端。
- 披露你如何使用智能体（生成代码、理解代码库等）。
- 如有可能，分享你的提示词。

你不可以：

- 在没有明确人类意图的情况下派遣高度自动化的智能体（例如“扫描代码库并修复你找到的所有 bug”）。
- 完全用 AI 生成 PR/Issue 内容。你必须能够理解并阐述你的设计/修复。
- 用 AI 生成长篇文档。
- 用智能体代替你与他人沟通。

### Skills

仓库中有一个 [`skills-lock.json`](skills-lock.json)。推荐安装以支持智能体开发：

```bash
npx -y skills experimental_install
```
