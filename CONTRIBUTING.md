[中文](CONTRIBUTING.zh.md) | English

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
- `task test` runs the default Go test suite, including the hermetic CAS, HTTP cache, file transaction, and CLI file tracking guard scenarios. They use temporary directories, local HTTP servers, and self-executed helper processes; no external server downloads are required.

See [The taskfile](Taskfile.yml) for all scripts.

CI includes testing and building. However, testing locally helps us merging your contribution faster.

## AI policy

We do not reject AI-written code. However, you have the duty to:

- Disclose the model and agent client used.
- Disclose how did you use agents (generate code, understand the codebase, etc.).
- If possible, share your prompts.

You must not:

- Dispatch highly automated agents without clear human intent (e.g., "scan the codebase and fix all bugs you found").
- Generate PR/Issue messages completely with AI. You must be able to understand and elaborate your design/fix.
- Generate long pieces of documetations with AI.
- Use agents to communicate with others on behalf of you.

### Skills

There is a [`skills-lock.json`](skills-lock.json) in the repo. It is recommended to install them for agentic development:

```bash
npx -y skills experimental_install
```
