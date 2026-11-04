// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package k8s_utils

import (
	"fmt"
	"sync"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	versionutil "k8s.io/apimachinery/pkg/util/version"
	"k8s.io/apiserver/pkg/util/feature"
	"k8s.io/component-base/version"
	ndf "k8s.io/component-helpers/nodedeclaredfeatures"
	ndffeatures "k8s.io/component-helpers/nodedeclaredfeatures/features"
	kubefeatures "k8s.io/kubernetes/pkg/features"
)

type nodeDeclaredFeaturesInference struct {
	framework *ndf.Framework
	version   *versionutil.Version
}

// Built the same way as the upstream NodeDeclaredFeatures scheduler plugin, so
// callers infer exactly the requirements that the plugin filters on.
var getNodeDeclaredFeaturesInference = sync.OnceValues(func() (*nodeDeclaredFeaturesInference, error) {
	framework, err := ndf.New(ndffeatures.AllFeatures)
	if err != nil {
		return nil, fmt.Errorf("failed to create node declared features framework: %w", err)
	}
	ver, err := versionutil.Parse(version.Get().String())
	if err != nil {
		return nil, fmt.Errorf("failed to parse version: %w", err)
	}
	return &nodeDeclaredFeaturesInference{framework: framework, version: ver}, nil
})

// NodeDeclaredFeatureRequirements returns the sorted names of the node-declared
// features required to schedule the pod. It returns nil when the pod has no
// requirements or the NodeDeclaredFeatures feature gate is disabled.
func NodeDeclaredFeatureRequirements(pod *v1.Pod) ([]string, error) {
	if !feature.DefaultFeatureGate.Enabled(kubefeatures.NodeDeclaredFeatures) {
		return nil, nil
	}
	inference, err := getNodeDeclaredFeaturesInference()
	if err != nil {
		return nil, err
	}
	requirements, err := inference.framework.InferForPodScheduling(&ndf.PodInfo{Spec: &pod.Spec}, inference.version)
	if err != nil {
		return nil, err
	}
	if requirements.Len() == 0 {
		return nil, nil
	}
	return sets.List(requirements.Set), nil
}
