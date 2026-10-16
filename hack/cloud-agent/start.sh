#!/usr/bin/env bash
set -euo pipefail

if docker info >/dev/null 2>&1; then
  exit 0
fi

if ! command -v dockerd >/dev/null; then
  echo "dockerd is not installed" >&2
  exit 1
fi

sudo dockerd >/tmp/dockerd-start.log 2>&1 &
for _ in $(seq 1 60); do
  if docker info >/dev/null 2>&1; then
    exit 0
  fi
  sleep 1
done

echo "Docker daemon did not become ready" >&2
tail -50 /tmp/dockerd-start.log >&2 || true
exit 1
