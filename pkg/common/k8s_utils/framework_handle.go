/*
Copyright 2020 The Volcano Authors.

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

// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package k8s_utils

import (
	"context"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/events"
	resourceslicetracker "k8s.io/dynamic-resource-allocation/resourceslice/tracker"
	"k8s.io/klog/v2"
	ksf "k8s.io/kube-scheduler/framework"
	"k8s.io/kubernetes/pkg/scheduler/apis/config"
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"
	"k8s.io/kubernetes/pkg/scheduler/framework/parallelize"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/dynamicresources"
	scheduling "k8s.io/kubernetes/pkg/scheduler/framework/plugins/volumebinding"
	"k8s.io/kubernetes/pkg/scheduler/metrics"
	"k8s.io/kubernetes/pkg/scheduler/util/assumecache"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// This is a stand-in for K8sFramework Handle that kubernetes uses for its plugins.
// Only the methods needed for the predicate plugins are implemented.
type K8sFramework struct {
	kubeClient           kubernetes.Interface
	informerFactory      informers.SharedInformerFactory
	nodeInfoLister       k8sframework.NodeInfoLister
	parallelizer         parallelize.Parallelizer
	resourceClaimCache   *assumecache.AssumeCache
	resourceSliceTracker *resourceslicetracker.Tracker
	sharedDRAManager     k8sframework.SharedDRAManager
}

var _ k8sframework.Handle = &K8sFramework{}

func (f *K8sFramework) SharedDRAManager() k8sframework.SharedDRAManager {
	if f.resourceClaimCache == nil {
		rrInformer := f.informerFactory.Resource().V1().ResourceClaims().Informer()
		f.resourceClaimCache = assumecache.NewAssumeCache(
			klog.LoggerWithName(klog.Background(), "ResourceClaimCache"),
			rrInformer, "ResourceClaim", "", nil,
		)
	}

	var err error
	if f.resourceSliceTracker == nil {
		f.resourceSliceTracker, err = resourceslicetracker.StartTracker(context.Background(), resourceslicetracker.Options{
			SliceInformer: f.informerFactory.Resource().V1().ResourceSlices(),
			TaintInformer: f.informerFactory.Resource().V1alpha3().DeviceTaintRules(),
			ClassInformer: f.informerFactory.Resource().V1().DeviceClasses(),
			KubeClient:    f.kubeClient,
		})
		if err != nil {
			log.Log.Error(err, "Failed to create resource slice tracker")
			return nil
		}
	}

	if f.sharedDRAManager == nil {
		f.sharedDRAManager = dynamicresources.NewDRAManager(
			context.Background(), f.resourceClaimCache, f.resourceSliceTracker, f.informerFactory,
		)
	}
	return f.sharedDRAManager
}

type listersWrapper struct {
	nodeInfoLister k8sframework.NodeInfoLister
}

func (lw *listersWrapper) NodeInfos() k8sframework.NodeInfoLister {
	return lw.nodeInfoLister
}

func (lw *listersWrapper) StorageInfos() k8sframework.StorageInfoLister {
	return nil
}

func (f *K8sFramework) SnapshotSharedLister() k8sframework.SharedLister {
	return &listersWrapper{f.nodeInfoLister}
}

// IterateOverWaitingPods acquires a read lock and iterates over the WaitingPods map.
func (f *K8sFramework) IterateOverWaitingPods(callback func(k8sframework.WaitingPod)) {
	panic("not implemented")
}

// GetWaitingPod returns a reference to a WaitingPod given its UID.
func (f *K8sFramework) GetWaitingPod(uid types.UID) k8sframework.WaitingPod {
	panic("not implemented")
}

// HasFilterPlugins returns true if at least one filter plugin is defined.
func (f *K8sFramework) HasFilterPlugins() bool {
	panic("not implemented")
}

// HasScorePlugins returns true if at least one score plugin is defined.
func (f *K8sFramework) HasScorePlugins() bool {
	panic("not implemented")
}

// ListPlugins returns a map of extension point name to plugin names configured at each extension
// point. Returns nil if no plugins were configured.
func (f *K8sFramework) ListPlugins() map[string][]config.Plugin {
	panic("not implemented")
}

// ClientSet returns a kubernetes clientset.
func (f *K8sFramework) ClientSet() kubernetes.Interface {
	return f.kubeClient
}

// SharedInformerFactory returns a shared informer factory.
func (f *K8sFramework) SharedInformerFactory() informers.SharedInformerFactory {
	return f.informerFactory
}

// VolumeBinder returns the volume binder used by scheduler.
func (f *K8sFramework) VolumeBinder() scheduling.SchedulerVolumeBinder {
	panic("not implemented")
}

// EventRecorder was introduced in k8s v1.19.6 and to be implemented
func (f *K8sFramework) EventRecorder() events.EventRecorder {
	return nil
}

func (f *K8sFramework) AddNominatedPod(logger klog.Logger, pod ksf.PodInfo, nominatingInfo *k8sframework.NominatingInfo) {
	panic("implement me")
}

func (f *K8sFramework) DeleteNominatedPodIfExists(pod *v1.Pod) {
	panic("implement me")
}

func (f *K8sFramework) UpdateNominatedPod(logger klog.Logger, oldPod *v1.Pod, newPodInfo ksf.PodInfo) {
	panic("implement me")
}

func (f *K8sFramework) NominatedPodsForNode(nodeName string) []ksf.PodInfo {
	panic("implement me")
}

func (f *K8sFramework) RunPreScorePlugins(ctx context.Context, state ksf.CycleState, pod *v1.Pod, infos []ksf.NodeInfo) *ksf.Status {
	panic("implement me")
}

func (f *K8sFramework) RunScorePlugins(ctx context.Context, state ksf.CycleState, pod *v1.Pod, infos []ksf.NodeInfo) ([]k8sframework.NodePluginScores, *ksf.Status) {
	panic("implement me")
}

func (f *K8sFramework) RunFilterPlugins(ctx context.Context, state ksf.CycleState, pod *v1.Pod, info ksf.NodeInfo) *ksf.Status {
	panic("implement me")
}

func (f *K8sFramework) RunPreFilterExtensionAddPod(ctx context.Context, state ksf.CycleState, podToSchedule *v1.Pod, podInfoToAdd ksf.PodInfo, nodeInfo ksf.NodeInfo) *ksf.Status {
	panic("implement me")
}

func (f *K8sFramework) RunPreFilterExtensionRemovePod(ctx context.Context, state ksf.CycleState, podToSchedule *v1.Pod, podInfoToRemove ksf.PodInfo, nodeInfo ksf.NodeInfo) *ksf.Status {
	panic("implement me")
}

func (f *K8sFramework) RejectWaitingPod(uid types.UID) bool {
	panic("implement me")
}

func (f *K8sFramework) KubeConfig() *rest.Config {
	panic("implement me")
}

func (f *K8sFramework) RunFilterPluginsWithNominatedPods(ctx context.Context, state ksf.CycleState, pod *v1.Pod, info ksf.NodeInfo) *ksf.Status {
	panic("implement me")
}

func (f *K8sFramework) Extenders() []k8sframework.Extender {
	panic("implement me")
}

func (f *K8sFramework) Parallelizer() parallelize.Parallelizer {
	return f.parallelizer
}

func (f *K8sFramework) Activate(logger klog.Logger, pods map[string]*v1.Pod) {
	panic("implement me")
}

func (f *K8sFramework) APICacher() k8sframework.APICacher {
	panic("implement me")
}

func (f *K8sFramework) APIDispatcher() ksf.APIDispatcher {
	panic("implement me")
}

// NewFrameworkHandle creates a FrameworkHandle interface, which is used by k8s plugins.
func NewFrameworkHandle(
	client kubernetes.Interface,
	informerFactory informers.SharedInformerFactory,
	nodeInfoLister k8sframework.NodeInfoLister,
) k8sframework.Handle {
	metrics.InitMetrics()
	return &K8sFramework{
		kubeClient:      client,
		informerFactory: informerFactory,
		nodeInfoLister:  nodeInfoLister,
		parallelizer:    parallelize.NewParallelizer(parallelize.DefaultParallelism),
	}
}
