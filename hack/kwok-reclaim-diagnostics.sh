#!/usr/bin/env bash
# Copyright 2026 NVIDIA CORPORATION
# SPDX-License-Identifier: Apache-2.0

# Sample only while the zone-constrained Job exists. Its Pods and NodePools can
# disappear during cleanup, before a post-failure workflow step can read them.
set -u
while sleep 120; do
  namespace=$(kubectl get namespaces -o json | jq -r '.items[].metadata.name | select(startswith("kai-topology-reclaim-"))')
  [[ -n "$namespace" ]] || continue
  job=$(kubectl -n "$namespace" get jobs -l scale-test-batch -o name | head -1)
  [[ -n "$job" ]] || continue
  date -u
  kubectl -n "$namespace" get "$job" -o json | jq -c '{job:.metadata.name,status}'
  kubectl -n "$namespace" get podgroups.scheduling.run.ai -o json |
    jq -c '.items[] | {name:.metadata.name,spec:{queue:.spec.queue,minMember:.spec.minMember,priority:.spec.priorityClassName},status:.status}'
  kubectl get nodes -l type=kwok -o json | jq -c '[.items[] | {
    zone:.metadata.labels["cloud.provider.com/topology-zone"],gpu:.status.allocatable["nvidia.com/gpu"],
    ready:(.status.conditions[] | select(.type=="Ready").status), cidr:(.spec.podCIDR != null),
    taints:[.spec.taints[]? | .key]}] | group_by(.) | map({shape:.[0],nodes:length})'
  kubectl get pods -l app=engine-e2e -A -o json | jq -c '[.items[] | {
    ns:.metadata.namespace, phase:.status.phase,bound:(.spec.nodeName != null),deleting:(.metadata.deletionTimestamp != null),
    deletingOverTwoMinutes:(.metadata.deletionTimestamp | if . then (fromdateiso8601 < now - 120) else false end),
    finalizers:(.metadata.finalizers // [])}]
    | group_by(.) | map({state:.[0],pods:length})'
  svc=$(kubectl -n kai-scheduler get service -l app=kai-scheduler-default -o jsonpath='{.items[0].metadata.name}')
  kubectl get --raw "/api/v1/namespaces/kai-scheduler/services/http:$svc:http-metrics/proxy/metrics" |
    awk '/^scenario_search_(jobs|scenarios)_total\{/ && /action="reclaim"/'
done
