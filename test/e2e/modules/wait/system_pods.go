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

	"github.com/NVIDIA/KAI-scheduler/pkg/common/constants"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/testconfig"
	"github.com/NVIDIA/KAI-scheduler/test/e2e/modules/wait/watcher"
)

func ForKaiComponentPod(
	ctx context.Context, client runtimeClient.WithWatch,
	appLabelComponentName string, condition checkCondition,
) {
	pw := watcher.NewGenericWatcher[v1.PodList](client, watcher.CheckCondition(condition),
		runtimeClient.InNamespace(testconfig.GetConfig().SystemPodsNamespace),
		runtimeClient.MatchingLabels{constants.AppLabelName: appLabelComponentName})

	if !watcher.ForEvent(ctx, client, pw) {
		Fail(fmt.Sprintf("Failed to wait for %s pod", appLabelComponentName))
	}
}

func ForRunningSystemComponentEvent(ctx context.Context, client runtimeClient.WithWatch, appLabelComponentName string) {
	runningCondition := func(event watch.Event) bool {
		podListObj, ok := event.Object.(*v1.PodList)
		if !ok {
			Fail(fmt.Sprintf("Failed to process event for pod %s", event.Object))
		}
		if len(podListObj.Items) != 1 {
			return false
		}

		objPod := &podListObj.Items[0]
		return rd.IsPodRunning(objPod)
	}
	ForKaiComponentPod(ctx, client, appLabelComponentName, runningCondition)
}
