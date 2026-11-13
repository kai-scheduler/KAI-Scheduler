// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package common

import (
	"context"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	nvidiav1 "github.com/kai-scheduler/KAI-scheduler/third_party/nvidia/gpu-operator/api/nvidia/v1"
)

// clusterPolicyCRD mirrors the part of the GPU Operator CRD the detection reads:
// the stored version's schema default for spec.cdi.enabled. nil means no default.
func clusterPolicyCRD(enabledDefault *bool) *apiextensionsv1.CustomResourceDefinition {
	enabled := apiextensionsv1.JSONSchemaProps{Type: "boolean"}
	if enabledDefault != nil {
		raw := []byte("false")
		if *enabledDefault {
			raw = []byte("true")
		}
		enabled.Default = &apiextensionsv1.JSON{Raw: raw}
	}
	return &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: clusterPolicyCRDName},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "nvidia.com",
			Names: apiextensionsv1.CustomResourceDefinitionNames{Plural: "clusterpolicies", Kind: "ClusterPolicy"},
			Scope: apiextensionsv1.ClusterScoped,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{
				Name: "v1", Served: true, Storage: true,
				Schema: &apiextensionsv1.CustomResourceValidation{OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
					Type: "object",
					Properties: map[string]apiextensionsv1.JSONSchemaProps{"spec": {
						Type: "object",
						Properties: map[string]apiextensionsv1.JSONSchemaProps{"cdi": {
							Type:       "object",
							Properties: map[string]apiextensionsv1.JSONSchemaProps{"enabled": enabled},
						}},
					}},
				}},
			}},
		},
	}
}

func TestIsGPUOperatorCDIEnabled(t *testing.T) {
	tests := []struct {
		name       string
		crdDefault *bool // nil: CRD has no default; the "no CRD" cases set noCRD
		noCRD      bool
		enabled    *bool
		cdiDef     *bool
		expected   bool
	}{
		{name: "25.10 CRD, no spec.cdi", crdDefault: ptr.To(true), expected: true},
		{name: "25.10 CRD, enabled false", crdDefault: ptr.To(true), enabled: ptr.To(false), expected: false},
		{name: "25.10 CRD, enabled true and default false", crdDefault: ptr.To(true), enabled: ptr.To(true), cdiDef: ptr.To(false), expected: true},
		{name: "pre-25.10 CRD, no spec.cdi", crdDefault: ptr.To(false), expected: false},
		{name: "pre-25.10 CRD, enabled only", crdDefault: ptr.To(false), enabled: ptr.To(true), expected: false},
		{name: "pre-25.10 CRD, enabled and default", crdDefault: ptr.To(false), enabled: ptr.To(true), cdiDef: ptr.To(true), expected: true},
		{name: "CRD without a default, enabled and default", enabled: ptr.To(true), cdiDef: ptr.To(true), expected: true},
		{name: "CRD without a default, no spec.cdi", expected: false},
		{name: "no CRD readable, enabled and default", noCRD: true, enabled: ptr.To(true), cdiDef: ptr.To(true), expected: true},
		{name: "no CRD readable, enabled only", noCRD: true, enabled: ptr.To(true), expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			if err := nvidiav1.AddToScheme(scheme); err != nil {
				t.Fatal(err)
			}
			if err := apiextensionsv1.AddToScheme(scheme); err != nil {
				t.Fatal(err)
			}
			objects := []client.Object{&nvidiav1.ClusterPolicy{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster-policy"},
				Spec: nvidiav1.ClusterPolicySpec{
					CDI: nvidiav1.CDIConfigSpec{Enabled: tt.enabled, Default: tt.cdiDef},
				},
			}}
			if !tt.noCRD {
				objects = append(objects, clusterPolicyCRD(tt.crdDefault))
			}
			reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()

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
	if err := apiextensionsv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(clusterPolicyCRD(ptr.To(true))).Build()
	got, err := IsGPUOperatorCDIEnabled(context.Background(), reader)
	if err != nil {
		t.Fatalf("IsGPUOperatorCDIEnabled() error = %v", err)
	}
	if got {
		t.Error("IsGPUOperatorCDIEnabled() = true without a ClusterPolicy")
	}
}
