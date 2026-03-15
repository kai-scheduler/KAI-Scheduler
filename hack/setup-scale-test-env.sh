#!/bin/bash
# Copyright 2026 NVIDIA CORPORATION
# SPDX-License-Identifier: Apache-2.0


SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
SCALE_DIR="${REPO_ROOT}/test/e2e/scale"

KWOK_LATEST_RELEASE=v0.6.1
kubectl apply -f "https://github.com/kubernetes-sigs/kwok/releases/download/${KWOK_LATEST_RELEASE}/kwok.yaml"
kubectl apply -f "https://github.com/kubernetes-sigs/kwok/releases/download/${KWOK_LATEST_RELEASE}/stage-fast.yaml"
kubectl apply --server-side -f https://github.com/run-ai/kwok-operator/releases/download/1.0.1/kwok-operator.yaml
kubectl apply -f "${SCALE_DIR}/base_kwok_managed_nodepool.yaml"

helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo add grafana https://grafana.github.io/helm-charts
helm repo update

helm upgrade -i fake-gpu-operator oci://ghcr.io/run-ai/fake-gpu-operator/fake-gpu-operator --namespace gpu-operator --create-namespace --set topology.nodePools.default.gpuCount=8 --devel --version 0.0.58
kubectl delete deployment -n gpu-operator nvidia-dcgm-exporter --ignore-not-found
kubectl delete deployment -n gpu-operator device-plugin --ignore-not-found

helm upgrade -i --create-namespace -n monitoring kube-prometheus-stack prometheus-community/kube-prometheus-stack --set prometheus.enabled=true --set grafana.enabled=true
helm -n monitoring upgrade -i pyroscope grafana/pyroscope

kubectl patch nodepool managed-nodepool -p '{"spec":{"nodeCount": 0, "nodeTemplate": {"metadata": {"labels": {"run.ai/simulated-gpu-node-pool": "default"}}}}}' --type merge

kubectl apply -f "${SCALE_DIR}/scheduler-service-monitor.yaml"
kubectl apply -f "${SCALE_DIR}/binder.yaml"

kubectl delete stage pod-complete --ignore-not-found

# Disable consolidation and enable pyroscope
kubectl patch schedulingshard default --type merge -p '{"spec":{"args": {"max-consolidation-preemptees": "0", "pyroscope-address": "http://pyroscope.monitoring.svc.cluster.local.:4040"}}}'

# Set binder concurrency
kubectl patch config.kai.scheduler kai-config --type merge -p '{"spec":{"binder": {"maxConcurrentReconciles": 100}}}'
# Set admission replicas
kubectl patch config.kai.scheduler kai-config --type merge -p '{"spec":{"admission": {"replicas": 4}}}'

# Expand resources for selected services manually for scale tests
# Scheduler
kubectl patch config.kai.scheduler kai-config --type merge -p '{"spec":{"scheduler":{"service":{"resources":{"requests":{"cpu":"3","memory":"7Gi"},"limits":{"cpu":"5","memory":"7Gi"}}}}}}'

# Binder
kubectl patch config.kai.scheduler kai-config --type merge -p '{"spec":{"binder":{"service":{"resources":{"requests":{"cpu":"100m","memory":"2500Mi"},"limits":{"cpu":"400m","memory":"2500Mi"}}}}}}'

# pod group controller
kubectl patch config.kai.scheduler kai-config --type merge -p '{"spec":{"podGroupController":{"service":{"resources":{"requests":{"cpu":"50m","memory":"8000Mi"},"limits":{"cpu":"200m","memory":"8000Mi"}}}}}}'

# queue controller
kubectl patch config.kai.scheduler kai-config --type merge -p '{"spec":{"queueController":{"service":{"resources":{"requests":{"cpu":"50m","memory":"200Mi"},"limits":{"cpu":"200m","memory":"200Mi"}}}}}}'

# pod grouper
kubectl patch config.kai.scheduler kai-config --type merge -p '{"spec":{"podGrouper":{"service":{"resources":{"requests":{"cpu":"50m","memory":"2000Mi"},"limits":{"cpu":"200m","memory":"2000Mi"}}}}}}'
