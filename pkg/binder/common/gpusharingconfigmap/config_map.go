// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package gpusharingconfigmap

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	GPUSharingConfigMap         = "runai-sh-gpu"
	DesiredConfigMapPrefixKey   = "runai/shared-gpu-configmap"
	maxVolumeNameLength         = 63
	configMapNameNumRandomChars = 7
	configMapNameExtraChars     = configMapNameNumRandomChars + 6
)

func UpsertJobConfigMap(ctx context.Context,
	kubeClient client.Client, pod *v1.Pod, configMapName string, data map[string]string) (err error) {
	logger := log.FromContext(ctx)
	desiredConfigMap := &v1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: pod.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "v1",
					Kind:       "Pod",
					Name:       pod.Name,
					UID:        pod.UID,
				},
			},
		},
		Data: data,
	}

	existingCm := &v1.ConfigMap{}
	err = kubeClient.Get(ctx, client.ObjectKey{Name: desiredConfigMap.Name, Namespace: pod.Namespace}, existingCm)
	if err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("failed to check if configmap %s/%s exists for pod %s/%s, error: %v",
				pod.Namespace, desiredConfigMap.Name, pod.Namespace, pod.Name, err)
		}
		err := kubeClient.Create(ctx, desiredConfigMap)
		if err != nil {
			return fmt.Errorf("failed to create configmap %s/%s for pod %s/%s, error: %v",
				pod.Namespace, desiredConfigMap.Name, pod.Namespace, pod.Name, err)
		}
		return nil
	}
	logger.Info("Existing configmap was found for pod",
		"namespace", pod.Namespace, "name", pod.Name, "configMapName", desiredConfigMap.Name)
	err = patchConfigMap(ctx, kubeClient, desiredConfigMap, existingCm)
	if err != nil {
		err = fmt.Errorf("failed to patch configmap for pod %s/%s, error: %v", pod.Namespace, pod.Name, err)
	}
	return err
}

func patchConfigMap(
	ctx context.Context, kubeClient client.Client, desiredConfigMap, existingConfigMap *v1.ConfigMap,
) error {
	logger := log.FromContext(ctx)
	if different, reason := compareObjectOwners(desiredConfigMap.OwnerReferences, existingConfigMap.OwnerReferences); different {
		logger.Info("Going to override configmap, different owner",
			"namespace", desiredConfigMap.Namespace, "name", desiredConfigMap.Name, "reason", reason)

		err := kubeClient.Patch(ctx, desiredConfigMap, client.MergeFrom(existingConfigMap))
		if err != nil {
			return fmt.Errorf("failed to patch existing configmap %s/%s, error: %v",
				desiredConfigMap.Namespace, desiredConfigMap.Name, err)
		}
	} else {
		logger.Info("Configmap owner identical to existing configmap, updating data",
			"namespace", desiredConfigMap.Namespace, "name", desiredConfigMap.Name)

		updatedConfigMap := existingConfigMap.DeepCopy()
		for k, v := range desiredConfigMap.Data {
			if len(v) != 0 {
				updatedConfigMap.Data[k] = v
			}
		}
		err := kubeClient.Patch(ctx, updatedConfigMap, client.MergeFrom(existingConfigMap))
		if err != nil {
			return fmt.Errorf("failed to patch existing configmap %s/%s, error: %v",
				desiredConfigMap.Namespace, desiredConfigMap.Name, err)
		}
	}

	logger.Info("Successfully patched configmap",
		"namespace", desiredConfigMap.Namespace, "name", desiredConfigMap.Name)
	return nil
}

func SetGpuCapabilitiesConfigMapName(pod *v1.Pod, containerIndex int, containerType ContainerType) string {
	namePrefix, found := pod.Annotations[DesiredConfigMapPrefixKey]
	if !found {
		namePrefix = generateConfigMapNamePrefix(pod, containerIndex)
		setConfigMapNameAnnotation(pod, namePrefix)
	}
	containerIndexStr := strconv.Itoa(containerIndex)
	if containerType == InitContainer {
		containerIndexStr = "i" + containerIndexStr
	}
	capabilitiesConfigMapName := fmt.Sprintf("%s-%s", namePrefix, containerIndexStr)

	return capabilitiesConfigMapName
}

func generateConfigMapNamePrefix(pod *v1.Pod, containerIndex int) string {
	baseName := pod.Name
	if len(pod.OwnerReferences) > 0 {
		baseName = pod.OwnerReferences[0].Name
	}
	// volume name is the `${configMapName}-vol` and should be up to 63 bytes long,
	// 4 for "-vol" , 7 random chars, and 2 hyphens - 13 in total
	maxBaseNameLength := (maxVolumeNameLength - configMapNameExtraChars) - len(GPUSharingConfigMap)
	// also remove from the max length for "-{containerIndex}" or "-i{initContainerIndex}" in the name
	maxBaseNameLength = maxBaseNameLength - len(strconv.Itoa(containerIndex)) - 1
	// also allow for appending "-evar" in case of envFrom config map
	maxBaseNameLength = maxBaseNameLength - 5
	if len(baseName) > maxBaseNameLength {
		baseName = baseName[:maxBaseNameLength]
	}
	baseName = strings.TrimRight(baseName, ".-")
	return fmt.Sprintf("%v-%v-%v", baseName,
		utilrand.String(configMapNameNumRandomChars), GPUSharingConfigMap)
}

func ExtractCapabilitiesConfigMapName(pod *v1.Pod, containerIndex int, containerType ContainerType) (string, error) {
	containerIndexStr := strconv.Itoa(containerIndex)
	if containerType == InitContainer {
		containerIndexStr = "i" + containerIndexStr
	}

	namePrefix, found := pod.Annotations[DesiredConfigMapPrefixKey]
	if !found {
		return "", fmt.Errorf("no desired configmap name found in pod %s/%s annotations", pod.Namespace, pod.Name)
	}
	capabilitiesConfigMapName := fmt.Sprintf("%s-%s", namePrefix, containerIndexStr)
	return capabilitiesConfigMapName, nil
}

func ExtractDirectEnvVarsConfigMapName(pod *v1.Pod, containerIndex int, containerType ContainerType) (string, error) {
	configNameBase, err := ExtractCapabilitiesConfigMapName(pod, containerIndex, containerType)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-evar", configNameBase), nil
}

func HandleBCPod(ctx context.Context, kubeClient client.Client, pod *v1.Pod) (string, error) {
	logger := log.FromContext(ctx)
	desiredConfigMapName, err := GetDesiredConfigMapNameBC(pod)
	if err != nil {
		return "", err
	}
	logger.Info("Desired configmap name for backwards compatibility pod",
		"namespace", pod.Namespace, "name", pod.Name, "configMapName", desiredConfigMapName)
	err = UpdateBCPod(ctx, kubeClient, pod, desiredConfigMapName)
	return desiredConfigMapName, err
}

func GetDesiredConfigMapNameBC(pod *v1.Pod) (string, error) {
	cmName := ""
	for _, volume := range pod.Spec.Volumes {
		if volume.ConfigMap == nil {
			continue
		}

		possibleCmName := volume.ConfigMap.LocalObjectReference.Name
		if strings.HasSuffix(possibleCmName, GPUSharingConfigMap) {
			if cmName != "" {
				return "", fmt.Errorf("multiple desired gpu sharing configmap volumes detected for backwards "+
					"compatibility pod %s/%s", pod.Namespace, pod.Name)
			}
			cmName = possibleCmName
		}
	}

	if cmName == "" {
		return "", fmt.Errorf("no desired gpu sharing configmap volume detected for backwards compatibility pod "+
			"%s/%s", pod.Namespace, pod.Name)
	}

	return cmName, nil
}

func UpdateBCPod(ctx context.Context, kubeClient client.Client, pod *v1.Pod, desiredConfigMapName string) error {
	updatedPod := pod.DeepCopy()
	setConfigMapNameAnnotation(updatedPod, desiredConfigMapName)
	err := kubeClient.Patch(ctx, updatedPod, client.MergeFrom(pod))
	if err != nil {
		return fmt.Errorf("failed to update pod %v/%v with desired configmap name %v, error: %v",
			pod.Namespace, pod.Name, desiredConfigMapName, err)
	}
	return nil
}

func setConfigMapNameAnnotation(pod *v1.Pod, name string) {
	if pod.Annotations == nil {
		pod.Annotations = map[string]string{}
	}
	pod.Annotations[DesiredConfigMapPrefixKey] = name
}

// ownerReferencesDifferent compares two OwnerReferences and returns true if they are not the same
func ownerReferencesDifferent(lOwnerReferences, rOwnerReferences metav1.OwnerReference) bool {
	return !(lOwnerReferences.APIVersion == rOwnerReferences.APIVersion &&
		lOwnerReferences.Kind == rOwnerReferences.Kind &&
		lOwnerReferences.Name == rOwnerReferences.Name &&
		lOwnerReferences.UID == rOwnerReferences.UID)
}

// compareObjectOwners compares two lists of OwnerReferences and returns true if they are different and the reason:
// 1. Length
// 2. Different OwnerReference
func compareObjectOwners(lOwners, rOwners []metav1.OwnerReference) (bool, string) {
	different := false
	reason := ""

	if len(lOwners) != len(rOwners) {
		different = true
		reason += "OwnerReferences length is different, "
	}

	for _, lOwner := range lOwners {
		found := false
		for _, rOwner := range rOwners {
			if !ownerReferencesDifferent(lOwner, rOwner) {
				found = true
				break
			}
		}

		if !found {
			different = true
			reason += fmt.Sprintf("OwnerReference not found: \n%v", lOwner)
		}
	}

	return different, reason
}

func AddDataConfigField(data map[string]string, key, value string) {
	if foundValue, found := data[key]; !found || len(foundValue) == 0 {
		data[key] = value
	} else {
		data[key] += fmt.Sprintf("\n%v", value)
	}
}
