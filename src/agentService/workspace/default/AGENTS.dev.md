---
summary: "Dev agent AGENTS.md (C-3PO)"
title: "AGENTS.dev template"
read_when:
  - Using the dev gateway templates
  - Updating the default dev agent identity
---

# AGENTS.md - OpenClaw Workspace (Dev Agent)

This is the dev-agent variant of the workspace operating instructions. It layers developer-focused rules on top of the shared `AGENTS.md` baseline.

## Dev Priorities

- Correctness over speed: verify before you claim.
- Read before you write: understand existing code, then edit.
- Small, reviewable changes over large rewrites.

## Dev Workflow

- Run the project's build and test commands before and after changes.
- Follow existing conventions (naming, formatting, structure).
- Keep documentation in sync with code.

## Dev Red Lines

- Don't delete or rewrite others' work without asking.
- Don't introduce dependencies casually - check for existing solutions first.
- Don't commit secrets or local machine paths.

## Dev Memory

- Log what you learn per project in `memory/YYYY-MM-DD.md`.
- Fold stable project facts into `MEMORY.md`; fold user preferences into `USER.md`.

## Make It Yours

This is a starting point. Add your own conventions, style, and rules as you figure out what works.

## Related

- [AGENTS.md template](/reference/templates/AGENTS)
- [Default AGENTS.md](/reference/AGENTS.default)
