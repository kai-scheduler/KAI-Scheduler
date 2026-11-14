#!/usr/bin/env bash
# Copyright 2026 NVIDIA CORPORATION
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

REPO_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
: "${CLUSTER_NAME:?Set CLUSTER_NAME to the disposable kind cluster}"
: "${MOKKA_GPU_COUNT:=8}"
: "${FEATURE_CONFIG:=default}"
if [[ "$FEATURE_CONFIG" = numa-full && "$MOKKA_GPU_COUNT" != 8 ]]; then
  echo "The NUMA layout requires eight A100 GPUs (four per zone)." >&2
  exit 1
fi
: "${GPU_OPERATOR_VERSION:=v26.7.2}"
# PR 1109, the memory-limit fix, and opt-in NUMA sysfs/kubelet simulation.
# Keep this commit pinned until the integration is available in an upstream release.
: "${MOKKA_REPOSITORY:=https://github.com/JRosenboimNVIDIA/mokka.git}"
: "${MOKKA_REF:=8d5a0004f5dccf4f671b6d4f122c2523e9f814fb}"
: "${MOKKA_IMAGE_REPOSITORY:=mokka-e2e}"
: "${MOKKA_IMAGE_TAG:=${MOKKA_REF:0:12}}"

SOURCE_DIR=$(mktemp -d)
trap 'rm -rf "$SOURCE_DIR"' EXIT
git -C "$SOURCE_DIR" init --quiet
git -C "$SOURCE_DIR" fetch --depth=1 "$MOKKA_REPOSITORY" "$MOKKA_REF"
git -C "$SOURCE_DIR" checkout --quiet --detach FETCH_HEAD

docker build --file "$SOURCE_DIR/deployments/nvml-mock/Dockerfile" \
  --tag "$MOKKA_IMAGE_REPOSITORY:$MOKKA_IMAGE_TAG" "$SOURCE_DIR"
kind load docker-image --name "$CLUSTER_NAME" "$MOKKA_IMAGE_REPOSITORY:$MOKKA_IMAGE_TAG"

MOKKA_VALUES=(--values "$REPO_ROOT/hack/e2e-mokka-values.yaml"
  --set "image.repository=$MOKKA_IMAGE_REPOSITORY"
  --set "image.tag=$MOKKA_IMAGE_TAG" --set "gpu.count=$MOKKA_GPU_COUNT")
GPU_OPERATOR_VALUES=(--values "$REPO_ROOT/hack/e2e-gpu-operator-values.yaml")
if [[ "$FEATURE_CONFIG" = numa-full ]]; then
  # Separate releases keep the policy settings local to each selected node.
  for policy in single-numa-node restricted best-effort; do
    helm upgrade --install "nvml-mock-$policy" "$SOURCE_DIR/deployments/nvml-mock/helm/nvml-mock" \
      --namespace mokka --create-namespace "${MOKKA_VALUES[@]}" \
      --set 'nodeAgent.numa.enabled=true' \
      --set 'nodeAgent.numa.cpuManagerPolicy=static' --set 'nodeAgent.numa.memoryManagerPolicy=static' \
      --set 'nodeAgent.numa.systemReservedCPU=100m' \
      --set "nodeAgent.numa.topologyManagerPolicy=$policy" \
      --set "nodeSelector.mokka\\.nvidia\\.com/numa-policy=$policy" \
      --wait --timeout 10m
  done
  GPU_OPERATOR_VALUES+=(--values "$REPO_ROOT/hack/e2e-gpu-operator-numa-values.yaml")
else
  helm upgrade --install nvml-mock "$SOURCE_DIR/deployments/nvml-mock/helm/nvml-mock" \
    --namespace mokka --create-namespace "${MOKKA_VALUES[@]}" --wait --timeout 10m
fi

helm repo add nvidia https://helm.ngc.nvidia.com/nvidia --force-update
helm repo update nvidia
helm upgrade --install gpu-operator nvidia/gpu-operator \
  --version "$GPU_OPERATOR_VERSION" --namespace gpu-operator --create-namespace \
  "${GPU_OPERATOR_VALUES[@]}" --wait --timeout 15m

kubectl wait --for=jsonpath='{.status.state}'=ready clusterpolicy/cluster-policy --timeout=15m
kubectl wait \
  --for="jsonpath={.status.allocatable.nvidia\\.com/gpu}=$MOKKA_GPU_COUNT" \
  nodes --selector=mokka.nvidia.com/e2e-gpu=true --timeout=5m

if [[ "$FEATURE_CONFIG" = numa-full ]]; then
  # NFD caches allocatable resources at startup, before GPU registration can finish.
  # Refresh that snapshot only after kubelet reports the expected GPU count.
  kubectl --namespace gpu-operator rollout restart \
    daemonset/gpu-operator-node-feature-discovery-topology-updater
  kubectl --namespace gpu-operator rollout status \
    daemonset/gpu-operator-node-feature-discovery-topology-updater --timeout=5m
fi
