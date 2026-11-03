#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/../.."

mkdir -p "${HOME}/.cache/go-build-docker-gocache" "${HOME}/.cache/go-build-docker-gopath"
mkdir -p bin coverage

go mod download

if ! command -v helm >/dev/null 2>&1; then
  HELM_VERSION=3.17.2
  curl -fsSL "https://get.helm.sh/helm-v${HELM_VERSION}-linux-amd64.tar.gz" | tar -xz -C /tmp
  sudo mv /tmp/linux-amd64/helm /usr/local/bin/helm
  rm -rf /tmp/linux-amd64
fi

GOBIN="$(pwd)/bin" go install sigs.k8s.io/controller-runtime/tools/setup-envtest@release-0.23

if docker info >/dev/null 2>&1; then
  DOCKER="docker"
elif sg docker -c 'docker info >/dev/null 2>&1'; then
  DOCKER="sg docker -c"
else
  DOCKER=""
fi

run_docker() {
  if [[ -z "${DOCKER}" ]]; then
    return 1
  fi
  if [[ "${DOCKER}" == "docker" ]]; then
    docker "$@"
  else
    sg docker -c "docker $(printf '%q ' "$@")"
  fi
}

if [[ -n "${DOCKER}" ]]; then
  if ! run_docker image inspect golang:1.26.8-bookworm >/dev/null 2>&1; then
    run_docker pull mirror.gcr.io/library/golang:1.26.8-bookworm
    run_docker tag mirror.gcr.io/library/golang:1.26.8-bookworm golang:1.26.8-bookworm
  fi
  prefetch_image() {
    local name="$1"
    local mirror="mirror.gcr.io/${name}"
    if run_docker image inspect "${name}" >/dev/null 2>&1; then
      return 0
    fi
    if run_docker pull "${mirror}"; then
      run_docker tag "${mirror}" "${name}"
      return 0
    fi
    run_docker pull "${name}"
  }
  prefetch_image "golangci/golangci-lint:v2.11.3"
  prefetch_image "helmunittest/helm-unittest:3.17.2-0.8.1"
  if [[ "${DOCKER}" == "docker" ]]; then
    make builder
  else
    sg docker -c 'make builder'
  fi
  rm -f deployments/kai-scheduler/Chart.lock
  helm dependency build ./deployments/kai-scheduler
fi
