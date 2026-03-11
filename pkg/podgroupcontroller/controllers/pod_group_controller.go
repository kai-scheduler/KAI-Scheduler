// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"context"
	"fmt"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgroupcontroller/controllers/cluster_relations"
)

const (
	rateLimiterBaseDelay = time.Second
	rateLimiterMaxDelay  = time.Minute
)

type Configs struct {
	MaxConcurrentReconciles int
}

// PodGroupReconciler reconciles a Pod object
type PodGroupReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	config Configs
}

// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=pods/status,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch
// +kubebuilder:rbac:groups="scheduling.k8s.io",resources=priorityclasses,verbs=get;list;watch
// +kubebuilder:rbac:groups="scheduling.run.ai",resources=podgroups,verbs=get;list;watch
// +kubebuilder:rbac:groups="scheduling.run.ai",resources=podgroups/status,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups="resource.k8s.io",resources=resourceclaims,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.13.0/pkg/reconcile
func (r *PodGroupReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	logger.V(3).Info("Reconciling pod group")
	podGroup, err := r.getPodGroupObject(ctx, req)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info(fmt.Sprintf("PodGroup %v not found, it might have been deleted.", req))
			return ctrl.Result{}, nil
		} else {
			logger.Error(err, fmt.Sprintf("Failed to get pod group for request %s/%s", req.Namespace, req.Name))
			return ctrl.Result{}, err
		}
	}

	return r.handlePodGroupStatus(ctx, podGroup)
}

// SetupWithManager sets up the controller with the Manager.
func (r *PodGroupReconciler) SetupWithManager(mgr ctrl.Manager, configs Configs, skipNameValidation bool) error {
	r.config = configs

	err := mgr.GetFieldIndexer().IndexField(
		context.Background(), &v1.Pod{}, cluster_relations.PodGroupToPodsIndexer,
		cluster_relations.PodGroupNameIndexerFunc)
	if err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&v2alpha2.PodGroup{}).
		Watches(&v1.Pod{}, handler.EnqueueRequestsFromMapFunc(mapPodEventToPodGroup)).
		WithOptions(
			controller.Options{
				MaxConcurrentReconciles: r.config.MaxConcurrentReconciles,
				RateLimiter: workqueue.NewTypedItemExponentialFailureRateLimiter[ctrl.Request](
					rateLimiterBaseDelay, rateLimiterMaxDelay),
				SkipNameValidation: &skipNameValidation,
			}).
		Complete(r)
}

func (r *PodGroupReconciler) getPodGroupObject(ctx context.Context, req ctrl.Request) (
	*v2alpha2.PodGroup, error) {
	podGroup := v2alpha2.PodGroup{}
	err := r.Client.Get(ctx, req.NamespacedName, &podGroup)
	if err != nil {
		return nil, err
	}
	return &podGroup, nil
}

func mapPodEventToPodGroup(ctx context.Context, p client.Object) []reconcile.Request {
	logger := log.FromContext(ctx)

	logger.V(4).Info("Mapping pod to pod group")
	mappedPodGroupName, extractionError := cluster_relations.GetPodGroupName(p)

	if extractionError != nil {
		logger.V(4).Info(fmt.Sprintf("cann't map pod to pod-group: %v", extractionError))
		return []reconcile.Request{}
	}

	return []reconcile.Request{
		{
			NamespacedName: types.NamespacedName{Name: mappedPodGroupName, Namespace: p.GetNamespace()},
		},
	}
}
