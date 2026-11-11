// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package common

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	nvidiav1 "github.com/kai-scheduler/KAI-scheduler/third_party/nvidia/gpu-operator/api/nvidia/v1"
)

func TestIsGPUOperatorCDIEnabled(t *testing.T) {
	tests := []struct {
		name     string
		version  string
		enabled  *bool
		cdiDef   *bool
		expected bool
	}{
		{name: "25.10 with no spec.cdi", version: "v25.10.1", expected: true},
		{name: "26.7 with no spec.cdi", version: "v26.7.2", expected: true},
		{name: "25.10 with enabled false", version: "v25.10.1", enabled: ptr.To(false), expected: false},
		{name: "25.10 with enabled true and default false", version: "v25.10.1", enabled: ptr.To(true), cdiDef: ptr.To(false), expected: true},
		{name: "25.3 with no spec.cdi", version: "v25.3.0", expected: false},
		{name: "25.3 with enabled only", version: "v25.3.0", enabled: ptr.To(true), expected: false},
		{name: "25.3 with enabled and default", version: "v25.3.0", enabled: ptr.To(true), cdiDef: ptr.To(true), expected: true},
		{name: "no version label with no spec.cdi", expected: false},
		{name: "no version label with enabled and default", enabled: ptr.To(true), cdiDef: ptr.To(true), expected: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			if err := nvidiav1.AddToScheme(scheme); err != nil {
				t.Fatal(err)
			}
			policy := &nvidiav1.ClusterPolicy{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster-policy"},
				Spec: nvidiav1.ClusterPolicySpec{
					CDI: nvidiav1.CDIConfigSpec{Enabled: tt.enabled, Default: tt.cdiDef},
				},
			}
			if tt.version != "" {
				policy.Labels = map[string]string{gpuOperatorVersionLabelName: tt.version}
			}
			reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(policy).Build()

			got, err := IsGPUOperatorCDIEnabled(context.Background(), reader)
			if err != nil {
				t.Fatalf("IsGPUOperatorCDIEnabled() error = %v", err)
			}
			if got != tt.expected {
				t.Errorf("IsGPUOperatorCDIEnabled() = %t, expected %t", got, tt.expected)
			}
		})
	}
}

func TestIsGPUOperatorCDIEnabledWithoutClusterPolicy(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := nvidiav1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	got, err := IsGPUOperatorCDIEnabled(context.Background(), fake.NewClientBuilder().WithScheme(scheme).Build())
	if err != nil {
		t.Fatalf("IsGPUOperatorCDIEnabled() error = %v", err)
	}
	if got {
		t.Error("IsGPUOperatorCDIEnabled() = true without a ClusterPolicy")
	}
}
