// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package common

import "k8s.io/utils/ptr"

// K8sClientConfig specifies the configuration of k8s client
type K8sClientConfig struct {
	// Burst specifies the burst rate for the k8s client
	// +kubebuilder:validation:Optional
	Burst *int `json:"burst,omitempty"`

	// QPS specifies the QPS rate for the k8s client
	// +kubebuilder:validation:Optional
	QPS *int `json:"qps,omitempty"`
}

func (c *K8sClientConfig) SetDefaultsWhereNeeded() {
	c.Burst = SetDefault(c.Burst, ptr.To(100))
	c.QPS = SetDefault(c.QPS, ptr.To(20))
}
