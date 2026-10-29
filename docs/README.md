# lucy documentation

This directory contains the Mintlify documentation site. Site configuration and navigation live in `docs.json`.

## Pages

- [Introduction](index.mdx)
- [Quickstart](quickstart.mdx)
- Commands
  - [Global flags](commands/global-flags.mdx)
  - [`lucy init`](commands/init.mdx)
  - [`lucy install`](commands/install.mdx)
  - [`lucy java`](commands/java.mdx)
  - [`lucy config`](commands/config.mdx)
  - [`lucy add`](commands/add.mdx)
  - [`lucy remove`](commands/remove.mdx)
  - [`lucy search`](commands/search.mdx)
  - [`lucy compile`](commands/compile.mdx)
  - [`lucy info`](commands/info.mdx)
  - [`lucy status`](commands/status.mdx)

Shared operational notes live in [shared/cipher-key-operations.md](shared/cipher-key-operations.md).

## Contributing

Read [AGENTS.md](AGENTS.md) for the project terminology, style rules, and documentation boundaries. Architecture, package layout, and test workflow are in the repository root [AGENTS.md](../AGENTS.md).

## Preview

```bash
npm i -g mint
mint dev
```

View the local site at `http://localhost:3000`.
