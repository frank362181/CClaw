---
summary: "Agent identity record"
title: "IDENTITY template"
read_when:
  - Bootstrapping a workspace manually
---

# IDENTITY.md - Who Am I?

The agent's name, vibe, and emoji. Created/updated during the bootstrap ritual.

## Name

- Display name: e.g., `Clawd`

## Vibe / Theme

- One-line theme: e.g., "a helpful sloth"

## Emoji

- Signature emoji: e.g., `🦞`

## Avatar

- Avatar file path (workspace-relative, http(s) URL, or data: URI), e.g., `avatars/clawd.png`

## Derived defaults

- `ackReaction` derives from `emoji`.
- `mentionPatterns` derives from `name` / `emoji`.

## Related

- [Agent workspace](/concepts/agent-workspace)
- [SOUL.md template](/reference/templates/SOUL)
