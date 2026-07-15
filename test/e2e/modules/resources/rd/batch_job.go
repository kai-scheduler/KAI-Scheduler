/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package rd

import (
	"context"
	"fmt"

	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"knative.dev/pkg/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"
)

const BatchJobAppLabel = "batch-job-app-name"

func CreateBatchJobObject(podQueue *v2.Queue, resources v1.ResourceRequirements) *batchv1.Job {
	namespace := queue.GetConnectedNamespaceToQueue(podQueue)
	matchLabelValue := utils.GenerateRandomK8sName(10)

	pod := CreatePodObject(podQueue, resources)
	pod.Labels[BatchJobAppLabel] = matchLabelValue
	pod.Spec.RestartPolicy = v1.RestartPolicyNever

	return &batchv1.Job{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "batch/v1",
			Kind:       "Job",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      utils.GenerateRandomK8sName(10),
			Namespace: namespace,
			Labels: map[string]string{
				constants.AppLabelName: "engine-e2e",
				BatchJobAppLabel:       matchLabelValue,
			},
		},
		Spec: batchv1.JobSpec{
			Template: v1.PodTemplateSpec{
				ObjectMeta: pod.ObjectMeta,
				Spec:       pod.Spec,
			},
		},
	}
}

func GetJobPods(ctx context.Context, client *kubernetes.Clientset, job *batchv1.Job) []v1.Pod {
	pods, err := client.CoreV1().Pods(job.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s", BatchJobAppLabel, job.Labels[BatchJobAppLabel]),
	})
	Expect(err).To(Succeed())
	return pods.Items
}

func DeleteJob(ctx context.Context, client *kubernetes.Clientset, job *batchv1.Job) {
	propagationPolicy := metav1.DeletePropagationForeground
	err := client.BatchV1().Jobs(job.Namespace).Delete(ctx, job.Name, metav1.DeleteOptions{
		PropagationPolicy:  &propagationPolicy,
		GracePeriodSeconds: ptr.Int64(0),
	})
	Expect(err).To(Succeed())
}

func DeleteAllJobsInNamespace(ctx context.Context, client client.Client, namespace string) error {
	err := client.DeleteAllOf(
		ctx, &batchv1.Job{},
		runtimeClient.InNamespace(namespace),
		runtimeClient.GracePeriodSeconds(0),
		runtimeClient.PropagationPolicy(metav1.DeletePropagationForeground),
	)
	return runtimeClient.IgnoreNotFound(err)
}
