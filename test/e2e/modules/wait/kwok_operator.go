// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package wait

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/watch"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"

	kwokopv1beta1 "github.com/run-ai/kwok-operator/api/v1beta1"

	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/constant"
	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/wait/watcher"
)

func ForKWOKOperatorNodePool(
	ctx context.Context, client runtimeClient.WithWatch, nodePoolName string,
) {
	condition := func(event watch.Event) bool {
		nodepools, ok := event.Object.(*kwokopv1beta1.NodePoolList)
		if !ok {
			return false
		}

		if len(nodepools.Items) != 1 {
			return false
		}
		nodepool := &nodepools.Items[0]
		for _, condition := range nodepool.Status.Conditions {
			if condition.Type == "Available" && condition.Status == "True" {
				return true
			}
		}
		return false
	}

	w := watcher.NewGenericWatcher[kwokopv1beta1.NodePoolList](client, condition,
		runtimeClient.MatchingFields(map[string]string{
			"metadata.name": nodePoolName,
		}))
	if !watcher.ForEvent(ctx, client, w) {
		Fail("Failed to wait for nodepool to be ready")
	}
}

func ForExactlyNKWOKOperatorNodePools(
	ctx context.Context, client runtimeClient.WithWatch, nodePoolLabels map[string]string, n int,
) {
	condition := func(event watch.Event) bool {
		nodepools, ok := event.Object.(*kwokopv1beta1.NodePoolList)
		if !ok {
			return false
		}

		if len(nodepools.Items) != n {
			return false
		}

		readyCount := 0
		for _, nodepool := range nodepools.Items {
			for _, condition := range nodepool.Status.Conditions {
				if condition.Type == "Available" && condition.Status == "True" {
					readyCount++
					break
				}
			}
		}
		return readyCount >= n
	}

	w := watcher.NewGenericWatcher[kwokopv1beta1.NodePoolList](client, condition,
		runtimeClient.MatchingLabels(nodePoolLabels))
	if !watcher.ForEvent(ctx, client, w) {
		Fail(fmt.Sprintf("Failed to wait for at least %d nodepools to be ready", n))
	}
}

func ForAtLeastNNodes(
	ctx context.Context, client runtimeClient.WithWatch, nodeLabels map[string]string, n int,
) {
	condition := func(event watch.Event) bool {
		nodes, ok := event.Object.(*v1.NodeList)
		if !ok {
			return false
		}

		if len(nodes.Items) < n {
			return false
		}

		readyCount := 0
		for _, node := range nodes.Items {
			for _, condition := range node.Status.Conditions {
				if condition.Type == v1.NodeReady && condition.Status == v1.ConditionTrue {
					readyCount++
					break
				}
			}
		}
		return readyCount >= n
	}

	w := watcher.NewGenericWatcher[v1.NodeList](client, condition,
		runtimeClient.MatchingLabels(nodeLabels))
	if !watcher.ForEvent(ctx, client, w) {
		Fail(fmt.Sprintf("Failed to wait for at least %d nodes to be ready", n))
	}
}

func ForGPUOPeratorUpdateOnKWOKNodes(
	ctx context.Context, client runtimeClient.WithWatch, numberOfNodes, gpusPerNode int,
) {
	condition := func(event watch.Event) bool {
		nodes, ok := event.Object.(*v1.NodeList)
		if !ok {
			return false
		}
		if len(nodes.Items) != numberOfNodes {
			return false
		}
		for _, node := range nodes.Items {
			gpuResource, foundGPU := node.Status.Capacity[constant.NvidiaGPUResource]
			if !foundGPU || gpuResource.Value() != int64(gpusPerNode) {
				return false
			}
		}
		return true
	}

	w := watcher.NewGenericWatcher[v1.NodeList](client, condition,
		runtimeClient.MatchingLabels(map[string]string{
			"type": "kwok",
		}))
	if !watcher.ForEvent(ctx, client, w) {
		Fail("Failed to wait for nodes to be updated by fake gpu operator")
	}
}

func ForZeroKWOKNodes(
	ctx context.Context, client runtimeClient.WithWatch,
) {
	condition := func(event watch.Event) bool {
		nodes, ok := event.Object.(*v1.NodeList)
		if !ok {
			return false
		}
		return len(nodes.Items) == 0
	}

	w := watcher.NewGenericWatcher[v1.NodeList](client, condition,
		runtimeClient.MatchingLabels(map[string]string{
			"type": "kwok",
		}))
	if !watcher.ForEvent(ctx, client, w) {
		Fail("Failed to wait for 0 kwok nodes in the cluster")
	}
}
