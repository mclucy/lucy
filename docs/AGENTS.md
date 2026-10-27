# Documentation project instructions

## About this project

- The documentation site is built with [Mintlify](https://mintlify.com).
- The GitHub wiki mirrors this directory. `tools/genwiki` renders the pages and CI publishes them; never edit wiki pages in the browser.
- Pages are MDX files with YAML frontmatter.
- Navigation and site configuration live in `docs.json`.
- Preview with `npm i -g mint` and `mint dev` from this directory.
- Command examples and flag tables must match `go run . <command> --help` from the repository root.

## Human-facing style

- Follow the human-facing documentation rules in the repository root `AGENTS.md`.
- Use active voice, second person, concise sentences, and sentence-case headings.
- Use code formatting for file names, commands, paths, flags, schema fields, and package references.
- Prefer complete, runnable examples over fragments. Verify examples and flag descriptions against the current CLI.

## Content boundaries

- Document only behavior that exists in the current code. Verify commands against `go run . <command> --help`; do not restore planned or removed capabilities.
- Do not claim GitHub package search, SHA-256 local artifact lookup, `.zip` datapack support, or that `compile` produces an executable or plugin. `compile` builds a mod JAR.
- `install` syncs the server with the manifest and lock. It removes only files Lucy installed; it is not an append-only installer.
- Follow the agent-facing documentation rules in the repository root `AGENTS.md`. Document Lucy's architecture in `agents/*.mdx`; do not duplicate user tutorials or add agent pages to site navigation.
