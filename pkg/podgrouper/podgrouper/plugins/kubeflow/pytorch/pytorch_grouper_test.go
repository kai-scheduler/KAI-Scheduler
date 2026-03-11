// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package pytorch

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgroup"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/defaultgrouper"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/kubeflow"
)

const (
	minReplicasNum    = 66
	minAvailableNum   = 33
	workerReplicasNum = 4
	masterReplicasNum = 2
	queueLabelKey     = "kai.scheduler/queue"
	nodePoolLabelKey  = "kai.scheduler/node-pool"
)

func newTestPyTorchGrouper() *PyTorchGrouper {
	defaultGrouper := defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, fake.NewFakeClient())
	kubeFlowGrouper := kubeflow.NewKubeflowDistributedGrouper(defaultGrouper)
	return NewPyTorchGrouper(kubeFlowGrouper)
}

func TestGetPodGroupMetadata_OnlyReplicas(t *testing.T) {
	pytorchJob := getBasicPytorchJob()
	pod := &v1.Pod{}
	grouper := newTestPyTorchGrouper()
	metadata, err := grouper.GetPodGroupMetadata(pytorchJob, pod)
	assert.Nil(t, err, "Got error when getting pytorch pod group metadata")
	assert.EqualValues(t, workerReplicasNum+masterReplicasNum, metadata.MinAvailable)
}

func TestGetPodGroupMetadata_OnlyMinReplicas(t *testing.T) {
	pytorchJob := getBasicPytorchJob()
	err := unstructured.SetNestedField(pytorchJob.Object, int64(minReplicasNum), "spec", "elasticPolicy", "minReplicas")
	assert.Nil(t, err, "Got error when setting minReplicas for pytorch job")

	pod := &v1.Pod{}
	grouper := newTestPyTorchGrouper()
	metadata, err := grouper.GetPodGroupMetadata(pytorchJob, pod)
	assert.Nil(t, err, "Got error when getting pytorch pod group metadata")
	assert.EqualValues(t, minReplicasNum, metadata.MinAvailable)
}

func TestGetPodGroupMetadata_OnlyMinAvailable(t *testing.T) {
	pytorchJob := getBasicPytorchJob()

	err := unstructured.SetNestedField(pytorchJob.Object, int64(minAvailableNum), "spec", "runPolicy", "schedulingPolicy", "minAvailable")
	assert.Nil(t, err, "Got error when setting minAvailable for pytorch job")

	pod := &v1.Pod{}
	grouper := newTestPyTorchGrouper()
	metadata, err := grouper.GetPodGroupMetadata(pytorchJob, pod)

	assert.Nil(t, err, "Got error when getting pytorch pod group metadata")
	assert.EqualValues(t, minAvailableNum, metadata.MinAvailable)
}

// TestGetPodGroupMetadata_MinAvailableAndMinReplicas tests the case when both minAvailable and minReplicas are set -
// minAvailable should be used as the value for MinAvailable in the metadata.
func TestGetPodGroupMetadata_MinAvailableAndMinReplicas(t *testing.T) {
	pytorchJob := getBasicPytorchJob()

	err := unstructured.SetNestedField(pytorchJob.Object, int64(minReplicasNum), "spec", "elasticPolicy", "minReplicas")
	assert.Nil(t, err, "Got error when setting minReplicas for pytorch job")

	err = unstructured.SetNestedField(pytorchJob.Object, int64(minAvailableNum), "spec", "runPolicy", "schedulingPolicy", "minAvailable")
	assert.Nil(t, err, "Got error when setting minAvailable for pytorch job")

	pod := &v1.Pod{}
	grouper := newTestPyTorchGrouper()
	metadata, err := grouper.GetPodGroupMetadata(pytorchJob, pod)
	assert.Nil(t, err, "Got error when getting pytorch pod group metadata")
	assert.EqualValues(t, minAvailableNum, metadata.MinAvailable)
}

// TestGetPodGroupMetadata_OnlyMasterReplicas tests a PyTorch job with only Master replicas and no Worker replicas
func TestGetPodGroupMetadata_OnlyMasterReplicas(t *testing.T) {
	pytorchJob := getPytorchJobWithOnlyMaster()
	pod := &v1.Pod{}
	grouper := newTestPyTorchGrouper()
	metadata, err := grouper.GetPodGroupMetadata(pytorchJob, pod)
	assert.Nil(t, err, "Got error when getting pytorch pod group metadata")
	assert.EqualValues(t, masterReplicasNum, metadata.MinAvailable)
}

// TestGetPodGroupMetadata_OnlyWorkerReplicas tests a PyTorch job with only Worker replicas and no Master replicas
func TestGetPodGroupMetadata_OnlyWorkerReplicas(t *testing.T) {
	pytorchJob := getPytorchJobWithOnlyWorker()
	pod := &v1.Pod{}
	grouper := newTestPyTorchGrouper()
	metadata, err := grouper.GetPodGroupMetadata(pytorchJob, pod)
	assert.Nil(t, err, "Got error when getting pytorch pod group metadata")
	assert.EqualValues(t, workerReplicasNum, metadata.MinAvailable)
}

// TestGetPodGroupMetadata_OnlyMasterWithMinReplicas tests a PyTorch job with only Master replicas
// and a specified minReplicas value
func TestGetPodGroupMetadata_OnlyMasterWithMinReplicas(t *testing.T) {
	pytorchJob := getPytorchJobWithOnlyMaster()
	err := unstructured.SetNestedField(pytorchJob.Object, int64(minReplicasNum), "spec", "elasticPolicy", "minReplicas")
	assert.Nil(t, err, "Got error when setting minReplicas for pytorch job")

	pod := &v1.Pod{}
	grouper := newTestPyTorchGrouper()
	metadata, err := grouper.GetPodGroupMetadata(pytorchJob, pod)
	assert.Nil(t, err, "Got error when getting pytorch pod group metadata")
	assert.EqualValues(t, minReplicasNum, metadata.MinAvailable)
}

// TestGetPodGroupMetadata_OnlyWorkerWithMinReplicas tests a PyTorch job with only Worker replicas
// and a specified minReplicas value
func TestGetPodGroupMetadata_OnlyWorkerWithMinReplicas(t *testing.T) {
	pytorchJob := getPytorchJobWithOnlyWorker()
	err := unstructured.SetNestedField(pytorchJob.Object, int64(minReplicasNum), "spec", "elasticPolicy", "minReplicas")
	assert.Nil(t, err, "Got error when setting minReplicas for pytorch job")

	pod := &v1.Pod{}
	grouper := newTestPyTorchGrouper()
	metadata, err := grouper.GetPodGroupMetadata(pytorchJob, pod)
	assert.Nil(t, err, "Got error when getting pytorch pod group metadata")
	assert.EqualValues(t, minReplicasNum, metadata.MinAvailable)
}

// TestGetPodGroupMetadata_OnlyMasterWithMinAvailable tests a PyTorch job with only Master replicas
// and a specified minAvailable value
func TestGetPodGroupMetadata_OnlyMasterWithMinAvailable(t *testing.T) {
	pytorchJob := getPytorchJobWithOnlyMaster()
	err := unstructured.SetNestedField(pytorchJob.Object, int64(minAvailableNum), "spec", "runPolicy", "schedulingPolicy", "minAvailable")
	assert.Nil(t, err, "Got error when setting minAvailable for pytorch job")

	pod := &v1.Pod{}
	grouper := newTestPyTorchGrouper()
	metadata, err := grouper.GetPodGroupMetadata(pytorchJob, pod)
	assert.Nil(t, err, "Got error when getting pytorch pod group metadata")
	assert.EqualValues(t, minAvailableNum, metadata.MinAvailable)
}

// TestGetPodGroupMetadata_OnlyWorkerWithMinAvailable tests a PyTorch job with only Worker replicas
// and a specified minAvailable value
func TestGetPodGroupMetadata_OnlyWorkerWithMinAvailable(t *testing.T) {
	pytorchJob := getPytorchJobWithOnlyWorker()
	err := unstructured.SetNestedField(pytorchJob.Object, int64(minAvailableNum), "spec", "runPolicy", "schedulingPolicy", "minAvailable")
	assert.Nil(t, err, "Got error when setting minAvailable for pytorch job")

	pod := &v1.Pod{}
	grouper := newTestPyTorchGrouper()
	metadata, err := grouper.GetPodGroupMetadata(pytorchJob, pod)
	assert.Nil(t, err, "Got error when getting pytorch pod group metadata")
	assert.EqualValues(t, minAvailableNum, metadata.MinAvailable)
}

func getBasicPytorchJob() *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "PyTorchJob",
			"apiVersion": "kubeflow.org/v1",
			"metadata": map[string]interface{}{
				"name":      "test_job",
				"namespace": "test_namespace",
				"uid":       "1",
			},
			"spec": map[string]interface{}{
				"pytorchReplicaSpecs": map[string]interface{}{
					"Master": map[string]interface{}{
						"replicas": int64(masterReplicasNum),
					},
					"Worker": map[string]interface{}{
						"replicas": int64(workerReplicasNum),
					},
				},
			},
		},
	}
}

// getPytorchJobWithOnlyMaster returns a PyTorch job configuration with only Master replicas
func getPytorchJobWithOnlyMaster() *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "PyTorchJob",
			"apiVersion": "kubeflow.org/v1",
			"metadata": map[string]interface{}{
				"name":      "test_job_master_only",
				"namespace": "test_namespace",
				"uid":       "2",
			},
			"spec": map[string]interface{}{
				"pytorchReplicaSpecs": map[string]interface{}{
					"Master": map[string]interface{}{
						"replicas": int64(masterReplicasNum),
					},
				},
			},
		},
	}
}

// getPytorchJobWithOnlyWorker returns a PyTorch job configuration with only Worker replicas
func getPytorchJobWithOnlyWorker() *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "PyTorchJob",
			"apiVersion": "kubeflow.org/v1",
			"metadata": map[string]interface{}{
				"name":      "test_job_worker_only",
				"namespace": "test_namespace",
				"uid":       "3",
			},
			"spec": map[string]interface{}{
				"pytorchReplicaSpecs": map[string]interface{}{
					"Worker": map[string]interface{}{
						"replicas": int64(workerReplicasNum),
					},
				},
			},
		},
	}
}

func TestGetPodGroupMetadata_SubGroups_MasterAndWorker(t *testing.T) {
	pytorchJob := getBasicPytorchJob()
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod-master-0",
			Namespace: "test_namespace",
			Labels: map[string]string{
				replicaTypeLabel: "master",
			},
		},
	}
	grouper := newTestPyTorchGrouper()
	metadata, err := grouper.GetPodGroupMetadata(pytorchJob, pod)
	assert.Nil(t, err)

	assert.Equal(t, 2, len(metadata.SubGroups))

	masterSubGroup := findSubGroupByName(metadata.SubGroups, strings.ToLower(replicaTypeMaster))
	assert.NotNil(t, masterSubGroup)
	assert.Equal(t, 1, len(masterSubGroup.PodsReferences))
	assert.Equal(t, "test-pod-master-0", masterSubGroup.PodsReferences[0])

	workerSubGroup := findSubGroupByName(metadata.SubGroups, strings.ToLower(replicaTypeWorker))
	assert.NotNil(t, workerSubGroup)
	assert.Equal(t, 0, len(workerSubGroup.PodsReferences))
}

func TestGetPodGroupMetadata_SubGroups_WorkerPod(t *testing.T) {
	pytorchJob := getBasicPytorchJob()
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod-worker-0",
			Namespace: "test_namespace",
			Labels: map[string]string{
				replicaTypeLabel: "worker",
			},
		},
	}
	grouper := newTestPyTorchGrouper()
	metadata, err := grouper.GetPodGroupMetadata(pytorchJob, pod)
	assert.Nil(t, err)

	assert.Equal(t, 2, len(metadata.SubGroups))

	masterSubGroup := findSubGroupByName(metadata.SubGroups, strings.ToLower(replicaTypeMaster))
	assert.NotNil(t, masterSubGroup)
	assert.Equal(t, 0, len(masterSubGroup.PodsReferences))

	workerSubGroup := findSubGroupByName(metadata.SubGroups, strings.ToLower(replicaTypeWorker))
	assert.NotNil(t, workerSubGroup)
	assert.Equal(t, 1, len(workerSubGroup.PodsReferences))
	assert.Equal(t, "test-pod-worker-0", workerSubGroup.PodsReferences[0])
}

func TestGetPodGroupMetadata_SubGroups_OnlyMaster(t *testing.T) {
	pytorchJob := getPytorchJobWithOnlyMaster()
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod-master-0",
			Namespace: "test_namespace",
			Labels: map[string]string{
				replicaTypeLabel: "master",
			},
		},
	}
	grouper := newTestPyTorchGrouper()
	metadata, err := grouper.GetPodGroupMetadata(pytorchJob, pod)
	assert.Nil(t, err)

	assert.Equal(t, 1, len(metadata.SubGroups))

	masterSubGroup := findSubGroupByName(metadata.SubGroups, strings.ToLower(replicaTypeMaster))
	assert.NotNil(t, masterSubGroup)
	assert.Equal(t, 1, len(masterSubGroup.PodsReferences))
	assert.Equal(t, "test-pod-master-0", masterSubGroup.PodsReferences[0])
}

func TestGetPodGroupMetadata_SubGroups_OnlyWorker(t *testing.T) {
	pytorchJob := getPytorchJobWithOnlyWorker()
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod-worker-0",
			Namespace: "test_namespace",
			Labels: map[string]string{
				replicaTypeLabel: "worker",
			},
		},
	}
	grouper := newTestPyTorchGrouper()
	metadata, err := grouper.GetPodGroupMetadata(pytorchJob, pod)
	assert.Nil(t, err)

	assert.Equal(t, 1, len(metadata.SubGroups))

	workerSubGroup := findSubGroupByName(metadata.SubGroups, strings.ToLower(replicaTypeWorker))
	assert.NotNil(t, workerSubGroup)
	assert.Equal(t, 1, len(workerSubGroup.PodsReferences))
	assert.Equal(t, "test-pod-worker-0", workerSubGroup.PodsReferences[0])
}

func TestGetPodGroupMetadata_SubGroups_PodWithoutReplicaTypeLabel(t *testing.T) {
	pytorchJob := getBasicPytorchJob()
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod-0",
			Namespace: "test_namespace",
		},
	}
	grouper := newTestPyTorchGrouper()
	metadata, err := grouper.GetPodGroupMetadata(pytorchJob, pod)
	assert.Nil(t, err)

	assert.Equal(t, 2, len(metadata.SubGroups))
	for _, subGroup := range metadata.SubGroups {
		assert.Equal(t, 0, len(subGroup.PodsReferences))
	}
}

func findSubGroupByName(subGroups []*podgroup.SubGroupMetadata, name string) *podgroup.SubGroupMetadata {
	for _, sg := range subGroups {
		if sg.Name == name {
			return sg
		}
	}
	return nil
}

func TestGetPodGroupMetadata_Segments_HappyFlow_4Workers_2PerSegment(t *testing.T) {
	pytorchJob := getPytorchJobWithSegments(1, 4, "2")
	grouper := newTestPyTorchGrouper()

	// Test with master pod
	masterPod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-job-master-0",
			Namespace: "test_namespace",
			Labels: map[string]string{
				replicaTypeLabel:                      "master",
				"training.kubeflow.org/replica-index": "0",
			},
			Annotations: map[string]string{
				"kai.scheduler/segment-size": "2",
			},
		},
	}
	metadata, err := grouper.GetPodGroupMetadata(pytorchJob, masterPod)
	assert.Nil(t, err)
	assert.Equal(t, int32(5), metadata.MinAvailable) // 1 master + 4 workers

	// Verify subgroups: 1 master + 1 Worker parent + 2 worker segments
	assert.Equal(t, 4, len(metadata.SubGroups))
	masterSubGroup := findSubGroupByName(metadata.SubGroups, strings.ToLower(replicaTypeMaster))
	assert.NotNil(t, masterSubGroup)
	assert.Equal(t, int32(1), masterSubGroup.MinAvailable)
	assert.Equal(t, 1, len(masterSubGroup.PodsReferences))

	workerSubgroup := findSubGroupByName(metadata.SubGroups, strings.ToLower(replicaTypeWorker))
	assert.NotNil(t, workerSubgroup)

	workerSegment0 := findSubGroupByName(metadata.SubGroups, "worker-0")
	assert.NotNil(t, workerSegment0)

	workerSegment1 := findSubGroupByName(metadata.SubGroups, "worker-1")
	assert.NotNil(t, workerSegment1)

	// Test worker pod in segment 0 (replica index 0)
	workerPod0 := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-job-worker-0",
			Namespace: "test_namespace",
			Labels: map[string]string{
				replicaTypeLabel:                      "worker",
				"training.kubeflow.org/replica-index": "0",
			},
			Annotations: map[string]string{
				"kai.scheduler/segment-size": "2",
			},
		},
	}
	metadata, err = grouper.GetPodGroupMetadata(pytorchJob, workerPod0)
	assert.Nil(t, err)
	assert.Equal(t, 4, len(metadata.SubGroups))
	workerSegment0 = findSubGroupByName(metadata.SubGroups, "worker-0")
	assert.NotNil(t, workerSegment0)
	assert.Equal(t, 1, len(workerSegment0.PodsReferences))
	assert.Equal(t, "test-job-worker-0", workerSegment0.PodsReferences[0])

	// Test worker pod in segment 1 (replica index 2)
	workerPod2 := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-job-worker-2",
			Namespace: "test_namespace",
			Labels: map[string]string{
				replicaTypeLabel:                      "worker",
				"training.kubeflow.org/replica-index": "2",
			},
			Annotations: map[string]string{
				"kai.scheduler/segment-size": "2",
			},
		},
	}
	metadata, err = grouper.GetPodGroupMetadata(pytorchJob, workerPod2)
	assert.Nil(t, err)
	workerSegment1 = findSubGroupByName(metadata.SubGroups, "worker-1")
	assert.NotNil(t, workerSegment1)
	assert.Equal(t, 1, len(workerSegment1.PodsReferences))
	assert.Equal(t, "test-job-worker-2", workerSegment1.PodsReferences[0])
}

func TestGetPodGroupMetadata_Segments_5Workers_2PerSegment(t *testing.T) {
	// 5 workers with segment size 2 should create 3 segments (2 full ones, one partial)
	pytorchJob := getPytorchJobWithSegments(1, 5, "2")
	grouper := newTestPyTorchGrouper()

	workerPod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-job-worker-0",
			Namespace: "test_namespace",
			Labels: map[string]string{
				replicaTypeLabel:                      "worker",
				"training.kubeflow.org/replica-index": "0",
			},
			Annotations: map[string]string{
				"kai.scheduler/segment-size": "2",
			},
		},
	}

	metadata, err := grouper.GetPodGroupMetadata(pytorchJob, workerPod)
	assert.Nil(t, err)
	assert.Equal(t, int32(6), metadata.MinAvailable) // 1 master + 5 workers

	// Verify subgroups: 1 master + 1 Worker parent + 3 worker segments
	assert.Equal(t, 5, len(metadata.SubGroups))
	masterSubGroup := findSubGroupByName(metadata.SubGroups, strings.ToLower(replicaTypeMaster))
	assert.NotNil(t, masterSubGroup)

	workerSegment0 := findSubGroupByName(metadata.SubGroups, "worker-0")
	assert.NotNil(t, workerSegment0)
	assert.Equal(t, 1, len(workerSegment0.PodsReferences))
	assert.Equal(t, int32(2), workerSegment0.MinAvailable)

	workerSegment1 := findSubGroupByName(metadata.SubGroups, "worker-1")
	assert.NotNil(t, workerSegment1)
	assert.Equal(t, 0, len(workerSegment1.PodsReferences))
	assert.Equal(t, int32(2), workerSegment1.MinAvailable)
	workerSegment2 := findSubGroupByName(metadata.SubGroups, "worker-2")
	assert.NotNil(t, workerSegment2)
	assert.Equal(t, 0, len(workerSegment2.PodsReferences))
	assert.Equal(t, int32(1), workerSegment2.MinAvailable)

	workerPod4 := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-job-worker-4",
			Namespace: "test_namespace",
			Labels: map[string]string{
				replicaTypeLabel:                      "worker",
				"training.kubeflow.org/replica-index": "4",
			},
			Annotations: map[string]string{
				"kai.scheduler/segment-size": "2",
			},
		},
	}
	metadata, err = grouper.GetPodGroupMetadata(pytorchJob, workerPod4)
	assert.Nil(t, err)
	workerSegment2 = findSubGroupByName(metadata.SubGroups, "worker-2")
	assert.NotNil(t, workerSegment2)
	assert.Equal(t, 1, len(workerSegment2.PodsReferences))
	assert.Equal(t, "test-job-worker-4", workerSegment2.PodsReferences[0])
}

func TestGetPodGroupMetadata_Segments_MalformedAnnotation(t *testing.T) {
	pytorchJob := getPytorchJobWithSegments(1, 4, "invalid")
	grouper := newTestPyTorchGrouper()

	workerPod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-job-worker-0",
			Namespace: "test_namespace",
			Labels: map[string]string{
				replicaTypeLabel:                      "worker",
				"training.kubeflow.org/replica-index": "0",
			},
			Annotations: map[string]string{
				"kai.scheduler/segment-size": "not-a-number",
			},
		},
	}

	_, err := grouper.GetPodGroupMetadata(pytorchJob, workerPod)
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "invalid segment size")
}

func TestGetPodGroupMetadata_Segments_MalformedAnnotationInTemplate(t *testing.T) {
	pytorchJob := getPytorchJobWithSegments(1, 4, "invalid-size")
	grouper := newTestPyTorchGrouper()

	// Pod without segment annotation, relies on template annotation
	workerPod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-job-worker-0",
			Namespace: "test_namespace",
			Labels: map[string]string{
				replicaTypeLabel:                      "worker",
				"training.kubeflow.org/replica-index": "0",
			},
		},
	}

	_, err := grouper.GetPodGroupMetadata(pytorchJob, workerPod)
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "invalid segment size")
}

func TestGetPodGroupMetadata_Segments_SegmentSizeFromPodTemplate(t *testing.T) {
	pytorchJob := getPytorchJobWithSegments(1, 4, "2")
	grouper := newTestPyTorchGrouper()

	// Worker pod without segment annotation - should use template annotation
	workerPod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-job-worker-1",
			Namespace: "test_namespace",
			Labels: map[string]string{
				replicaTypeLabel:                      "worker",
				"training.kubeflow.org/replica-index": "1",
			},
		},
	}

	metadata, err := grouper.GetPodGroupMetadata(pytorchJob, workerPod)
	assert.Nil(t, err)

	// Should have 4 subgroups: master + Worker parent + 2 worker segments
	assert.Equal(t, 4, len(metadata.SubGroups))

	// Worker 1 should be in segment 0 (index 1 / segment_size 2 = 0)
	workerSegment0 := findSubGroupByName(metadata.SubGroups, "worker-0")
	assert.NotNil(t, workerSegment0)
	assert.Equal(t, 1, len(workerSegment0.PodsReferences))
	assert.Equal(t, "test-job-worker-1", workerSegment0.PodsReferences[0])
}

func getPytorchJobWithSegments(masterReplicas, workerReplicas int64, segmentSize string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "PyTorchJob",
			"apiVersion": "kubeflow.org/v1",
			"metadata": map[string]interface{}{
				"name":      "test-job-segments",
				"namespace": "test_namespace",
				"uid":       "seg-1",
			},
			"spec": map[string]interface{}{
				"pytorchReplicaSpecs": map[string]interface{}{
					"Master": map[string]interface{}{
						"replicas": masterReplicas,
					},
					"Worker": map[string]interface{}{
						"replicas": workerReplicas,
						"template": map[string]interface{}{
							"metadata": map[string]interface{}{
								"annotations": map[string]interface{}{
									"kai.scheduler/segment-size": segmentSize,
								},
							},
						},
					},
				},
			},
		},
	}
}

type segmentTopologyConfig struct {
	topology           string
	requiredPlacement  string
	preferredPlacement string
}

func getPytorchJobWithSegmentTopology(
	masterReplicas, workerReplicas int64, segmentSize string, topologyConfig segmentTopologyConfig, topologyOnRoot bool,
) *unstructured.Unstructured {
	workerAnnotations := map[string]interface{}{
		"kai.scheduler/segment-size": segmentSize,
	}
	if topologyConfig.requiredPlacement != "" {
		workerAnnotations["kai.scheduler/segment-topology-required-placement"] = topologyConfig.requiredPlacement
	}
	if topologyConfig.preferredPlacement != "" {
		workerAnnotations["kai.scheduler/segment-topology-preferred-placement"] = topologyConfig.preferredPlacement
	}
	if !topologyOnRoot && topologyConfig.topology != "" {
		workerAnnotations["kai.scheduler/topology"] = topologyConfig.topology
	}

	metadata := map[string]interface{}{
		"name":      "test-job-topology",
		"namespace": "test_namespace",
		"uid":       "topo-1",
	}
	if topologyOnRoot && topologyConfig.topology != "" {
		metadata["annotations"] = map[string]interface{}{
			"kai.scheduler/topology": topologyConfig.topology,
		}
	}

	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "PyTorchJob",
			"apiVersion": "kubeflow.org/v1",
			"metadata":   metadata,
			"spec": map[string]interface{}{
				"pytorchReplicaSpecs": map[string]interface{}{
					"Master": map[string]interface{}{
						"replicas": masterReplicas,
					},
					"Worker": map[string]interface{}{
						"replicas": workerReplicas,
						"template": map[string]interface{}{
							"metadata": map[string]interface{}{
								"annotations": workerAnnotations,
							},
						},
					},
				},
			},
		},
	}
}

func TestGetPodGroupMetadata_Segments_WithTopologyConstraints(t *testing.T) {
	pytorchJob := getPytorchJobWithSegmentTopology(1, 4, "2", segmentTopologyConfig{
		topology:           "cluster-topology",
		requiredPlacement:  "rack",
		preferredPlacement: "node",
	}, false)
	grouper := newTestPyTorchGrouper()

	workerPod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-job-worker-0",
			Namespace: "test_namespace",
			Labels: map[string]string{
				replicaTypeLabel:                      "worker",
				"training.kubeflow.org/replica-index": "0",
			},
			Annotations: map[string]string{
				"kai.scheduler/segment-size":                         "2",
				"kai.scheduler/topology":                             "cluster-topology",
				"kai.scheduler/segment-topology-required-placement":  "rack",
				"kai.scheduler/segment-topology-preferred-placement": "node",
			},
		},
	}

	metadata, err := grouper.GetPodGroupMetadata(pytorchJob, workerPod)
	assert.Nil(t, err)
	assert.Equal(t, 4, len(metadata.SubGroups))

	workerSegment0 := findSubGroupByName(metadata.SubGroups, "worker-0")
	assert.NotNil(t, workerSegment0)
	assert.NotNil(t, workerSegment0.TopologyConstraints)
	assert.Equal(t, "cluster-topology", workerSegment0.TopologyConstraints.Topology)
	assert.Equal(t, "rack", workerSegment0.TopologyConstraints.RequiredTopologyLevel)
	assert.Equal(t, "node", workerSegment0.TopologyConstraints.PreferredTopologyLevel)

	workerSegment1 := findSubGroupByName(metadata.SubGroups, "worker-1")
	assert.NotNil(t, workerSegment1)
	assert.NotNil(t, workerSegment1.TopologyConstraints)
	assert.Equal(t, "cluster-topology", workerSegment1.TopologyConstraints.Topology)
	assert.Equal(t, "rack", workerSegment1.TopologyConstraints.RequiredTopologyLevel)
	assert.Equal(t, "node", workerSegment1.TopologyConstraints.PreferredTopologyLevel)
}

func TestGetPodGroupMetadata_Segments_TopologyFromRootAnnotation(t *testing.T) {
	pytorchJob := getPytorchJobWithSegmentTopology(1, 4, "2", segmentTopologyConfig{
		topology:          "cluster-topology",
		requiredPlacement: "rack",
	}, true)
	grouper := newTestPyTorchGrouper()

	workerPod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-job-worker-0",
			Namespace: "test_namespace",
			Labels: map[string]string{
				replicaTypeLabel:                      "worker",
				"training.kubeflow.org/replica-index": "0",
			},
			Annotations: map[string]string{
				"kai.scheduler/segment-size":                        "2",
				"kai.scheduler/segment-topology-required-placement": "rack",
			},
		},
	}

	metadata, err := grouper.GetPodGroupMetadata(pytorchJob, workerPod)
	assert.Nil(t, err)
	assert.Equal(t, 4, len(metadata.SubGroups))

	workerSegment0 := findSubGroupByName(metadata.SubGroups, "worker-0")
	assert.NotNil(t, workerSegment0)
	assert.NotNil(t, workerSegment0.TopologyConstraints)
	assert.Equal(t, "cluster-topology", workerSegment0.TopologyConstraints.Topology)
	assert.Equal(t, "rack", workerSegment0.TopologyConstraints.RequiredTopologyLevel)
}

func TestGetPodGroupMetadata_Segments_NoTopologyWhenMissing(t *testing.T) {
	pytorchJob := getPytorchJobWithSegmentTopology(1, 4, "2", segmentTopologyConfig{
		topology:          "",
		requiredPlacement: "rack",
	}, false)
	grouper := newTestPyTorchGrouper()

	workerPod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-job-worker-0",
			Namespace: "test_namespace",
			Labels: map[string]string{
				replicaTypeLabel:                      "worker",
				"training.kubeflow.org/replica-index": "0",
			},
			Annotations: map[string]string{
				"kai.scheduler/segment-size":                        "2",
				"kai.scheduler/segment-topology-required-placement": "rack",
			},
		},
	}

	metadata, err := grouper.GetPodGroupMetadata(pytorchJob, workerPod)
	assert.Nil(t, err)
	assert.Equal(t, 4, len(metadata.SubGroups))

	workerSegment0 := findSubGroupByName(metadata.SubGroups, "worker-0")
	assert.NotNil(t, workerSegment0)
	assert.Nil(t, workerSegment0.TopologyConstraints)
}

func TestGetPodGroupMetadata_Segments_OnlyRequiredPlacement(t *testing.T) {
	pytorchJob := getPytorchJobWithSegmentTopology(1, 4, "2", segmentTopologyConfig{
		topology:          "cluster-topology",
		requiredPlacement: "rack",
	}, false)
	grouper := newTestPyTorchGrouper()

	workerPod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-job-worker-0",
			Namespace: "test_namespace",
			Labels: map[string]string{
				replicaTypeLabel:                      "worker",
				"training.kubeflow.org/replica-index": "0",
			},
			Annotations: map[string]string{
				"kai.scheduler/segment-size":                        "2",
				"kai.scheduler/topology":                            "cluster-topology",
				"kai.scheduler/segment-topology-required-placement": "rack",
			},
		},
	}

	metadata, err := grouper.GetPodGroupMetadata(pytorchJob, workerPod)
	assert.Nil(t, err)

	workerSegment0 := findSubGroupByName(metadata.SubGroups, "worker-0")
	assert.NotNil(t, workerSegment0)
	assert.NotNil(t, workerSegment0.TopologyConstraints)
	assert.Equal(t, "cluster-topology", workerSegment0.TopologyConstraints.Topology)
	assert.Equal(t, "rack", workerSegment0.TopologyConstraints.RequiredTopologyLevel)
	assert.Equal(t, "", workerSegment0.TopologyConstraints.PreferredTopologyLevel)
}

func TestGetPodGroupMetadata_Segments_OnlyPreferredPlacement(t *testing.T) {
	pytorchJob := getPytorchJobWithSegmentTopology(1, 4, "2", segmentTopologyConfig{
		topology:           "cluster-topology",
		preferredPlacement: "node",
	}, false)
	grouper := newTestPyTorchGrouper()

	workerPod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-job-worker-0",
			Namespace: "test_namespace",
			Labels: map[string]string{
				replicaTypeLabel:                      "worker",
				"training.kubeflow.org/replica-index": "0",
			},
			Annotations: map[string]string{
				"kai.scheduler/segment-size":                         "2",
				"kai.scheduler/topology":                             "cluster-topology",
				"kai.scheduler/segment-topology-preferred-placement": "node",
			},
		},
	}

	metadata, err := grouper.GetPodGroupMetadata(pytorchJob, workerPod)
	assert.Nil(t, err)

	workerSegment0 := findSubGroupByName(metadata.SubGroups, "worker-0")
	assert.NotNil(t, workerSegment0)
	assert.NotNil(t, workerSegment0.TopologyConstraints)
	assert.Equal(t, "cluster-topology", workerSegment0.TopologyConstraints.Topology)
	assert.Equal(t, "", workerSegment0.TopologyConstraints.RequiredTopologyLevel)
	assert.Equal(t, "node", workerSegment0.TopologyConstraints.PreferredTopologyLevel)
}

func TestGetPodGroupMetadata_Segments_TopologyFromTemplate(t *testing.T) {
	pytorchJob := getPytorchJobWithSegmentTopology(1, 4, "2", segmentTopologyConfig{
		topology:          "cluster-topology",
		requiredPlacement: "rack",
	}, false)
	grouper := newTestPyTorchGrouper()

	workerPod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-job-worker-0",
			Namespace: "test_namespace",
			Labels: map[string]string{
				replicaTypeLabel:                      "worker",
				"training.kubeflow.org/replica-index": "0",
			},
		},
	}

	metadata, err := grouper.GetPodGroupMetadata(pytorchJob, workerPod)
	assert.Nil(t, err)
	assert.Equal(t, 4, len(metadata.SubGroups))

	workerSegment0 := findSubGroupByName(metadata.SubGroups, "worker-0")
	assert.NotNil(t, workerSegment0)
	assert.NotNil(t, workerSegment0.TopologyConstraints)
	assert.Equal(t, "cluster-topology", workerSegment0.TopologyConstraints.Topology)
	assert.Equal(t, "rack", workerSegment0.TopologyConstraints.RequiredTopologyLevel)
}
