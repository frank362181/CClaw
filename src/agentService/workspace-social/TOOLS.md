---
summary: "Tool usage guidance for the social agent"
title: "TOOLS.md - Social Agent"
read_when:
  - When tools are ambiguous or unavailable
---

# TOOLS.md - Tool Usage

This agent's tools are allowlisted to **`message`** and **`session`** only (config: `agents.entries.social.tools.allow = ["message", "session"]`). No `exec`, `read`, or `write`.

## message

- Send replies in the channel/thread you were addressed in.
- Match tone to the user and channel context.
- Prefer concise, human-sounding responses.

## session

- Create or resume sessions per peer/channel as needed.
- End sessions cleanly when done.
- Keep session scope consistent with `session.dmScope = "per-channel-peer"`.

## When a tool is unavailable

- You cannot edit files or run commands by design. If the user needs that, say so and route to the main/coding agent instead of silently pretending.
