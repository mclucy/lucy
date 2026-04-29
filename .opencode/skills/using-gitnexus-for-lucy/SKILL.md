---
name: using-gitnexus-for-lucy
description: Use when working in lucy and needing current code intelligence, execution flows, dependency impact, refactoring safety, GitNexus MCP tools, or the local .gitnexus index.
---

# Using GitNexus for Lucy

## Overview

Use GitNexus when you need current, local knowledge-graph answers about `mclucy/lucy`: execution flows, callers/callees, dependency impact, refactoring blast radius, and changed-code risk. For this repo, GitNexus complements DeepWiki: DeepWiki gives fast architecture orientation; GitNexus answers graph-backed questions against the local `.gitnexus` index.

**Important**: This repo is `mclucy/lucy`, with the local index under `.gitnexus/`. Check freshness before relying on graph results for recent work.

## When to Use

- You need to trace how a command, installer path, provider, cache, probe, or TUI flow actually executes
- You need impact analysis before editing shared Go code, structs, methods, package boundaries, or installer behavior
- You need callers, callees, process participation, communities, or Cypher-backed graph queries
- You need to review uncommitted changes with `detect_changes` before finishing work
- You are doing refactoring, renaming, extracting, splitting, or moving code and need safety analysis

Do not use GitNexus as the only source when the index is stale. Use local code and docs for exact latest edits, and use DeepWiki for high-level map-first orientation.

## Local Lucy Facts

| Item | Value |
|---|---|
| GitHub repo | `mclucy/lucy` |
| Local index | `.gitnexus/` |
| Freshness file | `.gitnexus/meta.json` |
| Current indexed remote | `https://github.com/mclucy/lucy` |
| Useful companion skill | `using-deepwiki-for-lucy` |
| Tool reference skill | `gitnexus-guide` |

If `.gitnexus/meta.json` shows an old commit or recent commits are missing, use the GitNexus CLI workflow before trusting results.

## Workflow

1. Use `gitnexus_list_repos` when repo identity is uncertain or more than one repo may be indexed.
2. Check repo context/freshness through GitNexus resources or `.gitnexus/meta.json` before relying on the graph.
3. Match the task to the specific GitNexus workflow:
   - Explore architecture or execution flow: use `gitnexus-exploring`, then `gitnexus_query` and `gitnexus_context`.
   - Debug a bug or failure path: use `gitnexus-debugging`, then trace callers/callees and relevant processes.
   - Analyze safety before edits: use `gitnexus-impact-analysis`, then `gitnexus_impact`.
   - Refactor or rename: use `gitnexus-refactoring`, then preview with `gitnexus_rename` or impact first.
   - Re-index, status, clean, or generated wiki: use `gitnexus-cli`.
4. Prefer graph tools for relationships. Do not replace `impact`, `context`, or process queries with plain text search when the question is about dependencies.
5. After making code changes, use `gitnexus_detect_changes(scope="all")` to map changed symbols to affected processes when the change touches shared flows.

## Quick Reference

| Need | Start With | Then Use |
|---|---|---|
| “How does `lucy add` flow?” | `gitnexus_query` | `gitnexus_context` on key symbols |
| “What calls this function/type?” | `gitnexus_context` | `gitnexus_impact(direction="upstream")` if editing |
| “What will break if I change installer logic?” | `gitnexus_impact` | inspect depth 1 direct callers first |
| “What do my current changes affect?” | `gitnexus_detect_changes(scope="all")` | inspect affected processes |
| “Can I rename/extract/move this safely?” | `gitnexus-impact-analysis` skill | `gitnexus_rename(dry_run=true)` where appropriate |
| “I need raw graph structure.” | `gitnexus-guide` | `gitnexus_cypher` after checking schema |

## Lucy Question Patterns

- “Trace the execution flow for the `add` command from CLI entry to installation.”
- “Show the callers and downstream dependencies of the installer function I plan to modify.”
- “What processes involve server probing, topology detection, and compatibility checks?”
- “What depends on provider resolution for Modrinth, CurseForge, MCDR, or GitHub?”
- “Analyze the blast radius of changing cache lookup or artifact download behavior.”
- “Map my uncommitted changes to affected processes before I finalize.”

## Common Mistakes

| Mistake | Fix |
|---|---|
| Starting with grep for relationship questions | Use `query`, `context`, or `impact`; grep is only a supplement |
| Skipping index freshness | Check `.gitnexus/meta.json` or repo context first |
| Treating DeepWiki and GitNexus as interchangeable | DeepWiki for architecture overview; GitNexus for local graph analysis |
| Using `query` when asking “what breaks?” | Use `impact` for blast radius |
| Ignoring direct callers | Review depth 1 impact results before indirect effects |
| Running raw Cypher without schema | Read the schema/resource or `gitnexus-guide` first |

## Baseline Failure Counters

Without this skill, agents tend to fall back to manual file search, miss transitive dependencies, skip process tracing, and forget to check whether the local graph is stale. For lucy work, correct that by starting from GitNexus freshness, choosing the graph tool that matches the question, and only then reading exact code locations.
