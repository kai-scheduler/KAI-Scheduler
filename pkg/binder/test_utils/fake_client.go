// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package test_utils

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

var EmptyBind = interceptor.Funcs{
	SubResource: func(c client.WithWatch, subResource string) client.SubResourceClient {
		return &FakeSubResourceClient{}
	},
}

type FakeSubResourceClient struct {
	FailAll bool
	Msg     string
}

func (f *FakeSubResourceClient) Get(_ context.Context, _ client.Object, _ client.Object, _ ...client.SubResourceGetOption) error {
	if f.FailAll {
		return fmt.Errorf("%s", f.Msg)
	}
	return nil
}

func (f *FakeSubResourceClient) Create(_ context.Context, _ client.Object, _ client.Object, _ ...client.SubResourceCreateOption) error {
	if f.FailAll {
		return fmt.Errorf("%s", f.Msg)
	}
	return nil
}

func (f *FakeSubResourceClient) Update(_ context.Context, _ client.Object, _ ...client.SubResourceUpdateOption) error {
	if f.FailAll {
		return fmt.Errorf("%s", f.Msg)
	}
	return nil
}

func (f *FakeSubResourceClient) Patch(_ context.Context, _ client.Object, _ client.Patch, _ ...client.SubResourcePatchOption) error {
	if f.FailAll {
		return fmt.Errorf("%s", f.Msg)
	}
	return nil
}

func (f *FakeSubResourceClient) Apply(_ context.Context, _ runtime.ApplyConfiguration, _ ...client.SubResourceApplyOption) error {
	if f.FailAll {
		return fmt.Errorf("%s", f.Msg)
	}
	return nil
}

var FailedBindingFuncs = interceptor.Funcs{
	SubResource: func(c client.WithWatch, subResource string) client.SubResourceClient {
		if subResource == "bind" {
			return &FakeSubResourceClient{
				FailAll: true,
				Msg:     "bind",
			}
		}
		return c.SubResource(subResource)
	},
}
