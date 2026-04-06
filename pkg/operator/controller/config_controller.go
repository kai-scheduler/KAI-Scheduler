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

	admissionv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"

	nvidiav1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"

	"github.com/kai-scheduler/KAI-scheduler/pkg/operator/controller/status_reconciler"
	"github.com/kai-scheduler/KAI-scheduler/pkg/operator/operands"
	"github.com/kai-scheduler/KAI-scheduler/pkg/operator/operands/admission"
	"github.com/kai-scheduler/KAI-scheduler/pkg/operator/operands/binder"
	"github.com/kai-scheduler/KAI-scheduler/pkg/operator/operands/common"
	"github.com/kai-scheduler/KAI-scheduler/pkg/operator/operands/deployable"
	"github.com/kai-scheduler/KAI-scheduler/pkg/operator/operands/known_types"
	"github.com/kai-scheduler/KAI-scheduler/pkg/operator/operands/node_scale_adjuster"
	"github.com/kai-scheduler/KAI-scheduler/pkg/operator/operands/pod_group_controller"
	"github.com/kai-scheduler/KAI-scheduler/pkg/operator/operands/pod_grouper"
	"github.com/kai-scheduler/KAI-scheduler/pkg/operator/operands/prometheus"
	"github.com/kai-scheduler/KAI-scheduler/pkg/operator/operands/queue_controller"
	"github.com/kai-scheduler/KAI-scheduler/pkg/operator/operands/scheduler"
)

var ConfigReconcilerOperands = []operands.Operand{
	&pod_grouper.PodGrouper{},
	&binder.Binder{},
	&queue_controller.QueueController{},
	&pod_group_controller.PodGroupController{},
	&node_scale_adjuster.NodeScaleAdjuster{},
	&admission.Admission{},
	&prometheus.Prometheus{},
	&scheduler.SchedulerForConfig{},
}

// ConfigReconciler reconciles a Config object
type ConfigReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	deployable *deployable.DeployableOperands
	*status_reconciler.StatusReconciler
}

func (r *ConfigReconciler) SetOperands(ops []operands.Operand) {
	r.deployable = deployable.New(ops, known_types.KAIConfigRegisteredCollectible)
}

// +kubebuilder:rbac:groups=kai.scheduler,resources=configs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kai.scheduler,resources=configs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kai.scheduler,resources=configs/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments;daemonsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=services;secrets;serviceaccounts;configmaps;persistentvolumeclaims;pods;endpoints,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="admissionregistration.k8s.io",resources=mutatingwebhookconfigurations;validatingwebhookconfigurations,resourceNames=kai-podgroup-validation-v2alpha2;kai-queue-validation-v2;mutating-kai-admission;validating-kai-admission,verbs=delete;update;patch
// +kubebuilder:rbac:groups="admissionregistration.k8s.io",resources=mutatingwebhookconfigurations;validatingwebhookconfigurations,verbs=get;list;watch;create
// +kubebuilder:rbac:groups="apiextensions.k8s.io",resources=customresourcedefinitions,resourceNames=queues.scheduling.run.ai,verbs=delete;update;patch
// +kubebuilder:rbac:groups="apiextensions.k8s.io",resources=customresourcedefinitions,verbs=get;list;watch;create
// +kubebuilder:rbac:groups="nvidia.com",resources=clusterpolicies,verbs=get;list;watch
// +kubebuilder:rbac:groups="monitoring.coreos.com",resources=prometheuses;servicemonitors,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="scheduling.run.ai",resources=queues,verbs=get;list;watch
// +kubebuilder:rbac:groups="autoscaling.k8s.io",resources=verticalpodautoscalers,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.14.4/pkg/reconcile
func (r *ConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (response ctrl.Result, err error) {
	logger := log.FromContext(ctx)

	logger.Info("Received an event to reconcile: ", "req", req)
	if req.Name != known_types.SingletonInstanceName {
		logger.Info("Config is not in the singleton name, ignoring it.", "Name", req.Name)
		return ctrl.Result{}, nil
	}

	kaiConfig := &kaiv1.Config{}
	if err = r.Client.Get(ctx, req.NamespacedName, kaiConfig); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	defer func() {
		reconcileStatusErr := r.ReconcileStatus(
			ctx, &status_reconciler.KAIConfigWithStatusWrapper{Config: kaiConfig},
		)
		if reconcileStatusErr != nil {
			if err != nil {
				err = errors.New(err.Error() + reconcileStatusErr.Error())
			} else {
				err = reconcileStatusErr
			}
		}
	}()
	kaiConfig.Spec.SetDefaultsWhereNeeded()

	if err = r.UpdateStartReconcileStatus(
		ctx, &status_reconciler.KAIConfigWithStatusWrapper{Config: kaiConfig},
	); err != nil {
		return ctrl.Result{}, err
	}

	if err = r.deployable.Deploy(ctx, r.Client, kaiConfig, kaiConfig); err != nil {
		return ctrl.Result{}, err
	}

	// Monitor all operands
	if err = r.deployable.Monitor(ctx, r.Client, kaiConfig); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	for _, collectable := range known_types.KAIConfigRegisteredCollectible {
		if err := collectable.InitWithManager(context.Background(), mgr); err != nil {
			return err
		}
		known_types.MarkInitiatedWithManager(collectable)
	}

	r.deployable.RegisterFieldsInheritFromClusterObjects(&admissionv1.ValidatingWebhookConfiguration{},
		known_types.ValidatingWebhookConfigurationFieldInherit)
	r.deployable.RegisterFieldsInheritFromClusterObjects(&admissionv1.MutatingWebhookConfiguration{},
		known_types.MutatingWebhookConfigurationFieldInherit)
	r.deployable.RegisterFieldsInheritFromClusterObjects(&vpav1.VerticalPodAutoscaler{},
		known_types.VPAFieldInherit)
	r.StatusReconciler = status_reconciler.New(r.Client, r.deployable)

	builder := ctrl.NewControllerManagedBy(mgr).
		For(&kaiv1.Config{})

	if checkForClusterPolicy(mgr) {
		builder = builder.Watches(&nvidiav1.ClusterPolicy{}, handler.EnqueueRequestsFromMapFunc(enqueueWatched))
	}

	for _, collectable := range known_types.KAIConfigRegisteredCollectible {
		builder = collectable.InitWithBuilder(builder)
	}
	return builder.Complete(r)
}

func enqueueWatched(_ context.Context, _ client.Object) []ctrl.Request {
	return []ctrl.Request{
		{
			NamespacedName: types.NamespacedName{
				Name:      known_types.SingletonInstanceName,
				Namespace: "",
			},
		},
	}
}

func checkForClusterPolicy(mgr ctrl.Manager) bool {
	logger := log.FromContext(context.Background())
	tempClient, err := client.New(mgr.GetConfig(), client.Options{Scheme: mgr.GetScheme()})
	if err != nil {
		logger.Info("Failed to create temporary client to check for cluster policy", "error", err)
		return false
	}

	clusterPolicyExists, err := common.CheckCRDsAvailable(
		context.Background(), tempClient, "clusterpolicies.nvidia.com",
	)
	if err != nil {
		logger.Info("Failed to check for ClusterPolicy CRD existence", "error", err)
		return false
	}

	return clusterPolicyExists
}
