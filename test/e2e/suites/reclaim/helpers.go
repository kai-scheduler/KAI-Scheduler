/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package reclaim

import (
	"context"
	"fmt"

	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	v2 "github.com/NVIDIA/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/NVIDIA/KAI-scheduler/pkg/common/constants"
	testcontext "github.com/NVIDIA/KAI-scheduler/test/e2e/modules/context"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/utils"
)

func CreatePod(ctx context.Context, testCtx *testcontext.TestContext, q *v2.Queue, gpus float64) *v1.Pod {
	pod := rd.CreatePodObject(q, v1.ResourceRequirements{})
	if gpus >= 1 {
		pod.Spec.Containers[0].Resources.Limits = map[v1.ResourceName]resource.Quantity{
			constants.GpuResource: resource.MustParse(fmt.Sprintf("%f", gpus)),
		}
	} else if gpus > 0 {
		pod.Annotations = map[string]string{
			constants.GpuFraction: fmt.Sprintf("%f", gpus),
		}
	}

	pod, err := rd.CreatePod(ctx, testCtx.KubeClientset, pod)
	Expect(err).To(Succeed())
	return pod
}

func CreateQueues(parentQueueDeserved, queue1Deserved, queue2Deserved float64) (
	*v2.Queue, *v2.Queue, *v2.Queue) {
	parentQueueName := utils.GenerateRandomK8sName(10)
	parentQueue := queue.CreateQueueObjectWithGpuResource(parentQueueName,
		v2.QueueResource{
			Quota:           parentQueueDeserved,
			OverQuotaWeight: 2,
			Limit:           parentQueueDeserved,
		}, "")

	queue1Name := utils.GenerateRandomK8sName(10)
	queue1 := queue.CreateQueueObjectWithGpuResource(queue1Name,
		v2.QueueResource{
			Quota:           queue1Deserved,
			OverQuotaWeight: 2,
			Limit:           -1,
		}, parentQueueName)

	queue2Name := utils.GenerateRandomK8sName(10)
	queue2 := queue.CreateQueueObjectWithGpuResource(queue2Name,
		v2.QueueResource{
			Quota:           queue2Deserved,
			OverQuotaWeight: 2,
			Limit:           -1,
		}, parentQueueName)

	return parentQueue, queue1, queue2
}

func PodListToPodsSlice(podList *v1.PodList) []*v1.Pod {
	pods := make([]*v1.Pod, 0)
	for _, pod := range podList.Items {
		pods = append(pods, &pod)
	}
	return pods
}
