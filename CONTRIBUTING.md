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

### Static assets

The `assets` project publishes the installers, the manifest schema, and the release
manifest at `lucy.lc`. Workers Builds deploys it from `main` whenever `assets/` changes,
using `./node_modules/.bin/cf deploy`. The `manifest-schema` workflow regenerates
`assets/public/manifest.schema.json` and commits it, which triggers that deployment.

The release workflow mirrors every published archive to the `lucy-releases` R2 bucket
under `v<version>/`, and `/latest.json` resolves the release to install from that
bucket, preferring stable releases over prereleases. The mirror copies the assets
attached to the GitHub release, so a tag whose mirror step failed can be retried by
running the `Release` workflow manually with that tag. Two repository settings drive
this: `CLOUDFLARE_ACCOUNT_ID`, and a `CLOUDFLARE_API_TOKEN` secret with Workers R2
Storage write access. Worker deployments do not use that secret, because Workers
Builds deploys from `main` on its own credentials.

To deploy the worker locally, run `npm ci` and `npx cf deploy` in `assets`.

## AI policy

We do not reject AI-written code. However, you have the duty to:

- Disclose the model and agent client used.
- Disclose how did you use agents (generate code, understand the codebase, etc.).
- If possible, share your prompts.
- Set up your agent according to the [following section](#agents-setup).

You must not:

- Dispatch highly automated agents without clear human intent (e.g., "scan the codebase and fix all bugs you found").
- Generate PR/Issue messages completely with AI. You must be able to understand and elaborate your design/fix.
- Generate long pieces of documetations with AI.
- Use agents to communicate with others on behalf of you.

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
