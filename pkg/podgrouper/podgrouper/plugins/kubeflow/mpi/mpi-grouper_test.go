// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package mpi

import (
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kai-scheduler/kai-scheduler/pkg/podgrouper/podgroup"
	"github.com/kai-scheduler/kai-scheduler/pkg/podgrouper/podgrouper/plugins/defaultgrouper"
	"github.com/kai-scheduler/kai-scheduler/pkg/podgrouper/podgrouper/plugins/kubeflow"
)

const (
	queueLabelKey    = "kai.scheduler/queue"
	nodePoolLabelKey = "kai.scheduler/node-pool"
)

func TestLauncherPodExists(t *testing.T) {
	tests := []struct {
		name           string
		existingPods   []client.Object
		expectedResult bool
		expectedError  bool
	}{
		{
			name: "No launcher pods exist",
			existingPods: []client.Object{
				&v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "worker-1",
						Namespace: "test-ns",
						Labels: map[string]string{
							jobNameLabel:     "test-job",
							podRoleNameLabel: "worker",
						},
					},
				},
			},
			expectedResult: false,
			expectedError:  false,
		},
		{
			name: "Launcher pod exists and is not terminating",
			existingPods: []client.Object{
				&v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "launcher",
						Namespace: "test-ns",
						Labels: map[string]string{
							jobNameLabel:     "test-job",
							podRoleNameLabel: launcherPodRoleName,
						},
					},
				},
			},
			expectedResult: true,
			expectedError:  false,
		},
		{
			name: "Launcher pod exists and is terminating",
			existingPods: []client.Object{
				&v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "launcher",
						Namespace:         "test-ns",
						DeletionTimestamp: &metav1.Time{Time: time.Now()},
						Finalizers:        []string{"mpi-launcher"},
						Labels: map[string]string{
							jobNameLabel:     "test-job",
							podRoleNameLabel: launcherPodRoleName,
						},
					},
				},
			},
			expectedResult: false,
			expectedError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			_ = v1.AddToScheme(scheme)
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.existingPods...).Build()

			defaultGrouper := defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, fakeClient)
			kubeFlowGrouper := kubeflow.NewKubeflowDistributedGrouper(defaultGrouper)
			mg := NewMpiGrouper(fakeClient, kubeFlowGrouper)
			topOwner := &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"name":      "test-job",
						"namespace": "test-ns",
					},
				},
			}

			result, err := mg.launcherPodExists(topOwner)
			if (err != nil) != tt.expectedError {
				t.Errorf("launcherPodExists() error = %v, expectedError %v", err, tt.expectedError)
				return
			}
			if result != tt.expectedResult {
				t.Errorf("launcherPodExists() = %v, want %v", result, tt.expectedResult)
			}
		})
	}
}

func TestHandleDelayedLauncherPolicy(t *testing.T) {
	tests := []struct {
		name             string
		topOwner         *unstructured.Unstructured
		initialMinAvail  int32
		expectedMinAvail int32
		existingPods     []client.Object
		expectedError    bool
	}{
		{
			name: "Launcher exists, no change to minAvailable",
			topOwner: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"name":      "test-job",
						"namespace": "test-ns",
					},
					"spec": map[string]interface{}{
						"launcherCreationPolicy": delayedLauncherCreationPolicy,
						ReplicaSpecName: map[string]interface{}{
							MasterName: map[string]interface{}{
								"replicas": int64(1),
							},
						},
					},
				},
			},
			initialMinAvail:  5,
			expectedMinAvail: 5,
			existingPods: []client.Object{
				&v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "launcher",
						Namespace: "test-ns",
						Labels: map[string]string{
							jobNameLabel:     "test-job",
							podRoleNameLabel: launcherPodRoleName,
						},
					},
				},
			},
			expectedError: false,
		},
		{
			name: "Launcher doesn't exist, reduce minAvailable",
			topOwner: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"name":      "test-job",
						"namespace": "test-ns",
					},
					"spec": map[string]interface{}{
						"launcherCreationPolicy": delayedLauncherCreationPolicy,
						ReplicaSpecName: map[string]interface{}{
							MasterName: map[string]interface{}{
								"replicas": int64(1),
							},
						},
					},
				},
			},
			initialMinAvail:  5,
			expectedMinAvail: 4,
			existingPods:     []client.Object{},
			expectedError:    false,
		},
		{
			name: "Invalid replicas configuration",
			topOwner: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"name":      "test-job",
						"namespace": "test-ns",
					},
					"spec": map[string]interface{}{
						"launcherCreationPolicy": delayedLauncherCreationPolicy,
						ReplicaSpecName: map[string]interface{}{
							MasterName: map[string]interface{}{
								"replicas": int64(0),
							},
						},
					},
				},
			},
			initialMinAvail:  5,
			expectedMinAvail: 5,
			existingPods:     []client.Object{},
			expectedError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			_ = v1.AddToScheme(scheme)
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.existingPods...).Build()

			defaultGrouper := defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, fakeClient)
			kubeFlowGrouper := kubeflow.NewKubeflowDistributedGrouper(defaultGrouper)
			mg := NewMpiGrouper(fakeClient, kubeFlowGrouper)
			gp := &podgroup.Metadata{
				MinAvailable: tt.initialMinAvail,
			}

			err := mg.handleDelayedLauncherPolicy(tt.topOwner, gp)
			if (err != nil) != tt.expectedError {
				t.Errorf("handleDelayedLauncherPolicy() error = %v, expectedError %v", err, tt.expectedError)
				return
			}
			if !tt.expectedError && gp.MinAvailable != tt.expectedMinAvail {
				t.Errorf("handleDelayedLauncherPolicy() MinAvailable = %v, want %v", gp.MinAvailable, tt.expectedMinAvail)
			}
		})
	}
}

func TestGetPodGroupMetadata(t *testing.T) {
	tests := []struct {
		name             string
		topOwner         *unstructured.Unstructured
		pod              *v1.Pod
		existingPods     []client.Object
		expectedMinAvail int32
		expectedError    bool
	}{
		{
			name: "Basic pod group metadata",
			topOwner: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"name":      "test-job",
						"namespace": "test-ns",
					},
					"spec": map[string]interface{}{
						ReplicaSpecName: map[string]interface{}{
							MasterName: map[string]interface{}{
								"replicas": int64(1),
							},
							WorkerName: map[string]interface{}{
								"replicas": int64(2),
							},
						},
					},
				},
			},
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "worker-1",
					Namespace: "test-ns",
					Labels: map[string]string{
						jobNameLabel:     "test-job",
						podRoleNameLabel: "worker",
					},
				},
			},
			existingPods:     []client.Object{},
			expectedMinAvail: 3, // 1 launcher + 2 workers
			expectedError:    false,
		},
		{
			name: "Delayed launcher policy with no launcher",
			topOwner: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"name":      "test-job",
						"namespace": "test-ns",
					},
					"spec": map[string]interface{}{
						"launcherCreationPolicy": delayedLauncherCreationPolicy,
						ReplicaSpecName: map[string]interface{}{
							MasterName: map[string]interface{}{
								"replicas": int64(1),
							},
							WorkerName: map[string]interface{}{
								"replicas": int64(2),
							},
						},
					},
				},
			},
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "worker-1",
					Namespace: "test-ns",
					Labels: map[string]string{
						jobNameLabel:     "test-job",
						podRoleNameLabel: "worker",
					},
				},
			},
			existingPods:     []client.Object{},
			expectedMinAvail: 2, // Only workers since launcher is delayed
			expectedError:    false,
		},
		{
			name: "Delayed launcher policy with existing launcher",
			topOwner: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"name":      "test-job",
						"namespace": "test-ns",
					},
					"spec": map[string]interface{}{
						"launcherCreationPolicy": delayedLauncherCreationPolicy,
						ReplicaSpecName: map[string]interface{}{
							MasterName: map[string]interface{}{
								"replicas": int64(1),
							},
							WorkerName: map[string]interface{}{
								"replicas": int64(2),
							},
						},
					},
				},
			},
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "worker-1",
					Namespace: "test-ns",
					Labels: map[string]string{
						jobNameLabel:     "test-job",
						podRoleNameLabel: "worker",
					},
				},
			},
			existingPods: []client.Object{
				&v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "launcher",
						Namespace: "test-ns",
						Labels: map[string]string{
							jobNameLabel:     "test-job",
							podRoleNameLabel: launcherPodRoleName,
						},
					},
				},
			},
			expectedMinAvail: 3, // All pods including launcher
			expectedError:    false,
		},
		{
			name: "Invalid replicas configuration",
			topOwner: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"metadata": map[string]interface{}{
						"name":      "test-job",
						"namespace": "test-ns",
					},
					"spec": map[string]interface{}{
						ReplicaSpecName: map[string]interface{}{
							MasterName: map[string]interface{}{
								"replicas": int64(0),
							},
						},
					},
				},
			},
			pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "worker-1",
					Namespace: "test-ns",
					Labels: map[string]string{
						jobNameLabel:     "test-job",
						podRoleNameLabel: "worker",
					},
				},
			},
			existingPods:     []client.Object{},
			expectedMinAvail: 0,
			expectedError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			_ = v1.AddToScheme(scheme)
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.existingPods...).Build()

			defaultGrouper := defaultgrouper.NewDefaultGrouper(queueLabelKey, nodePoolLabelKey, fakeClient)
			kubeFlowGrouper := kubeflow.NewKubeflowDistributedGrouper(defaultGrouper)
			mg := NewMpiGrouper(fakeClient, kubeFlowGrouper)
			metadata, err := mg.GetPodGroupMetadata(tt.topOwner, tt.pod)

			if (err != nil) != tt.expectedError {
				t.Errorf("GetPodGroupMetadata() error = %v, expectedError %v", err, tt.expectedError)
				return
			}

			if !tt.expectedError && metadata.MinAvailable != tt.expectedMinAvail {
				t.Errorf("GetPodGroupMetadata() MinAvailable = %v, want %v", metadata.MinAvailable, tt.expectedMinAvail)
			}
		})
	}
}
