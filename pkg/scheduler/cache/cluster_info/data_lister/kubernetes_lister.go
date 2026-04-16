// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package data_lister

import (
	v1 "k8s.io/api/core/v1"
	k8spolicyv1 "k8s.io/api/policy/v1"
	v14 "k8s.io/api/scheduling/v1"
	storage "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	listv1 "k8s.io/client-go/listers/core/v1"
	policyv1 "k8s.io/client-go/listers/policy/v1"
	schedv1 "k8s.io/client-go/listers/scheduling/v1"
	v12 "k8s.io/client-go/listers/storage/v1"
	"k8s.io/client-go/tools/cache"

	kubeAiSchedulerInfo "github.com/kai-scheduler/KAI-scheduler/pkg/apis/client/informers/externalversions"
	scheudlinglistv1alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/client/listers/scheduling/v1alpha2"
	schedlistv2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/client/listers/scheduling/v2"
	schedlist2alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/client/listers/scheduling/v2alpha2"
	schedulingv1alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v1alpha2"
	enginev2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	enginev2alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
)

type k8sLister struct {
	podGroupLister schedlist2alpha2.PodGroupLister
	podInformer    cache.SharedIndexInformer
	podLister      listv1.PodLister
	nodeLister     listv1.NodeLister
	queueLister    schedlistv2.QueueLister
	pdbLister      policyv1.PodDisruptionBudgetLister
	pcLister       schedv1.PriorityClassLister
	cmLister       listv1.ConfigMapLister

	pvcLister             listv1.PersistentVolumeClaimLister
	storageCapacityLister v12.CSIStorageCapacityLister
	storageClassLister    v12.StorageClassLister
	csiDriverLister       v12.CSIDriverLister

	bindRequestLister scheudlinglistv1alpha2.BindRequestLister

	partitionSelector labels.Selector
}

// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch

func New(
	informerFactory informers.SharedInformerFactory, kubeAiSchedulerInformerFactory kubeAiSchedulerInfo.SharedInformerFactory,
	partitionSelector labels.Selector,
) *k8sLister {
	return &k8sLister{
		podGroupLister: kubeAiSchedulerInformerFactory.Scheduling().V2alpha2().PodGroups().Lister(),
		podInformer:    informerFactory.Core().V1().Pods().Informer(),
		podLister:      informerFactory.Core().V1().Pods().Lister(),
		nodeLister:     informerFactory.Core().V1().Nodes().Lister(),
		queueLister:    kubeAiSchedulerInformerFactory.Scheduling().V2().Queues().Lister(),
		pdbLister:      informerFactory.Policy().V1().PodDisruptionBudgets().Lister(),
		pcLister:       informerFactory.Scheduling().V1().PriorityClasses().Lister(),
		cmLister:       informerFactory.Core().V1().ConfigMaps().Lister(),

		pvcLister:             informerFactory.Core().V1().PersistentVolumeClaims().Lister(),
		storageCapacityLister: informerFactory.Storage().V1().CSIStorageCapacities().Lister(),
		storageClassLister:    informerFactory.Storage().V1().StorageClasses().Lister(),
		csiDriverLister:       informerFactory.Storage().V1().CSIDrivers().Lister(),

		bindRequestLister: kubeAiSchedulerInformerFactory.Scheduling().V1alpha2().BindRequests().Lister(),

		partitionSelector: partitionSelector,
	}
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

// +kubebuilder:rbac:groups=policy,resources=poddisruptionbudgets,verbs=get;list;watch

func (k *k8sLister) ListPodDisruptionBudgets() ([]*k8spolicyv1.PodDisruptionBudget, error) {
	return k.pdbLister.List(labels.Everything())
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
