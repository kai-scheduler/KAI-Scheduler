/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package queue

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"

	kaiClient "github.com/NVIDIA/KAI-scheduler/pkg/apis/client/clientset/versioned"
	v2 "github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/NVIDIA/KAI-scheduler/pkg/common/constants"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/testconfig"
)

func Create(kaiClientset *kaiClient.Clientset, ctx context.Context, queue *v2.Queue,
	opts metav1.CreateOptions) (result *v2.Queue, err error) {
	result = &v2.Queue{}
	err = kaiClientset.SchedulingV2().RESTClient().Post().
		Resource("queues").
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(queue).
		Do(ctx).
		Into(result)
	return result, err
}

func Delete(kaiClientset *kaiClient.Clientset, ctx context.Context, name string,
	opts metav1.DeleteOptions) error {
	return kaiClientset.SchedulingV2().RESTClient().Delete().
		Resource("queues").
		Name(name).
		Body(&opts).
		Do(ctx).
		Error()
}

func GetAllQueues(kaiClientset *kaiClient.Clientset, ctx context.Context) (*v2.QueueList, error) {
	return kaiClientset.SchedulingV2().Queues("").List(ctx, metav1.ListOptions{})
}

func CreateQueueObject(name string, parentQueueName string) *v2.Queue {
	queue := &v2.Queue{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "scheduling.run.ai/v2",
			Kind:       "Queue",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"project":              name,
				constants.AppLabelName: "engine-e2e",
			},
		},
		Spec: v2.QueueSpec{
			ParentQueue: parentQueueName,
			Resources: &v2.QueueResources{
				GPU: v2.QueueResource{
					Quota:           -1,
					OverQuotaWeight: 1,
					Limit:           -1,
				},
				CPU: v2.QueueResource{
					Quota:           -1,
					OverQuotaWeight: 1,
					Limit:           -1,
				},
				Memory: v2.QueueResource{
					Quota:           -1,
					OverQuotaWeight: 1,
					Limit:           -1,
				},
			},
		},
	}
	return queue
}

func CreateQueueObjectWithGpuResource(name string, gpuResource v2.QueueResource,
	parentQueueName string) *v2.Queue {
	queue := &v2.Queue{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "scheduling.run.ai/v2",
			Kind:       "Queue",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"project":              name,
				constants.AppLabelName: "engine-e2e",
			},
		},
		Spec: v2.QueueSpec{
			ParentQueue: parentQueueName,
			Resources: &v2.QueueResources{
				GPU: gpuResource,
				CPU: v2.QueueResource{
					Quota:           -1,
					OverQuotaWeight: 1,
					Limit:           -1,
				},
				Memory: v2.QueueResource{
					Quota:           -1,
					OverQuotaWeight: 1,
					Limit:           -1,
				},
			},
		},
	}
	return queue
}

func GetConnectedNamespaceToQueue(q *v2.Queue) string {
	return testconfig.GetConfig().QueueNamespacePrefix + q.Name
}

func ConnectQueuesWithSharedParent(parentQueue *v2.Queue, childrenQueues ...*v2.Queue) {
	parentQueue.Spec.ParentQueue = ""

	for _, childQueue := range childrenQueues {
		childQueue.Spec.ParentQueue = parentQueue.Name
	}
}
