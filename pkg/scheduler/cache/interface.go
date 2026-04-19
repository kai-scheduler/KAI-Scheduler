// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/eviction_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache/cluster_info/data_lister"
	k8splugins "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/k8s_internal/plugins"
)

type Cache interface {
	Run(stopCh <-chan struct{})
	Snapshot() (*api.ClusterInfo, error)
	WaitForCacheSync(stopCh <-chan struct{})
	Bind(podInfo *pod_info.PodInfo, hostname string) error
	Evict(ssnPod *v1.Pod, job *podgroup_info.PodGroupInfo, evictionMetadata eviction_info.EvictionMetadata, message string) error
	RecordJobStatusEvent(job *podgroup_info.PodGroupInfo) error
	TaskPipelined(task *pod_info.PodInfo, message string)
	KubeClient() kubernetes.Interface
	KubeInformerFactory() informers.SharedInformerFactory
	SnapshotSharedLister() k8sframework.NodeInfoLister
	InternalK8sPlugins() *k8splugins.K8sPlugins
	WaitForWorkers(stopCh <-chan struct{})
	GetDataLister() data_lister.DataLister
}
