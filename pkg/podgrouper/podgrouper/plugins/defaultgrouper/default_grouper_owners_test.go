// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package defaultgrouper

import (
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	queueLabelKey    = "kai.scheduler/queue"
	nodePoolLabelKey = "kai.scheduler/node-pool"
)

func TestGetPodGroupMetadata_KubeflowPipelineScheduledWorkflow(t *testing.T) {
	owner := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "ScheduledWorkflow",
			"apiVersion": "kubeflow.org/v1beta1",
			"metadata": map[string]interface{}{
				"name":      "test_name",
				"namespace": "test_namespace",
				"uid":       "1",
				"labels": map[string]interface{}{
					"test_label": "test_value",
					"scheduledworkflows.kubeflow.org/enabled": "true",
					"scheduledworkflows.kubeflow.org/status":  "Enabled",
				},
				"annotations": map[string]interface{}{
					"test_annotation": "test_value",
				},
			},
			"spec": map[string]interface{}{
				"enabled":        true,
				"maxConcurrency": 10,
				"noCatchup":      false,
				"trigger": map[string]interface{}{
					"periodicSchedule": map[string]interface{}{
						"intervalSecond": 300,
					},
				},
				"workflow": map[string]interface{}{
					"parameters": []interface{}{
						map[string]interface{}{
							"name":  "recipient",
							"value": "\"cron\"",
						},
					},
					"spec": "argo_workflow_json-spec",
				},
			},
		},
	}
	pod := &v1.Pod{}

	defaultGrouper := NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, fake.NewFakeClient())
	podGroupMetadata, err := defaultGrouper.GetPodGroupMetadata(owner, pod)

	assert.Nil(t, err)
	assert.Equal(t, "ScheduledWorkflow", podGroupMetadata.Owner.Kind)
	assert.Equal(t, "kubeflow.org/v1beta1", podGroupMetadata.Owner.APIVersion)
	assert.Equal(t, "1", string(podGroupMetadata.Owner.UID))
	assert.Equal(t, "test_name", podGroupMetadata.Owner.Name)
	assert.Equal(t, 2, len(podGroupMetadata.Annotations))
	assert.Equal(t, 3, len(podGroupMetadata.Labels))
	assert.Equal(t, "default-queue", podGroupMetadata.Queue)
	assert.Equal(t, "train", podGroupMetadata.PriorityClassName)
	assert.Equal(t, int32(1), podGroupMetadata.MinAvailable)
}

func TestGetPodGroupMetadata_ArgoWorkflow(t *testing.T) {
	owner := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "Workflow",
			"apiVersion": "argoproj.io/v1alpha1",
			"metadata": map[string]interface{}{
				"name":      "test_name",
				"namespace": "test_namespace",
				"uid":       "1",
				"labels": map[string]interface{}{
					"test_label": "test_value",
					"scheduledworkflows.kubeflow.org/isOwnedByScheduledWorkflow": "true",
					"workflows.argoproj.io/phase":                                "Succeeded",
				},
				"annotations": map[string]interface{}{
					"test_annotation": "test_value",
					"pipelines.kubeflow.org/components-comp-say-hello": "{\"executorLabel\":\"exec-say-hello\",\"inputDefinitions\":{\"parameters\":{\"name\":{\"parameterType\":\"STRING\"}}},\"outputDefinitions\":{\"parameters\":{\"Output\":{\"parameterType\":\"STRING\"}}}}",
				},
			},
			"spec": map[string]interface{}{
				"arguments":  map[string]interface{}{},
				"entrypoint": "entrypoint",
				"podMetadata": map[string]interface{}{
					"annotations": map[string]interface{}{
						"pipelines.kubeflow.org/v2_component": "true",
					},
					"labels": map[string]interface{}{
						"pipeline/runid":                      "feeccdcb-2482-434b-9108-4783eb56a677",
						"pipelines.kubeflow.org/v2_component": "true",
					},
				},
				"serviceAccountName": "pipeline-runner",
				"templates": []interface{}{
					map[string]interface{}{
						"container": map[string]interface{}{
							"args": []interface{}{
								"--type",
								"CONTAINER",
								"--pipeline_name",
								"hello-pipeline",
							},
							"command": []interface{}{
								"driver",
							},
							"image": "gcr.io/ml-pipeline/kfp-driver@sha256:8e60086b04d92b657898a310ca9757631d58547e76bbbb8bfc376d654bef1707",
						},
						"dag": map[string]interface{}{
							"tasks": map[string]interface{}{},
							"name":  "system-container-executor",
						},
					},
				},
			},
		},
	}
	pod := &v1.Pod{}

	defaultGrouper := NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, fake.NewFakeClient())
	podGroupMetadata, err := defaultGrouper.GetPodGroupMetadata(owner, pod)

	assert.Nil(t, err)
	assert.Equal(t, "Workflow", podGroupMetadata.Owner.Kind)
	assert.Equal(t, "argoproj.io/v1alpha1", podGroupMetadata.Owner.APIVersion)
	assert.Equal(t, "1", string(podGroupMetadata.Owner.UID))
	assert.Equal(t, "test_name", podGroupMetadata.Owner.Name)
	assert.Equal(t, 3, len(podGroupMetadata.Annotations))
	assert.Equal(t, 3, len(podGroupMetadata.Labels))
	assert.Equal(t, "default-queue", podGroupMetadata.Queue)
	assert.Equal(t, "train", podGroupMetadata.PriorityClassName)
	assert.Equal(t, int32(1), podGroupMetadata.MinAvailable)
}

func TestGetPodGroupMetadata_Tekton_TaskRun(t *testing.T) {
	owner := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "TaskRun",
			"apiVersion": "tekton.dev/v1",
			"metadata": map[string]interface{}{
				"name":      "test_name",
				"namespace": "test_namespace",
				"uid":       "1",
				"labels": map[string]interface{}{
					"test_label":      "test_value",
					"tekton.dev/task": "hello",
				},
				"annotations": map[string]interface{}{
					"test_annotation":             "test_value",
					"pipeline.tekton.dev/release": "e59ee42",
				},
			},
			"spec": map[string]interface{}{
				"arguments":          map[string]interface{}{},
				"serviceAccountName": "default",
				"taskRef": map[string]interface{}{
					"kind": "Task",
					"name": "hello",
				},
				"timeout": "1h0m0s",
			},
			"status": map[string]interface{}{
				"podName": "hello-task-run-pod",
				"provenance": map[string]interface{}{
					"featureFlags": map[string]interface{}{
						"AwaitSidecarReadiness": true,
						"Coschedule":            "workspaces",
					},
				},
				"steps": []interface{}{
					map[string]interface{}{
						"container": "step-echo",
						"imageID":   "docker.io/library/alpine@sha256:51b67269f354137895d43f3b3d810bfacd3945438e94dc5ac55fdac340352f48",
					},
				},
				"taskSpec": map[string]interface{}{
					"steps": []interface{}{
						map[string]interface{}{
							"computeResources": map[string]interface{}{},
							"image":            "alpine",
						},
					},
				},
			},
		},
	}
	pod := &v1.Pod{}

	defaultGrouper := NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, fake.NewFakeClient())
	podGroupMetadata, err := defaultGrouper.GetPodGroupMetadata(owner, pod)

	assert.Nil(t, err)
	assert.Equal(t, "TaskRun", podGroupMetadata.Owner.Kind)
	assert.Equal(t, "tekton.dev/v1", podGroupMetadata.Owner.APIVersion)
	assert.Equal(t, "1", string(podGroupMetadata.Owner.UID))
	assert.Equal(t, "test_name", podGroupMetadata.Owner.Name)
	assert.Equal(t, 3, len(podGroupMetadata.Annotations))
	assert.Equal(t, 2, len(podGroupMetadata.Labels))
	assert.Equal(t, "default-queue", podGroupMetadata.Queue)
	assert.Equal(t, "train", podGroupMetadata.PriorityClassName)
	assert.Equal(t, int32(1), podGroupMetadata.MinAvailable)
}

func TestGetPodGroupMetadata_Tekton_PipelineRun(t *testing.T) {
	owner := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "PipelineRun",
			"apiVersion": "tekton.dev/v1",
			"metadata": map[string]interface{}{
				"name":      "test_name",
				"namespace": "test_namespace",
				"uid":       "1",
				"labels": map[string]interface{}{
					"test_label":      "test_value",
					"tekton.dev/task": "hello",
				},
				"annotations": map[string]interface{}{
					"test_annotation": "test_value",
				},
			},
			"spec": map[string]interface{}{
				"params": []interface{}{
					map[string]interface{}{
						"name":  "username",
						"value": "Tekton",
					},
				},
				"taskRunTemplate": map[string]interface{}{
					"serviceAccountName": "default",
				},
				"timeouts": map[string]interface{}{
					"pipeline": "1h0m0s",
				},
			},
			"status": map[string]interface{}{
				"podName": "hello-task-run-pod",
				"provenance": map[string]interface{}{
					"featureFlags": map[string]interface{}{
						"AwaitSidecarReadiness": true,
						"Coschedule":            "workspaces",
					},
				},
				"steps": []interface{}{
					map[string]interface{}{
						"container": "step-echo",
						"imageID":   "docker.io/library/alpine@sha256:51b67269f354137895d43f3b3d810bfacd3945438e94dc5ac55fdac340352f48",
					},
				},
				"pipelineSpec": map[string]interface{}{
					"params": []interface{}{
						map[string]interface{}{
							"name":  "username",
							"value": "Tekton",
						},
					},
					"tasks": []interface{}{
						map[string]interface{}{
							"name": "hello",
							"taskRef": map[string]interface{}{
								"kind": "Task",
								"name": "hello",
							},
						},
						map[string]interface{}{
							"name": "goodbye",
							"params": []interface{}{
								map[string]interface{}{
									"name":  "username",
									"value": "Tekton",
								},
							},
							"runAfter": []interface{}{
								"hello",
							},
							"taskRef": map[string]interface{}{
								"kind": "Task",
								"name": "goodbye",
							},
						},
					},
				},
			},
		},
	}
	pod := &v1.Pod{}

	defaultGrouper := NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, fake.NewFakeClient())
	podGroupMetadata, err := defaultGrouper.GetPodGroupMetadata(owner, pod)

	assert.Nil(t, err)
	assert.Equal(t, "PipelineRun", podGroupMetadata.Owner.Kind)
	assert.Equal(t, "tekton.dev/v1", podGroupMetadata.Owner.APIVersion)
	assert.Equal(t, "1", string(podGroupMetadata.Owner.UID))
	assert.Equal(t, "test_name", podGroupMetadata.Owner.Name)
	assert.Equal(t, 2, len(podGroupMetadata.Annotations))
	assert.Equal(t, 2, len(podGroupMetadata.Labels))
	assert.Equal(t, "default-queue", podGroupMetadata.Queue)
	assert.Equal(t, "train", podGroupMetadata.PriorityClassName)
	assert.Equal(t, int32(1), podGroupMetadata.MinAvailable)
}
