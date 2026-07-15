// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kaiv1alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v1alpha2"
	schedulingv2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	schedulingv2alpha2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	commonconsts "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
)

type PodGroupConfig struct {
	QueueName          string
	PodgroupName       string
	MinMember          int32
	TopologyConstraint *schedulingv2alpha2.TopologyConstraint
}

// NodeConfig holds the configuration for a test node
type NodeConfig struct {
	Name        string
	CPUs        string // CPU capacity in cores
	Memory      string // Memory capacity in bytes
	GPUs        int    // Number of GPUs
	Labels      map[string]string
	Annotations map[string]string
}

// DefaultNodeConfig returns a default node configuration
func DefaultNodeConfig(name string) NodeConfig {
	return NodeConfig{
		Name:   name,
		Labels: map[string]string{},
		CPUs:   "8",
		Memory: "16Gi",
		GPUs:   4,
	}
}

// CreateNode creates a node with the specified configuration
func CreateNodeObject(ctx context.Context, c client.Client, config NodeConfig) *corev1.Node {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:        config.Name,
			Labels:      config.Labels,
			Annotations: config.Annotations,
		},
		Spec: corev1.NodeSpec{
			Taints: []corev1.Taint{
				{
					Key:    corev1.TaintNodeNotReady,
					Value:  "false",
					Effect: corev1.TaintEffectNoSchedule,
				},
			},
		},
		Status: corev1.NodeStatus{
			Capacity: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(config.CPUs),
				corev1.ResourceMemory: resource.MustParse(config.Memory),
				corev1.ResourcePods:   resource.MustParse("110"),
			},
			Allocatable: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(config.CPUs),
				corev1.ResourceMemory: resource.MustParse(config.Memory),
				corev1.ResourcePods:   resource.MustParse("110"),
			},
			Conditions: []corev1.NodeCondition{
				{
					Type:   corev1.NodeReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}

	// Add GPU resources if applicable
	if config.GPUs > 0 {
		node.Status.Capacity["nvidia.com/gpu"] = *resource.NewQuantity(int64(config.GPUs), resource.DecimalSI)
		node.Status.Allocatable["nvidia.com/gpu"] = *resource.NewQuantity(int64(config.GPUs), resource.DecimalSI)
	}

	return node
}

func CreatePodObject(namespace, name string, resources corev1.ResourceRequirements) *corev1.Pod {
	pod := &corev1.Pod{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Pod",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Image: "ubuntu",
					Name:  "ubuntu-container",
					Args: []string{
						"sleep",
						"infinity",
					},
					Resources: resources,
				},
			},
			Tolerations: []corev1.Toleration{
				{
					Key:      corev1.TaintNodeNotReady,
					Operator: corev1.TolerationOpEqual,
					Value:    "false",
				},
			},
			TerminationGracePeriodSeconds: ptr.To(int64(0)),
			SchedulerName:                 "kai-scheduler",
		},
	}

	return pod
}

func GroupPods(ctx context.Context, c client.Client, podGroupConfig PodGroupConfig, pods []*corev1.Pod) error {
	if len(pods) == 0 {
		return fmt.Errorf("no pods to group")
	}

	podgroup := &schedulingv2alpha2.PodGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podGroupConfig.PodgroupName,
			Namespace: pods[0].Namespace,
			Labels: map[string]string{
				commonconsts.DefaultQueueLabel: podGroupConfig.QueueName,
			},
		},
		Spec: schedulingv2alpha2.PodGroupSpec{
			Queue:             podGroupConfig.QueueName,
			MinMember:         ptr.To(podGroupConfig.MinMember),
			MarkUnschedulable: ptr.To(true),
		},
	}
	if podGroupConfig.TopologyConstraint != nil {
		podgroup.Spec.TopologyConstraint = *podGroupConfig.TopologyConstraint
	}
	err := c.Create(ctx, podgroup, &client.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create podgroup: %w", err)
	}

	for _, pod := range pods {
		originalPod := pod.DeepCopy()
		if pod.Annotations == nil {
			pod.Annotations = make(map[string]string)
		}
		pod.Annotations[commonconsts.PodGroupAnnotationForPod] = podGroupConfig.PodgroupName
		err = c.Patch(ctx, pod, client.MergeFrom(originalPod))
		if err != nil {
			return fmt.Errorf("failed to patch pod with podgroup annotation: %w", err)
		}
	}

	return nil
}

func CreateQueueObject(queueName, parentName string) *schedulingv2.Queue {
	return &schedulingv2.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: queueName,
		},
		Spec: schedulingv2.QueueSpec{
			ParentQueue: parentName,
			Resources: &schedulingv2.QueueResources{
				GPU: schedulingv2.QueueResource{
					Quota:           0,
					Limit:           -1,
					OverQuotaWeight: 1,
				},
				CPU: schedulingv2.QueueResource{
					Quota:           0,
					Limit:           -1,
					OverQuotaWeight: 1,
				},
				Memory: schedulingv2.QueueResource{
					Quota:           0,
					Limit:           -1,
					OverQuotaWeight: 1,
				},
			},
		},
	}
}

// WaitForPodScheduled waits for a pod to be scheduled (have a bind request created)
func WaitForPodScheduled(ctx context.Context, c client.Client, podName, namespace string, timeout, interval time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, interval, timeout, true, func(ctx context.Context) (bool, error) {
		var pod corev1.Pod
		err := c.Get(ctx, client.ObjectKey{Name: podName, Namespace: namespace}, &pod)
		if err != nil {
			return false, err
		}
		for _, condition := range pod.Status.Conditions {
			if condition.Type != corev1.PodScheduled {
				continue
			}

			if condition.Status == corev1.ConditionTrue {
				return true, nil
			}
		}

		var bindRequestList kaiv1alpha2.BindRequestList
		err = c.List(ctx, &bindRequestList, client.InNamespace(namespace))
		if err != nil {
			return false, nil // Continue polling
		}

		for _, bindRequest := range bindRequestList.Items {
			if bindRequest.Spec.PodName == podName && bindRequest.Namespace == namespace {
				return true, nil
			}
		}
		return false, nil
	})
}

// WaitForPodUnschedulable waits for a pod to be unschedulable
func WaitForPodUnschedulable(ctx context.Context, c client.Client, podName, namespace string, timeout, interval time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, interval, timeout, true, func(ctx context.Context) (bool, error) {
		var pod corev1.Pod
		err := c.Get(ctx, client.ObjectKey{Name: podName, Namespace: namespace}, &pod)
		if err != nil {
			return false, err
		}
		for _, condition := range pod.Status.Conditions {
			if condition.Type != corev1.PodScheduled {
				continue
			}

			if condition.Status != corev1.ConditionFalse {
				return false, nil
			}

			if condition.Reason == corev1.PodReasonUnschedulable {
				return true, nil
			}
		}

		return false, nil
	})
}

// WaitForPodBound waits for a pod to be bound (have a NodeName set)
func WaitForPodBound(ctx context.Context, c client.Client, podName, namespace string, timeout, interval time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, interval, timeout, true, func(ctx context.Context) (bool, error) {
		var pod corev1.Pod
		err := c.Get(ctx, client.ObjectKey{Name: podName, Namespace: namespace}, &pod)
		if err != nil {
			return false, err
		}

		if pod.Spec.NodeName == "" {
			return false, nil
		}

		return true, nil
	})
}

func DeleteAllInNamespace(ctx context.Context, c client.Client, namespace string, resources ...client.Object) error {
	for _, resource := range resources {
		err := c.DeleteAllOf(ctx, resource, client.InNamespace(namespace), client.GracePeriodSeconds(0))
		if err != nil {
			return fmt.Errorf("failed to delete resource: %w", err)
		}
	}
	return nil
}

func WaitForNoObjectsInNamespace(ctx context.Context, c client.Client, namespace string, timeout, interval time.Duration, resources ...client.ObjectList) error {
	return wait.PollUntilContextTimeout(ctx, interval, timeout, true, func(ctx context.Context) (bool, error) {
		for _, resource := range resources {
			err := c.List(ctx, resource, client.InNamespace(namespace))
			if err != nil {
				return false, err
			}
			if resource.GetRemainingItemCount() != nil && *resource.GetRemainingItemCount() > 0 {
				return false, nil
			}
		}
		return true, nil
	})
}

func WaitForNamespaceDeletion(ctx context.Context, c client.Client, namespace string, timeout, interval time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, interval, timeout, true, func(ctx context.Context) (bool, error) {
		err := c.Get(ctx, client.ObjectKey{Name: namespace}, &corev1.Namespace{})
		if errors.IsNotFound(err) {
			return true, nil
		}
		return false, err
	})
}

func WaitForObjectDeletion(ctx context.Context, c client.Client, obj client.Object, timeout, interval time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, interval, timeout, true, func(ctx context.Context) (bool, error) {
		err := c.Get(ctx, client.ObjectKeyFromObject(obj), obj)
		if errors.IsNotFound(err) {
			return true, nil
		}
		return false, err
	})
}

func PrettyPrintBindRequestList(bindRequestList *kaiv1alpha2.BindRequestList) string {
	var result string
	for _, bindRequest := range bindRequestList.Items {
		result += PrettyPrintBindRequest(&bindRequest)
	}
	return result
}

func PrettyPrintBindRequest(bindRequest *kaiv1alpha2.BindRequest) string {
	return fmt.Sprintf("BindRequest: %s\nPod: %s\nNode: %s\nDRAStatus: %s\n",
		bindRequest.Name, bindRequest.Spec.PodName, bindRequest.Spec.SelectedNode, bindRequest.Spec.ResourceClaimAllocations)
}
