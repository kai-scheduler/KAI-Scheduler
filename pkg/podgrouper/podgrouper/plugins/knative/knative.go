// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package knative

import (
	"context"
	"fmt"
	"strconv"

	"golang.org/x/exp/maps"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgroup"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/defaultgrouper"
)

const (
	knativeRevisionLabel      = "serving.knative.dev/revision"
	knativeMinScaleAnnotation = "autoscaling.knative.dev/min-scale"
)

var logger = log.FromContext(context.Background())

type KnativeGrouper struct {
	client       client.Client
	gangSchedule bool
	*defaultgrouper.DefaultGrouper
}

func NewKnativeGrouper(client client.Client, defaultGrouper *defaultgrouper.DefaultGrouper, gangSchedule bool) *KnativeGrouper {
	return &KnativeGrouper{
		client:         client,
		gangSchedule:   gangSchedule,
		DefaultGrouper: defaultGrouper,
	}
}

func (g *KnativeGrouper) Name() string {
	return "KNative Grouper"
}

// +kubebuilder:rbac:groups=serving.knative.dev,resources=revisions/finalizers;configurations/finalizers;services/finalizers,verbs=patch;update;create
// +kubebuilder:rbac:groups=serving.knative.dev,resources=revisions;configurations;services,verbs=get;list;watch

func (g *KnativeGrouper) GetPodGroupMetadata(_ *unstructured.Unstructured, pod *v1.Pod, _ ...*metav1.PartialObjectMetadata) (*podgroup.Metadata, error) {
	revisionName, found := pod.Labels[knativeRevisionLabel]
	if !found {
		return nil, fmt.Errorf("revisionName not found for knative service pod %s/%s", pod.GetNamespace(), pod.GetName())
	}
	rev := &unstructured.Unstructured{}
	rev.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "serving.knative.dev",
		Version: "v1",
		Kind:    "Revision",
	})
	err := g.client.Get(context.Background(), types.NamespacedName{Namespace: pod.Namespace, Name: revisionName}, rev)
	if err != nil {
		return nil, err
	}

	if !g.gangSchedule {
		return g.getPodGroupFromPod(rev, pod)
	}

	isBC, err := g.isBCRevision(rev)
	if err != nil {
		return nil, fmt.Errorf("unable to determine gang behavior for revision %s/%s and pod %s/%s: %w", rev.GetNamespace(), revisionName, pod.Namespace, pod.Name, err)
	}

	if isBC {
		return g.getPodGroupFromPod(rev, pod)
	}

	return g.getPodGroupFromRevision(rev, pod)
}

func (g *KnativeGrouper) getPodGroupFromRevision(revision *unstructured.Unstructured, pod *v1.Pod) (*podgroup.Metadata, error) {
	annotations := revision.GetAnnotations()
	minScale, exists := annotations[knativeMinScaleAnnotation]
	if !exists {
		logger.V(1).Info("minScale annotation not found in revision, using 1", "revision",
			fmt.Sprintf("%s/%s", revision.GetNamespace(), revision.GetName()))
		minScale = "1"
	}
	minScaleInt, err := strconv.Atoi(minScale)
	if err != nil {
		logger.V(1).Info("unable to convert minscale, using 1", "minscale", minScale, "revision",
			fmt.Sprintf("%s/%s", revision.GetNamespace(), revision.GetName()))
		minScaleInt = 1
	}

	topOwnerRef := &metav1.OwnerReference{
		APIVersion: revision.GetAPIVersion(),
		Kind:       revision.GetKind(),
		Name:       revision.GetName(),
		UID:        revision.GetUID(),
	}

	return &podgroup.Metadata{
		Annotations:       g.CalcPodGroupAnnotations(revision, pod),
		Labels:            g.CalcPodGroupLabels(revision, pod),
		PriorityClassName: g.CalcPodGroupPriorityClass(revision, pod, constants.InferencePriorityClass),
		Queue:             g.CalcPodGroupQueue(revision, pod),
		Namespace:         revision.GetNamespace(),
		Name:              calcPodGroupName(revision),
		MinAvailable:      int32(minScaleInt),
		Owner:             *topOwnerRef,
	}, nil
}

func (g *KnativeGrouper) getPodGroupFromPod(revision *unstructured.Unstructured, pod *v1.Pod) (*podgroup.Metadata, error) {
	ownerRef := &metav1.OwnerReference{
		APIVersion: pod.APIVersion,
		Kind:       pod.Kind,
		Name:       pod.GetName(),
		UID:        pod.GetUID(),
	}

	return &podgroup.Metadata{
		Annotations:       g.CalcPodGroupAnnotations(revision, pod),
		Labels:            g.CalcPodGroupLabels(revision, pod),
		PriorityClassName: constants.InferencePriorityClass,
		Queue:             g.CalcPodGroupQueue(revision, pod),
		Namespace:         pod.GetNamespace(),
		Name:              calcPodGroupName(pod),
		MinAvailable:      1,
		Owner:             *ownerRef,
	}, nil
}

func calcPodGroupName(obj metav1.Object) string {
	return fmt.Sprintf("%s-%s-%s", constants.PodGroupNamePrefix, obj.GetName(), obj.GetUID())
}

// isBCRevision checks if the revision already has podgroups per pod. In that case, we will maintain this behavior for the revision.
func (g *KnativeGrouper) isBCRevision(revision *unstructured.Unstructured) (bool, error) {
	var pods v1.PodList
	err := g.client.List(context.Background(), &pods, client.InNamespace(revision.GetNamespace()), client.MatchingLabels{
		knativeRevisionLabel: revision.GetName(),
	})
	if err != nil {
		return false, err
	}

	podGroups := make(map[string]bool)
	for _, pod := range pods.Items {
		podGroupName, found := pod.Annotations[commonconstants.PodGroupAnnotationForPod]
		if !found {
			continue
		}

		if podGroups[podGroupName] { // found 2 pods pointing to the same podgroup - it's a gang scheduled revision
			return false, nil
		}

		podGroups[podGroupName] = true
	}

	if len(podGroups) > 1 { // found more than 1 podgroup - is not a gang scheduled revision
		return true, nil
	}

	if len(podGroups) == 1 {
		pgName := maps.Keys(podGroups)[0]

		var podGroup v2alpha2.PodGroup
		err := g.client.Get(context.Background(), types.NamespacedName{Namespace: revision.GetNamespace(), Name: pgName}, &podGroup)
		if err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}

			return false, fmt.Errorf("failed to get the only podgroup %s: %w", pgName, err)
		}

		if len(podGroup.OwnerReferences) == 0 {
			return false, fmt.Errorf("podgroup %s has no owner references", pgName)
		}

		if podGroup.OwnerReferences[0].Kind == revision.GetKind() {
			return false, nil
		}

		if podGroup.OwnerReferences[0].Kind == "Pod" {
			return true, nil
		}

		return false, fmt.Errorf("podgroup %s has unexpected owner reference: %v", pgName, podGroup.OwnerReferences[0])
	}
	return false, nil
}
