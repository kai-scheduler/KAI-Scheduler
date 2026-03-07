// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package data_lister

import (
	"fmt"

	v1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	v14 "k8s.io/api/scheduling/v1"
	storage "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/labels"
	featureutil "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/informers"
	listv1 "k8s.io/client-go/listers/core/v1"
	resourcev1 "k8s.io/client-go/listers/resource/v1"
	schedv1 "k8s.io/client-go/listers/scheduling/v1"
	v12 "k8s.io/client-go/listers/storage/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/kubernetes/pkg/features"

	kubeAiSchedulerInfo "github.com/NVIDIA/KAI-scheduler/pkg/apis/client/informers/externalversions"
	scheudlinglistv1alpha2 "github.com/NVIDIA/KAI-scheduler/pkg/apis/client/listers/scheduling/v1alpha2"
	schedlistv2 "github.com/NVIDIA/KAI-scheduler/pkg/apis/client/listers/scheduling/v2"
	schedlist2alpha2 "github.com/NVIDIA/KAI-scheduler/pkg/apis/client/listers/scheduling/v2alpha2"
	schedulingv1alpha2 "github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v1alpha2"
	enginev2 "github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v2"
	enginev2alpha2 "github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/api/queue_info"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/cache/usagedb"

	kaiv1alpha1Listers "github.com/NVIDIA/KAI-scheduler/pkg/apis/client/listers/kai/v1alpha1"
	kaiv1alpha1 "github.com/NVIDIA/KAI-scheduler/pkg/apis/kai/v1alpha1"
)

type k8sLister struct {
	podGroupLister schedlist2alpha2.PodGroupLister
	podInformer    cache.SharedIndexInformer
	podLister      listv1.PodLister
	nodeLister     listv1.NodeLister
	queueLister    schedlistv2.QueueLister
	pcLister       schedv1.PriorityClassLister
	cmLister       listv1.ConfigMapLister
	usageLister    *usagedb.UsageLister

	pvcLister             listv1.PersistentVolumeClaimLister
	storageCapacityLister v12.CSIStorageCapacityLister
	storageClassLister    v12.StorageClassLister
	csiDriverLister       v12.CSIDriverLister

	bindRequestLister scheudlinglistv1alpha2.BindRequestLister

	kaiTopologyLister kaiv1alpha1Listers.TopologyLister

	resourceSliceLister resourcev1.ResourceSliceLister
	resourceClaimLister resourcev1.ResourceClaimLister

	partitionSelector labels.Selector
}

// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch

var _ DataLister = &k8sLister{}

func New(
	informerFactory informers.SharedInformerFactory, kubeAiSchedulerInformerFactory kubeAiSchedulerInfo.SharedInformerFactory,
	usageLister *usagedb.UsageLister,
	partitionSelector labels.Selector,
) *k8sLister {
	lister := &k8sLister{
		podGroupLister: kubeAiSchedulerInformerFactory.Scheduling().V2alpha2().PodGroups().Lister(),
		podInformer:    informerFactory.Core().V1().Pods().Informer(),
		podLister:      informerFactory.Core().V1().Pods().Lister(),
		nodeLister:     informerFactory.Core().V1().Nodes().Lister(),
		queueLister:    kubeAiSchedulerInformerFactory.Scheduling().V2().Queues().Lister(),
		pcLister:       informerFactory.Scheduling().V1().PriorityClasses().Lister(),
		cmLister:       informerFactory.Core().V1().ConfigMaps().Lister(),
		usageLister:    usageLister,

		pvcLister:             informerFactory.Core().V1().PersistentVolumeClaims().Lister(),
		storageCapacityLister: informerFactory.Storage().V1().CSIStorageCapacities().Lister(),
		storageClassLister:    informerFactory.Storage().V1().StorageClasses().Lister(),
		csiDriverLister:       informerFactory.Storage().V1().CSIDrivers().Lister(),

		bindRequestLister: kubeAiSchedulerInformerFactory.Scheduling().V1alpha2().BindRequests().Lister(),
		kaiTopologyLister: kubeAiSchedulerInformerFactory.Kai().V1alpha1().Topologies().Lister(),

		partitionSelector: partitionSelector,
	}

	if featureutil.DefaultMutableFeatureGate.Enabled(features.DynamicResourceAllocation) {
		lister.resourceSliceLister = informerFactory.Resource().V1().ResourceSlices().Lister()
		lister.resourceClaimLister = informerFactory.Resource().V1().ResourceClaims().Lister()
	}

	return lister
}

// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch

func (k *k8sLister) ListPods() ([]*v1.Pod, error) {
	return k.podLister.List(labels.Everything())
}

// +kubebuilder:rbac:groups="scheduling.run.ai",resources=podgroups,verbs=get;list;watch

func (k *k8sLister) ListPodGroups() ([]*enginev2alpha2.PodGroup, error) {
	return k.podGroupLister.List(k.partitionSelector)
}

// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch

func (k *k8sLister) ListNodes() ([]*v1.Node, error) {
	return k.nodeLister.List(k.partitionSelector)
}

// +kubebuilder:rbac:groups="scheduling.run.ai",resources=queues,verbs=get;list;watch

func (k *k8sLister) ListQueues() ([]*enginev2.Queue, error) {
	return k.queueLister.List(k.partitionSelector)
}

func (k *k8sLister) ListResourceUsage() (*queue_info.ClusterUsage, error) {
	if k.usageLister == nil {
		return queue_info.NewClusterUsage(), fmt.Errorf("usage lister is not set")
	}

	return k.usageLister.GetResourceUsage()
}

// +kubebuilder:rbac:groups="scheduling.k8s.io",resources=priorityclasses,verbs=get;list;watch

func (k *k8sLister) ListPriorityClasses() ([]*v14.PriorityClass, error) {
	return k.pcLister.List(labels.Everything())
}

func (k *k8sLister) GetPriorityClassByName(name string) (*v14.PriorityClass, error) {
	return k.pcLister.Get(name)
}

func (k *k8sLister) ListPodByIndex(index, value string) ([]interface{}, error) {
	return k.podInformer.GetIndexer().ByIndex(index, value)
}

// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims;persistentvolumes,verbs=get;list;watch

func (k *k8sLister) ListPersistentVolumeClaims() ([]*v1.PersistentVolumeClaim, error) {
	return k.pvcLister.List(labels.Everything())
}

// +kubebuilder:rbac:groups="storage.k8s.io",resources=csistoragecapacities,verbs=get;list;watch

func (k *k8sLister) ListCSIStorageCapacities() ([]*storage.CSIStorageCapacity, error) {
	return k.storageCapacityLister.List(labels.Everything())
}

// +kubebuilder:rbac:groups="storage.k8s.io",resources=storageclasses,verbs=get;list;watch

func (k *k8sLister) ListStorageClasses() ([]*storage.StorageClass, error) {
	return k.storageClassLister.List(labels.Everything())
}

// +kubebuilder:rbac:groups="storage.k8s.io",resources=csidrivers;csinodes,verbs=get;list;watch

func (k *k8sLister) ListCSIDrivers() ([]*storage.CSIDriver, error) {
	return k.csiDriverLister.List(labels.Everything())
}

// +kubebuilder:rbac:groups="scheduling.run.ai",resources=bindrequests,verbs=get;list;watch

func (k *k8sLister) ListBindRequests() ([]*schedulingv1alpha2.BindRequest, error) {
	return k.bindRequestLister.List(labels.Everything())
}

// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch

func (k *k8sLister) ListConfigMaps() ([]*v1.ConfigMap, error) {
	return k.cmLister.List(labels.Everything())
}

// +kubebuilder:rbac:groups="kai.scheduler",resources=topologies,verbs=get;list;watch

func (k *k8sLister) ListTopologies() ([]*kaiv1alpha1.Topology, error) {
	return k.kaiTopologyLister.List(labels.Everything())
}

// +kubebuilder:rbac:groups="resource.k8s.io",resources=resourceslices,verbs=get;list;watch

func (k *k8sLister) ListResourceSlicesByNode() (map[string][]*resourceapi.ResourceSlice, error) {
	if k.resourceSliceLister == nil {
		return nil, nil
	}
	slices, err := k.resourceSliceLister.List(labels.Everything())
	if err != nil {
		return nil, err
	}

	result := make(map[string][]*resourceapi.ResourceSlice)
	for _, slice := range slices {
		nodeName := ""
		if slice.Spec.AllNodes == nil || !*slice.Spec.AllNodes {
			if slice.Spec.NodeName != nil {
				nodeName = *slice.Spec.NodeName
			}
		}
		result[nodeName] = append(result[nodeName], slice)
	}
	return result, nil
}

// +kubebuilder:rbac:groups="resource.k8s.io",resources=resourceclaims,verbs=get;list;watch

func (k *k8sLister) ListResourceClaims() ([]*resourceapi.ResourceClaim, error) {
	if k.resourceClaimLister == nil {
		return nil, nil
	}
	return k.resourceClaimLister.List(labels.Everything())
}
