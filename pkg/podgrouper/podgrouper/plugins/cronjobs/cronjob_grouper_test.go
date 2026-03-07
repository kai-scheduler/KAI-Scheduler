// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package cronjobs

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v12 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kai-scheduler/kai-scheduler/pkg/podgrouper/podgrouper/plugins/defaultgrouper"
)

const (
	podName          = "my-pod"
	jobName          = "cron-27958584"
	jobUID           = types.UID("123456789")
	cronjobName      = "cron"
	queueLabelKey    = "kai.scheduler/queue"
	nodePoolLabelKey = "kai.scheduler/node-pool"
)

func TestGetPodGroupMetadata(t *testing.T) {
	cronjob := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "CronJob",
			"apiVersion": "batch/v1",
			"metadata": map[string]interface{}{
				"name":      cronjobName,
				"namespace": "test_namespace",
				"uid":       "1",
			},
		},
	}
	pod := &v1.Pod{
		ObjectMeta: v12.ObjectMeta{
			Name: podName,
			OwnerReferences: []metav1.OwnerReference{
				{
					Name:       jobName,
					Kind:       "Job",
					UID:        jobUID,
					APIVersion: "batch/v1",
				},
			},
		},
	}
	job := &batchv1.Job{
		ObjectMeta: v12.ObjectMeta{
			Name: jobName,
			UID:  jobUID,
		},
	}

	schema := runtime.NewScheme()
	err := batchv1.AddToScheme(schema)
	assert.Nil(t, err)

	client := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(job).Build()
	grouper := NewCronJobGrouper(client, defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client))
	podGroupMetadata, err := grouper.GetPodGroupMetadata(cronjob, pod)

	assert.Nil(t, err)
	assert.Equal(t, fmt.Sprintf("pg-%s-%s", jobName, jobUID), podGroupMetadata.Name)
	assert.Equal(t, "Job", podGroupMetadata.Owner.Kind)
	assert.Equal(t, jobName, podGroupMetadata.Owner.Name)
	assert.Equal(t, jobUID, podGroupMetadata.Owner.UID)
	assert.Equal(t, "batch/v1", podGroupMetadata.Owner.APIVersion)
}

func TestGetPodGroupMetadataJobOwnerNotFound(t *testing.T) {
	cronjob := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "CronJob",
			"apiVersion": "batch/v1",
			"metadata": map[string]interface{}{
				"name":      cronjobName,
				"namespace": "test_namespace",
				"uid":       "1",
			},
		},
	}
	pod := &v1.Pod{
		ObjectMeta: v12.ObjectMeta{
			Name: podName,
			OwnerReferences: []metav1.OwnerReference{
				{
					Name:       jobName,
					Kind:       "CronJob",
					UID:        jobUID,
					APIVersion: "batch/v1",
				},
			},
		},
	}

	client := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(cronjob).Build()
	grouper := NewCronJobGrouper(client, defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client))
	podGroupMetadata, err := grouper.GetPodGroupMetadata(cronjob, pod)

	assert.NotNil(t, err)
	assert.Equal(t, "no owner reference of type 'Job'", err.Error())
	assert.Nil(t, podGroupMetadata)
}

func TestGetPodGroupMetadataJobNotExists(t *testing.T) {
	cronjob := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "CronJob",
			"apiVersion": "batch/v1",
			"metadata": map[string]interface{}{
				"name":      cronjobName,
				"namespace": "test_namespace",
				"uid":       "1",
			},
		},
	}
	pod := &v1.Pod{
		ObjectMeta: v12.ObjectMeta{
			Name: podName,
			OwnerReferences: []metav1.OwnerReference{
				{
					Name:       jobName,
					Kind:       "Job",
					UID:        jobUID,
					APIVersion: "batch/v1",
				},
			},
		},
	}

	schema := runtime.NewScheme()
	err := batchv1.AddToScheme(schema)
	assert.Nil(t, err)

	client := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(cronjob).Build()
	grouper := NewCronJobGrouper(client, defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, client))
	podGroupMetadata, err := grouper.GetPodGroupMetadata(cronjob, pod)
	assert.NotNil(t, err)
	assert.Equal(t, fmt.Sprintf("jobs.batch \"%s\" not found", jobName), err.Error())
	assert.Nil(t, podGroupMetadata)
}
