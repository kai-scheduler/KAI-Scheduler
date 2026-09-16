// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package helmhooks

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	configKind         = "Config"
	configFieldManager = "kai-config-deployer"
)

// Left over from chart versions that managed kai-config as a Helm hook resource (#1536).
var helmHookAnnotations = []string{"helm.sh/hook", "helm.sh/hook-weight"}

// ApplyConfig server-side applies the Config manifest at manifestPath, first dropping stale Helm
// hook annotations from an existing object.
func ApplyConfig(ctx context.Context, c client.Client, manifestPath string) error {
	logger := logf.FromContext(ctx)

	content, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("failed to read manifest %s: %w", manifestPath, err)
	}
	obj, err := decodeManifest(content)
	if err != nil {
		return fmt.Errorf("failed to decode manifest %s: %w", manifestPath, err)
	}
	if obj.GetKind() != configKind {
		return fmt.Errorf("manifest %s has kind %q, expected %q", manifestPath, obj.GetKind(), configKind)
	}
	if obj.GetName() == "" {
		return fmt.Errorf("manifest %s has no metadata.name", manifestPath)
	}

	if err := stripHelmHookAnnotations(ctx, c, obj); err != nil {
		return fmt.Errorf("failed to strip Helm hook annotations from %s: %w", obj.GetName(), err)
	}

	if err := c.Apply(ctx, client.ApplyConfigurationFromUnstructured(obj),
		client.FieldOwner(configFieldManager), client.ForceOwnership); err != nil {
		return fmt.Errorf("failed to apply %s %s: %w", obj.GetKind(), obj.GetName(), err)
	}
	logger.Info("Applied Config", "name", obj.GetName())
	return nil
}

func decodeManifest(content []byte) (*unstructured.Unstructured, error) {
	jsonContent, err := yaml.ToJSON(content)
	if err != nil {
		return nil, err
	}
	obj := &unstructured.Unstructured{}
	if err := obj.UnmarshalJSON(jsonContent); err != nil {
		return nil, err
	}
	return obj, nil
}

func stripHelmHookAnnotations(ctx context.Context, c client.Client, obj *unstructured.Unstructured) error {
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(obj.GroupVersionKind())
	if err := c.Get(ctx, client.ObjectKeyFromObject(obj), existing); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	annotations := existing.GetAnnotations()
	removals := map[string]interface{}{}
	for _, key := range helmHookAnnotations {
		if _, found := annotations[key]; found {
			removals[key] = nil
		}
	}
	if len(removals) == 0 {
		return nil
	}

	patch, err := json.Marshal(map[string]interface{}{
		"metadata": map[string]interface{}{"annotations": removals},
	})
	if err != nil {
		return err
	}
	return c.Patch(ctx, existing, client.RawPatch(types.MergePatchType, patch))
}
