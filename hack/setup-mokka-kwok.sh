#!/usr/bin/env bash
# Copyright 2026 NVIDIA CORPORATION
# SPDX-License-Identifier: Apache-2.0

# Install the simulated GPU provider on the disposable Kind cluster used by the scale suite.
set -euo pipefail
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
namespace="mokka-kwok-poc"

kubectl apply --server-side -f https://github.com/kubernetes-sigs/kwok/releases/download/v0.8.0/kwok.yaml
kubectl wait --for=condition=Established crd/stages.kwok.x-k8s.io --timeout=90s
kubectl apply -f https://github.com/kubernetes-sigs/kwok/releases/download/v0.8.0/stage-fast.yaml
kubectl delete stage pod-complete --ignore-not-found
kubectl apply --server-side -f https://github.com/run-ai/kwok-operator/releases/download/1.0.4/kwok-operator.yaml
kubectl wait --for=condition=Established crd/nodepools.kwok.sigs.run-ai.com --timeout=90s
kubectl patch --local -f test/e2e/scale/base_kwok_managed_nodepool.yaml --type=merge \
  -p '{"spec":{"nodeCount":0}}' -o yaml | kubectl apply -f -

if [[ ${KWOK_GPU_PROVIDER:-mokka} == fgo ]]; then
  helm upgrade --install fake-gpu-operator oci://ghcr.io/run-ai/fake-gpu-operator/fake-gpu-operator \
    --version 0.0.73 --namespace gpu-operator --create-namespace --set topology.nodePools.default.gpuCount=8
  kubectl -n gpu-operator delete deployment nvidia-dcgm-exporter device-plugin --ignore-not-found
  kubectl -n gpu-operator rollout status deployment/kwok-gpu-device-plugin --timeout=5m
else
(
  cd "${1:?pass the path to the Mokka PoC checkout}"
  arch=$(kubectl get nodes -l node-role.kubernetes.io/control-plane -o jsonpath='{.items[0].status.nodeInfo.architecture}')
  CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -o "$work/" ./cmd/control-plane ./cmd/kwok-gpu
  docker build --platform "linux/$arch" -t mokka-kwok-poc:dev -f examples/kwok-gpu/Dockerfile "$work"
  kind load docker-image --name "${CLUSTER_NAME:-kind}" mokka-kwok-poc:dev
  helm install mokka-crds deployments/mokka-crds/helm/mokka-crds -n "$namespace" --create-namespace
  helm template kwok-poc deployments/nvml-mock/helm/nvml-mock \
    -n "$namespace" --kube-version 1.35.0 \
    --set controlPlane.enabled=true --set controlPlane.replicas=1 \
    --set controlPlane.image.repository=mokka-kwok-poc --set controlPlane.image.tag=dev \
    --show-only templates/controlplane/rbac.yaml --show-only templates/controlplane/serviceaccount.yaml \
    --show-only templates/controlplane/deployment.yaml | kubectl -n "$namespace" apply -f -
  kubectl apply -f examples/kwok-gpu/controller.yaml

  # The topology case uses 512 nodes. Each rack has eight nodes with eight GPUs each.
  sed 's/__NODES_PER_RACK__/8/' tests/kwok/profile.yaml |
    kubectl create --dry-run=client -f - -o json |
    jq '.spec.node.gpus.count = 8 | .spec.node.topology.gpuSlots |=
      [.[0] as $slot | range(8) as $i | $slot + {index: $i, pciAddress: "0000:0\($i+1):00.0"}]' |
    kubectl apply -f -
  sed -e 's/__RACK_COUNT__/64/' \
      -e 's/mokka.nvidia.com\/pool: kwok-scale/mokka.nvidia.com\/node-type: KWOK/' \
      tests/kwok/inventory.yaml | kubectl apply -f -
)
kubectl -n "$namespace" rollout status deployment --timeout=5m
fi
kubectl patch schedulingshard default --type=merge -p '{"spec":{"args":{"max-consolidation-preemptees":"0"}}}'
kubectl patch config.kai.scheduler kai-config --type=merge --patch-file hack/mokka-kwok-scale-config.yaml
# Wait for the operator to apply the scale config before starting the load.
kubectl -n kai-scheduler wait deployment/admission --for=jsonpath='{.spec.replicas}'=4 --timeout=3m
kubectl -n kai-scheduler rollout status deployment/admission --timeout=5m
