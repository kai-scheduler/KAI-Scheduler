// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resources

import (
	"context"
	"reflect"
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kai-scheduler/kai-scheduler/pkg/common/constants"
)

func Test_extractReceivedResources(t *testing.T) {
	type args struct {
		pod  *v1.Pod
		node *v1.Node
	}
	tests := []struct {
		name    string
		args    args
		want    v1.ResourceList
		wantErr bool
	}{
		{
			"receivedTypeNone",
			args{
				&v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{receivedResourceTypeAnnotationName: receivedTypeNone},
					},
				},
				&v1.Node{},
			},
			v1.ResourceList{},
			false,
		},
		{
			"receivedTypeRegular",
			args{
				&v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{receivedResourceTypeAnnotationName: receivedTypeRegular},
					},
				},
				&v1.Node{},
			},
			v1.ResourceList{},
			false,
		},
		{
			"receivedTypeFraction",
			args{
				&v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							receivedResourceTypeAnnotationName: receivedTypeFraction,
							constants.GpuFraction:              "0.4",
						},
					},
				},
				&v1.Node{},
			},
			v1.ResourceList{constants.NvidiaGpuResource: resource.MustParse("0.4")},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			err := v1.AddToScheme(scheme)
			if err != nil {
				t.Fatal(err)
			}
			kubeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.args.node).Build()

			got, err := ExtractGPUSharingReceivedResources(context.TODO(), tt.args.pod, kubeClient)
			if (err != nil) != tt.wantErr {
				t.Errorf("extractReceivedResources() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("extractReceivedResources() got = %v, want %v", got, tt.want)
			}
		})
	}
}
