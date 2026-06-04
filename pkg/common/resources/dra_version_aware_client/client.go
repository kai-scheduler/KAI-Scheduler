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

// IsWatchListSemanticsUnSupported forwards the optional reflector hint
// (see k8s.io/client-go/util/watchlist.DoesClientNotSupportWatchListSemantics)
// from the wrapped client. Embedding kubernetes.Interface does not promote
// optional methods on concrete implementations (e.g. fake.Clientset), so
// without this forward the reflector cannot detect that fake clients don't
// support WatchList and hangs waiting for an initial-events bookmark.
func (c *draAwareKubeClient) IsWatchListSemanticsUnSupported() bool {
	type unSupported interface {
		IsWatchListSemanticsUnSupported() bool
	}
	if lw, ok := c.Interface.(unSupported); ok {
		return lw.IsWatchListSemanticsUnSupported()
	}
	return false
}
