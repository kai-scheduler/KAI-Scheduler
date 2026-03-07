// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package pluginshub

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kai-scheduler/kai-scheduler/pkg/podgrouper/podgrouper/plugins/aml"
	"github.com/kai-scheduler/kai-scheduler/pkg/podgrouper/podgrouper/plugins/cronjobs"
	"github.com/kai-scheduler/kai-scheduler/pkg/podgrouper/podgrouper/plugins/defaultgrouper"
	"github.com/kai-scheduler/kai-scheduler/pkg/podgrouper/podgrouper/plugins/deployment"
	"github.com/kai-scheduler/kai-scheduler/pkg/podgrouper/podgrouper/plugins/grouper"
	"github.com/kai-scheduler/kai-scheduler/pkg/podgrouper/podgrouper/plugins/grove"
	"github.com/kai-scheduler/kai-scheduler/pkg/podgrouper/podgrouper/plugins/job"
	jobsetplugin "github.com/kai-scheduler/kai-scheduler/pkg/podgrouper/podgrouper/plugins/jobset"
	"github.com/kai-scheduler/kai-scheduler/pkg/podgrouper/podgrouper/plugins/knative"
	"github.com/kai-scheduler/kai-scheduler/pkg/podgrouper/podgrouper/plugins/kubeflow"
	jaxplugin "github.com/kai-scheduler/kai-scheduler/pkg/podgrouper/podgrouper/plugins/kubeflow/jax"
	"github.com/kai-scheduler/kai-scheduler/pkg/podgrouper/podgrouper/plugins/kubeflow/mpi"
	notebookplugin "github.com/kai-scheduler/kai-scheduler/pkg/podgrouper/podgrouper/plugins/kubeflow/notebook"
	pytorchplugin "github.com/kai-scheduler/kai-scheduler/pkg/podgrouper/podgrouper/plugins/kubeflow/pytorch"
	tensorflowlugin "github.com/kai-scheduler/kai-scheduler/pkg/podgrouper/podgrouper/plugins/kubeflow/tensorflow"
	xgboostplugin "github.com/kai-scheduler/kai-scheduler/pkg/podgrouper/podgrouper/plugins/kubeflow/xgboost"
	leader_worker_set "github.com/kai-scheduler/kai-scheduler/pkg/podgrouper/podgrouper/plugins/leaderworkerset"
	"github.com/kai-scheduler/kai-scheduler/pkg/podgrouper/podgrouper/plugins/podjob"
	"github.com/kai-scheduler/kai-scheduler/pkg/podgrouper/podgrouper/plugins/ray"
	"github.com/kai-scheduler/kai-scheduler/pkg/podgrouper/podgrouper/plugins/runaijob"
	"github.com/kai-scheduler/kai-scheduler/pkg/podgrouper/podgrouper/plugins/skiptopowner"
	"github.com/kai-scheduler/kai-scheduler/pkg/podgrouper/podgrouper/plugins/spark"
	"github.com/kai-scheduler/kai-scheduler/pkg/podgrouper/podgrouper/plugins/spotrequest"
)

const (
	apiGroupArgo                     = "argoproj.io"
	apiGroupRunai                    = "run.ai"
	apiGroupTrainer                  = "trainer.kubeflow.org"
	kindTrainingWorkload             = "TrainingWorkload"
	kindInteractiveWorkload          = "InteractiveWorkload"
	kindDistributedWorkload          = "DistributedWorkload"
	kindInferenceWorkload            = "InferenceWorkload"
	kindDistributedInferenceWorkload = "DistributedInferenceWorkload"
)

// +kubebuilder:rbac:groups=apps,resources=replicasets;statefulsets,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=replicasets/finalizers;statefulsets/finalizers,verbs=patch;update;create
// +kubebuilder:rbac:groups=machinelearning.seldon.io,resources=seldondeployments,verbs=get;list;watch
// +kubebuilder:rbac:groups=machinelearning.seldon.io,resources=seldondeployments/finalizers,verbs=patch;update;create
// +kubebuilder:rbac:groups=kubevirt.io,resources=virtualmachines;virtualmachineinstances,verbs=get;list;watch
// +kubebuilder:rbac:groups=kubevirt.io,resources=virtualmachines/finalizers;virtualmachineinstances/finalizers,verbs=patch;update;create
// +kubebuilder:rbac:groups=workspace.devfile.io,resources=devworkspaces,verbs=get;list;watch
// +kubebuilder:rbac:groups=workspace.devfile.io,resources=devworkspaces/finalizers,verbs=patch;update;create
// +kubebuilder:rbac:groups=argoproj.io,resources=workflows,verbs=get;list;watch
// +kubebuilder:rbac:groups=argoproj.io,resources=workflows/finalizers,verbs=patch;update;create
// +kubebuilder:rbac:groups=tekton.dev,resources=pipelineruns;taskruns,verbs=get;list;watch
// +kubebuilder:rbac:groups=tekton.dev,resources=pipelineruns/finalizers;taskruns/finalizers,verbs=patch;update;create
// +kubebuilder:rbac:groups=run.ai,resources=trainingworkloads;interactiveworkloads;distributedworkloads;inferenceworkloads;distributedinferenceworkloads,verbs=get;list;watch
// +kubebuilder:rbac:groups=trainer.kubeflow.org,resources=trainjobs,verbs=get;list;watch
// +kubebuilder:rbac:groups=trainer.kubeflow.org,resources=trainjobs/finalizers,verbs=patch;update;create

type DefaultPluginsHub struct {
	defaultPlugin *defaultgrouper.DefaultGrouper
	customPlugins map[metav1.GroupVersionKind]grouper.Grouper
}

type PluginsHub interface {
	GetPodGrouperPlugin(gvk metav1.GroupVersionKind) grouper.Grouper
}

func (ph *DefaultPluginsHub) GetPodGrouperPlugin(gvk metav1.GroupVersionKind) grouper.Grouper {
	if f, found := ph.customPlugins[gvk]; found {
		return f
	}

	// search using wildcard version
	gvk.Version = "*"
	if f, found := ph.customPlugins[gvk]; found {
		return f
	}
	return ph.defaultPlugin
}

func (ph *DefaultPluginsHub) GetDefaultPlugin() grouper.Grouper {
	return ph.defaultPlugin
}

func (ph *DefaultPluginsHub) HasMatchingPlugin(gvk metav1.GroupVersionKind) bool {
	if _, found := ph.customPlugins[gvk]; found {
		return true
	}

	// search using wildcard version
	gvk.Version = "*"
	if _, found := ph.customPlugins[gvk]; found {
		return true
	}
	return false
}

func NewDefaultPluginsHub(kubeClient client.Client, searchForLegacyPodGroups,
	gangScheduleKnative bool, queueLabelKey, nodePoolLabelKey string,
	defaultConfigPerTypeConfigMapName, defaultConfigPerTypeConfigMapNamespace string) *DefaultPluginsHub {
	defaultGrouper := defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, kubeClient)
	defaultGrouper.SetDefaultConfigPerTypeConfigMapParams(defaultConfigPerTypeConfigMapName, defaultConfigPerTypeConfigMapNamespace)

	kubeFlowDistributedGrouper := kubeflow.NewKubeflowDistributedGrouper(defaultGrouper)
	mpiGrouper := mpi.NewMpiGrouper(kubeClient, kubeFlowDistributedGrouper)

	rayGrouper := ray.NewRayGrouper(kubeClient, defaultGrouper)
	rayClusterGrouper := ray.NewRayClusterGrouper(rayGrouper)
	rayJobGrouper := ray.NewRayJobGrouper(rayGrouper)
	rayServiceGrouper := ray.NewRayServiceGrouper(rayGrouper)

	sparkGrouper := spark.NewSparkGrouper(defaultGrouper)
	podJobGrouper := podjob.NewPodJobGrouper(defaultGrouper, sparkGrouper)

	groveGrouper := grove.NewGroveGrouper(kubeClient, defaultGrouper)
	jobSetGrouper := jobsetplugin.NewJobSetGrouper(defaultGrouper)

	table := map[metav1.GroupVersionKind]grouper.Grouper{
		{
			Group:   "apps",
			Version: "v1",
			Kind:    "Deployment",
		}: deployment.NewDeploymentGrouper(defaultGrouper),
		{
			Group:   "machinelearning.seldon.io",
			Version: "v1alpha2",
			Kind:    "SeldonDeployment",
		}: defaultGrouper,
		{
			Group:   "machinelearning.seldon.io",
			Version: "v1",
			Kind:    "SeldonDeployment",
		}: defaultGrouper,
		{
			Group:   "kubevirt.io",
			Version: "v1",
			Kind:    "VirtualMachineInstance",
		}: defaultGrouper,
		{
			Group:   "kubeflow.org",
			Version: "v1",
			Kind:    "TFJob",
		}: tensorflowlugin.NewTensorFlowGrouper(kubeFlowDistributedGrouper),
		{
			Group:   "kubeflow.org",
			Version: "v1",
			Kind:    "PyTorchJob",
		}: pytorchplugin.NewPyTorchGrouper(kubeFlowDistributedGrouper),
		{
			Group:   "kubeflow.org",
			Version: "v1",
			Kind:    "XGBoostJob",
		}: xgboostplugin.NewXGBoostGrouper(kubeFlowDistributedGrouper),
		{
			Group:   "kubeflow.org",
			Version: "v1",
			Kind:    "JAXJob",
		}: jaxplugin.NewJaxGrouper(kubeFlowDistributedGrouper),
		{
			Group:   "kubeflow.org",
			Version: "v1",
			Kind:    "MPIJob",
		}: mpiGrouper,
		{
			Group:   "kubeflow.org",
			Version: "v2beta1",
			Kind:    "MPIJob",
		}: mpiGrouper,
		{
			Group:   "kubeflow.org",
			Version: "v1beta1",
			Kind:    "Notebook",
		}: notebookplugin.NewNotebookGrouper(defaultGrouper),
		{
			Group:   "batch",
			Version: "v1",
			Kind:    "Job",
		}: job.NewK8sJobGrouper(kubeClient, defaultGrouper, searchForLegacyPodGroups),
		{
			Group:   "apps",
			Version: "v1",
			Kind:    "StatefulSet",
		}: defaultGrouper,
		{
			Group:   "apps",
			Version: "v1",
			Kind:    "ReplicaSet",
		}: defaultGrouper,
		{
			Group:   "run.ai",
			Version: "v1",
			Kind:    "RunaiJob",
		}: runaijob.NewRunaiJobGrouper(kubeClient, defaultGrouper, searchForLegacyPodGroups),
		{
			Group:   "",
			Version: "v1",
			Kind:    "Pod",
		}: podJobGrouper,
		{
			Group:   "amlarc.azureml.com",
			Version: "v1alpha1",
			Kind:    "AmlJob",
		}: aml.NewAmlGrouper(defaultGrouper),
		{
			Group:   "serving.knative.dev",
			Version: "v1",
			Kind:    "Service",
		}: knative.NewKnativeGrouper(kubeClient, defaultGrouper, gangScheduleKnative),
		{
			Group:   "batch",
			Version: "v1",
			Kind:    "CronJob",
		}: cronjobs.NewCronJobGrouper(kubeClient, defaultGrouper),
		{
			Group:   "workspace.devfile.io",
			Version: "v1alpha2",
			Kind:    "DevWorkspace",
		}: defaultGrouper,
		{
			Group:   "ray.io",
			Version: "v1alpha1",
			Kind:    "RayCluster",
		}: rayClusterGrouper,
		{
			Group:   "ray.io",
			Version: "v1alpha1",
			Kind:    "RayJob",
		}: rayJobGrouper,
		{
			Group:   "ray.io",
			Version: "v1alpha1",
			Kind:    "RayService",
		}: rayServiceGrouper,
		{
			Group:   "ray.io",
			Version: "v1",
			Kind:    "RayCluster",
		}: rayClusterGrouper,
		{
			Group:   "ray.io",
			Version: "v1",
			Kind:    "RayJob",
		}: rayJobGrouper,
		{
			Group:   "ray.io",
			Version: "v1",
			Kind:    "RayService",
		}: rayServiceGrouper,
		{
			Group:   "kubeflow.org",
			Version: "v1alpha1",
			Kind:    "ScheduledWorkflow",
		}: defaultGrouper,
		{
			Group:   "tekton.dev",
			Version: "v1",
			Kind:    "PipelineRun",
		}: defaultGrouper,
		{
			Group:   "tekton.dev",
			Version: "v1",
			Kind:    "TaskRun",
		}: defaultGrouper,
		{
			Group:   "egx.nvidia.io",
			Version: "v1",
			Kind:    "SPOTRequest",
		}: spotrequest.NewSpotRequestGrouper(defaultGrouper),
		{
			Group:   "leaderworkerset.x-k8s.io",
			Version: "v1",
			Kind:    "LeaderWorkerSet",
		}: leader_worker_set.NewLwsGrouper(defaultGrouper),
		{
			Group:   "jobset.x-k8s.io",
			Version: "v1alpha2",
			Kind:    "JobSet",
		}: jobSetGrouper,
		{
			Group:   "grove.io",
			Version: "v1alpha1",
			Kind:    "PodGangSet",
		}: groveGrouper,
		{
			Group:   "grove.io",
			Version: "v1alpha1",
			Kind:    "PodCliqueSet",
		}: groveGrouper,
	}

	skipTopOwnerGrouper := skiptopowner.NewSkipTopOwnerGrouper(kubeClient, defaultGrouper, table)
	table[metav1.GroupVersionKind{
		Group:   apiGroupArgo,
		Version: "v1alpha1",
		Kind:    "Workflow",
	}] = skipTopOwnerGrouper

	for _, kind := range []string{
		kindInferenceWorkload,
		kindTrainingWorkload,
		kindDistributedWorkload,
		kindInteractiveWorkload,
		kindDistributedInferenceWorkload,
	} {
		table[metav1.GroupVersionKind{
			Group:   apiGroupRunai,
			Version: "*",
			Kind:    kind,
		}] = skipTopOwnerGrouper
	}

	// TrainJob (Kubeflow Trainer v2) wraps a JobSet; skip TrainJob and delegate to JobSet grouper
	table[metav1.GroupVersionKind{
		Group:   apiGroupTrainer,
		Version: "v1alpha1",
		Kind:    "TrainJob",
	}] = skipTopOwnerGrouper

	// Dynamo uses Grove Grouper and needs to propagate metadata from DynamoGraphDeployment to PodGang and PodClique.
	table[metav1.GroupVersionKind{
		Group:   "nvidia.com",
		Version: "v1alpha1",
		Kind:    "DynamoGraphDeployment",
	}] = skipTopOwnerGrouper

	return &DefaultPluginsHub{
		defaultPlugin: defaultGrouper,
		customPlugins: table,
	}
}
