// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package snapshot

import (
	"context"
	"testing"

	nrtv1alpha2 "github.com/k8stopologyawareschedwg/noderesourcetopology-api/pkg/apis/topology/v1alpha2"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/version"

	kaiv1alpha1 "github.com/kai-scheduler/api/kai/v1alpha1"
	schedulingv1alpha2 "github.com/kai-scheduler/api/scheduling/v1alpha2"
	enginev2 "github.com/kai-scheduler/api/scheduling/v2"
	enginev2alpha2 "github.com/kai-scheduler/api/scheduling/v2alpha2"
)

func TestLoadClientsWithSnapshotRestoresListedObjects(t *testing.T) {
	DisableWatchListClientForFakeReplay()
	raw := &RawKubernetesObjects{
		Pods: []*v1.Pod{
			{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "job-0"}},
			{ObjectMeta: metav1.ObjectMeta{Name: ""}},
		},
		Nodes:                  []*v1.Node{{ObjectMeta: metav1.ObjectMeta{Name: "node-a"}}},
		Queues:                 []*enginev2.Queue{{ObjectMeta: metav1.ObjectMeta{Name: "team"}}},
		PodGroups:              []*enginev2alpha2.PodGroup{{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "job"}}},
		BindRequests:           []*schedulingv1alpha2.BindRequest{{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "bind-0"}}},
		PriorityClasses:        []*schedulingv1.PriorityClass{{ObjectMeta: metav1.ObjectMeta{Name: "high"}, Value: 80}},
		ConfigMaps:             []*v1.ConfigMap{{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "app-config"}}},
		PersistentVolumes:      []*v1.PersistentVolume{{ObjectMeta: metav1.ObjectMeta{Name: "pv-a"}}},
		PersistentVolumeClaims: []*v1.PersistentVolumeClaim{{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "pvc-a"}}},
		CSIStorageCapacities:   []*storagev1.CSIStorageCapacity{{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "cap-a"}}},
		StorageClasses:         []*storagev1.StorageClass{{ObjectMeta: metav1.ObjectMeta{Name: "fast"}}},
		CSIDrivers:             []*storagev1.CSIDriver{{ObjectMeta: metav1.ObjectMeta{Name: "csi.example.com"}}},
		ResourceClaims:         []*resourceapi.ResourceClaim{{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "claim-a"}}},
		ResourceSlices:         []*resourceapi.ResourceSlice{{ObjectMeta: metav1.ObjectMeta{Name: "slice-a"}}},
		DeviceClasses:          []*resourceapi.DeviceClass{{ObjectMeta: metav1.ObjectMeta{Name: "gpu"}}},
		Topologies:             []*kaiv1alpha1.Topology{{ObjectMeta: metav1.ObjectMeta{Name: "rack"}}},
		NodeResourceTopologies: []*nrtv1alpha2.NodeResourceTopology{{ObjectMeta: metav1.ObjectMeta{Name: "node-a"}}},
	}

	kubeClient, kaiClient, nrtClient := LoadClientsWithSnapshot(raw, nil)
	ctx := context.Background()
	_, err := kubeClient.CoreV1().Pods("team").Get(ctx, "job-0", metav1.GetOptions{})
	require.NoError(t, err)
	_, err = kubeClient.CoreV1().Nodes().Get(ctx, "node-a", metav1.GetOptions{})
	require.NoError(t, err)
	_, err = kaiClient.SchedulingV2().Queues("").Get(ctx, "team", metav1.GetOptions{})
	require.NoError(t, err)
	_, err = kaiClient.SchedulingV2alpha2().PodGroups("team").Get(ctx, "job", metav1.GetOptions{})
	require.NoError(t, err)
	_, err = kaiClient.SchedulingV1alpha2().BindRequests("team").Get(ctx, "bind-0", metav1.GetOptions{})
	require.NoError(t, err)
	_, err = kubeClient.SchedulingV1().PriorityClasses().Get(ctx, "high", metav1.GetOptions{})
	require.NoError(t, err)
	_, err = kubeClient.CoreV1().ConfigMaps("team").Get(ctx, "app-config", metav1.GetOptions{})
	require.NoError(t, err)
	_, err = kubeClient.CoreV1().PersistentVolumes().Get(ctx, "pv-a", metav1.GetOptions{})
	require.NoError(t, err)
	_, err = kubeClient.CoreV1().PersistentVolumeClaims("team").Get(ctx, "pvc-a", metav1.GetOptions{})
	require.NoError(t, err)
	_, err = kubeClient.StorageV1().CSIStorageCapacities("team").Get(ctx, "cap-a", metav1.GetOptions{})
	require.NoError(t, err)
	_, err = kubeClient.StorageV1().StorageClasses().Get(ctx, "fast", metav1.GetOptions{})
	require.NoError(t, err)
	_, err = kubeClient.StorageV1().CSIDrivers().Get(ctx, "csi.example.com", metav1.GetOptions{})
	require.NoError(t, err)
	_, err = kubeClient.ResourceV1().ResourceClaims("team").Get(ctx, "claim-a", metav1.GetOptions{})
	require.NoError(t, err)
	_, err = kubeClient.ResourceV1().ResourceSlices().Get(ctx, "slice-a", metav1.GetOptions{})
	require.NoError(t, err)
	_, err = kubeClient.ResourceV1().DeviceClasses().Get(ctx, "gpu", metav1.GetOptions{})
	require.NoError(t, err)
	_, err = kaiClient.KaiV1alpha1().Topologies().Get(ctx, "rack", metav1.GetOptions{})
	require.NoError(t, err)
	_, err = nrtClient.TopologyV1alpha2().NodeResourceTopologies().Get(ctx, "node-a", metav1.GetOptions{})
	require.NoError(t, err)

	info, err := kubeClient.Discovery().ServerVersion()
	require.NoError(t, err)
	require.Equal(t, "32", info.Minor)
}

func TestLoadClientsUsesAnExplicitDiscoverySnapshot(t *testing.T) {
	kubeClient, kaiClient, nrtClient := LoadClientsWithSnapshot(nil, nil)
	require.NotNil(t, kubeClient)
	require.NotNil(t, kaiClient)
	require.NotNil(t, nrtClient)
	pods, err := kubeClient.CoreV1().Pods("").List(context.Background(), metav1.ListOptions{})
	require.NoError(t, err)
	require.Empty(t, pods.Items)

	kubeClient, _, _ = LoadClientsWithSnapshot(&RawKubernetesObjects{}, &DiscoverySnapshot{
		ServerVersion: &version.Info{Major: "1", Minor: "34"},
		Resources: []*metav1.APIResourceList{{
			GroupVersion: "v1",
			APIResources: []metav1.APIResource{{Name: "pods", Kind: "Pod", Namespaced: true}},
		}},
	})
	info, err := kubeClient.Discovery().ServerVersion()
	require.NoError(t, err)
	require.Equal(t, "34", info.Minor)
}

func TestLoadClientsReportsACreateFailureAndKeepsTheRest(t *testing.T) {
	kubeClient, _, _ := LoadClientsWithSnapshot(&RawKubernetesObjects{
		Nodes: []*v1.Node{
			{ObjectMeta: metav1.ObjectMeta{Name: "node-a"}},
			{ObjectMeta: metav1.ObjectMeta{Name: "node-a"}},
		},
	}, &DiscoverySnapshot{})
	_, err := kubeClient.CoreV1().Nodes().Get(context.Background(), "node-a", metav1.GetOptions{})
	require.NoError(t, err)
}
