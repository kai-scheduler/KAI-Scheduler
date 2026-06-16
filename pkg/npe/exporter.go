// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// Package npe is the NUMA Placement Exporter: it ties the kubelet podresources observations to
// pod annotations. On each tick it lists local pod allocations, computes their NUMA placement,
// and patches the result onto pods whose placement changed.
package npe

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	podresourcesv1 "k8s.io/kubelet/pkg/apis/podresources/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kai-scheduler/KAI-scheduler/pkg/npe/consts"
	"github.com/kai-scheduler/KAI-scheduler/pkg/npe/cputopology"
	"github.com/kai-scheduler/KAI-scheduler/pkg/npe/placement"
)

// resourceLister lists per-pod resource allocations from the local kubelet. *podresources.Client
// satisfies it; the interface keeps the exporter testable without a real podresources socket.
type resourceLister interface {
	List(ctx context.Context) ([]*podresourcesv1.PodResources, error)
}

// Exporter reconciles observed NUMA placement onto pods on a single node.
type Exporter struct {
	nodeName            string
	pollInterval        time.Duration
	driftResyncInterval time.Duration

	resources resourceLister
	cpuToNUMA cputopology.CPUToNUMA
	clientset kubernetes.Interface

	// written caches the last annotation value pushed per pod, so unchanged placement does not
	// generate repeated patches. Keyed by "namespace/name". Both reconcile loops run on a single
	// goroutine, so no synchronization is needed.
	written map[string]string
}

// New constructs an Exporter. A driftResyncInterval of 0 disables the API-server drift pass.
func New(nodeName string, pollInterval, driftResyncInterval time.Duration, resources resourceLister,
	cpuToNUMA cputopology.CPUToNUMA, clientset kubernetes.Interface) *Exporter {
	return &Exporter{
		nodeName:            nodeName,
		pollInterval:        pollInterval,
		driftResyncInterval: driftResyncInterval,
		resources:           resources,
		cpuToNUMA:           cpuToNUMA,
		clientset:           clientset,
		written:             map[string]string{},
	}
}

// Run reconciles once immediately, then drives the fast podresources pass and (unless disabled)
// the slower API-server drift pass off their own tickers until the context is cancelled. Both
// passes run on this single goroutine, so the write cache needs no locking.
func (a *Exporter) Run(ctx context.Context) error {
	logger := log.FromContext(ctx)
	logger.Info("Starting NUMA placement exporter", "node", a.nodeName,
		"pollInterval", a.pollInterval, "driftResyncInterval", a.driftResyncInterval)

	if err := a.reconcile(ctx); err != nil {
		logger.Error(err, "Reconcile failed")
	}

	pollTicker := time.NewTicker(a.pollInterval)
	defer pollTicker.Stop()

	driftC, stopDrift := a.driftTicker()
	defer stopDrift()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-pollTicker.C:
			if err := a.reconcile(ctx); err != nil {
				logger.Error(err, "Reconcile failed")
			}
		case <-driftC:
			if err := a.reconcileDrift(ctx); err != nil {
				logger.Error(err, "Drift reconcile failed")
			}
		}
	}
}

// driftTicker returns the drift pass's tick channel and a stop func. When the interval is 0 the
// channel is nil, which blocks forever in select — disabling the pass without a special case.
func (a *Exporter) driftTicker() (<-chan time.Time, func()) {
	if a.driftResyncInterval <= 0 {
		return nil, func() {}
	}
	ticker := time.NewTicker(a.driftResyncInterval)
	return ticker.C, ticker.Stop
}

func (a *Exporter) reconcile(ctx context.Context) error {
	logger := log.FromContext(ctx)

	pods, err := a.resources.List(ctx)
	if err != nil {
		return err
	}

	logger.Info("Reconciling NUMA placement", "pods", len(pods))

	seen := make(map[string]struct{}, len(pods))
	for _, pod := range pods {
		key := pod.GetNamespace() + "/" + pod.GetName()
		seen[key] = struct{}{}

		value, ok := a.placementValue(ctx, pod)
		if !ok {
			continue
		}

		if a.written[key] == value {
			continue
		}

		if err := a.patchAnnotation(ctx, pod.GetNamespace(), pod.GetName(), value); err != nil {
			if apierrors.IsNotFound(err) {
				logger.V(1).Info("Pod not found, skipping placement annotation patch", "pod", key)
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

// reconcileDrift lists the pods assigned to this node from the API server and repairs any whose
// live annotation no longer matches the observed placement (removed or modified out-of-band).
// Unlike reconcile it does not trust the write cache: the live annotation is the comparison
// baseline. The cache is refreshed to match reality as a side effect.
func (a *Exporter) reconcileDrift(ctx context.Context) error {
	logger := log.FromContext(ctx)

	pods, err := a.resources.List(ctx)
	if err != nil {
		return err
	}

	expected := make(map[string]string, len(pods))
	for _, pod := range pods {
		value, ok := a.placementValue(ctx, pod)
		if !ok {
			continue
		}
		expected[pod.GetNamespace()+"/"+pod.GetName()] = value
	}

	if len(expected) == 0 {
		return nil
	}

	podList, err := a.clientset.CoreV1().Pods(metav1.NamespaceAll).List(ctx, metav1.ListOptions{
		FieldSelector: fields.OneTermEqualSelector("spec.nodeName", a.nodeName).String(),
	})
	if err != nil {
		return err
	}

	for i := range podList.Items {
		pod := &podList.Items[i]
		key := pod.Namespace + "/" + pod.Name
		value, ok := expected[key]
		if !ok {
			continue
		}

		if pod.Annotations[consts.NumaPlacementAnnotation] == value {
			a.written[key] = value
			continue
		}

		if err := a.patchAnnotation(ctx, pod.Namespace, pod.Name, value); err != nil {
			logger.Error(err, "Failed to patch drifted placement annotation", "pod", key)
			continue
		}
		a.written[key] = value
		logger.V(1).Info("Repaired drifted NUMA placement", "pod", key, "placement", value)
	}
	return nil
}

// placementValue computes a pod's observed placement and marshals it to the annotation value.
// Returns ok=false when the pod holds no topology-aligned resources or marshaling fails.
func (a *Exporter) placementValue(ctx context.Context, pod *podresourcesv1.PodResources) (string, bool) {
	observed := placement.Compute(pod, a.cpuToNUMA)
	if observed == nil {
		return "", false
	}
	value, err := observed.Marshal()
	if err != nil {
		log.FromContext(ctx).Error(err, "Failed to marshal placement",
			"pod", pod.GetNamespace()+"/"+pod.GetName())
		return "", false
	}
	return value, true
}

func (a *Exporter) patchAnnotation(ctx context.Context, namespace, name, value string) error {
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
