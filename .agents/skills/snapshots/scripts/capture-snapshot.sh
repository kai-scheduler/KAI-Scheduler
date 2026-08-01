#!/usr/bin/env bash
# Copyright 2026 NVIDIA CORPORATION
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

namespace="kai-scheduler"
deployment="kai-scheduler-default"
pod=""
local_port="8081"
remote_port="8081"
output=""
context=""
kubectl_bin="kubectl"
curl_bin="curl"

usage() {
  cat <<'USAGE'
Capture a KAI Scheduler snapshot through the snapshot plugin endpoint.

Usage:
  capture-snapshot.sh [options]

Options:
  --output PATH          Output snapshot archive path. Defaults to snapshot-<timestamp>.gzip.
  --namespace NAME       Kubernetes namespace. Default: kai-scheduler.
  --deployment NAME      Scheduler deployment name. Default: kai-scheduler-default.
  --pod NAME             Scheduler pod name. Overrides --deployment.
  --local-port PORT      Local port for kubectl port-forward. Default: 8081.
  --remote-port PORT     Remote scheduler HTTP port. Default: 8081.
  --context NAME         Kubernetes context.
  --kubectl PATH         kubectl binary. Default: kubectl.
  --curl PATH            curl binary. Default: curl.
  -h, --help             Show this help.
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --output)
      output="$2"
      shift 2
      ;;
    --namespace)
      namespace="$2"
      shift 2
      ;;
    --deployment)
      deployment="$2"
      shift 2
      ;;
    --pod)
      pod="$2"
      shift 2
      ;;
    --local-port)
      local_port="$2"
      shift 2
      ;;
    --remote-port)
      remote_port="$2"
      shift 2
      ;;
    --context)
      context="$2"
      shift 2
      ;;
    --kubectl)
      kubectl_bin="$2"
      shift 2
      ;;
    --curl)
      curl_bin="$2"
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

if [[ -z "$output" ]]; then
  output="snapshot-$(date -u +%Y%m%dT%H%M%SZ).gzip"
fi

command -v "$kubectl_bin" >/dev/null 2>&1 || {
  echo "kubectl binary not found: $kubectl_bin" >&2
  exit 1
}
command -v "$curl_bin" >/dev/null 2>&1 || {
  echo "curl binary not found: $curl_bin" >&2
  exit 1
}

mkdir -p "$(dirname "$output")"

kubectl_args=()
if [[ -n "$context" ]]; then
  kubectl_args+=(--context "$context")
fi
kubectl_args+=(-n "$namespace")

target="deployment/$deployment"
if [[ -n "$pod" ]]; then
  target="pod/$pod"
fi

tmpdir="$(mktemp -d)"
port_forward_log="$tmpdir/port-forward.log"
port_forward_pid=""

cleanup() {
  if [[ -n "$port_forward_pid" ]] && kill -0 "$port_forward_pid" >/dev/null 2>&1; then
    kill "$port_forward_pid" >/dev/null 2>&1 || true
    # Kill the port-forward if wait does not return after SIGTERM.
    (
      sleep 2
      kill -0 "$port_forward_pid" >/dev/null 2>&1 && kill -9 "$port_forward_pid" >/dev/null 2>&1 || true
    ) &
    wait_timeout_pid="$!"

    wait "$port_forward_pid" >/dev/null 2>&1 || true
    kill "$wait_timeout_pid" >/dev/null 2>&1 || true
    wait "$wait_timeout_pid" >/dev/null 2>&1 || true
  fi
  rm -rf "$tmpdir"
}
trap cleanup EXIT

"$kubectl_bin" "${kubectl_args[@]}" port-forward "$target" "${local_port}:${remote_port}" >"$port_forward_log" 2>&1 &
port_forward_pid="$!"

ready="false"
for _ in {1..40}; do
  if ! kill -0 "$port_forward_pid" >/dev/null 2>&1; then
    echo "kubectl port-forward exited before it became ready:" >&2
    cat "$port_forward_log" >&2
    exit 1
  fi
  if "$curl_bin" -fsS "http://127.0.0.1:${local_port}/get-snapshot" -o "$output"; then
    ready="true"
    break
  fi
  sleep 0.25
done

if [[ "$ready" != "true" ]]; then
  echo "Timed out waiting for snapshot endpoint. Port-forward log:" >&2
  cat "$port_forward_log" >&2
  exit 1
fi

echo "Snapshot written to $output"
