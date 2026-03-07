/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package crd

import (
	"context"
	"time"

	testcontext "github.com/kai-scheduler/kai-scheduler/test/e2e/modules/context"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensions "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

const (
	TestcrdPlural   = "testcrds"
	TestcrdKind     = "Testcrd"
	TestcrdSingular = "testcrd"
	TestcrdGroup    = "test.kai"
	TestcrdVersion  = "v1"
)

type Testcrd struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`
}

func (t *Testcrd) DeepCopyObject() runtime.Object {
	out := &Testcrd{
		TypeMeta:   t.TypeMeta,
		ObjectMeta: t.ObjectMeta,
	}
	out.TypeMeta = t.TypeMeta
	out.ObjectMeta.DeepCopyInto(&t.ObjectMeta)
	return out
}

func CreateTestcrd(ctx context.Context, apiExtensionClient *apiextensions.Clientset) error {
	_, err := apiExtensionClient.ApiextensionsV1().CustomResourceDefinitions().Create(ctx, &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: TestcrdPlural + "." + TestcrdGroup,
		},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: TestcrdGroup,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
				{
					Name: TestcrdVersion,
					Schema: &apiextensionsv1.CustomResourceValidation{
						OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
							Description: "Testcrd is the Schema for the testcrds API",
							Properties: map[string]apiextensionsv1.JSONSchemaProps{
								"apiVersion": {
									Description: "APIVersion defines the versioned schema of this representation of an object. Servers should convert recognized schemas to the latest internal value, and may reject unrecognized values. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources",
									Type:        "string",
								},
								"kind": {
									Description: "Kind is a string value representing the REST resource this object represents. Servers may infer this from the endpoint the client submits requests to. Cannot be updated. In CamelCase. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds",
									Type:        "string",
								},
								"metadata": {
									Type: "object",
								},
							},
							Type: "object",
						},
					},
					Served:  true,
					Storage: true,
				},
			},
			Scope: apiextensionsv1.NamespaceScoped,
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Plural:     TestcrdPlural,
				Kind:       TestcrdKind,
				Singular:   TestcrdSingular,
				ShortNames: []string{TestcrdSingular},
			},
		},
	}, metav1.CreateOptions{})
	if err != nil && !errors.IsAlreadyExists(err) {
		return err
	}

	time.Sleep(10 * time.Second)
	return nil
}

func DeleteTestcrd(ctx context.Context, apiExtensionClient *apiextensions.Clientset) error {
	return apiExtensionClient.ApiextensionsV1().CustomResourceDefinitions().Delete(ctx, TestcrdPlural+"."+TestcrdGroup, metav1.DeleteOptions{})
}

func CreateTestcrdInstance(ctx context.Context, testCtx *testcontext.TestContext, namespace, name string) error {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   TestcrdGroup,
		Version: TestcrdVersion,
		Kind:    TestcrdKind,
	})
	obj.SetName(name)
	obj.SetNamespace(namespace)
	err := testCtx.ControllerClient.Create(ctx, obj)
	return err
}

func GetTestcrdInstance(ctx context.Context, testCtx *testcontext.TestContext, namespace, name string) (*unstructured.Unstructured, error) {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   TestcrdGroup,
		Version: TestcrdVersion,
		Kind:    TestcrdKind,
	})
	err := testCtx.ControllerClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, obj)
	return obj, err
}

func DeleteTestcrdInstance(ctx context.Context, testCtx *testcontext.TestContext, namespace, name string) error {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   TestcrdGroup,
		Version: TestcrdVersion,
		Kind:    TestcrdKind,
	})
	obj.SetName(name)
	obj.SetNamespace(namespace)
	return testCtx.ControllerClient.Delete(ctx, obj)
}
