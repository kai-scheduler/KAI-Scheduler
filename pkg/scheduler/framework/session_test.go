// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	schedulingv1alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v1alpha2"
	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/resources"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/bindrequest_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	scheduler_cache "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf"
)

const benchmarkBindRequestCount = 5000

func TestSessionClearDropsRetainedReferences(t *testing.T) {
	ssn := &Session{
		ClusterInfo: &api.ClusterInfo{
			BindRequests: bindrequest_info.BindRequestMap{
				bindrequest_info.NewKey("namespace", "pod"): &bindrequest_info.BindRequestInfo{},
			},
			BindRequestsForDeletedNodes: []*bindrequest_info.BindRequestInfo{{}},
		},
		Config:               &conf.SchedulerConfiguration{},
		plugins:              map[string]Plugin{"plugin": nil},
		eventHandlers:        []*EventHandler{{}},
		TaskOrderFns:         []common_info.CompareFn{nil},
		PrePredicateFns:      []api.PrePredicateFn{nil},
		BindRequestMutateFns: []api.BindRequestMutateFn{nil},
	}
	ssn.k8sResourceStateCache.Store("resource", "state")

	ssn.clear()

	assert.Nil(t, ssn.ClusterInfo)
	assert.Nil(t, ssn.Config)
	assert.Nil(t, ssn.plugins)
	assert.Nil(t, ssn.eventHandlers)
	assert.Nil(t, ssn.TaskOrderFns)
	assert.Nil(t, ssn.PrePredicateFns)
	assert.Nil(t, ssn.BindRequestMutateFns)
	_, found := ssn.k8sResourceStateCache.Load("resource")
	assert.False(t, found)
}

func TestCloseSessionReleasesSnapshotReferencesWhileSessionIsLive(t *testing.T) {
	finalized := make(chan struct{})
	cacheMock := scheduler_cache.NewMockCache(gomock.NewController(t))
	cacheMock.EXPECT().WaitForWorkers(gomock.Any()).Times(1)
	ssn := newSessionWithFinalizedBindRequest(t, cacheMock, finalized)

	closeSession(ssn)

	requireFinalized(t, finalized, ssn)
}

func BenchmarkOpenCloseSessionWithLargeSnapshot(b *testing.B) {
	cacheMock := scheduler_cache.NewMockCache(gomock.NewController(b))
	cacheMock.EXPECT().Snapshot().AnyTimes().DoAndReturn(func() (*api.ClusterInfo, error) {
		return newClusterInfoWithBindRequests(benchmarkBindRequestCount), nil
	})
	cacheMock.EXPECT().WaitForWorkers(gomock.Any()).AnyTimes()

	runtime.GC()
	before := heapAlloc()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ssn, err := openSession(cacheMock, "benchmark", conf.SchedulerParams{}, nil)
		if err != nil {
			b.Fatal(err)
		}
		closeSession(ssn)
	}
	b.StopTimer()

	runtime.GC()
	after := heapAlloc()
	retained := int64(after) - int64(before)
	if retained < 0 {
		retained = 0
	}
	b.ReportMetric(benchmarkBindRequestCount, "bind_requests/op")
	b.ReportMetric(float64(retained)/float64(b.N), "retained_after_1_gc_B/op")
}

func newSessionWithFinalizedBindRequest(
	t *testing.T, cache scheduler_cache.Cache, finalized chan<- struct{},
) *Session {
	t.Helper()

	bindRequest := &schedulingv1alpha2.BindRequest{
		Spec: schedulingv1alpha2.BindRequestSpec{
			PodName: "pod",
		},
	}
	runtime.SetFinalizer(bindRequest, func(*schedulingv1alpha2.BindRequest) {
		close(finalized)
	})

	ssn := &Session{
		Cache: cache,
		ClusterInfo: &api.ClusterInfo{
			BindRequests: bindrequest_info.BindRequestMap{
				bindrequest_info.NewKey("namespace", "pod"): bindrequest_info.NewBindRequestInfo(bindRequest),
			},
			BindRequestsForDeletedNodes: []*bindrequest_info.BindRequestInfo{
				bindrequest_info.NewBindRequestInfo(bindRequest),
			},
		},
	}
	if err := ssn.InitNodeScoringPool(); err != nil {
		t.Fatalf("failed to initialize node scoring pool: %v", err)
	}
	return ssn
}

func requireFinalized(t *testing.T, finalized <-chan struct{}, keepAlive *Session) {
	t.Helper()

	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()

	for {
		runtime.GC()
		select {
		case <-finalized:
			runtime.KeepAlive(keepAlive)
			return
		case <-deadline.C:
			runtime.KeepAlive(keepAlive)
			t.Fatal("snapshot bind request was still reachable after closeSession")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func heapAlloc() uint64 {
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	return stats.HeapAlloc
}

func newClusterInfoWithBindRequests(count int) *api.ClusterInfo {
	clusterInfo := &api.ClusterInfo{
		BindRequests: make(bindrequest_info.BindRequestMap, count),
	}
	for i := 0; i < count; i++ {
		podName := "pod-" + strconv.Itoa(i)
		bindRequest := &schedulingv1alpha2.BindRequest{
			Spec: schedulingv1alpha2.BindRequestSpec{
				PodName: podName,
			},
		}
		clusterInfo.BindRequests[bindrequest_info.NewKey("namespace", podName)] =
			bindrequest_info.NewBindRequestInfo(bindRequest)
	}
	return clusterInfo
}

// A GPU with room to spare on memory but none on compute must be filtered out,
// otherwise the scheduler would place an sm-sharing pod on a device whose SMs
// are already fully claimed.
func TestFilterGpusByEnoughResourcesSkipsComputeExhaustedGpu(t *testing.T) {
	const (
		gpuMemoryMiB = int64(23028)
		gpuGroup     = "gpu-group-0"
	)

	vectorMap := resource_info.NewResourceVectorMap()
	node := &node_info.NodeInfo{
		Name:                    "node1",
		PodInfos:                map[common_info.PodID]*pod_info.PodInfo{},
		VectorMap:               vectorMap,
		MemoryOfEveryGpuOnNode:  gpuMemoryMiB,
		ComputeOfEveryGpuOnNode: node_info.WholeGpuCompute,
		IdleVector:              resource_info.NewResourceVector(vectorMap),
		ReleasingVector:         resource_info.NewResourceVector(vectorMap),
		GpuSharingNodeInfo: node_info.GpuSharingNodeInfo{
			ReleasingSharedGPUs:        map[string]bool{},
			UsedSharedGPUsMemory:       map[string]int64{gpuGroup: gpuMemoryMiB / 5},
			ReleasingSharedGPUsMemory:  map[string]int64{},
			AllocatedSharedGPUsMemory:  map[string]int64{gpuGroup: gpuMemoryMiB / 5},
			UsedSharedGPUsCompute:      map[string]int64{gpuGroup: 80},
			ReleasingSharedGPUsCompute: map[string]int64{},
			AllocatedSharedGPUsCompute: map[string]int64{gpuGroup: 80},
			DRASharedDeviceRefCount:    map[string]int{},
		},
	}

	smSharingPod := func(name, computeRequest string) *pod_info.PodInfo {
		pod := pod_info.NewTaskInfo(&v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: "default",
				UID:       types.UID(name),
				Annotations: map[string]string{
					commonconstants.PodGroupAnnotationForPod:                        "pg-" + name,
					commonconstants.GpuFraction:                                     "0.2",
					resources.CalcGpuComputeRequestAnnotationForContainer("c1"):     computeRequest,
					resources.CalcGpuComputeSharingModeAnnotationForContainer("c1"): string(schedulingv1alpha2.GPUComputeSharingModeSMSharing),
				},
			},
			Spec:   v1.PodSpec{Containers: []v1.Container{{Name: "c1"}}},
			Status: v1.PodStatus{Phase: v1.PodPending},
		}, vectorMap)
		return pod
	}

	// The gpuGroup's mode comes from a pod already placed on it.
	placed := smSharingPod("placed", "0.8")
	placed.SetGPUGroupIDs([]string{gpuGroup})
	node.PodInfos[common_info.PodID("default/placed")] = placed

	assert.Empty(t, filterGpusByEnoughResources(node, smSharingPod("over-committing", "0.8")),
		"a compute-exhausted GPU must not be offered for sharing")
	assert.Equal(t, []string{gpuGroup},
		filterGpusByEnoughResources(node, smSharingPod("fitting", "0.2")),
		"a GPU with compute left must still be offered")
}
