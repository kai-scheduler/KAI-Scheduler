/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package context

import (
	goctx "context"
	"fmt"
	"sync"

	"k8s.io/apimachinery/pkg/api/meta"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	commonconstants "github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/constant"
)

const (
	defaultParentQueueName = "default-parent-queue"
	defaultQueueName       = "default-queue"
)

// preflightOnce ensures the cluster guard runs at most once per process even
// though GetConnectivity is invoked from many different specs.
var (
	preflightOnce sync.Once
	preflightErr  error
)

// runPreflight inspects the target cluster for Queues that were not created
// by this e2e suite. The presence of any such Queue is a strong signal that
// the kubeconfig points at a real cluster rather than a throwaway test
// cluster, and continuing would mutate cluster-scoped state in ways that
// survive process termination (operator-reconciled feature flags, leftover
// Queues, etc.). There is no opt-in: the user must clean the conflicting
// Queues themselves before re-running.
func runPreflight(ctx goctx.Context, cli runtimeClient.Client) error {
	preflightOnce.Do(func() {
		preflightErr = checkForeignQueues(ctx, cli)
	})
	return preflightErr
}

func checkForeignQueues(ctx goctx.Context, cli runtimeClient.Client) error {
	queues := &v2.QueueList{}
	if err := cli.List(ctx, queues); err != nil {
		// No Queue CRD means KAI is not installed, so there are no foreign
		// Queues by definition (e.g. the gitops suite connects before its
		// ArgoCD Application installs the CRDs).
		if meta.IsNoMatchError(err) {
			return nil
		}
		return fmt.Errorf("preflight: failed to list Queues: %w", err)
	}

	var foreign []string
	for _, q := range queues.Items {
		if q.Labels[commonconstants.AppLabelName] == constant.EngineTestPodsApp {
			continue
		}
		// The defaults are created by default on a fresh install. They shouldn't block an e2e test run.
		if q.Name == defaultParentQueueName || q.Name == defaultQueueName {
			continue
		}
		foreign = append(foreign, q.Name)
	}
	if len(foreign) == 0 {
		return nil
	}

	return fmt.Errorf(
		"preflight: refusing to run e2e tests: cluster already contains %d Queue(s) not labeled %s=%s: %v. "+
			"This usually means the kubeconfig points at a real cluster rather than a test cluster. "+
			"Delete the conflicting Queues (or switch contexts) before re-running",
		len(foreign), commonconstants.AppLabelName, constant.EngineTestPodsApp, foreign,
	)
}
