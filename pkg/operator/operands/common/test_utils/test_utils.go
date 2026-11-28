// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package test_utils

import (
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func FindTypeInObjects[T client.Object](objects []client.Object) *T {
	for _, obj := range objects {
		typedObj, ok := obj.(T)
		if ok {
			return &typedObj
		}
	}

	return nil
}

func FindTypesInObjects[T client.Object](objects []client.Object) []T {
	var result []T
	for _, obj := range objects {
		typedObj, ok := obj.(T)
		if ok {
			result = append(result, typedObj)
		}
	}

	return result
}

// ClusterPolicyCRD returns the part of the GPU Operator ClusterPolicy CRD that CDI
// detection reads: the stored version's default for spec.cdi.enabled. A nil
// enabledDefault leaves the field without a default.
func ClusterPolicyCRD(enabledDefault *bool) *apiextensionsv1.CustomResourceDefinition {
	enabled := apiextensionsv1.JSONSchemaProps{Type: "boolean"}
	if enabledDefault != nil {
		raw := []byte("false")
		if *enabledDefault {
			raw = []byte("true")
		}
		enabled.Default = &apiextensionsv1.JSON{Raw: raw}
	}
	return &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: "clusterpolicies.nvidia.com"},
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
