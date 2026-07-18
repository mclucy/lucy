# Contributing to lucy

## Development environment

### Toolchain

1. Install the latest `go` on your system. See <https://go.dev/dl>.
2. `go-task`. See <https://taskfile.dev/docs/installation> or install natively with Go: `go install github.com/go-task/task/v3/cmd/task@latest`.
3. We use `gofumpt` for code formatting. Install by `go install mvdan.cc/gofumpt@latest`, and run `gofumpt -w .` to format the codebase. Always run this before making commits.

### Building and testing

- `task build` builds a dev artifact under `dist/`.
- `task build:watch` updates the build when codebase changes.
- `task run -- [args]` runs the codebase without building.
- `task test` runs all test in the codebase.

See [The Taskfile](Taskfile.yml) for all scripts.

CI includes testing and building. However, testing locally helps us merging your contribution faster.

## AI policy

We do not reject AI-written code. However, you have the duty to:

- Disclose the model and agent client used
- Dislcose how did you use agents (generate code, understand, etc.)
- If possible, share your prompts
- Setup your agent according to the [following section](#agents-setup)

You must not:

- Dispatch highly automated agents without clear human intent (e.g., "scan the codebase and fix all bugs you found")
- Generate PR/Issue messages completely with AI. You must be able to elaborate your design/fix.

## Agents setup

The recommended agent for this project is [OpenCode](https://github.com/anomalyco/opencode) and [oh-my-pi](https://github.com/can1357/oh-my-pi). These 2 agents are able to read the repo's [agent config file](opencode.json). You may use other agents, but you will have to replicate the repo's setups manually.

### MCPs

The config file sets up the following MCPs:

- `codegraph`: A code indexing tool to more accurately and efficiently present relevant source code to the agent. You might have to run `npx -y @colbymchenry/codegraph init` before use.
- `github-lucy`: Requires GitHub PAT, for internal maintainers only.
- `goland`: If you have GoLand on and turned on its MCP server, the agent can access GoLand's powerful editing feautres through this MCP.
- `godoc`: Allows agents to lookup documentations of go symbols.
- `gopls`: Go language server.

### Skills

There is a [`skills-lock.json`](skills-lock.json) in the repo. It is recommended to install them for agentic development:

```bash
npx -y skills experimental_install
```
