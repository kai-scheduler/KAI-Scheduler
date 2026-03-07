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

	"github.com/kai-scheduler/kai-scheduler/test/e2e/modules/wait/watcher"
)

func ForConfigMapCreation(ctx context.Context, client runtimeClient.WithWatch, namespace, name string) {
	condition := func(event watch.Event) bool {
		configMapsList, ok := event.Object.(*v1.ConfigMapList)
		if !ok {
			return false
		}

		for _, cm := range configMapsList.Items {
			if cm.Namespace == namespace && cm.Name == name {
				return true
			}
		}
		return false
	}

	configMapWatcher := watcher.NewGenericWatcher[v1.ConfigMapList](
		client,
		condition,
		runtimeClient.InNamespace(namespace),
		runtimeClient.MatchingFields{"metadata.name": name})

	if !watcher.ForEvent(ctx, client, configMapWatcher) {
		Fail(fmt.Sprintf("Failed to watch configmap for %s/%s to be created", namespace, name))
	}
}
