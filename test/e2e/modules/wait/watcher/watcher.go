/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package watcher

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/watch"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/utils"
)

type Interface interface {
	watch(ctx context.Context) watch.Interface
	sync(ctx context.Context)
	processEvent(ctx context.Context, event watch.Event)
	satisfied() bool
}

type CheckCondition func(watch.Event) bool

var (
	FlowTimeout = 5 * time.Minute
)

func ForEvent(ctx context.Context, client runtimeClient.WithWatch, eventWatcher Interface) bool {
	return ForEventCustomTimeout(ctx, client, eventWatcher, FlowTimeout)
}

func ForEventCustomTimeout(ctx context.Context, client runtimeClient.WithWatch, eventWatcher Interface,
	waitTime time.Duration) bool {
	logger := log.FromContext(ctx)

	watcher := eventWatcher.watch(ctx)
	defer func() { watcher.Stop() }()

	eventWatcher.sync(ctx)
	if eventWatcher.satisfied() {
		return true
	}

	timer := time.NewTimer(waitTime)
	defer timer.Stop()

	pollInterval := 2 * time.Second
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.Canceled) {
				logger.Error(ctx.Err(), "WaitForEvent has been canceled")
				utils.LogClusterState(client, logger)
				return true
			}
			return false
		case <-timer.C:
			eventWatcher.sync(ctx)
			if eventWatcher.satisfied() {
				return true
			}
			logger.Error(nil, "WaitForEvent timed out")
			utils.LogClusterState(client, logger)
			return false
		case <-ticker.C:
			eventWatcher.sync(ctx)
			if eventWatcher.satisfied() {
				return true
			}
		case event, ok := <-watcher.ResultChan():
			if !ok {
				eventWatcher.sync(ctx)
				if eventWatcher.satisfied() {
					return true
				}
				// Watch channel closed (normal K8s server-side timeout); restart
				watcher.Stop()
				watcher = eventWatcher.watch(ctx)
				continue
			}
			if event.Type == watch.Error {
				err := ignoreContextCancelled(event)
				if err != nil {
					logger.Error(err, "Error event received for WaitForEvent function")
					utils.LogClusterState(client, logger)
					return false
				}
				return true
			}

			eventWatcher.processEvent(ctx, event)
			if eventWatcher.satisfied() {
				return true
			}
		}
	}
}

func ignoreContextCancelled(event watch.Event) error {
	objectAsString := fmt.Sprintf("%v", event.Object)
	if strings.Contains(objectAsString, "context cancelled") {
		fmt.Printf("Cancellation event received %v, %v", event.Type, event.Object)
		return nil
	}

	return fmt.Errorf("error event received %v, %v", event.Type, event.Object)
}
