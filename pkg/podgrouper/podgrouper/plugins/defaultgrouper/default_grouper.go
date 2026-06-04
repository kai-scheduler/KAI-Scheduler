// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package defaultgrouper

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"golang.org/x/exp/maps"
	v1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2alpha2"
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

	// default config per type - includes the default priority class name and preemptibility per workload type
	defaultConfigPerTypeConfigMapName      string
	defaultConfigPerTypeConfigMapNamespace string
	kubeReader                             client.Reader
}

func NewDefaultGrouper(queueLabelKey, nodePoolLabelKey string, kubeReader client.Reader) *DefaultGrouper {
	return &DefaultGrouper{
		queueLabelKey:    queueLabelKey,
		nodePoolLabelKey: nodePoolLabelKey,
		kubeReader:       kubeReader,
	}
}

func (dg *DefaultGrouper) SetDefaultConfigPerTypeConfigMapParams(defaultConfigPerTypeConfigMapName, defaultConfigPerTypeConfigMapNamespace string) {
	dg.defaultConfigPerTypeConfigMapName = defaultConfigPerTypeConfigMapName
	dg.defaultConfigPerTypeConfigMapNamespace = defaultConfigPerTypeConfigMapNamespace
}

func (dg *DefaultGrouper) Name() string {
	return "Default Grouper"
}

func (dg *DefaultGrouper) GetPodGroupMetadata(topOwner *unstructured.Unstructured, pod *v1.Pod, allOwners ...*metav1.PartialObjectMetadata) (*podgroup.Metadata, error) {
	if len(allOwners) == 0 {
		// If the allOwners list is empty, set the top owner as the only owner.
		// This supports the podJob case, where we consider the actual pod as the "topOwner", although it's not an actual owner.
		allOwners = []*metav1.PartialObjectMetadata{unstructuredToPartialObjectMetadata(topOwner)}
	}
	priorityClassName, defaults := dg.calcPriorityClassWithDefaults(allOwners, pod, constants.TrainPriorityClass)
	preemptibility := dg.calcPodGroupPreemptibilityWithDefaults(allOwners, pod, defaults)

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
		PriorityClassName: priorityClassName,
		Preemptibility:    preemptibility,
		MinAvailable:      1,
	}

	annotations := topOwner.GetAnnotations()
	podGroupMetadata.PreferredTopologyLevel = annotations["kai.scheduler/topology-preferred-placement"]
	podGroupMetadata.RequiredTopologyLevel = annotations["kai.scheduler/topology-required-placement"]
	podGroupMetadata.Topology = annotations["kai.scheduler/topology"]

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
	// Convert topOwner to PartialObjectMetadata for compatibility
	ownerPartial := &metav1.PartialObjectMetadata{
		TypeMeta: metav1.TypeMeta{
			APIVersion: topOwner.GetAPIVersion(),
			Kind:       topOwner.GetKind(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      topOwner.GetName(),
			Namespace: topOwner.GetNamespace(),
			Labels:    topOwner.GetLabels(),
		},
	}
	priorityClassName, _ := dg.calcPriorityClassWithDefaults([]*metav1.PartialObjectMetadata{ownerPartial}, pod, defaultPriorityClassForJob)
	return priorityClassName
}

// calcPriorityClassWithDefaults - resolves priority class using:
// 1) explicit labels (owners/pod), if valid
// 2) defaults from ConfigMap (returned to allow reuse by caller)
// 3) final fallback to defaultPriorityClassForJob
// Returns the resolved priority class name and the defaults mapping used (if any).
func (dg *DefaultGrouper) calcPriorityClassWithDefaults(allOwners []*metav1.PartialObjectMetadata, pod *v1.Pod,
	defaultPriorityClassForJob string) (string, map[string]workloadTypePriorityConfig) {
	// First, try to get priority class from explicit labels (owners/pod)
	for _, owner := range allOwners {
		priorityClassName := dg.calcPodGroupPriorityClass(owner, pod)
		if dg.validatePriorityClassExists(priorityClassName) {
			return priorityClassName, nil
		}
		if priorityClassName != "" {
			logger.V(1).Info("priorityClassName from pod or owner labels is not valid",
				"priorityClassName", priorityClassName, "owner", owner.GetName(), "pod", pod.GetName())
		}
	}

	// If no explicit priority class found, try defaults from ConfigMap for each owner
	defaultConfigs, err := dg.getDefaultConfigsPerTypeMapping()
	if err != nil {
		logger.Error(err, "Unable to get default values mapping for priority class", "pod", pod.GetName())
		return defaultPriorityClassForJob, nil
	}

	// Loop through owners to find a default priority class
	for _, owner := range allOwners {
		groupKind := owner.GroupVersionKind().GroupKind()
		priorityClassName := dg.getDefaultPriorityClassNameForKind(&groupKind, defaultConfigs)
		if dg.validatePriorityClassExists(priorityClassName) {
			return priorityClassName, defaultConfigs
		}
	}

	logger.V(1).Info("No default priority class found for any owner, using default fallback",
		"defaultFallback", defaultPriorityClassForJob)
	return defaultPriorityClassForJob, defaultConfigs
}

func (dg *DefaultGrouper) calcPodGroupPreemptibilityWithDefaults(
	allOwners []*metav1.PartialObjectMetadata,
	pod *v1.Pod,
	defaults map[string]workloadTypePriorityConfig) v2alpha2.Preemptibility {
	// First, try to get preemptibility from explicit labels (owners/pod)
	for _, owner := range allOwners {
		if preemptibilityStr, found := owner.GetLabels()[constants.PreemptibilityLabelKey]; found {
			if preemptibility, err := v2alpha2.ParsePreemptibility(preemptibilityStr); err == nil {
				return preemptibility
			} else {
				logger.Error(err, "Invalid preemptibility label found on owner", "owner", owner.GetName(), "preemptibility", preemptibilityStr)
			}
		}
	}
	if preemptibilityStr, found := pod.GetLabels()[constants.PreemptibilityLabelKey]; found {
		if preemptibility, err := v2alpha2.ParsePreemptibility(preemptibilityStr); err == nil {
			return preemptibility
		} else {
			logger.Error(err, "Invalid preemptibility label found on pod", "pod", pod.GetName())
		}
	}

	// If no explicit preemptibility found, try defaults from ConfigMap for each owner
	if len(defaults) == 0 {
		var err error
		defaults, err = dg.getDefaultConfigsPerTypeMapping()
		if err != nil {
			logger.Error(err, "Unable to get default values mapping for preemptibility", "pod", pod.GetName())
			return ""
		}
	}

	// Loop through owners to find a default preemptibility
	for _, owner := range allOwners {
		groupKind := owner.GroupVersionKind().GroupKind()
		defaultConfig, found := selectDefaultsForKind(defaults, &groupKind)
		if found && defaultConfig.Preemptibility != "" {
			if preemptibility, err := v2alpha2.ParsePreemptibility(strings.ToLower(defaultConfig.Preemptibility)); err == nil {
				return preemptibility
			} else {
				logger.Error(err, "Invalid preemptibility found in defaults configmap")
			}
		}
	}

	logger.V(1).Info("No valid preemptibility label or default found", "pod", pod.GetName())
	return ""
}

func (dg *DefaultGrouper) calcPodGroupPriorityClass(owner *metav1.PartialObjectMetadata, pod *v1.Pod) string {
	if priorityClassName, found := owner.GetLabels()[constants.PriorityLabelKey]; found {
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
		logger.V(1).Info("Failed to get priority class", "priorityClassName", priorityClassName, "error", err.Error())
		return false
	}
	return true
}

// getDefaultPriorityClassNameForKind - returns the default priority class name for a given group kind
func (dg *DefaultGrouper) getDefaultPriorityClassNameForKind(groupKind *schema.GroupKind, defaultConfigs map[string]workloadTypePriorityConfig) string {
	if defaultConfigs == nil || len(defaultConfigs) == 0 {
		logger.V(3).Info("Unable to get default priority class name: defaults mapping is empty, using default priority class fallback")
		return ""
	}

	if groupKind == nil || groupKind.String() == "" || groupKind.Kind == "" {
		logger.V(3).Info("Unable to get default priority class name: GroupKind is empty, using default priority class fallback")
		return ""
	}

	defaultConfig, found := selectDefaultsForKind(defaultConfigs, groupKind)
	if found {
		return defaultConfig.PriorityName
	}

	return ""
}

func (dg *DefaultGrouper) ResolveDefaultsForKind(groupKind schema.GroupKind) (string, string, error) {
	defaults, err := dg.getDefaultConfigsPerTypeMapping()
	if err != nil {
		return "", "", err
	}

	defaultConfig, found := selectDefaultsForKind(defaults, &groupKind)
	if !found {
		return "", "", nil
	}

	priorityClassName := defaultConfig.PriorityName
	if !dg.validatePriorityClassExists(priorityClassName) {
		priorityClassName = ""
	}

	return priorityClassName, defaultConfig.Preemptibility, nil
}

// getDefaultConfigsPerTypeMapping - returns a map of workload groupKind to default workload-type config (priorityClassName and preemptibility).
// It fetches the defaults from a ConfigMap if configured, otherwise returns an empty map.
func (dg *DefaultGrouper) getDefaultConfigsPerTypeMapping() (map[string]workloadTypePriorityConfig, error) {
	if dg.defaultConfigPerTypeConfigMapName == "" || dg.defaultConfigPerTypeConfigMapNamespace == "" ||
		dg.kubeReader == nil {
		return map[string]workloadTypePriorityConfig{}, nil
	}

	configMap := &v1.ConfigMap{}
	err := dg.kubeReader.Get(context.Background(), client.ObjectKey{
		Name:      dg.defaultConfigPerTypeConfigMapName,
		Namespace: dg.defaultConfigPerTypeConfigMapNamespace,
	}, configMap)
	if err != nil {
		return nil, fmt.Errorf("failed to get default configs per type configmap: %w", err)
	}

	return parseConfigMapDataToDefaultConfigs(configMap)
}

// workloadTypePriorityConfig - an internal struct type
// to be able to json-parse the configmap data.
type workloadTypePriorityConfig struct {
	TypeName       string `json:"typeName"`
	Group          string `json:"group"`
	PriorityName   string `json:"priorityName"`
	Preemptibility string `json:"preemptibility"`
}

// configsToMapPerGroupKind - returns a map of groupKind -> default workload-type config
func configsToMapPerGroupKind(configs *[]workloadTypePriorityConfig) map[string]workloadTypePriorityConfig {
	res := map[string]workloadTypePriorityConfig{}
	for _, config := range *configs {
		groupKind := schema.GroupKind{Group: config.Group, Kind: config.TypeName}.String()
		res[groupKind] = config
	}
	return res
}

// parseConfigMapDataToDefaultConfigs - parses the data from the ConfigMap and returns it as a map of groupKind -> default config.
func parseConfigMapDataToDefaultConfigs(cm *v1.ConfigMap) (map[string]workloadTypePriorityConfig, error) {
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
	return configsToMapPerGroupKind(&configs), nil
}

// selectDefaultsForKind - returns defaults for a given group kind from a combined defaults mapping
func selectDefaultsForKind(defaults map[string]workloadTypePriorityConfig, groupKind *schema.GroupKind) (workloadTypePriorityConfig, bool) {
	if defaults == nil || len(defaults) == 0 || groupKind == nil || groupKind.String() == "" {
		return workloadTypePriorityConfig{}, false
	}

	// Check if the groupKind is in the default configs map.
	// It could be defined by its full name (e.g., "Deployment.apps") or just the kind (e.g., "Deployment").
	// This is to support the cases where we have two different group versions for the same kind.

	if defaultConfig, found := defaults[groupKind.String()]; found {
		return defaultConfig, true
	}
	if defaultConfig, found := defaults[groupKind.Kind]; found {
		return defaultConfig, true
	}
	return workloadTypePriorityConfig{}, false
}

func unstructuredToPartialObjectMetadata(topOwner *unstructured.Unstructured) *metav1.PartialObjectMetadata {
	return &metav1.PartialObjectMetadata{
		TypeMeta: metav1.TypeMeta{
			APIVersion: topOwner.GetAPIVersion(),
			Kind:       topOwner.GetKind(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        topOwner.GetName(),
			Namespace:   topOwner.GetNamespace(),
			Labels:      topOwner.GetLabels(),
			Annotations: topOwner.GetAnnotations(),
		},
	}
}
