#!/usr/bin/env bash
# Copyright 2026 NVIDIA CORPORATION
# SPDX-License-Identifier: Apache-2.0

# Install the simulated GPU provider on the disposable Kind cluster used by the scale suite.
set -euo pipefail
mokka=$(cd "${1:?pass the path to the Mokka checkout}" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
namespace=mokka

# Process simulated Pod deletions in parallel with Job replacement at scale.
curl --fail --silent --show-error --location --retry 3 \
  https://github.com/kubernetes-sigs/kwok/releases/download/v0.8.0/kwok.yaml |
  sed 's/podPlayStageParallelism: 4$/podPlayStageParallelism: 64/' |
  kubectl apply --server-side -f -
kubectl wait --for=condition=Established crd/stages.kwok.x-k8s.io --timeout=90s
kubectl apply -f https://github.com/kubernetes-sigs/kwok/releases/download/v0.8.0/stage-fast.yaml
kubectl apply -f "$mokka/deployments/mokka-kwok/stages/pod-admission.yaml"
kubectl delete stage pod-complete --ignore-not-found
kubectl apply -f test/e2e/scale/kwok-terminating-job-pod.yaml
kubectl patch stage pod-delete --type=json -p '[
  {"op":"add","path":"/spec/selector/matchExpressions/-","value":{
    "key":".metadata.finalizers.[]","operator":"NotIn","values":["batch.kubernetes.io/job-tracking"]
  }}
]'
kubectl apply --server-side -f https://github.com/run-ai/kwok-operator/releases/download/1.0.4/kwok-operator.yaml
kubectl wait --for=condition=Established crd/nodepools.kwok.sigs.run-ai.com --timeout=90s
kubectl patch --local -f test/e2e/scale/base_kwok_managed_nodepool.yaml --type=merge \
  -p '{"spec":{"nodeCount":0}}' -o yaml | kubectl apply -f -

(
  cd "$mokka"
  arch=$(kubectl get nodes -l node-role.kubernetes.io/control-plane -o jsonpath='{.items[0].status.nodeInfo.architecture}')
  docker build --platform "linux/$arch" -f deployments/control-plane/Dockerfile -t mokka-control-plane:e2e .
  mkdir -p "$work/linux/$arch"
  CGO_ENABLED=0 GOOS=linux GOARCH="$arch" GOTOOLCHAIN=go1.27.4 go build -trimpath -o "$work/linux/$arch/mokka-kwok" ./cmd/mokka-kwok
  docker build --platform "linux/$arch" -f deployments/mokka-kwok/Dockerfile -t mokka-kwok:e2e "$work"
  kind load docker-image --name "${CLUSTER_NAME:-kind}" mokka-control-plane:e2e mokka-kwok:e2e

  helm upgrade --install mokka-crds deployments/mokka-crds/helm/mokka-crds -n "$namespace" --create-namespace --wait
  kubectl wait --for=condition=Established --timeout=90s \
    crd/sgpurackprofiles.mokka.nvidia.com crd/sgpuinventories.mokka.nvidia.com crd/sgpurackallocations.mokka.nvidia.com
  # Only the virtual GPU pool is eligible. The chart excludes KWOK Nodes from its CPU node daemon.
  helm upgrade --install mokka deployments/nvml-mock/helm/nvml-mock -n "$namespace" \
    --set-string 'nodeSelector.mokka\.nvidia\.com/sgpu-node=true' \
    --set controlPlane.enabled=true,controlPlane.image.repository=mokka-control-plane,controlPlane.image.tag=e2e,controlPlane.image.pullPolicy=Never \
    --set kwok.enabled=true,kwok.dataPlane.image.repository=mokka-kwok,kwok.dataPlane.image.tag=e2e,kwok.dataPlane.image.pullPolicy=Never \
    --wait --timeout=5m

  # The topology case uses 512 nodes. Each rack has eight nodes with eight GPUs each.
  sed 's/__NODES_PER_RACK__/8/' tests/kwok/profile.yaml |
    kubectl create --dry-run=client -f - -o json |
    jq '.spec.node.gpus.count = 8 | .spec.node.topology.gpuSlots |=
      [.[0] as $slot | range(8) as $i | $slot + {index: $i, pciAddress: "0000:0\($i+1):00.0"}]' |
    kubectl apply -f -
)
kubectl apply -f test/e2e/scale/mokka-inventory.yaml
kubectl patch schedulingshard default --type=merge -p '{"spec":{"args":{"max-consolidation-preemptees":"0","qps":"250","burst":"500"}}}'
kubectl patch config.kai.scheduler kai-config --type=merge --patch-file hack/mokka-kwok-scale-config.yaml
# Wait for the operator to apply the scale config before starting the load.
kubectl -n kai-scheduler wait deployment/admission --for=jsonpath='{.spec.replicas}'=4 --timeout=3m
kubectl -n kai-scheduler rollout status deployment/admission --timeout=5m
