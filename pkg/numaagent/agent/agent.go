// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// Package agent ties the kubelet podresources observations to pod annotations: on each tick it
// lists local pod allocations, computes their NUMA placement, and patches the result onto pods
// whose placement changed.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kai-scheduler/KAI-scheduler/pkg/numaagent/consts"
	"github.com/kai-scheduler/KAI-scheduler/pkg/numaagent/cputopology"
	"github.com/kai-scheduler/KAI-scheduler/pkg/numaagent/placement"
	"github.com/kai-scheduler/KAI-scheduler/pkg/numaagent/podresources"
)

// Agent reconciles observed NUMA placement onto pods on a single node.
type Agent struct {
	nodeName     string
	pollInterval time.Duration

	resources *podresources.Client
	cpuToNUMA cputopology.CPUToNUMA
	clientset kubernetes.Interface

	// written caches the last annotation value pushed per pod, so unchanged placement does not
	// generate repeated patches. Keyed by "namespace/name".
	written map[string]string
}

// New constructs an Agent.
func New(nodeName string, pollInterval time.Duration, resources *podresources.Client,
	cpuToNUMA cputopology.CPUToNUMA, clientset kubernetes.Interface) *Agent {
	return &Agent{
		nodeName:     nodeName,
		pollInterval: pollInterval,
		resources:    resources,
		cpuToNUMA:    cpuToNUMA,
		clientset:    clientset,
		written:      map[string]string{},
	}
}

// Run reconciles once immediately, then on every tick until the context is cancelled.
func (a *Agent) Run(ctx context.Context) error {
	logger := log.FromContext(ctx)
	logger.Info("Starting NUMA placement agent", "node", a.nodeName, "pollInterval", a.pollInterval)

	ticker := time.NewTicker(a.pollInterval)
	defer ticker.Stop()

	for {
		if err := a.reconcile(ctx); err != nil {
			logger.Error(err, "Reconcile failed")
		}

		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (a *Agent) reconcile(ctx context.Context) error {
	logger := log.FromContext(ctx)

	pods, err := a.resources.List(ctx)
	if err != nil {
		return err
	}

	seen := make(map[string]struct{}, len(pods))
	for _, pod := range pods {
		key := pod.GetNamespace() + "/" + pod.GetName()
		seen[key] = struct{}{}

		observed := placement.Compute(pod, a.cpuToNUMA)
		if observed == nil {
			continue
		}

		value, err := observed.Marshal()
		if err != nil {
			logger.Error(err, "Failed to marshal placement", "pod", key)
			continue
		}

		if a.written[key] == value {
			continue
		}

		if err := a.patchAnnotation(ctx, pod.GetNamespace(), pod.GetName(), value); err != nil {
			if apierrors.IsNotFound(err) {
				// Pod not yet visible in the API (or already gone); retry on the next tick.
				continue
			}
			logger.Error(err, "Failed to patch placement annotation", "pod", key)
			continue
		}

		a.written[key] = value
		logger.V(1).Info("Published NUMA placement", "pod", key, "placement", value)
	}

	// Drop cache entries for pods no longer on this node.
	for key := range a.written {
		if _, ok := seen[key]; !ok {
			delete(a.written, key)
		}
	}
	return nil
}

func (a *Agent) patchAnnotation(ctx context.Context, namespace, name, value string) error {
	patch := map[string]any{
		"metadata": map[string]any{
			"annotations": map[string]string{
				consts.NumaPlacementAnnotation: value,
			},
		},
	}
	raw, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("marshaling patch: %w", err)
	}

	_, err = a.clientset.CoreV1().Pods(namespace).Patch(
		ctx, name, types.MergePatchType, raw, metav1.PatchOptions{})
	return err
}
