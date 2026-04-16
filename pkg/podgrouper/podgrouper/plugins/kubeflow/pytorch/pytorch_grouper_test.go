// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package pytorch

import (
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

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

func TestGetPodGroupMetadata_OnlyReplicas(t *testing.T) {
	pytorchJob := getBasicPytorchJob()
	pod := &v1.Pod{}
	defaultGrouper := defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, fake.NewFakeClient())
	kubeFlowGrouper := kubeflow.NewKubeflowDistributedGrouper(defaultGrouper)
	grouper := NewPyTorchGrouper(kubeFlowGrouper)
	metadata, err := grouper.GetPodGroupMetadata(pytorchJob, pod)
	assert.Nil(t, err, "Got error when getting pytorch pod group metadata")
	assert.EqualValues(t, workerReplicasNum+masterReplicasNum, metadata.MinAvailable)
}

func TestGetPodGroupMetadata_OnlyMinReplicas(t *testing.T) {
	pytorchJob := getBasicPytorchJob()
	err := unstructured.SetNestedField(pytorchJob.Object, int64(minReplicasNum), "spec", "elasticPolicy", "minReplicas")
	assert.Nil(t, err, "Got error when setting minReplicas for pytorch job")

	pod := &v1.Pod{}
	defaultGrouper := defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, fake.NewFakeClient())
	kubeFlowGrouper := kubeflow.NewKubeflowDistributedGrouper(defaultGrouper)
	grouper := NewPyTorchGrouper(kubeFlowGrouper)
	metadata, err := grouper.GetPodGroupMetadata(pytorchJob, pod)
	assert.Nil(t, err, "Got error when getting pytorch pod group metadata")
	assert.EqualValues(t, minReplicasNum, metadata.MinAvailable)
}

func TestGetPodGroupMetadata_OnlyMinAvailable(t *testing.T) {
	pytorchJob := getBasicPytorchJob()

	err := unstructured.SetNestedField(pytorchJob.Object, int64(minAvailableNum), "spec", "runPolicy", "schedulingPolicy", "minAvailable")
	assert.Nil(t, err, "Got error when setting minAvailable for pytorch job")

	pod := &v1.Pod{}
	defaultGrouper := defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, fake.NewFakeClient())
	kubeFlowGrouper := kubeflow.NewKubeflowDistributedGrouper(defaultGrouper)
	grouper := NewPyTorchGrouper(kubeFlowGrouper)
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
	defaultGrouper := defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, fake.NewFakeClient())
	kubeFlowGrouper := kubeflow.NewKubeflowDistributedGrouper(defaultGrouper)
	grouper := NewPyTorchGrouper(kubeFlowGrouper)
	metadata, err := grouper.GetPodGroupMetadata(pytorchJob, pod)
	assert.Nil(t, err, "Got error when getting pytorch pod group metadata")
	assert.EqualValues(t, minAvailableNum, metadata.MinAvailable)
}

// TestGetPodGroupMetadata_OnlyMasterReplicas tests a PyTorch job with only Master replicas and no Worker replicas
func TestGetPodGroupMetadata_OnlyMasterReplicas(t *testing.T) {
	pytorchJob := getPytorchJobWithOnlyMaster()
	pod := &v1.Pod{}
	defaultGrouper := defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, fake.NewFakeClient())
	kubeFlowGrouper := kubeflow.NewKubeflowDistributedGrouper(defaultGrouper)
	grouper := NewPyTorchGrouper(kubeFlowGrouper)
	metadata, err := grouper.GetPodGroupMetadata(pytorchJob, pod)
	assert.Nil(t, err, "Got error when getting pytorch pod group metadata")
	assert.EqualValues(t, masterReplicasNum, metadata.MinAvailable)
}

// TestGetPodGroupMetadata_OnlyWorkerReplicas tests a PyTorch job with only Worker replicas and no Master replicas
func TestGetPodGroupMetadata_OnlyWorkerReplicas(t *testing.T) {
	pytorchJob := getPytorchJobWithOnlyWorker()
	pod := &v1.Pod{}
	defaultGrouper := defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, fake.NewFakeClient())
	kubeFlowGrouper := kubeflow.NewKubeflowDistributedGrouper(defaultGrouper)
	grouper := NewPyTorchGrouper(kubeFlowGrouper)
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
	defaultGrouper := defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, fake.NewFakeClient())
	kubeFlowGrouper := kubeflow.NewKubeflowDistributedGrouper(defaultGrouper)
	grouper := NewPyTorchGrouper(kubeFlowGrouper)
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
	defaultGrouper := defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, fake.NewFakeClient())
	kubeFlowGrouper := kubeflow.NewKubeflowDistributedGrouper(defaultGrouper)
	grouper := NewPyTorchGrouper(kubeFlowGrouper)
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
	defaultGrouper := defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, fake.NewFakeClient())
	kubeFlowGrouper := kubeflow.NewKubeflowDistributedGrouper(defaultGrouper)
	grouper := NewPyTorchGrouper(kubeFlowGrouper)
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
	defaultGrouper := defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, fake.NewFakeClient())
	kubeFlowGrouper := kubeflow.NewKubeflowDistributedGrouper(defaultGrouper)
	grouper := NewPyTorchGrouper(kubeFlowGrouper)
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
