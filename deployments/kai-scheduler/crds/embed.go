// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package crds

import (
	"embed"
	"fmt"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
)

//go:embed *.yaml
var embeddedCRDs embed.FS

// LoadEmbeddedCRDs parses all embedded CRD YAML files and returns them as CRD objects.
// This allows the CRDs to be bundled into binaries without depending on file paths.
func LoadEmbeddedCRDs() ([]*apiextensionsv1.CustomResourceDefinition, error) {
	entries, err := embeddedCRDs.ReadDir(".")
	if err != nil {
		return nil, fmt.Errorf("failed to read embedded crds directory: %w", err)
	}

	var crds []*apiextensionsv1.CustomResourceDefinition
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == "embed.go" {
			continue
		}

		content, err := embeddedCRDs.ReadFile(entry.Name())
		if err != nil {
			return nil, fmt.Errorf("failed to read embedded CRD file %s: %w", entry.Name(), err)
		}

		crd := &apiextensionsv1.CustomResourceDefinition{}
		if err := yaml.Unmarshal(content, crd); err != nil {
			return nil, fmt.Errorf("failed to unmarshal CRD %s: %w", entry.Name(), err)
		}

		crds = append(crds, crd)
	}

	return crds, nil
}
