// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package snapshot

import (
	"archive/zip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	resourcev1beta1 "k8s.io/api/resource/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	version "k8s.io/apimachinery/pkg/version"
	fakediscovery "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/utils/ptr"

	kubeaischedulerver "github.com/kai-scheduler/KAI-scheduler/pkg/apis/client/clientset/versioned/fake"
	kaiv1alpha1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1alpha1"
	enginev2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	enginev2alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/cache"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
)

const (
	NodePoolLabelKey   = "test-key"
	NodePoolLabelValue = "test-value"
)

func TestSnapshotPlugin(t *testing.T) {
	fakeKubeClient := fake.NewSimpleClientset()
	fakeKubeAISchedulerClient := kubeaischedulerver.NewSimpleClientset()
	fakeDiscoveryClient := fakeKubeClient.Discovery().(*fakediscovery.FakeDiscovery)
	fakeDiscoveryClient.FakedServerVersion = &version.Info{
		Major: "1",
		Minor: "32+",
	}
	fakeKubeClient.Resources = append(fakeKubeClient.Resources, &metav1.APIResourceList{
		GroupVersion: resourcev1beta1.SchemeGroupVersion.String(),
	})

	testPod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name:  "test-container",
					Image: "test-image",
				},
			},
		},
	}

	testNodes := []*v1.Node{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-node",
				Labels: map[string]string{
					NodePoolLabelKey: NodePoolLabelValue,
				},
			},
			Spec: v1.NodeSpec{
				Unschedulable: false,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-node-other-shard",
				Labels: map[string]string{
					NodePoolLabelKey: "other",
				},
			},
			Spec: v1.NodeSpec{
				Unschedulable: false,
			},
		},
	}

	testQueue := &enginev2.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-queue",
			Labels: map[string]string{
				NodePoolLabelKey: NodePoolLabelValue,
			},
		},
		Spec: enginev2.QueueSpec{
			DisplayName: "test-queue",
			Resources: &enginev2.QueueResources{
				GPU: enginev2.QueueResource{
					Quota:           1,
					OverQuotaWeight: 1,
					Limit:           1,
				},
				CPU: enginev2.QueueResource{
					Quota:           1,
					OverQuotaWeight: 1,
					Limit:           1,
				},
				Memory: enginev2.QueueResource{
					Quota:           1,
					OverQuotaWeight: 1,
					Limit:           1,
				},
			},
		},
	}

	testPodGroup := &enginev2alpha2.PodGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-podgroup",
			Namespace: "default",
			Labels: map[string]string{
				NodePoolLabelKey: NodePoolLabelValue,
			},
		},
		Spec: enginev2alpha2.PodGroupSpec{
			MinMember: ptr.To(int32(1)),
		},
	}

	testTopology := &kaiv1alpha1.Topology{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-topology",
		},
	}

	schedulerConfig := &conf.SchedulerConfiguration{
		Actions: "allocate",
		Tiers: []conf.Tier{
			{
				Plugins: []conf.PluginOption{
					{
						Name: "predicates",
					},
				},
			},
		},
	}

	schedulerParams := conf.SchedulerParams{
		SchedulerName: "test-scheduler",
		PartitionParams: &conf.SchedulingNodePoolParams{
			NodePoolLabelKey:   NodePoolLabelKey,
			NodePoolLabelValue: NodePoolLabelValue,
		},
	}

	schedulerCache := cache.New(&cache.SchedulerCacheParams{
		KubeClient:                  fakeKubeClient,
		KAISchedulerClient:          fakeKubeAISchedulerClient,
		SchedulerName:               schedulerParams.SchedulerName,
		NodePoolParams:              schedulerParams.PartitionParams,
		RestrictNodeScheduling:      false,
		DetailedFitErrors:           false,
		ScheduleCSIStorage:          false,
		FullHierarchyFairness:       true,
		NumOfStatusRecordingWorkers: 1,
		DiscoveryClient:             fakeKubeClient.Discovery(),
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_, err := fakeKubeClient.CoreV1().Pods("default").Create(ctx, testPod, metav1.CreateOptions{})
	assert.NoError(t, err)

	for _, node := range testNodes {
		_, err = fakeKubeClient.CoreV1().Nodes().Create(ctx, node, metav1.CreateOptions{})
		assert.NoError(t, err)
	}

	_, err = fakeKubeAISchedulerClient.SchedulingV2().Queues("").Create(ctx, testQueue, metav1.CreateOptions{})
	assert.NoError(t, err)

	_, err = fakeKubeAISchedulerClient.SchedulingV2alpha2().PodGroups("default").Create(ctx, testPodGroup, metav1.CreateOptions{})
	assert.NoError(t, err)

	_, err = fakeKubeAISchedulerClient.KaiV1alpha1().Topologies().Create(ctx, testTopology, metav1.CreateOptions{})
	assert.NoError(t, err)

	schedulerCache.Run(ctx.Done())
	schedulerCache.WaitForCacheSync(ctx.Done())

	session := &framework.Session{
		Config:          schedulerConfig,
		SchedulerParams: schedulerParams,
		Cache:           schedulerCache,
	}

	plugin := New(nil)
	plugin.OnSessionOpen(session)

	req := httptest.NewRequest("GET", "/get-snapshot", nil)
	w := httptest.NewRecorder()

	plugin.(*snapshotPlugin).serveSnapshot(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/zip", w.Header().Get("Content-Type"))

	zipReader, err := zip.NewReader(strings.NewReader(w.Body.String()), int64(len(w.Body.Bytes())))
	assert.NoError(t, err)

	var snapshotFile io.ReadCloser
	for _, file := range zipReader.File {
		if file.Name == "snapshot.json" {
			snapshotFile, err = file.Open()
			assert.NoError(t, err)
			break
		}
	}
	assert.NotNil(t, snapshotFile)
	defer snapshotFile.Close()

	snapshotBytes, err := io.ReadAll(snapshotFile)
	assert.NoError(t, err)

	var snapshot Snapshot
	err = json.Unmarshal(snapshotBytes, &snapshot)
	assert.NoError(t, err)

	assert.Equal(t, schedulerConfig.Actions, snapshot.Config.Actions)
	assert.Equal(t, schedulerParams.SchedulerName, snapshot.SchedulerParams.SchedulerName)
	assert.Equal(t, schedulerParams.PartitionParams.NodePoolLabelKey, snapshot.SchedulerParams.PartitionParams.NodePoolLabelKey)
	assert.Equal(t, schedulerParams.PartitionParams.NodePoolLabelValue, snapshot.SchedulerParams.PartitionParams.NodePoolLabelValue)

	assert.NotNil(t, snapshot.RawObjects)

	assert.Len(t, snapshot.RawObjects.Pods, 1)
	assert.Equal(t, testPod.Name, snapshot.RawObjects.Pods[0].Name)
	assert.Equal(t, testPod.Namespace, snapshot.RawObjects.Pods[0].Namespace)

	assert.Len(t, snapshot.RawObjects.Nodes, 1)
	if len(snapshot.RawObjects.Nodes) > 0 {
		assert.Equal(t, testNodes[0].Name, snapshot.RawObjects.Nodes[0].Name)
	}

	assert.Len(t, snapshot.RawObjects.Queues, 1)
	if len(snapshot.RawObjects.Queues) > 0 {
		assert.Equal(t, testQueue.Name, snapshot.RawObjects.Queues[0].Name)
	}

	assert.Len(t, snapshot.RawObjects.PodGroups, 1)
	if len(snapshot.RawObjects.PodGroups) > 0 {
		assert.Equal(t, testPodGroup.Name, snapshot.RawObjects.PodGroups[0].Name)
		assert.Equal(t, testPodGroup.Namespace, snapshot.RawObjects.PodGroups[0].Namespace)
	}

	assert.Len(t, snapshot.RawObjects.Topologies, 1)
	if len(snapshot.RawObjects.Topologies) > 0 {
		assert.Equal(t, testTopology.Name, snapshot.RawObjects.Topologies[0].Name)
	}

	assert.NotNil(t, snapshot.Discovery)
	assert.Equal(t, "1", snapshot.Discovery.ServerVersion.Major)
	assert.Equal(t, "32+", snapshot.Discovery.ServerVersion.Minor)
	assert.Len(t, snapshot.Discovery.Resources, 1)
	assert.Equal(t, resourcev1beta1.SchemeGroupVersion.String(), snapshot.Discovery.Resources[0].GroupVersion)
}
