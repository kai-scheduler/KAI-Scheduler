#!/usr/bin/env bash
# Copyright 2026 NVIDIA CORPORATION
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

snapshot_file=""
verbosity="4"
tool=""
build="true"
arch="amd64"
cpuprofile=""
memprofile=""
ref=""
log_file=""
allow_dirty="false"

usage() {
  cat <<'USAGE'
Run a KAI Scheduler snapshot with snapshot-tool.

Usage:
  run-snapshot.sh --snapshot PATH [options]

Options:
  --snapshot PATH        Snapshot archive to replay. Required.
  --ref REF              Branch, tag, or commit to test.
  --verbosity LEVEL      snapshot-tool verbosity. Default: 4.
  --tool PATH            Existing snapshot-tool binary path.
  --arch ARCH            Built binary arch suffix. Default: amd64.
  --no-build             Do not run make build-go SERVICE_NAME=snapshot-tool.
  --log-file PATH        Write combined replay output to a log file.
  --allow-dirty          Permit switching refs with dirty tracked files.
  --cpuprofile PATH      Write CPU profile.
  --memprofile PATH      Write heap profile.
  -h, --help             Show this help.
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --snapshot)
      snapshot_file="$2"
      shift 2
      ;;
    --verbosity)
      verbosity="$2"
      shift 2
      ;;
    --ref)
      ref="$2"
      shift 2
      ;;
    --tool)
      tool="$2"
      shift 2
      ;;
    --arch)
      arch="$2"
      shift 2
      ;;
    --no-build)
      build="false"
      shift
      ;;
    --log-file)
      log_file="$2"
      shift 2
      ;;
    --allow-dirty)
      allow_dirty="true"
      shift
      ;;
    --cpuprofile)
      cpuprofile="$2"
      shift 2
      ;;
    --memprofile)
      memprofile="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [[ -z "$snapshot_file" ]]; then
  echo "--snapshot is required" >&2
  usage >&2
  exit 2
fi
if [[ ! -f "$snapshot_file" ]]; then
  echo "Snapshot not found: $snapshot_file" >&2
  exit 1
fi

repo_root="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
cd "$repo_root"
snapshot_abs="$(cd "$(dirname "$snapshot_file")" && pwd)/$(basename "$snapshot_file")"

restore_original_ref() {
  if [[ -n "${original_branch:-}" ]]; then
    git switch "$original_branch" >/dev/null
  elif [[ -n "${original_head:-}" ]]; then
    git switch --detach "$original_head" >/dev/null
  fi
}

if [[ -n "$ref" ]]; then
  if [[ "$allow_dirty" != "true" && -n "$(git status --porcelain --untracked-files=no)" ]]; then
    echo "Tracked files are dirty; refusing to switch refs. Commit, stash, or pass --allow-dirty." >&2
    git status --short --untracked-files=no >&2
    exit 1
  fi

  original_branch="$(git branch --show-current)"
  original_head="$(git rev-parse HEAD)"
  trap restore_original_ref EXIT
  git switch --detach "$ref" >/dev/null
fi

if [[ "$build" == "true" ]]; then
  make build-go SERVICE_NAME=snapshot-tool
fi

if [[ -z "$tool" ]]; then
  tool="$repo_root/bin/snapshot-tool-${arch}"
fi

if [[ ! -x "$tool" ]]; then
  echo "snapshot-tool is not executable: $tool" >&2
  echo "Build it first or pass --tool PATH." >&2
  exit 1
fi

args=(--filename "$snapshot_abs" --verbosity "$verbosity")
if [[ -n "$cpuprofile" ]]; then
  mkdir -p "$(dirname "$cpuprofile")"
  args+=(--cpuprofile "$cpuprofile")
fi
if [[ -n "$memprofile" ]]; then
  mkdir -p "$(dirname "$memprofile")"
  args+=(--memprofile "$memprofile")
fi

if [[ -n "$log_file" ]]; then
  mkdir -p "$(dirname "$log_file")"
  "$tool" "${args[@]}" 2>&1 | tee "$log_file"
else
  "$tool" "${args[@]}"
fi
