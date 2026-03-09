// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package podgrouper_test

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	argov1alpha1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	"github.com/stretchr/testify/assert"
	"go.uber.org/multierr"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	commonconsts "github.com/NVIDIA/KAI-scheduler/pkg/common/constants"
	"github.com/NVIDIA/KAI-scheduler/pkg/podgrouper/podgroup"
	"github.com/NVIDIA/KAI-scheduler/pkg/podgrouper/podgrouper"
	pluginshub "github.com/NVIDIA/KAI-scheduler/pkg/podgrouper/podgrouper/hub"
)

const (
	queueLabelKey    = "kai.scheduler/queue"
	nodePoolLabelKey = "kai.scheduler/node-pool"
)

var pod = v1.Pod{
	TypeMeta: metav1.TypeMeta{
		Kind:       "Pod",
		APIVersion: "v1",
	},
	ObjectMeta: metav1.ObjectMeta{
		Name:      "pod",
		Namespace: "namespace",
		OwnerReferences: []metav1.OwnerReference{{
			APIVersion: "run.ai/v1",
			Kind:       "RunaiJob",
			Name:       "job",
		}},
	},
	Spec:   v1.PodSpec{},
	Status: v1.PodStatus{},
}

var podJob = v1.Pod{
	TypeMeta: metav1.TypeMeta{
		Kind:       "Pod",
		APIVersion: "v1",
	},
	ObjectMeta: metav1.ObjectMeta{
		Name:      "pod-job",
		Namespace: "namespace",
	},
	Spec:   v1.PodSpec{},
	Status: v1.PodStatus{},
}

var podJobSpawn = v1.Pod{
	TypeMeta: metav1.TypeMeta{
		Kind:       "Pod",
		APIVersion: "v1",
	},
	ObjectMeta: metav1.ObjectMeta{
		Name:      "pod-job-spawn",
		Namespace: "namespace",
		OwnerReferences: []metav1.OwnerReference{{
			APIVersion: "v1",
			Kind:       "Pod",
			Name:       "pod-job",
		}},
	},
	Spec:   v1.PodSpec{},
	Status: v1.PodStatus{},
}

var testResources = []runtime.Object{
	&unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "RunaiJob",
			"apiVersion": "run.ai/v1",
			"metadata": map[string]interface{}{
				"name":      "job",
				"namespace": "namespace",
			},
			"Spec":   map[string]interface{}{},
			"Status": map[string]interface{}{},
		},
	},
	&unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "RunaiJob",
			"apiVersion": "run.ai/v1",
			"metadata": map[string]interface{}{
				"name":      "job-2",
				"namespace": "namespace",
			},
			"Spec":   map[string]interface{}{},
			"Status": map[string]interface{}{},
		},
	},
}

var nativeK8sTestResources = []runtime.Object{
	&pod,
	&podJob,
	&podJobSpawn,
}

func TestNewPodgrouper(t *testing.T) {
	scheme := runtime.NewScheme()
	err := v1.AddToScheme(scheme)
	if err != nil {
		t.Fail()
	}

	resources := append(nativeK8sTestResources, testResources...)
	client := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(resources...).Build()

	pluginsHub := pluginshub.NewDefaultPluginsHub(client, false, true,
		queueLabelKey, nodePoolLabelKey, "", "")
	grouper := podgrouper.NewPodgrouper(client, client, pluginsHub)

	topOwner, owners, err := grouper.GetPodOwners(context.Background(), &pod)
	assert.Nil(t, err)
	_, err = grouper.GetPGMetadata(context.Background(), &pod, topOwner, owners)
	assert.Nil(t, err)

	topOwner, owners, err = grouper.GetPodOwners(context.Background(), &podJob)
	assert.Nil(t, err)
	podGroup, err := grouper.GetPGMetadata(context.Background(), &podJob, topOwner, owners)
	assert.Nil(t, err)
	assert.Equal(t, podJob.Name, podGroup.Owner.Name)
	assert.Equal(t, podJob.Kind, podGroup.Owner.Kind)

	topOwner, owners, err = grouper.GetPodOwners(context.Background(), &podJob)
	assert.Nil(t, err)
	podGroup, err = grouper.GetPGMetadata(context.Background(), &podJobSpawn, topOwner, owners)
	assert.Nil(t, err)
	assert.Equal(t, podJob.Name, podGroup.Owner.Name)
	assert.Equal(t, podJob.Kind, podGroup.Owner.Kind)
}

func Test_Podgrouper_Full_Flow(t *testing.T) {
	type podGrouperOptions struct {
		searchForLegacyPodGroups bool
		gangScheduleKnative      bool
	}

	tests := []struct {
		name              string
		podGrouperOptions *podGrouperOptions
		resources         []runtime.Object
		reconciledPod     *v1.Pod
		expectedMetadata  *podgroup.Metadata
	}{
		{
			name: "sanity - pod job",
			resources: []runtime.Object{
				&v1.Pod{
					TypeMeta: metav1.TypeMeta{
						Kind:       "Pod",
						APIVersion: "v1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod-job",
						Namespace: "namespace",
					},
					Spec:   v1.PodSpec{},
					Status: v1.PodStatus{},
				},
			},
			reconciledPod: &v1.Pod{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Pod",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod-job",
					Namespace: "namespace",
				},
				Spec:   v1.PodSpec{},
				Status: v1.PodStatus{},
			},
			expectedMetadata: &podgroup.Metadata{
				Annotations: map[string]string{
					commonconsts.TopOwnerMetadataKey: `name: pod-job
uid: ""
group: ""
version: v1
kind: Pod
`,
				},
				Labels:            map[string]string{},
				PriorityClassName: "train",
				Queue:             "default-queue",
				Namespace:         "namespace",
				Name:              "pg-pod-job-",
				MinAvailable:      1,
				Owner: metav1.OwnerReference{
					APIVersion: "v1",
					Kind:       "Pod",
					Name:       "pod-job",
					UID:        "",
				},
			},
		},
		{
			name: "argo - non gang schedule",
			podGrouperOptions: &podGrouperOptions{
				searchForLegacyPodGroups: false,
				gangScheduleKnative:      true,
			},
			resources: []runtime.Object{
				&v1.Pod{
					TypeMeta: metav1.TypeMeta{
						Kind:       "Pod",
						APIVersion: "v1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "argo-pod",
						Namespace: "namespace",
						UID:       "uid-pod",
						OwnerReferences: []metav1.OwnerReference{{
							APIVersion: "argoproj.io/v1alpha1",
							Kind:       "Workflow",
							Name:       "hello-world-workflow",
							UID:        "uid",
						}},
					},
					Spec:   v1.PodSpec{},
					Status: v1.PodStatus{},
				},
				&argov1alpha1.Workflow{
					TypeMeta: metav1.TypeMeta{
						Kind:       "Workflow",
						APIVersion: "argoproj.io/v1alpha1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "hello-world-workflow",
						Namespace: "namespace",
						UID:       "uid",
						Labels: map[string]string{
							queueLabelKey: "test",
						},
					},
					Spec:   argov1alpha1.WorkflowSpec{},
					Status: argov1alpha1.WorkflowStatus{},
				},
			},
			reconciledPod: &v1.Pod{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Pod",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "argo-pod",
					Namespace: "namespace",
					UID:       "uid-pod",
					OwnerReferences: []metav1.OwnerReference{{
						APIVersion: "argoproj.io/v1alpha1",
						Kind:       "Workflow",
						Name:       "hello-world-workflow",
						UID:        "uid",
					}},
				},
				Spec:   v1.PodSpec{},
				Status: v1.PodStatus{},
			},
			expectedMetadata: &podgroup.Metadata{
				Annotations: map[string]string{
					commonconsts.TopOwnerMetadataKey: `name: argo-pod
uid: uid-pod
group: ""
version: v1
kind: Pod
`,
				},
				Labels: map[string]string{
					queueLabelKey: "test",
				},
				PriorityClassName: "train",
				Queue:             "test",
				Namespace:         "namespace",
				Name:              "pg-argo-pod-uid-pod",
				MinAvailable:      1,
				Owner: metav1.OwnerReference{
					APIVersion: "v1",
					Kind:       "Pod",
					Name:       "argo-pod",
					UID:        "uid-pod",
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			err := v1.AddToScheme(scheme)
			if err != nil {
				t.Fail()
			}

			assert.Nil(t, argov1alpha1.AddToScheme(scheme))

			client := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(tt.resources...).Build()

			if tt.podGrouperOptions == nil {
				tt.podGrouperOptions = &podGrouperOptions{
					searchForLegacyPodGroups: false,
					gangScheduleKnative:      true,
				}
			}
			pluginsHub := pluginshub.NewDefaultPluginsHub(client,
				tt.podGrouperOptions.searchForLegacyPodGroups,
				tt.podGrouperOptions.gangScheduleKnative,
				queueLabelKey,
				nodePoolLabelKey,
				"",
				"",
			)
			grouper := podgrouper.NewPodgrouper(client, client, pluginsHub)

			topOwner, owners, err := grouper.GetPodOwners(context.Background(), tt.reconciledPod)
			assert.Nil(t, err)
			metadata, err := grouper.GetPGMetadata(context.Background(), tt.reconciledPod, topOwner, owners)
			assert.Nil(t, err)
			assert.Nil(t, compareMetadata(tt.expectedMetadata, metadata))
		})
	}
}

func compareMetadata(expected, got *podgroup.Metadata) error {
	var err error
	if !reflect.DeepEqual(expected.Annotations, got.Annotations) {
		err = multierr.Append(err, fmt.Errorf("annotations not equal:\n expected:\n%v\ngot:\n%v\n", expected.Annotations, got.Annotations))
	}

	if !reflect.DeepEqual(expected.Labels, got.Labels) {
		err = multierr.Append(err, fmt.Errorf("labels not equal:\n expected:\n%v\ngot:\n%v\n", expected.Labels, got.Labels))
	}

	if expected.PriorityClassName != got.PriorityClassName {
		err = multierr.Append(err, fmt.Errorf("priority class name not equal:\n expected:\n%v\ngot:\n%v\n", expected.PriorityClassName, got.PriorityClassName))
	}

	if expected.Queue != got.Queue {
		err = multierr.Append(err, fmt.Errorf("queue not equal:\n expected:\n%v\ngot:\n%v\n", expected.Queue, got.Queue))
	}

	if expected.Namespace != got.Namespace {
		err = multierr.Append(err, fmt.Errorf("namespace not equal:\n expected:\n%v\ngot:\n%v\n", expected.Namespace, got.Namespace))
	}

	if expected.Name != got.Name {
		err = multierr.Append(err, fmt.Errorf("name not equal:\n expected:\n%v\ngot:\n%v\n", expected.Name, got.Name))
	}

	if expected.MinAvailable != got.MinAvailable {
		err = multierr.Append(err, fmt.Errorf("min available not equal:\n expected:\n%v\ngot:\n%v\n", expected.MinAvailable, got.MinAvailable))
	}

	if !reflect.DeepEqual(expected.Owner, got.Owner) {
		err = multierr.Append(err, fmt.Errorf("owner not equal:\n expected:\n%v\ngot:\n%v\n", expected.Owner, got.Owner))
	}

	if !reflect.DeepEqual(expected, got) {
		err = multierr.Append(err, fmt.Errorf("metadata not equal:\n expected:\n%v\ngot:\n%v\n", expected, got))
	}

	return err
}

func TestPodGrouper_OwnerChainFallback_UsesIntermediatePlugin(t *testing.T) {
	scheme := runtime.NewScheme()
	assert.NoError(t, v1.AddToScheme(scheme))

	// Create a real batch/v1 Job object
	job := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "batch/v1",
			"kind":       "Job",
			"metadata": map[string]interface{}{
				"name":      "job-owner",
				"namespace": "namespace",
				"uid":       "uid-job",
			},
			"spec": map[string]interface{}{
				"template": map[string]interface{}{
					"spec": map[string]interface{}{},
				},
			},
		},
	}

	// Simulate a JobSet-like top owner as an unstructured object
	jobSet := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "JobSet",
			"apiVersion": "jobset.x-k8s.io/v1alpha2",
			"metadata": map[string]interface{}{
				"name":      "jobset-owner",
				"namespace": "namespace",
			},
		},
	}
	testPod := &v1.Pod{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-with-owner-chain",
			Namespace: "namespace",
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "batch/v1",
					Kind:       "Job",
					Name:       "job-owner",
					UID:        "uid-job",
				},
			},
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(
			job,
			jobSet,
			testPod,
		).
		Build()

	pluginsHub := pluginshub.NewDefaultPluginsHub(
		client,
		false, // searchForLegacyPodGroups
		true,  // gangScheduleKnative
		queueLabelKey,
		nodePoolLabelKey,
		"",
		"",
	)

	pg := podgrouper.NewPodgrouper(client, client, pluginsHub)

	topOwner, owners, err := pg.GetPodOwners(context.Background(), testPod)
	assert.NoError(t, err)

	assert.Contains(t, []string{"JobSet", "Job"}, topOwner.GetKind())

	metadata, err := pg.GetPGMetadata(context.Background(), testPod, topOwner, owners)
	assert.NoError(t, err)
	assert.Equal(t, "Job", metadata.Owner.Kind)
	assert.Equal(t, "batch/v1", metadata.Owner.APIVersion)
}
