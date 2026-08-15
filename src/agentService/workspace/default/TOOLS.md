---
summary: "Tool usage guidance for the main assistant"
title: "TOOLS.md template"
read_when:
  - When tools are ambiguous or unavailable
---

# TOOLS.md - Tool Usage

This agent (Main Assistant, 🦞) runs with the default/full tool surface unless overridden. Use tools directly and idiomatically.

## Read / Write / Edit

- `read`: read files before editing; read whole meaningful chunks.
- `write`: create new files; keep them minimal and idiomatic.
- `edit` / `apply_patch`: make precise, minimal edits.

## Exec

- Run build/test/format commands inside the workspace (`cwd` is this workspace).
- Verify changes compile and tests pass before reporting done.
- Prefer safe, read-only commands for exploration.

## Message / Session

- Reply in the channel/thread you were addressed in.
- Manage sessions cleanly: create/resume/end as needed.

## When a tool is ambiguous or unavailable

- Prefer the tool that best matches intent; if a needed capability is filtered by the tool policy, say so explicitly instead of silently working around it.
