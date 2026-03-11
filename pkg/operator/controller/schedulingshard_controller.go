/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"errors"

	"golang.org/x/exp/slices"
	admissionv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/runtime"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
	"github.com/kai-scheduler/KAI-scheduler/pkg/operator/controller/status_reconciler"
	"github.com/kai-scheduler/KAI-scheduler/pkg/operator/operands"
	"github.com/kai-scheduler/KAI-scheduler/pkg/operator/operands/deployable"
	"github.com/kai-scheduler/KAI-scheduler/pkg/operator/operands/known_types"
	"github.com/kai-scheduler/KAI-scheduler/pkg/operator/operands/scheduler"
)

func OperandsForShard(shard *kaiv1.SchedulingShard) []operands.Operand {
	return []operands.Operand{
		scheduler.NewSchedulerForShard(shard),
	}
}

// SchedulingShardReconciler reconciles a SchedulingShard object
type SchedulingShardReconciler struct {
	client.Client
	Scheme                *runtime.Scheme
	shardOperandsForShard func(*kaiv1.SchedulingShard) []operands.Operand
	deployablePerShard    map[string]*deployable.DeployableOperands
	statusReconcilers     map[string]*status_reconciler.StatusReconciler
}

func NewSchedulingShardReconciler(client client.Client, scheme *runtime.Scheme) *SchedulingShardReconciler {
	return &SchedulingShardReconciler{
		Client:             client,
		Scheme:             scheme,
		deployablePerShard: map[string]*deployable.DeployableOperands{},
		statusReconcilers:  map[string]*status_reconciler.StatusReconciler{},
	}
}

func (r *SchedulingShardReconciler) SetOperands(shardOperandsForShard func(*kaiv1.SchedulingShard) []operands.Operand) {
	r.shardOperandsForShard = shardOperandsForShard
}

// +kubebuilder:rbac:groups=kai.scheduler,resources=schedulingshards,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kai.scheduler,resources=schedulingshards/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kai.scheduler,resources=schedulingshards/finalizers,verbs=update

// These permissions are granted through the KAI Config reconciler
// kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// kubebuilder:rbac:groups="",resources=serviceaccounts;configmaps;services,verbs=get;list;watch;create;update;patch;delete

func (r *SchedulingShardReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, err error) {
	logger := log.FromContext(ctx)
	logger.Info("Received an event to reconcile: ", "req", req)

	shard := &kaiv1.SchedulingShard{}
	if err := r.Get(ctx, req.NamespacedName, shard); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	shard.Spec.SetDefaultsWhereNeeded()

	r.deployablePerShard[shard.Name] = deployable.New(
		r.shardOperandsForShard(shard),
		known_types.SchedulingShardRegisteredCollectable,
	)
	r.deployablePerShard[shard.Name].RegisterFieldsInheritFromClusterObjects(&admissionv1.ValidatingWebhookConfiguration{},
		known_types.ValidatingWebhookConfigurationFieldInherit)
	r.deployablePerShard[shard.Name].RegisterFieldsInheritFromClusterObjects(&admissionv1.MutatingWebhookConfiguration{},
		known_types.MutatingWebhookConfigurationFieldInherit)
	r.deployablePerShard[shard.Name].RegisterFieldsInheritFromClusterObjects(&vpav1.VerticalPodAutoscaler{},
		known_types.VPAFieldInherit)
	r.statusReconcilers[shard.Name] = status_reconciler.New(r.Client, r.deployablePerShard[shard.Name])

	deployable := r.deployablePerShard[shard.Name]
	statusReconciler := r.statusReconcilers[shard.Name]

	defer func() {
		reconcileStatusErr := statusReconciler.ReconcileStatus(
			ctx, &status_reconciler.SchedulingShardWithStatusWrapper{SchedulingShard: shard},
		)
		if reconcileStatusErr != nil {
			if err != nil {
				err = errors.New(err.Error() + reconcileStatusErr.Error())
			} else {
				err = reconcileStatusErr
			}
		}
	}()

	kaiConfig := &kaiv1.Config{}
	if err := r.Get(ctx, client.ObjectKey{Name: known_types.SingletonInstanceName}, kaiConfig); err != nil {
		logger.Info("Failed to get the singleton KAI Config instance")
		return ctrl.Result{}, err
	}
	kaiConfig.Spec.SetDefaultsWhereNeeded()
	kaiConfig.Name = shard.Name

	if err = statusReconciler.UpdateStartReconcileStatus(
		ctx, &status_reconciler.SchedulingShardWithStatusWrapper{SchedulingShard: shard},
	); err != nil {
		return ctrl.Result{}, err
	}

	if err := deployable.Deploy(ctx, r.Client, kaiConfig, shard); err != nil {
		return ctrl.Result{}, err
	}
	if shard.DeletionTimestamp != nil {
		logger.Info("SchedulingShard is being deleted", "Name", shard.Name)
		defer func() {
			delete(r.deployablePerShard, shard.Name)
			delete(r.statusReconcilers, shard.Name)
		}()
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *SchedulingShardReconciler) SetupWithManager(mgr ctrl.Manager) error {
	for _, collectable := range known_types.SchedulingShardRegisteredCollectable {
		if slices.Contains(known_types.InitiatedCollectables, collectable) {
			continue
		}
		if err := collectable.InitWithManager(context.Background(), mgr); err != nil {
			return err
		}
		known_types.MarkInitiatedWithManager(collectable)
	}

	r.deployablePerShard = map[string]*deployable.DeployableOperands{}
	r.statusReconcilers = map[string]*status_reconciler.StatusReconciler{}

	builder := ctrl.NewControllerManagedBy(mgr).
		For(&kaiv1.SchedulingShard{}).
		Watches(&kaiv1.Config{}, handler.EnqueueRequestsFromMapFunc(r.requestAllSchedulingShards))

	for _, collectable := range known_types.SchedulingShardRegisteredCollectable {
		builder = collectable.InitWithBuilder(builder)
	}
	return builder.Complete(r)
}

// requestAllSchedulingShards returns all SchedulingShards making each change to the kai config reconcile every scheduling shard
func (r *SchedulingShardReconciler) requestAllSchedulingShards(_ context.Context, obj client.Object) []reconcile.Request {
	ctx := context.Background()
	logger := log.FromContext(ctx)

	shedulingShards := &kaiv1.SchedulingShardList{}
	if err := r.Client.List(ctx, shedulingShards); err != nil {
		logger.Error(err, "failed to list SchedulingShards")
		return nil
	}

	requests := []reconcile.Request{}
	for _, si := range shedulingShards.Items {
		requests = append(
			requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&si)})
	}

	return requests
}
