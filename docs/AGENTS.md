# Documentation project instructions

## About this project

- The documentation site is built with [Mintlify](https://mintlify.com).
- Pages are MDX files with YAML frontmatter.
- Navigation and site configuration live in `docs.json`.
- Preview with `npm i -g mint` and `mint dev` from this directory.
- Command examples and flag tables must match `go run . <command> --help` from the repository root.

## Style preferences

- Use active voice and second person ("you").
- Keep sentences concise: one idea per sentence.
- Use sentence case for headings.
- Bold UI elements: click **Settings**.
- Use code formatting for file names, commands, paths, flags, schema fields, and package references.
- Prefer complete, runnable examples over fragments.

## Content boundaries

- Document only behavior that exists in the current code. Verify commands against `go run . <command> --help`; do not restore planned or removed capabilities.
- Do not claim GitHub package search, SHA-256 local artifact lookup, `.zip` datapack support, or that `compile` produces an executable or plugin. `compile` builds a mod JAR.
- `install` syncs the server with the manifest and lock. It removes only files Lucy installed; it is not an append-only installer.
- Keep architecture and contributor workflow in the repository root `AGENTS.md`. This file owns documentation style.
