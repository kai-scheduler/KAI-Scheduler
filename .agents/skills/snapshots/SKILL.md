---
name: snapshots
description: Use when investigating KAI Scheduler behavior with captured cluster state, especially to replay scheduler decisions on specific refs or compare behavior across versions.
license: MIT
compatibility: Requires bash, git, kubectl for capture, curl for capture, make/docker or a prebuilt snapshot-tool for replay.
metadata:
  author: KAI Scheduler maintainers
  version: "1.0"
---

# Snapshots

Use this skill when investigating KAI Scheduler behavior with captured cluster state, especially for reproducing scheduling bugs, comparing behavior across KAI versions, or gathering evidence for issues like `kai-scheduler/KAI-Scheduler#1517`.

## Facts

- `docs/plugins/snapshot.md` is the source of truth for capture.
- The snapshot endpoint is `/get-snapshot` on plugin port `8081`, not the scheduler `--listen-address` port. In the observed clusters here, remote `8080` returned `404` while remote `8081` worked.
- Snapshot files are ZIP archives containing `snapshot.json`, even when named `.gzip`.
- `cmd/snapshot-tool/main.go` rebuilds fake clients from `snapshot.json` and replays the configured scheduler actions.
- Replay is a simulation of scheduler behavior, not a full cluster reproduction.

## Commands

Run scripts from the repository root:

```bash
.agents/skills/snapshots/scripts/capture-snapshot.sh --output snapshots/issue-123.gzip
.agents/skills/snapshots/scripts/inspect-snapshot.sh snapshots/issue-123.gzip
.agents/skills/snapshots/scripts/run-snapshot.sh --snapshot snapshots/issue-123.gzip --verbosity 8
.agents/skills/snapshots/scripts/run-snapshot.sh --ref v0.14.2 --snapshot snapshots/issue-123.gzip
.agents/skills/snapshots/scripts/compare-snapshot-refs.sh --snapshot snapshots/issue-123.gzip --refs main,v0.14.2
```

- `capture-snapshot.sh`: port-forward the scheduler and download `/get-snapshot`. Default target is `deployment/kai-scheduler-default` in namespace `kai-scheduler` on local/remote port `8081`. The script inherits `KUBECONFIG`, for example:

```bash
KUBECONFIG=$HOME/.kube/engine-scale-test \
  .agents/skills/snapshots/scripts/capture-snapshot.sh --output snapshots/example.gzip
```

- `inspect-snapshot.sh`: validate that the archive contains `snapshot.json` and print top-level structure. Run this before replaying user-provided artifacts.
- `run-snapshot.sh`: build `snapshot-tool` with `make build-go SERVICE_NAME=snapshot-tool` and replay on the current checkout, or use `--ref` to switch to one Git ref, replay, and restore the original branch or commit. For large snapshots, start with `--verbosity 2`. For reruns, prefer `--no-build --tool bin/snapshot-tool-amd64`. If a ref-based run is interrupted hard enough that the shell trap does not execute, the repo can stay detached; check with `git status --short --branch` and restore with `git switch <branch>`.
- `compare-snapshot-refs.sh`: run the same snapshot against several refs and save one log per ref plus `summary.tsv`.

## Workflow

1. Capture or receive the snapshot. Avoid committing snapshot artifacts unless the user explicitly asks.
2. Inspect the archive and confirm it contains `snapshot.json`.
3. If capture fails, verify the scheduler pod is running, verify the scheduler ConfigMap includes `- name: snapshot`, and verify the scheduler logs contain `Snapshot plugin registering get-snapshot`.
4. Replay on the reported KAI version first, aligned to the exact tag or commit.
5. Use `--verbosity 2` first. Compare timing from action timestamps inside the logs, not whole-command wall clock, because builds and verbosity can dominate.
6. Replay on candidate fixed or regressed refs only after the reported version is understood.
7. If a version appears stuck, stop waiting indefinitely and keep the partial log as evidence. In the runs here, `v0.14.0` completed `reclaim` materially faster than `v0.13.0`, while `v0.14.4` appeared to stall in `reclaim` past an interactive timeout.
8. Report refs, commands, log paths, action timings, errors, and whether the issue reproduced.
