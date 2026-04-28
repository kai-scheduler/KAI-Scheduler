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

const workloadRefIndex = "spec.workloadRef.name"

func registerWorkloadWatch(mgr ctrl.Manager, b *builder.Builder) error {
	err := mgr.GetFieldIndexer().IndexField(context.Background(), &corev1.Pod{}, workloadRefIndex, podsByWorkloadRef)
	if err != nil {
		return fmt.Errorf("failed to index pods by workload ref name: %w", err)
	}

	b.Watches(
		&schedulingv1alpha1.Workload{},
		handler.EnqueueRequestsFromMapFunc(workloadToPodRequests(mgr.GetClient())),
	)
	return nil
}

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
			client.MatchingFields{workloadRefIndex: wl.Name},
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
