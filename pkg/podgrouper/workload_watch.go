// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	schedulingv1alpha1 "k8s.io/api/scheduling/v1alpha1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// WorkloadRefIndex is the field-indexer key used to look up Pods by the
// upstream Kubernetes Workload they reference (pod.spec.workloadRef.name).
const WorkloadRefIndex = "spec.workloadRef.name"

// podsByWorkloadRef is an indexer func that lets us list Pods referencing a
// given Workload via `client.MatchingFields{WorkloadRefIndex: name}`.
func podsByWorkloadRef(obj client.Object) []string {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		return nil
	}
	if pod.Spec.WorkloadRef == nil {
		return nil
	}
	return []string{pod.Spec.WorkloadRef.Name}
}

// registerWorkloadWatch installs a field-indexer on Pods and attaches a
// secondary watch on scheduling.k8s.io/v1alpha1 Workload objects. Workload
// events map back to Pods referencing them, implementing the "instant
// recovery" behaviour described in section 4 of the design — a Pod that
// referenced a not-yet-existing Workload is re-reconciled immediately when
// the Workload appears.
//
// Must only be called when the Workload API is available on the cluster (see
// featuregates.IsWorkloadAPIEnabled).
func registerWorkloadWatch(mgr ctrl.Manager, b *builder.Builder) error {
	if err := mgr.GetFieldIndexer().IndexField(
		context.Background(), &corev1.Pod{}, WorkloadRefIndex, podsByWorkloadRef,
	); err != nil {
		return fmt.Errorf("failed to index pods by %s: %w", WorkloadRefIndex, err)
	}

	b.Watches(
		&schedulingv1alpha1.Workload{},
		handler.EnqueueRequestsFromMapFunc(workloadToPodRequests(mgr.GetClient())),
	)
	return nil
}

// workloadToPodRequests returns a MapFunc that, given a Workload event,
// lists every Pod in the same namespace that references it and produces a
// reconcile.Request per Pod.
func workloadToPodRequests(c client.Client) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		logger := log.FromContext(ctx)
		wl, ok := obj.(*schedulingv1alpha1.Workload)
		if !ok {
			return nil
		}
		pods := &corev1.PodList{}
		if err := c.List(ctx, pods,
			client.InNamespace(wl.Namespace),
			client.MatchingFields{WorkloadRefIndex: wl.Name},
		); err != nil {
			logger.V(1).Error(err, "failed to list pods referencing Workload",
				"workload", fmt.Sprintf("%s/%s", wl.Namespace, wl.Name))
			return nil
		}
		reqs := make([]reconcile.Request, 0, len(pods.Items))
		for i := range pods.Items {
			p := &pods.Items[i]
			reqs = append(reqs, reconcile.Request{NamespacedName: types.NamespacedName{
				Namespace: p.Namespace,
				Name:      p.Name,
			}})
		}
		return reqs
	}
}
