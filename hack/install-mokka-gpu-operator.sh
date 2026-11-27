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
# Release-26.7 backport of GPU Operator #2959, immediately after v26.7.2.
: "${GPU_OPERATOR_REF:=158ec57fa19eb71ad484d5e337f05a2b612ca170}"
: "${GPU_OPERATOR_IMAGE_REPOSITORY:=local/gpu-operator}"
: "${GPU_OPERATOR_IMAGE_TAG:=${GPU_OPERATOR_REF:0:12}}"
GPU_OPERATOR_IMAGE="$GPU_OPERATOR_IMAGE_REPOSITORY:$GPU_OPERATOR_IMAGE_TAG"
# Main-based NUMA PR 1148; no simulated kernel-driver or GPUCluster changes.
: "${MOKKA_REPOSITORY:=https://github.com/JRosenboimNVIDIA/mokka.git}"
: "${MOKKA_REF:=9489ec15a576e55d073b82e6265c6255c1a6ba53}"
: "${MOKKA_IMAGE_REPOSITORY:=mokka-e2e}"
: "${MOKKA_IMAGE_TAG:=${MOKKA_REF:0:12}}"

SOURCE_DIR=$(mktemp -d)
trap 'rm -rf "$SOURCE_DIR"' EXIT
MOKKA_SOURCE="$SOURCE_DIR/mokka"
OPERATOR_SOURCE="$SOURCE_DIR/gpu-operator"
git init --quiet "$MOKKA_SOURCE"
git -C "$MOKKA_SOURCE" fetch --depth=1 "$MOKKA_REPOSITORY" "$MOKKA_REF"
git -C "$MOKKA_SOURCE" checkout --quiet --detach FETCH_HEAD

docker build --file "$MOKKA_SOURCE/deployments/nvml-mock/Dockerfile" \
  --tag "$MOKKA_IMAGE_REPOSITORY:$MOKKA_IMAGE_TAG" "$MOKKA_SOURCE"
kind load docker-image --name "$CLUSTER_NAME" "$MOKKA_IMAGE_REPOSITORY:$MOKKA_IMAGE_TAG"

# The backport changes only six operand manifests and their unit test. Keep
# NVIDIA's release binaries and copy the exact upstream assets into the image.
git init --quiet "$OPERATOR_SOURCE"
git -C "$OPERATOR_SOURCE" fetch --depth=1 https://github.com/NVIDIA/gpu-operator.git "$GPU_OPERATOR_REF"
git -C "$OPERATOR_SOURCE" checkout --quiet --detach FETCH_HEAD
docker build --file "$REPO_ROOT/hack/gpu-operator-sysfs.Dockerfile" \
  --build-arg "GPU_OPERATOR_VERSION=$GPU_OPERATOR_VERSION" \
  --build-arg "GPU_OPERATOR_REF=$GPU_OPERATOR_REF" \
  --tag "$GPU_OPERATOR_IMAGE" "$OPERATOR_SOURCE"
kind load docker-image --name "$CLUSTER_NAME" "$GPU_OPERATOR_IMAGE"

MOKKA_VALUES=(--values "$REPO_ROOT/hack/e2e-mokka-values.yaml"
  --set "image.repository=$MOKKA_IMAGE_REPOSITORY"
  --set "image.tag=$MOKKA_IMAGE_TAG" --set "gpu.count=$MOKKA_GPU_COUNT")
GPU_OPERATOR_VALUES=(--values "$REPO_ROOT/hack/e2e-gpu-operator-values.yaml")
if [[ "$FEATURE_CONFIG" = numa-full ]]; then
  # Separate releases keep the policy settings local to each selected node.
  for policy in single-numa-node restricted best-effort; do
    helm upgrade --install "nvml-mock-$policy" "$MOKKA_SOURCE/deployments/nvml-mock/helm/nvml-mock" \
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
  helm upgrade --install nvml-mock "$MOKKA_SOURCE/deployments/nvml-mock/helm/nvml-mock" \
    --namespace mokka --create-namespace "${MOKKA_VALUES[@]}" --wait --timeout 10m
fi

helm repo add nvidia https://helm.ngc.nvidia.com/nvidia --force-update
helm repo update nvidia
helm upgrade --install gpu-operator nvidia/gpu-operator \
  --version "$GPU_OPERATOR_VERSION" --namespace gpu-operator --create-namespace \
  --set-string "operator.repository=${GPU_OPERATOR_IMAGE_REPOSITORY%/*}" \
  --set-string "operator.image=${GPU_OPERATOR_IMAGE_REPOSITORY##*/}" \
  --set-string "operator.version=$GPU_OPERATOR_IMAGE_TAG" \
  "${GPU_OPERATOR_VALUES[@]}" --wait --timeout 15m

kubectl --namespace gpu-operator wait --for=create daemonset/nvidia-device-plugin-daemonset --timeout=5m
kubectl --namespace gpu-operator get daemonset nvidia-device-plugin-daemonset \
  -o 'jsonpath={.spec.template.spec.initContainers[?(@.name=="toolkit-validation")].args}' |
  grep -Fq '/sys/module/nvidia/initstate'
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
