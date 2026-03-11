// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package gpusharingconfigmap

import (
	"context"
	"fmt"
	"strconv"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	gpuSharingConfigMapAnnotation = "runai/shared-gpu-configmap"
	gpuSharingConfigMap           = "shared-gpu"
	maxVolumeNameLength           = 63
	configMapNameNumRandomChars   = 7
	configMapNameExtraChars       = configMapNameNumRandomChars + 6
)

type PodContainerRef struct {
	Container *v1.Container
	Index     int
	Type      ContainerType
}

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

func SetGpuCapabilitiesConfigMapName(pod *v1.Pod, containerRef *PodContainerRef) string {
	namePrefix, found := pod.Annotations[gpuSharingConfigMapAnnotation]
	if !found {
		namePrefix = generateConfigMapNamePrefix(pod, containerRef.Index)
		setConfigMapNameAnnotation(pod, namePrefix)
	}
	containerIndexStr := strconv.Itoa(containerRef.Index)
	if containerRef.Type == InitContainer {
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
	maxBaseNameLength := (maxVolumeNameLength - configMapNameExtraChars) - len(gpuSharingConfigMap)
	// also remove from the max length for "-{containerIndex}" or "-i{initContainerIndex}" in the name
	maxBaseNameLength = maxBaseNameLength - len(strconv.Itoa(containerIndex)) - 1
	// also allow for appending "-evar" in case of envFrom config map
	maxBaseNameLength = maxBaseNameLength - 5
	if len(baseName) > maxBaseNameLength {
		baseName = baseName[:maxBaseNameLength]
	}
	return fmt.Sprintf("%v-%v-%v", baseName,
		utilrand.String(configMapNameNumRandomChars), gpuSharingConfigMap)
}

func ExtractCapabilitiesConfigMapName(pod *v1.Pod, containerRef *PodContainerRef) (string, error) {
	containerIndexStr := strconv.Itoa(containerRef.Index)
	if containerRef.Type == InitContainer {
		containerIndexStr = "i" + containerIndexStr
	}

	namePrefix, found := pod.Annotations[gpuSharingConfigMapAnnotation]
	if !found {
		return "", fmt.Errorf("no desired configmap name found in pod %s/%s annotations", pod.Namespace, pod.Name)
	}
	capabilitiesConfigMapName := fmt.Sprintf("%s-%s", namePrefix, containerIndexStr)
	return capabilitiesConfigMapName, nil
}

func ExtractDirectEnvVarsConfigMapName(pod *v1.Pod, containerRef *PodContainerRef) (string, error) {
	configNameBase, err := ExtractCapabilitiesConfigMapName(pod, containerRef)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-evar", configNameBase), nil
}

func setConfigMapNameAnnotation(pod *v1.Pod, name string) {
	if pod.Annotations == nil {
		pod.Annotations = map[string]string{}
	}
	pod.Annotations[gpuSharingConfigMapAnnotation] = name
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
