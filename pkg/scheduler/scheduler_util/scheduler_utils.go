// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scheduler_util

import (
	"fmt"

	v1 "k8s.io/api/core/v1"
	corev1helpers "k8s.io/component-helpers/scheduling/corev1"
	"k8s.io/klog/v2"
)

var unschedulableTaint = &v1.Taint{
	Key:    v1.TaintNodeUnschedulable,
	Effect: v1.TaintEffectNoSchedule,
}

// CheckNodeConditionPredicate reports whether a node is fit for scheduling based on its
// conditions and schedulability. A cordoned node (spec.unschedulable) is rejected unless
// tolerations tolerate the node.kubernetes.io/unschedulable:NoSchedule taint, matching
// upstream kube-scheduler's NodeUnschedulable plugin. Pass nil tolerations for
// pod-independent checks (resource accounting), which always reject cordoned nodes.
func CheckNodeConditionPredicate(node *v1.Node, tolerations []v1.Toleration) (bool, []string, error) {
	if node == nil {
		return false, nil, fmt.Errorf("node is nil")
	}
	reasons := []string{}

	if node.Spec.Unschedulable && !toleratesUnschedulable(tolerations) {
		reasons = append(reasons, "node is unschedulable")
	}

	for _, c := range node.Status.Conditions {
		switch c.Type {
		case v1.NodeReady:
			if c.Status != v1.ConditionTrue {
				reasons = append(reasons, "node has NotReady condition")
			}
		case v1.NodeMemoryPressure,
			v1.NodeDiskPressure,
			v1.NodePIDPressure,
			v1.NodeNetworkUnavailable:

			if c.Status != v1.ConditionFalse {
				reasons = append(reasons, fmt.Sprintf("node has %s condition", c.Type))
			}
		}
	}

	return len(reasons) == 0, reasons, nil
}

func toleratesUnschedulable(tolerations []v1.Toleration) bool {
	if len(tolerations) == 0 {
		return false
	}
	return corev1helpers.TolerationsTolerateTaint(klog.Background(), tolerations, unschedulableTaint, false)
}

func ValidateIsNodeReady(node *v1.Node) bool {
	ready, _, _ := CheckNodeConditionPredicate(node, nil)
	return ready
}
