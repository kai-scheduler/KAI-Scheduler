// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package cluster_info

import (
	v1 "k8s.io/api/core/v1"

	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
)

const (
	podByPodGroupIndexerName = "podByPodGroupIndexer"
)

func podByPodGroupIndexer(obj interface{}) ([]string, error) {
	pod, ok := obj.(*v1.Pod)
	if !ok {
		log.InfraLogger.V(2).Warnf("error converting object to pod: \n%+v", obj)
		return []string{""}, nil
	}

	podGroup := pod.Annotations[commonconstants.PodGroupAnnotationForPod]
	return []string{podGroup}, nil
}
