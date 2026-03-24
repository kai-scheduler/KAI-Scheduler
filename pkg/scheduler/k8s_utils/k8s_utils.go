// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package k8s_utils

import (
	"context"
	"encoding/json"
	"fmt"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
)

type Interface interface {
	PatchPodAnnotationsAndLabelsInterface(
		kubeclient kubernetes.Interface, pod *v1.Pod, annotations, labels map[string]interface{},
	) error
}

type K8sHelpers struct{}

var Helpers Interface = &K8sHelpers{}

// PatchPodAnnotationsAndLabelsInterface allows to delete keys from annotations and labels dicts
func (_k8s *K8sHelpers) PatchPodAnnotationsAndLabelsInterface(
	kubeclient kubernetes.Interface, pod *v1.Pod, annotations, labels map[string]interface{},
) error {
	patchBytes, err := annotationAndLabelsPatchBytes(annotations, labels)
	if err != nil {
		log.InfraLogger.Errorf("Failed to patch labels and annotations for pod <%s/%s>. error: %v",
			pod.Namespace, pod.Name, err)
		return err
	}
	if _, err := kubeclient.CoreV1().Pods(pod.Namespace).Patch(
		context.Background(), pod.Name, types.MergePatchType,
		patchBytes, metav1.PatchOptions{},
	); err != nil {
		return fmt.Errorf("failed to update pod after updating configmap: %v", err)
	}
	return nil
}

func annotationAndLabelsPatchBytes(annotations, labels map[string]interface{}) ([]byte, error) {
	return json.Marshal(map[string]interface{}{"metadata": map[string]interface{}{
		"labels":      labels,
		"annotations": annotations,
	}})
}

func StringResourceList(resources v1.ResourceList) string {
	output := ""
	for name, value := range resources {
		output += fmt.Sprintf(" %s: %s", name, value.String())
	}

	return output
}
