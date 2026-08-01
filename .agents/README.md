# Agent Skills

This repository keeps repo-local agent skills under `.agents/skills/`.

## Consuming Repo Skills

`.agents/skills/` is the canonical repo-local location for shared agent skills in this repository.

### Codex

Codex can consume skills from `.agents/skills/` when working inside this repository.

Example prompts:

```text
Use the snapshots skill to capture a snapshot from the current cluster.
Take a scheduler snapshot and compare it on v0.13.0 and v0.14.0.
```

### Claude Code

Claude Code can use the same repo-owned skill content, but may require exposing the skill through Claude's configured skill directory if the current setup does not scan `.agents/skills/` directly.

Example:

```bash
ln -s /path/to/KAI-Scheduler/.agents/skills/snapshots ~/.claude/skills/snapshots
```

### Other Harnesses

Other agent harnesses should treat `.agents/skills/` as the source of truth and either scan it directly or mirror the needed skills into their own configured skill directory.

## Current Skills

- [`snapshots`](skills/snapshots/SKILL.md): capture KAI Scheduler snapshots, inspect archives, replay them with `snapshot-tool`, and compare behavior across refs.
