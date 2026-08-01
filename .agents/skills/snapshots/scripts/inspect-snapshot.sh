#!/usr/bin/env bash
# Copyright 2026 NVIDIA CORPORATION
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

usage() {
  cat <<'USAGE'
Inspect a KAI Scheduler snapshot archive.

Usage:
  inspect-snapshot.sh SNAPSHOT

The snapshot archive must contain snapshot.json.
USAGE
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 0
fi

snapshot_file="${1:-}"
if [[ -z "$snapshot_file" ]]; then
  usage >&2
  exit 2
fi

if [[ ! -f "$snapshot_file" ]]; then
  echo "Snapshot not found: $snapshot_file" >&2
  exit 1
fi

if ! command -v unzip >/dev/null 2>&1; then
  echo "unzip is required to inspect snapshot archives" >&2
  exit 1
fi

if ! unzip -l "$snapshot_file" | awk '{print $4}' | grep -qx 'snapshot.json'; then
  echo "snapshot.json not found in archive: $snapshot_file" >&2
  unzip -l "$snapshot_file" >&2
  exit 1
fi

echo "Archive contains snapshot.json"
echo
unzip -l "$snapshot_file"

if command -v jq >/dev/null 2>&1; then
  echo
  echo "Top-level keys:"
  unzip -p "$snapshot_file" snapshot.json | jq 'keys'

  echo
  echo "Object counts:"
  unzip -p "$snapshot_file" snapshot.json | jq -r '
    .rawObjects as $raw |
    if $raw == null then
      "rawObjects: null"
    else
      $raw | to_entries[] | select(.value | type == "array") | "\(.key)\t\(.value | length)"
    end
  '
else
  echo
  echo "jq not found; skipped JSON summary"
fi
