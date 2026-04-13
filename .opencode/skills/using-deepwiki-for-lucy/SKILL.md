---
name: using-deepwiki-for-lucy
description: Use this prior to any reading of the actual code to get a fast understanding of the architecture and relationships in the codebase.
---

# Using DeepWiki for Lucy

## Overview

Use DeepWiki to get architecture fast, then switch to targeted questions once you know which subsystem you are in. For this repo, DeepWiki is best for structure and code relationships; pair it with local docs when you need roadmap or product-direction nuance.

**Important**: This repo is `mclucy/lucy`, do NOT read the wrong wiki.

## When to Use

- You need a fast map of the `mclucy/lucy` repository
- You are tracing a command into its implementation
- You need to understand a subsystem before editing Go code
- You want grounded answers about how package identity, probing, routing, or installation work

Do not use this as the only source for product direction or unfinished work. Supplement with `README.md`, `docs/lucy-strategy.md`, and `documents/TODO.md`.

## Workflow

1. Start with `deepwiki_read_wiki_structure("mclucy/lucy")` to see the map.
2. Read the relevant wiki page with `deepwiki_read_wiki_contents("mclucy/lucy")` for architecture and terminology.
3. Ask a targeted question with `deepwiki_ask_question(...)` once you know the subsystem.

## Lucy Wiki Entry Points

| Need | Start Here |
|---|---|
| Command behavior | `1.2 CLI Command Reference` |
| Package IDs and versions | `2.1 Package Identity and Syntax`, `2.2 Version Resolution and Dependency Engine` |
| Server detection and compatibility | `2.3 Server Topology and Compatibility`, `3 Server Environment Probing` |
| Remote sources | `5 Upstream Provider Layer` |
| Installation flow | `4 Installation System` |
| Cache and downloads | `6 Cache and Download Infrastructure` |
| Output rendering | `7 Terminal UI (TUI) System` |
| Build and tests | `9 Testing and Development Infrastructure` |

## Question Patterns

- “Which wiki section explains how `lucy add` resolves compatible versions?”
- “How does the probe system build and evaluate runtime topology?”
- “Which providers are used for Modrinth, CurseForge, MCDR, and GitHub?”
- “Where does cache lookup happen before downloading artifacts?”

## Common Mistakes

- Reading the full wiki before choosing a subsystem
- Using `ask_question` before learning the section names
- Treating DeepWiki as roadmap truth when this repo is still incomplete
- Skipping local docs that explain strategy or unfinished gaps
