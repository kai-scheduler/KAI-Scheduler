// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package predicates

import (
	v1 "k8s.io/api/core/v1"
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/volumebinding"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/k8s_internal"
)

// NewVolumeBindingFilter returns a function that wraps k8s internal volume binding filter, while ignoring insufficient
// resources error if configured to.
func NewVolumeBindingFilter(ssn *framework.Session, plugin k8sframework.Plugin, ignoreInsufficientResources bool) k8s_internal.FitPredicateFilter {
	filterFunc := k8s_internal.FitPredicateConverter(ssn, plugin.(*volumebinding.VolumeBinding))
	return func(pod *v1.Pod, nodeInfo *k8sframework.NodeInfo) (bool, []string, error) {
		success, reasons, err := filterFunc(pod, nodeInfo)
		if ignoreInsufficientResources && err != nil {
			if err.Error() == volumebinding.ErrReasonNotEnoughSpace {
				return true, nil, nil
			}
		}
		return success, reasons, err
	}
}
