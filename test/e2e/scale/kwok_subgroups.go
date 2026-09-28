// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scale

import (
	"context"
	"errors"
	"maps"
	"sync"

	v1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/pod_group"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/queue"
)

// subGroupSpec describes one leaf sub-group of a pod group: its pods, their resources and its own
// topology constraint.
type subGroupSpec struct {
	name      string
	pods      int
	resources v1.ResourceRequirements
	topology  *v2alpha2.TopologyConstraint
}

// createSubGroupPodGroupForKwok creates a pod group made of flat leaf sub-groups and all of its pods.
// pod_group.CreateWithHierarchy cannot be used here: it creates pods serially and offers no hook for the
// KWOK toleration and affinity, so its pods never land on the simulated nodes.
func createSubGroupPodGroupForKwok(
	ctx context.Context, testCtx *testcontext.TestContext, jobQueue *v2.Queue,
	podGroupName string, podGroupTopology *v2alpha2.TopologyConstraint,
	subGroups []subGroupSpec, extraLabels map[string]string,
) (*v2alpha2.PodGroup, []*v1.Pod, error) {
	namespace := queue.GetConnectedNamespaceToQueue(jobQueue)

	podGroup := pod_group.Create(namespace, podGroupName, jobQueue.Name)
	podGroup.Spec.MinMember = nil
	podGroup.Spec.MinSubGroup = ptr.To(int32(len(subGroups)))
	if podGroupTopology != nil {
		podGroup.Spec.TopologyConstraint = *podGroupTopology
	}
	maps.Copy(podGroup.Labels, extraLabels)

	totalPods := 0
	for _, subGroup := range subGroups {
		podGroup.Spec.SubGroups = append(podGroup.Spec.SubGroups, v2alpha2.SubGroup{
			Name:               subGroup.name,
			MinMember:          ptr.To(int32(subGroup.pods)),
			TopologyConstraint: subGroup.topology,
		})
		totalPods += subGroup.pods
	}

	if err := rd.CreateObjectWithRetries(ctx, testCtx.ControllerClient, podGroup); err != nil {
		return nil, nil, err
	}

	pods := make([]*v1.Pod, 0, totalPods)
	var wg sync.WaitGroup
	var lock sync.Mutex
	var creationError error
	for _, subGroup := range subGroups {
		for range subGroup.pods {
			wg.Add(1)
			go func(subGroup subGroupSpec) {
				defer wg.Done()

				pod := rd.CreatePodWithPodGroupReference(jobQueue, podGroupName, subGroup.resources)
				pod.Labels[constants.SubGroupLabelKey] = subGroup.name
				maps.Copy(pod.Labels, extraLabels)
				addKWOKTaintsAndAffinity(&pod.Spec)

				err := rd.CreateObjectWithRetries(ctx, testCtx.ControllerClient, pod)

				lock.Lock()
				defer lock.Unlock()
				if err != nil {
					creationError = errors.Join(creationError, err)
					return
				}
				pods = append(pods, pod)
			}(subGroup)
		}
	}
	wg.Wait()

	return podGroup, pods, creationError
}
