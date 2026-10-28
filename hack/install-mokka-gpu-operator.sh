#!/usr/bin/env bash
# Copyright 2026 NVIDIA CORPORATION
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

REPO_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
: "${CLUSTER_NAME:?Set CLUSTER_NAME to the disposable kind cluster}"
: "${MOKKA_GPU_COUNT:=8}"
: "${GPU_OPERATOR_VERSION:=v26.7.2}"
# PR 1109's tested GPU Operator support plus the memory-limit fix from PR 1130.
# Keep this commit pinned until the integration is available in an upstream release.
: "${MOKKA_REPOSITORY:=https://github.com/JRosenboimNVIDIA/mokka.git}"
: "${MOKKA_REF:=fe859af25b05f1ea4e4e91e56c89491b8e6dc7b4}"
: "${MOKKA_IMAGE_REPOSITORY:=mokka-e2e}"
: "${MOKKA_IMAGE_TAG:=${MOKKA_REF:0:12}}"

SOURCE_DIR=$(mktemp -d)
trap 'rm -rf "$SOURCE_DIR"' EXIT
git -C "$SOURCE_DIR" init --quiet
git -C "$SOURCE_DIR" fetch --depth=1 "$MOKKA_REPOSITORY" "$MOKKA_REF"
git -C "$SOURCE_DIR" checkout --quiet --detach FETCH_HEAD

docker build --file "$SOURCE_DIR/deployments/nvml-mock/Dockerfile" \
  --build-arg GOLANG_VERSION=1.27.3 \
  --tag "$MOKKA_IMAGE_REPOSITORY:$MOKKA_IMAGE_TAG" "$SOURCE_DIR"
kind load docker-image --name "$CLUSTER_NAME" "$MOKKA_IMAGE_REPOSITORY:$MOKKA_IMAGE_TAG"

helm upgrade --install nvml-mock "$SOURCE_DIR/deployments/nvml-mock/helm/nvml-mock" \
  --namespace mokka --create-namespace \
  --values "$REPO_ROOT/hack/e2e-mokka-values.yaml" \
  --set "image.repository=$MOKKA_IMAGE_REPOSITORY" \
  --set "image.tag=$MOKKA_IMAGE_TAG" --set "gpu.count=$MOKKA_GPU_COUNT" \
  --wait --timeout 10m

helm repo add nvidia https://helm.ngc.nvidia.com/nvidia --force-update
helm repo update nvidia
helm upgrade --install gpu-operator nvidia/gpu-operator \
  --version "$GPU_OPERATOR_VERSION" --namespace gpu-operator --create-namespace \
  --values "$REPO_ROOT/hack/e2e-gpu-operator-values.yaml" --wait --timeout 15m

kubectl wait --for=jsonpath='{.status.state}'=ready clusterpolicy/cluster-policy --timeout=15m
kubectl wait \
  --for="jsonpath={.status.allocatable.nvidia\\.com/gpu}=$MOKKA_GPU_COUNT" \
  nodes --selector=mokka.nvidia.com/e2e-gpu=true --timeout=5m
