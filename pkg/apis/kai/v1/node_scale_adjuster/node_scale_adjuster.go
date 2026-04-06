// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// +kubebuilder:object:generate:=true
package node_scale_adjuster

import (
	"k8s.io/utils/ptr"

	"github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1/common"
	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
)

const (
	imageName           = "node-scale-adjuster"
	scalingPodImageName = "scalingpod"
)

type NodeScaleAdjuster struct {
	Service *common.Service `json:"service,omitempty"`

	// Args specifies the CLI arguments for node-scale-adjuster
	// +kubebuilder:validation:Optional
	Args *Args `json:"args,omitempty"`

	// VPA specifies Vertical Pod Autoscaler configuration for the node-scale-adjuster
	// +kubebuilder:validation:Optional
	VPA *common.VPASpec `json:"vpa,omitempty"`
}

// Args specifies the CLI arguments for node-scale-adjuster
type Args struct {
	// ScalingPodImage is the image to use for the scaling pod
	// +kubebuilder:validation:Optional
	ScalingPodImage *common.Image `json:"scalingPodImage,omitempty"`

	// NodeScaleNamespace is the namespace in which the scaling pods will be created
	// +kubebuilder:validation:Optional
	NodeScaleNamespace *string `json:"nodeScaleNamespace,omitempty"`

	// NodeScaleServiceAccount is the name of the service account used by the scale adjust pods
	// +kubebuilder:validation:Optional
	NodeScaleServiceAccount *string `json:"nodeScaleServiceAccount,omitempty"`

	// GPUMemoryToFractionRatio is the ratio of GPU memory to fraction conversion
	// +kubebuilder:validation:Optional
	GPUMemoryToFractionRatio *float64 `json:"gpuMemoryToFractionRatio,omitempty"`
}

// SetDefaultsWhereNeeded sets default for unset fields
func (args *Args) SetDefaultsWhereNeeded() {
	args.ScalingPodImage = common.SetDefault(args.ScalingPodImage, &common.Image{})
	args.ScalingPodImage.Name = common.SetDefault(args.ScalingPodImage.Name, ptr.To(scalingPodImageName))
	args.ScalingPodImage.SetDefaultsWhereNeeded()

	args.NodeScaleNamespace = common.SetDefault(args.NodeScaleNamespace, ptr.To(string(constants.DefaultScaleAdjustName)))
	args.NodeScaleServiceAccount = common.SetDefault(args.NodeScaleServiceAccount, ptr.To(constants.DefaultScaleAdjustName))
}

// SetDefaultsWhereNeeded sets default for unset fields
func (nsa *NodeScaleAdjuster) SetDefaultsWhereNeeded(globalVPA *common.VPASpec) {
	nsa.Service = common.SetDefault(nsa.Service, &common.Service{})
	nsa.Service.SetDefaultsWhereNeeded(imageName)

	nsa.Args = common.SetDefault(nsa.Args, &Args{})
	nsa.Args.SetDefaultsWhereNeeded()

	if nsa.VPA == nil {
		nsa.VPA = globalVPA
	}
}
