/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package context

import (
	"context"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	v2 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/scheduling/v2"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/resources/rd/queue"
	"github.com/kai-scheduler/KAI-scheduler/test/e2e/modules/testconfig"
)

const (
	webhookRetryInterval = 5 * time.Second
	webhookRetryTimeout  = 2 * time.Minute
)

func (tc *TestContext) createClusterQueues(ctx context.Context) error {
	for _, testQueue := range tc.Queues {
		err := wait.PollUntilContextTimeout(ctx, webhookRetryInterval, webhookRetryTimeout, true,
			func(ctx context.Context) (bool, error) {
				err := createQueueContext(ctx, testQueue)
				if err != nil {
					if isWebhookUnavailable(err) {
						return false, nil
					}
					return false, err
				}
				return true, nil
			})
		if err != nil {
			return err
		}
	}
	return nil
}

// isWebhookUnavailable returns true for transient admission webhook connection errors
// that occur when the webhook server hasn't finished starting yet.
func isWebhookUnavailable(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "connection refused") || strings.Contains(msg, "failed to call webhook")
}

func createQueueContext(ctx context.Context, q *v2.Queue) error {
	_, err := queue.Create(kubeAiSchedClientset, ctx, q, metav1.CreateOptions{})
	if err != nil {
		return err
	}

	namespaceName := queue.GetConnectedNamespaceToQueue(q)
	ns := rd.CreateNamespaceObject(namespaceName, q.Name)
	_, err = kubeClientset.
		CoreV1().
		Namespaces().
		Create(ctx, ns, metav1.CreateOptions{})
	if err != nil {
		return err
	}

	// TODO: add RBAC role bindings
	// TODO: patch the namespace to add appropriate secret to the service account

	if hook := testconfig.GetConfig().OnNamespaceCreated; hook != nil {
		if err := hook(ctx, kubeClientset, namespaceName, q.Name); err != nil {
			return err
		}
	}

	return nil
}
