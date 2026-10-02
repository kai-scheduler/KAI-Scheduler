// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package crds

import (
	"embed"
	"fmt"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
)

//go:embed *.yaml
var embeddedCRDs embed.FS

// LoadEmbeddedCRDs parses all embedded CRD YAML files and returns them as CRD objects.
// This allows the CRDs to be bundled into binaries without depending on file paths.
func LoadEmbeddedCRDs() ([]*apiextensionsv1.CustomResourceDefinition, error) {
	var crds []*apiextensionsv1.CustomResourceDefinition
	err := forEachEmbeddedCRD(func(name string, content []byte) error {
		crd := &apiextensionsv1.CustomResourceDefinition{}
		if err := yaml.Unmarshal(content, crd); err != nil {
			return fmt.Errorf("failed to unmarshal CRD %s: %w", name, err)
		}
		crds = append(crds, crd)
		return nil
	})
	return crds, err
}

// LoadEmbeddedCRDsUnstructured returns the embedded CRDs exactly as written in the YAML files,
// without typed round-tripping, so they can be server-side applied verbatim.
func LoadEmbeddedCRDsUnstructured() ([]*unstructured.Unstructured, error) {
	var crds []*unstructured.Unstructured
	err := forEachEmbeddedCRD(func(name string, content []byte) error {
		jsonContent, err := yaml.ToJSON(content)
		if err != nil {
			return fmt.Errorf("failed to convert CRD %s to JSON: %w", name, err)
		}
		crd := &unstructured.Unstructured{}
		if err := crd.UnmarshalJSON(jsonContent); err != nil {
			return fmt.Errorf("failed to unmarshal CRD %s: %w", name, err)
		}
		crds = append(crds, crd)
		return nil
	})
	return crds, err
}

func forEachEmbeddedCRD(fn func(name string, content []byte) error) error {
	entries, err := embeddedCRDs.ReadDir(".")
	if err != nil {
		return fmt.Errorf("failed to read embedded crds directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		content, err := embeddedCRDs.ReadFile(entry.Name())
		if err != nil {
			return fmt.Errorf("failed to read embedded CRD file %s: %w", entry.Name(), err)
		}
		if err := fn(entry.Name(), content); err != nil {
			return err
		}
	}
	return nil
}
