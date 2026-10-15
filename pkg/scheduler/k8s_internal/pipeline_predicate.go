// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package k8s_internal

import (
	"context"
	"errors"
	"fmt"
	"slices"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	ksf "k8s.io/kube-scheduler/framework"
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
)

// FitPipelinePredicateConverter evaluates a filter without terminating pods on the candidate node.
func FitPipelinePredicateConverter(stateProvider SessionStateProvider, nodeFilter NodeFilter) FitPredicateFilter {
	var extensions ksf.PreFilterExtensions
	if plugin, ok := nodeFilter.(interface {
		PreFilterExtensions() ksf.PreFilterExtensions
	}); ok {
		extensions = plugin.PreFilterExtensions()
	}
	if extensions == nil {
		log.InfraLogger.Warningf("Predicate %T has no PreFilter AddPod/RemovePod extensions; pipeline checks only remove terminating pods from NodeInfo", nodeFilter)
	}

	return func(pod *v1.Pod, nodeInfo *k8sframework.NodeInfo) (fit bool, reasons []string, err error) {
		ctx := context.Background()
		// Isolate node changes and prefilter extension updates from subsequent allocation checks.
		state := stateProvider.GetSessionStateForResource(pod.UID).Clone()
		node := nodeInfo.SnapshotConcrete()
		var removed []ksf.PodInfo
		defer func() {
			for _, podInfo := range removed {
				node.AddPodInfo(podInfo)
				if extensions != nil {
					if status := extensions.AddPod(ctx, state, pod, podInfo, node); !status.IsSuccess() {
						fit = false
						err = errors.Join(err, fmt.Errorf("predicate %T AddPod: %w", nodeFilter, status.AsError()))
					}
				}
			}
		}()

		for _, podInfo := range slices.Clone(node.GetPods()) {
			if podInfo.GetPod().DeletionTimestamp == nil {
				continue
			}
			if err := node.RemovePod(klog.FromContext(ctx), podInfo.GetPod()); err != nil {
				return false, nil, err
			}
			removed = append(removed, podInfo)
			if extensions != nil {
				if status := extensions.RemovePod(ctx, state, pod, podInfo, node); !status.IsSuccess() {
					return false, nil, fmt.Errorf("predicate %T RemovePod: %w", nodeFilter, status.AsError())
				}
			}
		}

		result := nodeFilter.Filter(ctx, state, pod, node)
		if result == nil {
			return true, nil, nil
		}
		return result.IsSuccess(), result.Reasons(), result.AsError()
	}
}
