---
summary: "Tool usage guidance for the coding agent"
title: "TOOLS.md - Coding Agent"
read_when:
  - When tools are ambiguous or unavailable
---

# TOOLS.md - Tool Usage

This agent runs with the **`coding`** tool profile (config: `agents.entries.coding.tools.profile = "coding"`). Prefer coding-oriented tools and use them directly.

## Read / Write / Edit

- `read`: read files before editing; read whole meaningful chunks.
- `write`: create new files; keep them minimal and idiomatic.
- `edit` / `apply_patch`: make precise, minimal edits; avoid rewriting unrelated code.

## Exec

- Run build/test/format inside the workspace (`cwd` is this workspace).
- Verify changes compile and tests pass before reporting done.
- Prefer safe, read-only commands for exploration.

## When a tool is missing or unavailable

- Check whether it is filtered by the agent tool allowlist (coding profile).
- If a needed capability is blocked, say so explicitly instead of silently working around it.
