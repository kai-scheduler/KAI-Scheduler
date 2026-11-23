// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package helmhooks

import (
	"context"
	"fmt"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kai-scheduler/KAI-scheduler/deployments/kai-scheduler/crds"
)

// crdFieldManager matches the default manager of `kubectl apply --server-side`, which earlier chart
// versions used for this hook. Reusing it takes over that managedFields entry instead of adding a
// second owner, so fields removed from a CRD get pruned on upgrade.
const crdFieldManager = "kubectl"

// ApplyCRDs server-side applies the CRDs embedded in the binary, claiming ownership on conflicts.
func ApplyCRDs(ctx context.Context, c client.Client) error {
	logger := logf.FromContext(ctx)

	objects, err := crds.LoadEmbeddedCRDsUnstructured()
	if err != nil {
		return err
	}

	if err := checkStoredVersions(ctx, c, objects); err != nil {
		return err
	}

	for _, obj := range objects {
		if err := c.Apply(ctx, client.ApplyConfigurationFromUnstructured(obj),
			client.FieldOwner(crdFieldManager), client.ForceOwnership); err != nil {
			return fmt.Errorf("failed to apply CRD %s: %w", obj.GetName(), err)
		}
		logger.Info("Applied CRD", "name", obj.GetName())
	}
	return nil
}

// checkStoredVersions fails before any CRD is applied if a live CRD still lists a stored version that the
// embedded CRD no longer declares. The API server rejects such an apply midway, leaving the CRDs half upgraded.
func checkStoredVersions(ctx context.Context, c client.Client, objects []*unstructured.Unstructured) error {
	for _, obj := range objects {
		live := &apiextensionsv1.CustomResourceDefinition{}
		if err := c.Get(ctx, client.ObjectKey{Name: obj.GetName()}, live); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return fmt.Errorf("failed to get CRD %s: %w", obj.GetName(), err)
		}

		declared := map[string]bool{}
		versions, _, err := unstructured.NestedSlice(obj.Object, "spec", "versions")
		if err != nil {
			return fmt.Errorf("failed to read versions of CRD %s: %w", obj.GetName(), err)
		}
		for _, v := range versions {
			if name, ok := v.(map[string]any)["name"].(string); ok {
				declared[name] = true
			}
		}

		var stale []string
		for _, v := range live.Status.StoredVersions {
			if !declared[v] {
				stale = append(stale, v)
			}
		}
		if len(stale) > 0 {
			return fmt.Errorf("CRD %s has stored versions %v that the new version no longer declares; "+
				"re-save its objects (kubectl get <resource> -A -o yaml | kubectl replace -f -) and then run: "+
				"kubectl patch crd %s --subresource=status --type=json "+
				"-p='[{\"op\":\"replace\",\"path\":\"/status/storedVersions\",\"value\":[\"<current-storage-version>\"]}]'",
				obj.GetName(), stale, obj.GetName())
		}
	}
	return nil
}
