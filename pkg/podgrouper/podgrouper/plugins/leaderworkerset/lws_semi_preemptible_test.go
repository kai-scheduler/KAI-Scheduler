// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package leader_worker_set

import (
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/constants"
	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/defaultgrouper"
)

func lwsSemiPreemptiblePod(name, workerIndex string, semiPreemptible bool) *v1.Pod {
	labels := map[string]string{lwsWorkerIndexLabel: workerIndex}
	if semiPreemptible {
		labels[constants.PreemptibilityLabelKey] = "semi-preemptible"
	}
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default", Labels: labels},
	}
}

func TestLwsWarnOnSemiPreemptibleSegmented(t *testing.T) {
	tests := []struct {
		name            string
		segmentSize     *int64
		semiPreemptible bool
		expectWarning   bool
	}{
		{name: "segmented and semi-preemptible warns", segmentSize: ptr.To(int64(2)), semiPreemptible: true, expectWarning: true},
		{name: "segmented but not semi-preemptible no warning", segmentSize: ptr.To(int64(2)), semiPreemptible: false, expectWarning: false},
		{name: "semi-preemptible but not segmented no warning", segmentSize: nil, semiPreemptible: true, expectWarning: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner := lwsOwner("lws-test", "LeaderCreated", 5, tt.segmentSize, nil, nil)
			pod := lwsSemiPreemptiblePod("lws-test-0-1", "1", tt.semiPreemptible)

			grouper := NewLwsGrouper(defaultgrouper.NewDefaultGrouper("", "", fake.NewFakeClient()))
			metadata, err := grouper.GetPodGroupMetadata(owner, pod)
			assert.NoError(t, err)

			if tt.expectWarning {
				assert.NotEmpty(t, metadata.Warnings)
			} else {
				assert.Empty(t, metadata.Warnings)
			}
		})
	}
}
