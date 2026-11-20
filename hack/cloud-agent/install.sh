#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT}"

ensure_docker() {
  if docker info >/dev/null 2>&1; then
    return 0
  fi
  if ! command -v dockerd >/dev/null; then
    echo "dockerd is not installed; install docker.io in the base image" >&2
    exit 1
  fi
  sudo dockerd >/tmp/dockerd-install.log 2>&1 &
  for _ in $(seq 1 60); do
    if docker info >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  echo "Docker daemon did not become ready" >&2
  tail -50 /tmp/dockerd-install.log >&2 || true
  exit 1
}

install_host_tooling() {
  if command -v apt-get >/dev/null; then
    sudo DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
      docker.io docker-buildx \
      g++-x86-64-linux-gnu g++-aarch64-linux-gnu \
      libc6-dev-amd64-cross libc6-dev-arm64-cross \
      >/dev/null 2>&1 || true
  fi
  if ! command -v golangci-lint >/dev/null; then
    curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh \
      | sudo sh -s -- -b /usr/local/bin v2.11.3
  fi
}

ensure_docker
install_host_tooling

if ! command -v helm >/dev/null; then
  mkdir -p "${ROOT}/bin"
  GOBIN="${ROOT}/bin" go install helm.sh/helm/v3/cmd/helm@v3.17.3
  sudo ln -sf "${ROOT}/bin/helm" /usr/local/bin/helm
fi

go mod download
make envtest

if ! docker image inspect "builder:1.26.8-bookworm" >/dev/null 2>&1; then
  if ! DOCKER_BUILDKIT=1 docker build -f hack/cloud-agent/kai-builder.Dockerfile -t builder:1.26.8-bookworm . \
    && ! make builder; then
    :
  elif ! docker image inspect "builder:1.26.8-bookworm" >/dev/null 2>&1; then
    echo "Warning: builder:1.26.8-bookworm is missing (Docker Hub layer CDN egress required for make build-go and make test)" >&2
  fi
fi

if ! make chart-deps; then
  echo "Warning: make chart-deps failed (ghcr.io egress required for Helm chart tests). Run 'make chart-deps' once egress is enabled." >&2
fi
