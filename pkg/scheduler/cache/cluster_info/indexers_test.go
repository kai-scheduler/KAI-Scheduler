// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package cluster_info

import (
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	v12 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
)

func TestPodByPodGroupIndexerValidPod(t *testing.T) {
	pod := &v1.Pod{
		ObjectMeta: v12.ObjectMeta{
			Annotations: map[string]string{
				commonconstants.PodGroupAnnotationForPod: "my-pod-group",
			},
		},
	}

	podGroups, err := podByPodGroupIndexer(pod)

	assert.Equal(t, nil, err)
	assert.Equal(t, 1, len(podGroups))
	assert.Equal(t, "my-pod-group", podGroups[0])
}

func TestPodByPodGroupIndexerNotPod(t *testing.T) {
	node := &v1.Node{}

	podGroups, err := podByPodGroupIndexer(node)

	assert.Equal(t, nil, err)
	assert.Equal(t, 1, len(podGroups))
	assert.Equal(t, "", podGroups[0])
}
