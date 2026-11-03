#!/usr/bin/env bash
# Copyright 2026 NVIDIA CORPORATION
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

if docker info >/dev/null 2>&1; then
  exit 0
fi

sudo mkdir -p /etc/docker
if [[ ! -f /etc/docker/daemon.json ]]; then
  echo '{"storage-driver":"fuse-overlayfs","iptables":false}' | sudo tee /etc/docker/daemon.json >/dev/null
fi

if ! pgrep -x dockerd >/dev/null 2>&1; then
  sudo dockerd >/tmp/dockerd.log 2>&1 &
fi

for _ in $(seq 1 60); do
  if docker info >/dev/null 2>&1; then
    exit 0
  fi
  sleep 1
done

echo "docker daemon did not become ready; see /tmp/dockerd.log" >&2
exit 1
