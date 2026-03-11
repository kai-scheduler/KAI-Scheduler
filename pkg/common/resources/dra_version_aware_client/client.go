// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package draversionawareclient

import (
	"k8s.io/client-go/kubernetes"
	resourcev1 "k8s.io/client-go/kubernetes/typed/resource/v1"
	draclient "k8s.io/dynamic-resource-allocation/client"
)

type draAwareKubeClient struct {
	kubernetes.Interface
	draClient *draclient.Client
}

func NewDRAAwareClient(client kubernetes.Interface) kubernetes.Interface {
	return &draAwareKubeClient{
		Interface: client,
		draClient: draclient.New(client),
	}
}

func (c *draAwareKubeClient) ResourceV1() resourcev1.ResourceV1Interface {
	return c.draClient
}
