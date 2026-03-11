// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v2 "github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	"github.com/NVIDIA/KAI-scheduler/pkg/queuecontroller/common"
	"github.com/NVIDIA/KAI-scheduler/pkg/queuecontroller/controllers/childqueues_updater"
	"github.com/NVIDIA/KAI-scheduler/pkg/queuecontroller/controllers/resource_updater"
	"github.com/NVIDIA/KAI-scheduler/pkg/queuecontroller/metrics"
)

// QueueReconciler reconciles a Queue object
type QueueReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	resourceUpdater    resource_updater.ResourceUpdater
	childQueuesUpdater childqueues_updater.ChildQueuesUpdater
}

//+kubebuilder:rbac:groups=scheduling.run.ai,resources=queues,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=scheduling.run.ai,resources=queues/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=scheduling.run.ai,resources=queues/finalizers,verbs=update

//+kubebuilder:rbac:groups=scheduling.run.ai,resources=podgroups,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Queue object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.13.0/pkg/reconcile
func (r *QueueReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	logger.V(1).Info("Reconcile for queue", "queue name", req.Name)

	queue := &v2.Queue{}
	err := r.Get(ctx, req.NamespacedName, queue)
	if err != nil {
		ignoreNotFoundErr := client.IgnoreNotFound(err)
		if ignoreNotFoundErr == nil {
			// If the queue is not found, reset its metrics
			metrics.ResetQueueMetrics(req.Name)
		}
		return ctrl.Result{}, ignoreNotFoundErr
	}
	originalQueue := queue.DeepCopy()

	err = r.resourceUpdater.UpdateQueue(ctx, queue)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update queue resources: %v", err)
	}

	err = r.childQueuesUpdater.UpdateQueue(ctx, queue)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update child queues: %v", err)
	}

	err = r.Client.Status().Patch(ctx, queue, client.MergeFrom(originalQueue))
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to patch status for queue %s, error: %v", queue.Name, err)
	}

	metrics.SetQueueMetrics(queue)

	return ctrl.Result{}, err
}

// SetupWithManager sets up the controller with the Manager.
func (r *QueueReconciler) SetupWithManager(mgr ctrl.Manager, skipNameValidation bool) error {
	err := mgr.GetFieldIndexer().IndexField(context.Background(), &v2.Queue{}, common.ParentQueueIndexName,
		indexQueueByParent)
	if err != nil {
		return fmt.Errorf("failed to setup queue parent indexer: %w", err)
	}

	err = mgr.GetFieldIndexer().IndexField(context.Background(), &v2alpha2.PodGroup{}, common.PodGroupQueueIndexName,
		indexPodGroupByQueue)
	if err != nil {
		return fmt.Errorf("failed to setup podgroup queue indexer: %w", err)
	}

	r.resourceUpdater = resource_updater.ResourceUpdater{
		Client: r.Client,
	}
	r.childQueuesUpdater = childqueues_updater.ChildQueuesUpdater{
		Client: r.Client,
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&v2.Queue{}).
		Watches(&v2.Queue{},
			handler.EnqueueRequestsFromMapFunc(enqueueQueue)).
		Watches(&v2alpha2.PodGroup{},
			handler.EnqueueRequestsFromMapFunc(enqueuePodGroup)).
		WithOptions(
			controller.Options{
				SkipNameValidation: &skipNameValidation,
			}).
		Complete(r)
}

func indexQueueByParent(object client.Object) []string {
	queue := object.(*v2.Queue)
	if queue.Spec.ParentQueue == "" {
		return []string{}
	}
	return []string{queue.Spec.ParentQueue}
}

func indexPodGroupByQueue(object client.Object) []string {
	pg := object.(*v2alpha2.PodGroup)
	if pg.Spec.Queue == "" {
		return []string{}
	}
	return []string{pg.Spec.Queue}
}

func enqueueQueue(_ context.Context, q client.Object) []reconcile.Request {
	queue, ok := q.(*v2.Queue)
	if !ok {
		return []reconcile.Request{}
	}

	if queue.Spec.ParentQueue == "" {
		return []reconcile.Request{}
	}

	return []reconcile.Request{
		{
			NamespacedName: types.NamespacedName{Name: queue.Spec.ParentQueue},
		},
	}
}

func enqueuePodGroup(_ context.Context, pg client.Object) []reconcile.Request {
	podGroup, ok := pg.(*v2alpha2.PodGroup)

	if !ok {
		return []reconcile.Request{}
	}

	if podGroup.Spec.Queue == "" {
		return []reconcile.Request{}
	}

	return []reconcile.Request{
		{
			NamespacedName: types.NamespacedName{Name: podGroup.Spec.Queue},
		},
	}
}
