// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package scheduler_util

import (
	"testing"

	v1 "k8s.io/api/core/v1"
)

func createTestNode(conditions []v1.NodeCondition) *v1.Node {
	return &v1.Node{
		Spec: v1.NodeSpec{
			Unschedulable: false,
		},
		Status: v1.NodeStatus{
			Conditions: conditions,
		},
	}
}

func TestValidateReadyNode(t *testing.T) {
	node := createTestNode([]v1.NodeCondition{
		{
			Type:   v1.NodeReady,
			Status: v1.ConditionTrue,
		},
	})
	if !ValidateIsNodeReady(node) {
		t.Errorf("ValidateIsNodeReady - ready node")
	}
}

func TestValidateUnreadyNode(t *testing.T) {
	node := createTestNode([]v1.NodeCondition{
		{
			Type:   v1.NodeReady,
			Status: v1.ConditionFalse,
		},
	})
	if ValidateIsNodeReady(node) {
		t.Errorf("ValidateIsNodeReady - unready node")
	}
}

func TestValidateUnknownReadyNode(t *testing.T) {
	node := createTestNode([]v1.NodeCondition{
		{
			Type:   v1.NodeReady,
			Status: v1.ConditionUnknown,
		},
	})
	if ValidateIsNodeReady(node) {
		t.Errorf("ValidateIsNodeReady - unknown ready node")
	}
}

func TestValidateUnschedulableNode(t *testing.T) {
	node := createTestNode([]v1.NodeCondition{
		{
			Type:   v1.NodeReady,
			Status: v1.ConditionTrue,
		},
	})
	node.Spec.Unschedulable = true
	if ValidateIsNodeReady(node) {
		t.Errorf("ValidateIsNodeReady - unschedulable node")
	}
}

func TestValidateNodeWithCustomCondition(t *testing.T) {
	node := createTestNode([]v1.NodeCondition{
		{
			Type:   v1.NodeReady,
			Status: v1.ConditionTrue,
		},
		{
			Type:   "CustomCondition",
			Status: v1.ConditionTrue,
		},
	})
	if !ValidateIsNodeReady(node) {
		t.Errorf("ValidateIsNodeReady - node with custom condition")
	}
}

func TestNodeWithMemoryPressure(t *testing.T) {
	node := createTestNode([]v1.NodeCondition{
		{
			Type:   v1.NodeReady,
			Status: v1.ConditionTrue,
		},
		{
			Type:   v1.NodeMemoryPressure,
			Status: v1.ConditionTrue,
		},
	})
	if ValidateIsNodeReady(node) {
		t.Errorf("ValidateIsNodeReady - node with memory preasure")
	}
}

func TestNodeWithDiskPressure(t *testing.T) {
	node := createTestNode([]v1.NodeCondition{
		{
			Type:   v1.NodeReady,
			Status: v1.ConditionTrue,
		},
		{
			Type:   v1.NodeDiskPressure,
			Status: v1.ConditionTrue,
		},
	})
	if ValidateIsNodeReady(node) {
		t.Errorf("ValidateIsNodeReady - node with disk preasure")
	}
}

func TestNodeWithPIDPressure(t *testing.T) {
	node := createTestNode([]v1.NodeCondition{
		{
			Type:   v1.NodeReady,
			Status: v1.ConditionTrue,
		},
		{
			Type:   v1.NodePIDPressure,
			Status: v1.ConditionTrue,
		},
	})
	if ValidateIsNodeReady(node) {
		t.Errorf("ValidateIsNodeReady - node with PID preasure")
	}
}

func TestNodeWithUnavailableNetwork(t *testing.T) {
	node := createTestNode([]v1.NodeCondition{
		{
			Type:   v1.NodeReady,
			Status: v1.ConditionTrue,
		},
		{
			Type:   v1.NodeNetworkUnavailable,
			Status: v1.ConditionTrue,
		},
	})
	if ValidateIsNodeReady(node) {
		t.Errorf("ValidateIsNodeReady - node with unavailable network")
	}
}

func TestUnschedulableNodeWithTolerationIsFit(t *testing.T) {
	node := createTestNode([]v1.NodeCondition{
		{
			Type:   v1.NodeReady,
			Status: v1.ConditionTrue,
		},
	})
	node.Spec.Unschedulable = true
	tolerations := []v1.Toleration{
		{
			Key:      v1.TaintNodeUnschedulable,
			Operator: v1.TolerationOpExists,
			Effect:   v1.TaintEffectNoSchedule,
		},
	}
	fit, reasons, err := CheckNodeConditionPredicate(node, tolerations)
	if err != nil {
		t.Fatalf("CheckNodeConditionPredicate - unexpected error: %v", err)
	}
	if !fit {
		t.Errorf("CheckNodeConditionPredicate - unschedulable node with matching toleration should be fit, reasons: %v", reasons)
	}
}

func TestUnschedulableNodeWithUnrelatedTolerationIsNotFit(t *testing.T) {
	node := createTestNode([]v1.NodeCondition{
		{
			Type:   v1.NodeReady,
			Status: v1.ConditionTrue,
		},
	})
	node.Spec.Unschedulable = true
	tolerations := []v1.Toleration{
		{
			Key:      "some.other/taint",
			Operator: v1.TolerationOpExists,
			Effect:   v1.TaintEffectNoSchedule,
		},
	}
	fit, _, err := CheckNodeConditionPredicate(node, tolerations)
	if err != nil {
		t.Fatalf("CheckNodeConditionPredicate - unexpected error: %v", err)
	}
	if fit {
		t.Errorf("CheckNodeConditionPredicate - unschedulable node with unrelated toleration should not be fit")
	}
}
