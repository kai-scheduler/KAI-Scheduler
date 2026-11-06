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

	testcontext "github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/context"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/pod_group"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/api/constants"
	v2 "github.com/kai-scheduler/api/scheduling/v2"
	"github.com/kai-scheduler/api/scheduling/v2alpha2"
)

// subGroupSpec is a leaf SubGroup with pods, or, when other specs name it as parent, a pod-less SubGroup that requires
// all of its children.
type subGroupSpec struct {
	name         string
	parent       string
	pods         int
	resources    v1.ResourceRequirements
	nodeSelector map[string]string
	topology     *v2alpha2.TopologyConstraint
}

func createSubGroupPodGroupForKwok(
	ctx context.Context, testCtx *testcontext.TestContext, jobQueue *v2.Queue,
	podGroupName string, podGroupTopology *v2alpha2.TopologyConstraint,
	subGroups []subGroupSpec, extraLabels map[string]string,
) (*v2alpha2.PodGroup, []*v1.Pod, error) {
	namespace := queue.GetConnectedNamespaceToQueue(jobQueue)
	podGroup := pod_group.Create(namespace, podGroupName, jobQueue.Name)
	if podGroupTopology != nil {
		podGroup.Spec.TopologyConstraint = *podGroupTopology
	}
	maps.Copy(podGroup.Labels, extraLabels)

	children := map[string]int32{}
	topLevel := int32(0)
	for _, subGroup := range subGroups {
		if subGroup.parent == "" {
			topLevel++
		} else {
			children[subGroup.parent]++
		}
	}
	podGroup.Spec.MinMember = nil
	podGroup.Spec.MinSubGroup = ptr.To(topLevel)

	totalPods := 0
	for _, subGroup := range subGroups {
		spec := v2alpha2.SubGroup{Name: subGroup.name, TopologyConstraint: subGroup.topology}
		if subGroup.parent != "" {
			spec.Parent = ptr.To(subGroup.parent)
		}
		if childCount := children[subGroup.name]; childCount > 0 {
			spec.MinSubGroup = ptr.To(childCount)
		} else {
			spec.MinMember = ptr.To(int32(subGroup.pods))
		}
		podGroup.Spec.SubGroups = append(podGroup.Spec.SubGroups, spec)
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
				pod.Spec.NodeSelector = subGroup.nodeSelector

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
