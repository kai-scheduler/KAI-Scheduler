#!/usr/bin/env bash
# Copyright 2026 NVIDIA CORPORATION
# SPDX-License-Identifier: Apache-2.0
set -euo pipefail

chart=oci://ghcr.io/nvidia/k8s-test-infra/chart/nvml-mock
version=0.8.0

for policy in single-numa-node restricted best-effort; do
  helm upgrade --install "mokka-$policy" "$chart" --version "$version" \
    --namespace "mokka-$policy" --create-namespace \
    --set gpu.profile=a100 --set gpu.count=8 \
    --set nri.enabled=true --set-string nri.pluginIndex=05 \
    --set nodeAgent.numa.enabled=true --set nodeAgent.numa.cpuManagerPolicy=static \
    --set nodeAgent.numa.memoryManagerPolicy=static --set nodeAgent.numa.systemReservedCPU=100m \
    --set "nodeAgent.numa.topologyManagerPolicy=$policy" \
    --set-string "nodeSelector.mokka\\.nvidia\\.com/numa-policy=$policy" --wait --timeout 10m
done

helm repo add nvidia https://helm.ngc.nvidia.com/nvidia --force-update
helm upgrade --install gpu-operator nvidia/gpu-operator --version v26.7.3 \
  --namespace gpu-operator --create-namespace -f hack/numa/gpu-operator.yaml --wait --timeout 15m
kubectl wait --for="jsonpath={.status.allocatable.nvidia\\.com/gpu}=8" \
  nodes -l mokka.nvidia.com/e2e-gpu=true --timeout=10m

# Refresh NFD's device inventory after kubelet registers all GPUs.
updater=daemonset/gpu-operator-node-feature-discovery-topology-updater
kubectl -n gpu-operator rollout restart "$updater"
kubectl -n gpu-operator rollout status "$updater" --timeout=5m
for node in $(kubectl get nodes -l mokka.nvidia.com/e2e-gpu=true -o jsonpath='{.items[*].metadata.name}'); do
  kubectl wait --for=create "noderesourcetopology/$node" --timeout=5m
  for zone in node-0 node-1; do
    kubectl wait --timeout=5m --for="jsonpath={.zones[?(@.name=='$zone')].resources[?(@.name=='nvidia.com/gpu')].capacity}=4" "noderesourcetopology/$node"
  done
done
