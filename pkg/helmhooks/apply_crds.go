// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package helmhooks

import (
	"context"
	"fmt"

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

	for _, obj := range objects {
		if err := c.Apply(ctx, client.ApplyConfigurationFromUnstructured(obj),
			client.FieldOwner(crdFieldManager), client.ForceOwnership); err != nil {
			return fmt.Errorf("failed to apply CRD %s: %w", obj.GetName(), err)
		}
		logger.Info("Applied CRD", "name", obj.GetName())
	}
	return nil
}
