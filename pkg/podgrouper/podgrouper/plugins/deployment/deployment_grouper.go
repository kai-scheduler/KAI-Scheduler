// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package deployment

import (
	"context"
	"fmt"

	"golang.org/x/exp/maps"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgroup"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/defaultgrouper"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/minmember"
)

var logger = log.FromContext(context.Background())

type DeploymentGrouper struct {
	client       client.Client
	gangSchedule bool
	*defaultgrouper.DefaultGrouper
}

func NewDeploymentGrouper(
	client client.Client, defaultGrouper *defaultgrouper.DefaultGrouper, gangSchedule bool,
) *DeploymentGrouper {
	return &DeploymentGrouper{
		client:         client,
		gangSchedule:   gangSchedule,
		DefaultGrouper: defaultGrouper,
	}
}

func (dg *DeploymentGrouper) Name() string {
	return "Deployment Grouper"
}

// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments/finalizers,verbs=patch;update;create

func (dg *DeploymentGrouper) GetPodGroupMetadata(
	topOwner *unstructured.Unstructured, pod *v1.Pod, _ ...*metav1.PartialObjectMetadata,
) (*podgroup.Metadata, error) {
	metadata, err := dg.DefaultGrouper.GetPodGroupMetadata(topOwner, pod)
	if err != nil {
		return nil, err
	}
	metadata.PriorityClassName = dg.CalcPodGroupPriorityClass(topOwner, pod, constants.InferencePriorityClass)

	if !dg.gangSchedule {
		return setPodGroupPerPod(metadata, pod), nil
	}

	legacy, err := dg.hasPodGroupPerPod(topOwner)
	if err != nil {
		return nil, fmt.Errorf("unable to determine gang behavior for deployment %s/%s and pod %s/%s: %w",
			topOwner.GetNamespace(), topOwner.GetName(), pod.Namespace, pod.Name, err)
	}
	if legacy {
		return setPodGroupPerPod(metadata, pod), nil
	}

	metadata.MinAvailable, err = minmember.FromAnnotations(topOwner, "Deployment", 1)
	if err != nil {
		return nil, err
	}

	return metadata, nil
}

func setPodGroupPerPod(metadata *podgroup.Metadata, pod *v1.Pod) *podgroup.Metadata {
	metadata.Owner = metav1.OwnerReference{
		APIVersion: pod.APIVersion,
		Kind:       pod.Kind,
		Name:       pod.GetName(),
		UID:        pod.GetUID(),
	}
	metadata.Name = fmt.Sprintf("%s-%s-%s", constants.PodGroupNamePrefix, pod.GetName(), pod.GetUID())
	metadata.MinAvailable = 1
	return metadata
}

// hasPodGroupPerPod checks whether the deployment pods are already spread over a podgroup per pod. In that case, we
// will maintain this behavior for the deployment, so that running pods aren't re-parented during an upgrade.
func (dg *DeploymentGrouper) hasPodGroupPerPod(deployment *unstructured.Unstructured) (bool, error) {
	selector, err := podSelector(deployment)
	if err != nil {
		return false, err
	}
	if selector == nil {
		logger.V(1).Info("deployment has no pod selector, assuming no legacy podgroups", "deployment",
			fmt.Sprintf("%s/%s", deployment.GetNamespace(), deployment.GetName()))
		return false, nil
	}

	var pods v1.PodList
	err = dg.client.List(context.Background(), &pods, client.InNamespace(deployment.GetNamespace()),
		client.MatchingLabelsSelector{Selector: selector})
	if err != nil {
		return false, err
	}

	podGroups := make(map[string]bool)
	for _, pod := range pods.Items {
		podGroupName, found := pod.Annotations[commonconstants.PodGroupAnnotationForPod]
		if !found {
			continue
		}

		if podGroups[podGroupName] { // found 2 pods pointing to the same podgroup - it's a gang scheduled deployment
			return false, nil
		}

		podGroups[podGroupName] = true
	}

	if len(podGroups) > 1 { // found more than 1 podgroup - is not a gang scheduled deployment
		return true, nil
	}

	if len(podGroups) == 1 {
		pgName := maps.Keys(podGroups)[0]

		var podGroup v2alpha2.PodGroup
		err = dg.client.Get(context.Background(),
			types.NamespacedName{Namespace: deployment.GetNamespace(), Name: pgName}, &podGroup)
		if err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}

			return false, fmt.Errorf("failed to get the only podgroup %s: %w", pgName, err)
		}

		if len(podGroup.OwnerReferences) == 0 {
			return false, fmt.Errorf("podgroup %s has no owner references", pgName)
		}

		if podGroup.OwnerReferences[0].Kind == deployment.GetKind() {
			return false, nil
		}

		if podGroup.OwnerReferences[0].Kind == "Pod" {
			return true, nil
		}

		return false, fmt.Errorf("podgroup %s has unexpected owner reference: %v", pgName, podGroup.OwnerReferences[0])
	}

	return false, nil
}

// podSelector returns the deployment pod selector, or nil if the deployment doesn't select any pod. A nil selector is
// returned rather than labels.Everything() to avoid listing unrelated pods in the namespace.
func podSelector(deployment *unstructured.Unstructured) (labels.Selector, error) {
	selectorMap, found, err := unstructured.NestedMap(deployment.Object, "spec", "selector")
	if err != nil || !found {
		return nil, err
	}

	labelSelector := &metav1.LabelSelector{}
	if err = runtime.DefaultUnstructuredConverter.FromUnstructured(selectorMap, labelSelector); err != nil {
		return nil, fmt.Errorf("failed to parse the selector of deployment %s/%s: %w",
			deployment.GetNamespace(), deployment.GetName(), err)
	}
	if len(labelSelector.MatchLabels) == 0 && len(labelSelector.MatchExpressions) == 0 {
		return nil, nil
	}

	return metav1.LabelSelectorAsSelector(labelSelector)
}
