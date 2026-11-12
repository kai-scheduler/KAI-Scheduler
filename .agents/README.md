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

`.claude/skills` is a checked-in symlink to `.agents/skills/`, so Claude Code discovers these skills automatically.

### Other Harnesses

Other agent harnesses should treat `.agents/skills/` as the source of truth and either scan it directly or mirror the needed skills into their own configured skill directory.

## Current Skills

- [`snapshots`](skills/snapshots/SKILL.md): capture KAI Scheduler snapshots, inspect archives, replay them with `snapshot-tool`, and compare behavior across refs.
- [`kai-pending`](skills/kai-pending/SKILL.md): diagnose why a pod or PodGroup is stuck Pending
