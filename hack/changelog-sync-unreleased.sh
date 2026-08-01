#!/usr/bin/env bash
# Copyright 2026 NVIDIA CORPORATION
# SPDX-License-Identifier: Apache-2.0

# Bridge tool for the fragment-migration PR only: while this branch is open, main still
# collects entries in CHANGELOG.md's `## [Unreleased]` block the old way. This mirrors any
# such entry that isn't yet represented by a fragment into `.changes/unreleased/`, so the two
# don't drift before merge. Idempotent — re-run it whenever main advances; already-present
# bullets are skipped. Delete this script once the PR merges (main has no Unreleased block after).
#
# ponytail: dedup is exact-bullet-text match. If an entry is REWORDED on main it looks new and
#           gets added as a duplicate — reconcile those by hand (rare).
#
# Usage: hack/changelog-sync-unreleased.sh [git-ref]   # ref defaults to origin/main
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"
REF="${1:-origin/main}"
CHANGIE="${CHANGIE:-bin/changie}"
[ -x "$CHANGIE" ] || { echo "changie not found at '$CHANGIE' — run: make changie" >&2; exit 1; }

git fetch --quiet origin "${REF#origin/}" 2>/dev/null || true

# Bullets already rendered by the current fragments.
"$CHANGIE" batch v0.0.0-sync --dry-run 2>/dev/null | grep '^- ' | sort -u > /tmp/cl-have.txt || true

# Ref's Unreleased bullets, each tagged with its section kind, in order.
git show "$REF:CHANGELOG.md" \
  | awk '/^## \[/{n++} n==1; n==2{exit}' \
  | awk '
      /^### Added/   { kind="Added";   next }
      /^### Changed/ { kind="Changed"; next }
      /^### Fixed/   { kind="Fixed";   next }
      /^- /          { print kind "\t" $0 }
    ' > /tmp/cl-ref.tsv

# Next fragment number = highest existing NNNN prefix + 1.
n=$(ls .changes/unreleased/ 2>/dev/null | grep -oE '^[0-9]{4}' | sort -n | tail -1 || echo 0)
n=$((10#${n:-0}))

added=0
while IFS=$'\t' read -r kind bullet; do
  grep -Fxq -e "$bullet" /tmp/cl-have.txt && continue
  n=$((n+1)); num=$(printf '%04d' "$n")
  klow=$(printf '%s' "$kind" | tr '[:upper:]' '[:lower:]')
  printf 'kind: %s\nbody: |-\n  %s\n' "$kind" "${bullet#- }" > ".changes/unreleased/${num}-${klow}.yaml"
  echo "added .changes/unreleased/${num}-${klow}.yaml  [$kind]"
  added=$((added+1))
done < /tmp/cl-ref.tsv

echo "changelog-sync: $added new fragment(s) from $REF ($(ls .changes/unreleased/*.yaml | wc -l | tr -d ' ') total)."
