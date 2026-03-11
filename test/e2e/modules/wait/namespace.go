/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package wait

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/watch"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/wait/watcher"
)

// ForNamespacesToBeDeleted waits for all specified namespaces to be deleted (not exist in the cluster).
// It takes a list of namespace names and waits until none of them exist.
func ForNamespacesToBeDeleted(ctx context.Context, client runtimeClient.WithWatch, namespaces []string) {
	if len(namespaces) == 0 {
		return
	}

	namespaceSet := make(map[string]struct{})
	for _, ns := range namespaces {
		namespaceSet[ns] = struct{}{}
	}

	condition := func(event watch.Event) bool {
		namespaceListObj, ok := event.Object.(*v1.NamespaceList)
		if !ok {
			return false
		}

		// Check if any of the specified namespaces still exist
		for _, ns := range namespaceListObj.Items {
			if _, exists := namespaceSet[ns.Name]; exists {
				return false
			}
		}

		// All specified namespaces have been deleted
		return true
	}

	nw := watcher.NewGenericWatcher[v1.NamespaceList](client, condition)
	if !watcher.ForEvent(ctx, client, nw) {
		Fail(fmt.Sprintf("Failed to wait for namespaces %v to be deleted", namespaces))
	}
}
