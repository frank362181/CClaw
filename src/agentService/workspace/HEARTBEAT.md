---
summary: "Retired heartbeat workspace file"
title: "HEARTBEAT template (retired)"
read_when:
  - Handling retired workspace files
---

# HEARTBEAT.md - Retired

> **Deprecated**: This workspace file is retired. Heartbeat context moved to the cron monitor scratch; do not rely on `HEARTBEAT.md`.

## What changed

- Heartbeat scheduling and monitor scratch are now owned by the cron/automation system, not a workspace file.
- Configure heartbeats via `agents.defaults.heartbeat` / `agents.entries.*.heartbeat` instead.

## Migration

- If this file still exists in an old workspace, it is a no-op. You can delete it.
- Move any persistent heartbeat instructions into `AGENTS.md` or a monitor scratch file.

## Related

- [Heartbeat](/gateway/heartbeat)
- [Automations vs heartbeat](/automation#automations-vs-heartbeat)
