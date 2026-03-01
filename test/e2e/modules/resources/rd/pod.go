/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package rd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"

	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"

	v1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"

	v2 "github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/NVIDIA/KAI-scheduler/pkg/common/constants"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/constant"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/testconfig"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/utils"
)

const (
	PodGroupLabelName   = "pod-group-name"
	numCreatePodRetries = 10
)

func IsPodScheduled(pod *v1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == v1.PodScheduled && condition.Status == v1.ConditionTrue {
			return true
		}
	}
	return false
}

func IsPodUnschedulable(pod *v1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == v1.PodScheduled && condition.Status == v1.ConditionFalse &&
			condition.Reason == v1.PodReasonUnschedulable {
			return true
		}
	}
	return false
}

func IsPodReady(pod *v1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == v1.PodReady && condition.Status == v1.ConditionTrue {
			return true
		}
	}
	return false
}

func IsPodRunning(pod *v1.Pod) bool {
	return pod.Status.Phase == v1.PodRunning
}

func IsPodEnded(pod *v1.Pod) bool {
	return IsPodSucceeded(pod) || IsPodFailed(pod)
}

func IsPodSucceeded(pod *v1.Pod) bool {
	return pod.Status.Phase == v1.PodSucceeded
}

func IsPodFailed(pod *v1.Pod) bool {
	return pod.Status.Phase == v1.PodFailed
}

func GetPod(ctx context.Context, client *kubernetes.Clientset, namespace string, name string) (*v1.Pod, error) {
	pod, err := client.
		CoreV1().
		Pods(namespace).
		Get(ctx, name, metav1.GetOptions{})
	return pod, err
}

func GetPodLogs(ctx context.Context, client *kubernetes.Clientset, namespace string, name string) (string, error) {
	req := client.
		CoreV1().
		Pods(namespace).
		GetLogs(name, &v1.PodLogOptions{})
	podLogs, err := req.Stream(ctx)
	if err != nil {
		return "", err
	}

	defer podLogs.Close()
	buf := new(bytes.Buffer)
	_, err = io.Copy(buf, podLogs)
	if err != nil {
		return "", err
	}

	return buf.String(), nil
}

func CreatePod(ctx context.Context, client *kubernetes.Clientset, pod *v1.Pod) (*v1.Pod, error) {
	_, err := GetPod(ctx, client, pod.Namespace, pod.Name)
	if err == nil {
		// pod is not expected to exist in the cluster
		return nil, fmt.Errorf("pod %s/%s already exists in the cluster", pod.Namespace, pod.Name)
	}

	for range numCreatePodRetries {
		actualPod, err := client.
			CoreV1().
			Pods(pod.Namespace).
			Create(ctx, pod, metav1.CreateOptions{})
		if err == nil {
			return actualPod, nil
		}
		if errors.IsAlreadyExists(err) {
			return GetPod(ctx, client, pod.Namespace, pod.Name)
		}
		time.Sleep(time.Second * 2)
	}
	return nil, fmt.Errorf("failed to create pod <%s/%s>, error: %s",
		pod.Namespace, pod.Name, err)
}

func CreatePodObject(podQueue *v2.Queue, resources v1.ResourceRequirements) *v1.Pod {
	cfg := testconfig.GetConfig()
	namespace := queue.GetConnectedNamespaceToQueue(podQueue)
	pod := &v1.Pod{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Pod",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        utils.GenerateRandomK8sName(10),
			Namespace:   namespace,
			Annotations: map[string]string{},
			Labels: map[string]string{
				constants.AppLabelName: "engine-e2e",
				cfg.QueueLabelKey:      podQueue.Name,
			},
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Image: cfg.ContainerImage,
					Name:  "ubuntu-container",
					Args: []string{
						"sleep",
						"infinity",
					},
					Resources:       resources,
					SecurityContext: DefaultSecurityContext(),
					ImagePullPolicy: v1.PullIfNotPresent,
				},
			},
			TerminationGracePeriodSeconds: ptr.To(int64(0)),
			SchedulerName:                 cfg.SchedulerName,
			Tolerations: []v1.Toleration{
				{
					Key:      "nvidia.com/gpu",
					Operator: v1.TolerationOpExists,
					Effect:   v1.TaintEffectNoSchedule,
				},
			},
		},
	}

	return pod
}

func CreatePodWithPodGroupReference(queue *v2.Queue, podGroupName string,
	resources v1.ResourceRequirements) *v1.Pod {
	pod := CreatePodObject(queue, resources)
	pod.Annotations[PodGroupLabelName] = podGroupName
	pod.Labels[PodGroupLabelName] = podGroupName
	return pod
}

func CreatePodWithGpuClaim(ctx context.Context, client *kubernetes.Clientset, controllerClient runtimeClient.Client, q *v2.Queue, namespace string, deviceCount int, extraPodSpec func(*v1.Pod)) *v1.Pod {
	return createPodWithGpuClaim(ctx, client, controllerClient, q, namespace, deviceCount, extraPodSpec)
}

func CreatePodWithGpuClaimAndPodGroup(ctx context.Context, client *kubernetes.Clientset, controllerClient runtimeClient.Client, q *v2.Queue, namespace string, deviceCount int, podGroupName string) *v1.Pod {
	setPodGroup := func(pod *v1.Pod) {
		if podGroupName != "" {
			pod.Annotations[PodGroupLabelName] = podGroupName
			pod.Labels[PodGroupLabelName] = podGroupName
		}
	}
	return createPodWithGpuClaim(ctx, client, controllerClient, q, namespace, deviceCount, setPodGroup)
}

func createPodWithGpuClaim(ctx context.Context, client *kubernetes.Clientset, controllerClient runtimeClient.Client, q *v2.Queue, namespace string, deviceCount int, extraPodSpec func(*v1.Pod)) *v1.Pod {
	claim := CreateResourceClaim(namespace, q.Name, constant.GPUDeviceClassName, deviceCount)
	claim, err := client.ResourceV1().ResourceClaims(namespace).Create(ctx, claim, metav1.CreateOptions{})
	Expect(err).To(Succeed())

	Eventually(func() error {
		claimObj := &resourceapi.ResourceClaim{}
		return controllerClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: claim.Name}, claimObj)
	}).Should(Succeed(), "ResourceClaim should be accessible via controller client")

	claimName := "gpu-claim"
	pod := CreatePodObject(q, v1.ResourceRequirements{
		Claims: []v1.ResourceClaim{
			{Name: claimName, Request: claimName},
		},
	})
	pod.Spec.ResourceClaims = []v1.PodResourceClaim{{
		Name:              claimName,
		ResourceClaimName: ptr.To(claim.Name),
	}}
	if extraPodSpec != nil {
		extraPodSpec(pod)
	}
	pod, err = CreatePod(ctx, client, pod)
	Expect(err).To(Succeed())

	claim, err = client.ResourceV1().ResourceClaims(namespace).Get(ctx, claim.Name, metav1.GetOptions{})
	Expect(err).To(Succeed())
	claim.OwnerReferences = []metav1.OwnerReference{{
		APIVersion: "v1",
		Kind:       "Pod",
		Name:       pod.Name,
		UID:        pod.UID,
	}}
	_, err = client.ResourceV1().ResourceClaims(namespace).Update(ctx, claim, metav1.UpdateOptions{})
	Expect(err).To(Succeed())

	return pod
}

func NodeAffinity(nodeName string, operator v1.NodeSelectorOperator) *v1.Affinity {
	return &v1.Affinity{
		NodeAffinity: &v1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
				NodeSelectorTerms: []v1.NodeSelectorTerm{
					{
						MatchExpressions: []v1.NodeSelectorRequirement{
							{
								Key:      constant.NodeNamePodLabelName,
								Operator: operator,
								Values:   []string{nodeName},
							},
						},
					},
				},
			},
		},
	}
}

func DeleteAllPodsInNamespace(
	ctx context.Context, client runtimeClient.Client, namespace string,
) error {
	err := client.DeleteAllOf(
		ctx, &v1.Pod{},
		runtimeClient.InNamespace(namespace),
		runtimeClient.GracePeriodSeconds(0),
	)
	return runtimeClient.IgnoreNotFound(err)
}

func DeleteAllConfigMapsInNamespace(
	ctx context.Context, client runtimeClient.Client, namespace string,
) error {
	err := client.DeleteAllOf(
		ctx, &v1.ConfigMap{},
		runtimeClient.InNamespace(namespace),
		runtimeClient.GracePeriodSeconds(0),
	)
	return runtimeClient.IgnoreNotFound(err)
}

func GetPodNodeAffinitySelector(pod *v1.Pod) *v1.NodeSelector {
	if pod.Spec.Affinity == nil {
		pod.Spec.Affinity = &v1.Affinity{}
	}
	if pod.Spec.Affinity.NodeAffinity == nil {
		pod.Spec.Affinity.NodeAffinity = &v1.NodeAffinity{}
	}
	if pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil {
		pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution = &v1.NodeSelector{}
	}
	return pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution
}
