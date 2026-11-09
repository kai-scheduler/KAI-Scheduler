#!/usr/bin/env bash
# Copyright 2026 NVIDIA CORPORATION
# SPDX-License-Identifier: Apache-2.0

# Report which sub-chart versions a build of the chart contains, as
# `<chart>: <version>` lines without a leading v.
#
# Usage:
#   hack/subchart-versions.sh locked           # versions in the chart's Chart.lock
#   hack/subchart-versions.sh resolve          # re-resolve the ranges in Chart.yaml, then as `locked`
#   hack/subchart-versions.sh previous <tag>   # versions bundled in an earlier release
#   hack/subchart-versions.sh changes <tag>    # resolve, then print the ranged sub-charts that differ from <tag>
#   hack/subchart-versions.sh ranged           # sub-charts Chart.yaml declares by a range rather than a version
#
# `locked` reads the lock `helm dependency build` leaves behind. The version
# inside a downloaded sub-chart still carries the v of its release tag, so it is
# not used. `previous` reads the subcharts.yaml a release publishes, and for a
# release that predates it, the sub-charts bundled in its published chart.
set -euo pipefail

KAI_REPO=kai-scheduler/KAI-Scheduler
CHART_OCI=oci://ghcr.io/kai-scheduler/kai-scheduler/kai-scheduler
CHART_DIR="${CHART_DIR:-deployments/kai-scheduler}"
ASSET=subcharts.yaml

usage() {
  echo "usage: $0 locked | resolve | previous <tag> | changes <tag> | ranged" >&2
  exit 1
}

locked() {
  local lock="$CHART_DIR/Chart.lock"
  [ -f "$lock" ] || {
    echo "No $lock; run helm dependency build first." >&2
    exit 1
  }
  awk '
    /^dependencies:/ {inside = 1; next}
    /^[a-z]/ {inside = 0}
    inside && /^- name:/ {name = $3}
    inside && /^  version:/ {
      version = $2
      gsub(/["'\'']/, "", version)
      sub(/^v/, "", version)
      print name ": " version
    }' "$lock" | sort
}

# `update`, not `build`: build would serve a lock left over from an earlier
# build instead of re-resolving, and report a version the release will not ship.
resolve() {
  helm dependency update --skip-refresh "$CHART_DIR" >/dev/null
  locked
}

# A release published before subcharts.yaml existed has no asset, but its chart
# carries the sub-charts it was built with.
chart_version() {
  awk '
    /^name:/ {name = $2}
    /^version:/ {version = $2}
    END {
      gsub(/["'\'']/, "", version)
      sub(/^v/, "", version)
      print name ": " version
    }'
}

bundled() (
  local tag="$1" tmp entry
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' EXIT
  helm pull "$CHART_OCI" --version "$tag" --destination "$tmp" >/dev/null
  tar -xzf "$tmp"/*.tgz -C "$tmp"
  for entry in "$tmp"/kai-scheduler/charts/*/; do
    chart_version <"${entry}Chart.yaml"
  done | sort
)

previous() {
  local tag="$1" assets
  assets="$(gh release view "$tag" --repo "$KAI_REPO" --json assets --jq '.assets[].name')"
  if grep -qx "$ASSET" <<<"$assets"; then
    gh release download "$tag" --repo "$KAI_REPO" --pattern "$ASSET" --output -
  else
    bundled "$tag"
  fi
}

ranged() {
  awk '
    /^dependencies:/ {inside = 1; next}
    /^[a-z]/ {inside = 0}
    inside && /^ *- name:/ {name = $3}
    inside && /^ *version:/ {
      version = $2
      gsub(/["'\'']/, "", version)
      if (version !~ /^v?[0-9]+\.[0-9]+\.[0-9]+([-+].*)?$/) print name
    }' "$CHART_DIR/Chart.yaml"
}

changes() {
  local tag="$1" before after name
  [ -n "$(ranged)" ] || return 0
  before="$(previous "$tag")"
  # Resolved as a statement rather than through $(resolve): bash before 4.4 does
  # not carry set -e into a command substitution, so a failed resolve there would
  # be read as "nothing changed" and silently drop the changelog entry.
  helm dependency update --skip-refresh "$CHART_DIR" >/dev/null
  after="$(locked)"
  for name in $(ranged); do
    if [ "$(grep "^$name: " <<<"$before" || true)" != "$(grep "^$name: " <<<"$after" || true)" ]; then
      grep "^$name: " <<<"$after"
    fi
  done
}

[ $# -ge 1 ] || usage
case "$1" in
  locked) locked ;;
  resolve) resolve ;;
  previous)
    [ $# -eq 2 ] || usage
    previous "$2"
    ;;
  changes)
    [ $# -eq 2 ] || usage
    changes "$2"
    ;;
  ranged) ranged ;;
  *) usage ;;
esac
