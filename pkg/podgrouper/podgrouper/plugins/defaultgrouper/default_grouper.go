// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package defaultgrouper

import (
	"context"
	"encoding/json"
	"fmt"

	"golang.org/x/exp/maps"
	v1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	commonconsts "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"

	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgroup"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/topowner"
)

var (
	logger = log.FromContext(context.Background())
)

type DefaultGrouper struct {
	queueLabelKey    string
	nodePoolLabelKey string

	defaultPrioritiesConfigMapName      string
	defaultPrioritiesConfigMapNamespace string
	kubeReader                          client.Reader
}

func NewDefaultGrouper(queueLabelKey, nodePoolLabelKey string, kubeReader client.Reader) *DefaultGrouper {
	return &DefaultGrouper{
		queueLabelKey:    queueLabelKey,
		nodePoolLabelKey: nodePoolLabelKey,
		kubeReader:       kubeReader,
	}
}

func (dg *DefaultGrouper) SetDefaultPrioritiesConfigMapParams(defaultPrioritiesConfigMapName, defaultPrioritiesConfigMapNamespace string) {
	dg.defaultPrioritiesConfigMapName = defaultPrioritiesConfigMapName
	dg.defaultPrioritiesConfigMapNamespace = defaultPrioritiesConfigMapNamespace
}

func (dg *DefaultGrouper) Name() string {
	return "Default Grouper"
}

func (dg *DefaultGrouper) GetPodGroupMetadata(topOwner *unstructured.Unstructured, pod *v1.Pod, _ ...*metav1.PartialObjectMetadata) (*podgroup.Metadata, error) {
	podGroupMetadata := podgroup.Metadata{
		Owner: metav1.OwnerReference{
			APIVersion: topOwner.GetAPIVersion(),
			Kind:       topOwner.GetKind(),
			Name:       topOwner.GetName(),
			UID:        topOwner.GetUID(),
		},
		Namespace:         pod.GetNamespace(),
		Name:              dg.CalcPodGroupName(topOwner),
		Annotations:       dg.CalcPodGroupAnnotations(topOwner, pod),
		Labels:            dg.CalcPodGroupLabels(topOwner, pod),
		Queue:             dg.CalcPodGroupQueue(topOwner, pod),
		PriorityClassName: dg.CalcPodGroupPriorityClass(topOwner, pod, constants.TrainPriorityClass),
		MinAvailable:      1,
	}

	return &podGroupMetadata, nil
}

func (dg *DefaultGrouper) CalcPodGroupName(topOwner *unstructured.Unstructured) string {
	return fmt.Sprintf("%s-%s-%s", constants.PodGroupNamePrefix, topOwner.GetName(), topOwner.GetUID())
}

func (dg *DefaultGrouper) CalcPodGroupAnnotations(topOwner *unstructured.Unstructured, pod *v1.Pod) map[string]string {
	// Inherit all the annotations of the top owner
	pgAnnotations := make(map[string]string, len(topOwner.GetAnnotations())+2)

	if value, exists := pod.GetAnnotations()[constants.UserLabelKey]; exists {
		pgAnnotations[constants.UserLabelKey] = value
	}

	topOwnerMetadata := topowner.GetTopOwnerMetadata(topOwner)
	marshalledMetadata, err := topOwnerMetadata.MarshalYAML()
	if err != nil {
		logger.V(1).Error(err, "Unable to marshal top owner metadata", "metadata", topOwnerMetadata)
	} else {
		pgAnnotations[commonconsts.TopOwnerMetadataKey] = marshalledMetadata
	}

	maps.Copy(pgAnnotations, topOwner.GetAnnotations())

	return pgAnnotations
}

func (dg *DefaultGrouper) CalcPodGroupLabels(topOwner *unstructured.Unstructured, pod *v1.Pod) map[string]string {
	// Inherit all the labels of the top owner
	pgLabels := make(map[string]string, len(topOwner.GetLabels()))
	maps.Copy(pgLabels, topOwner.GetLabels())

	// Get podGroup user from the pod label
	if _, exists := pgLabels[constants.UserLabelKey]; !exists {
		if value, exists := pod.GetLabels()[constants.UserLabelKey]; exists {
			pgLabels[constants.UserLabelKey] = value
		}
	}

	return pgLabels
}

func (dg *DefaultGrouper) CalcPodGroupQueue(topOwner *unstructured.Unstructured, pod *v1.Pod) string {
	if queue, found := topOwner.GetLabels()[dg.queueLabelKey]; found {
		return queue
	} else if queue, found = pod.GetLabels()[dg.queueLabelKey]; found {
		return queue
	}

	queue := dg.calculateQueueName(topOwner, pod)
	if queue != "" {
		return queue
	}

	return constants.DefaultQueueName
}

func (dg *DefaultGrouper) calculateQueueName(topOwner *unstructured.Unstructured, pod *v1.Pod) string {
	project := ""
	if projectLabel, found := topOwner.GetLabels()[constants.ProjectLabelKey]; found {
		project = projectLabel
	} else if projectLabel, found := pod.GetLabels()[constants.ProjectLabelKey]; found {
		project = projectLabel
	}

	if project == "" {
		return ""
	}

	if nodePool, found := pod.GetLabels()[dg.nodePoolLabelKey]; found {
		return fmt.Sprintf("%s-%s", project, nodePool)
	}

	return project
}

func (dg *DefaultGrouper) CalcPodGroupPriorityClass(topOwner *unstructured.Unstructured, pod *v1.Pod,
	defaultPriorityClassForJob string) string {
	priorityClassName := dg.calcPodGroupPriorityClass(topOwner, pod)
	if dg.validatePriorityClassExists(priorityClassName) {
		return priorityClassName
	}

	if priorityClassName != "" {
		logger.V(2).Info("priorityClassName from pod or owner labels is not valid, falling back to default",
			"priorityClassName", priorityClassName, "topOwner", topOwner.GetName(), "pod", pod.GetName())
	}

	groupKind := topOwner.GroupVersionKind().GroupKind()
	priorityClassName = dg.getDefaultPriorityClassNameForKind(&groupKind)
	if dg.validatePriorityClassExists(priorityClassName) {
		return priorityClassName
	}

	logger.V(2).Info("No default priority class found for group kind, using default fallback",
		"groupKind", groupKind.String(), "defaultFallback", defaultPriorityClassForJob)
	return defaultPriorityClassForJob
}

func (dg *DefaultGrouper) calcPodGroupPriorityClass(topOwner *unstructured.Unstructured, pod *v1.Pod) string {
	if priorityClassName, found := topOwner.GetLabels()[constants.PriorityLabelKey]; found {
		return priorityClassName
	} else if priorityClassName, found = pod.GetLabels()[constants.PriorityLabelKey]; found {
		return priorityClassName
	} else if len(pod.Spec.PriorityClassName) != 0 {
		return pod.Spec.PriorityClassName
	}
	return ""
}

func (dg *DefaultGrouper) validatePriorityClassExists(priorityClassName string) bool {
	if priorityClassName == "" || dg.kubeReader == nil {
		return false
	}

	priorityClass := &schedulingv1.PriorityClass{}
	err := dg.kubeReader.Get(context.Background(), client.ObjectKey{Name: priorityClassName}, priorityClass)
	if err != nil {
		logger.V(1).Error(err, "Failed to get priority class", "priorityClassName", priorityClassName)
		return false
	}
	return true
}

// getDefaultPriorityClassNameForKind - returns the default priority class name for a given group kind.
func (dg *DefaultGrouper) getDefaultPriorityClassNameForKind(groupKind *schema.GroupKind) string {
	if groupKind == nil || groupKind.String() == "" || groupKind.Kind == "" {
		logger.V(3).Info("Unable to get default priority class name: GroupKind is empty, using default priority class fallback")
		return ""
	}

	defaultPriorities, err := dg.getDefaultPrioritiesPerTypeMapping()
	if err != nil {
		logger.V(1).Error(err, "Unable to get default priorities mapping")
		return ""
	}

	// Check if the groupKind is in the default priorities map.
	// It could be defined by its full name (e.g., "Deployment.apps") or just the kind (e.g., "Deployment").
	// This is to support the cases where we have two different group versions for the same kind.

	if priorityClassName, found := defaultPriorities[groupKind.String()]; found {
		return priorityClassName
	}
	if priorityClassName, found := defaultPriorities[groupKind.Kind]; found {
		return priorityClassName
	}

	return ""
}

// getDefaultPrioritiesPerTypeMapping - returns a map of workload type to default priority class name.
// It fetches the default priorities from a ConfigMap if configured, otherwise returns an empty map.
func (dg *DefaultGrouper) getDefaultPrioritiesPerTypeMapping() (map[string]string, error) {
	if dg.defaultPrioritiesConfigMapName == "" || dg.defaultPrioritiesConfigMapNamespace == "" ||
		dg.kubeReader == nil {
		return map[string]string{}, nil
	}

	configMap := &v1.ConfigMap{}
	err := dg.kubeReader.Get(context.Background(), client.ObjectKey{
		Name:      dg.defaultPrioritiesConfigMapName,
		Namespace: dg.defaultPrioritiesConfigMapNamespace,
	}, configMap)
	if err != nil {
		return nil, fmt.Errorf("failed to get default priorities configmap: %w", err)
	}

	return parseConfigMapDataToDefaultPriorities(configMap)
}

// workloadTypePriorityConfig - an internal struct type
// to be able to json-parse the configmap data.
type workloadTypePriorityConfig struct {
	TypeName     string `json:"typeName"`
	PriorityName string `json:"priorityName"`
}

// prioritiesConfigListToMapping - returns a map of type name -> default priority class name
func prioritiesConfigListToMapping(configs *[]workloadTypePriorityConfig) map[string]string {
	res := map[string]string{}
	for _, config := range *configs {
		res[config.TypeName] = config.PriorityName
	}
	return res
}

// parseConfigMapDataToDefaultPriorities - parses the data from the ConfigMap.
func parseConfigMapDataToDefaultPriorities(cm *v1.ConfigMap) (map[string]string, error) {
	if cm == nil || cm.Data == nil {
		return nil, fmt.Errorf("default priorities configmap is empty, cannot parse default priorities")
	}

	data, ok := cm.Data[constants.DefaultPrioritiesConfigMapTypesKey]
	if !ok {
		return nil, fmt.Errorf("default priorities configmap Data does not contain <%s> key", constants.DefaultPrioritiesConfigMapTypesKey)
	}

	var configs []workloadTypePriorityConfig
	err := json.Unmarshal([]byte(data), &configs)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal default priorities configmap data: %s", err.Error())
	}
	return prioritiesConfigListToMapping(&configs), nil
}
