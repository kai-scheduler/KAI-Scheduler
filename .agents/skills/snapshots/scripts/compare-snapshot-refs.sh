#!/usr/bin/env bash
# Copyright 2026 NVIDIA CORPORATION
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

snapshot_file=""
refs_csv=""
verbosity="4"
output_dir=""
allow_dirty="false"
arch="amd64"

usage() {
  cat <<'USAGE'
Run the same KAI Scheduler snapshot against multiple git refs.

Usage:
  compare-snapshot-refs.sh --snapshot PATH --refs REF1,REF2[,REF3...] [options]

Options:
  --snapshot PATH        Snapshot archive to replay. Required.
  --refs LIST            Comma-separated refs to test. Required.
  --verbosity LEVEL      snapshot-tool verbosity. Default: 4.
  --output-dir PATH      Output directory. Defaults to snapshot-runs/<timestamp>.
  --arch ARCH            Built binary arch suffix. Default: amd64.
  --allow-dirty          Permit switching refs with dirty tracked files.
  -h, --help             Show this help.
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --snapshot)
      snapshot_file="$2"
      shift 2
      ;;
    --refs)
      refs_csv="$2"
      shift 2
      ;;
    --verbosity)
      verbosity="$2"
      shift 2
      ;;
    --output-dir)
      output_dir="$2"
      shift 2
      ;;
    --arch)
      arch="$2"
      shift 2
      ;;
    --allow-dirty)
      allow_dirty="true"
      shift
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

if [[ -z "$snapshot_file" || -z "$refs_csv" ]]; then
  echo "--snapshot and --refs are required" >&2
  usage >&2
  exit 2
fi

repo_root="$(git rev-parse --show-toplevel)"
cd "$repo_root"

snapshot_abs="$(cd "$(dirname "$snapshot_file")" && pwd)/$(basename "$snapshot_file")"
if [[ ! -f "$snapshot_abs" ]]; then
  echo "Snapshot not found: $snapshot_file" >&2
  exit 1
fi

if [[ -z "$output_dir" ]]; then
  output_dir="snapshot-runs/$(date -u +%Y%m%dT%H%M%SZ)"
fi
mkdir -p "$output_dir"

summary="$output_dir/summary.tsv"
printf 'ref\tstatus\tlog\n' >"$summary"

IFS=',' read -r -a refs <<<"$refs_csv"
for ref in "${refs[@]}"; do
  if [[ -z "$ref" ]]; then
    continue
  fi

  safe_ref="$(printf '%s' "$ref" | tr '/: ' '___')"
  log_file="$output_dir/${safe_ref}.log"
  status="pass"

  args=(--ref "$ref" --snapshot "$snapshot_abs" --verbosity "$verbosity" --arch "$arch" --log-file "$log_file")
  if [[ "$allow_dirty" == "true" ]]; then
    args+=(--allow-dirty)
  fi

  if ! .agents/skills/snapshots/scripts/run-snapshot.sh "${args[@]}"; then
    status="fail"
  fi

  printf '%s\t%s\t%s\n' "$ref" "$status" "$log_file" >>"$summary"
done

echo "Comparison summary written to $summary"
