// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package rd

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"
)

// CreateObjectWithRetries allows admission and the API server to recover during a
// burst of scale-test creates. Workloads have stable names across attempts.
func CreateObjectWithRetries(
	ctx context.Context, kubeClient runtimeClient.Client, obj runtimeClient.Object,
) error {
	var lastErr error
	backoff := wait.Backoff{Steps: 7, Duration: 500 * time.Millisecond, Factor: 2, Jitter: 0.2}
	err := wait.ExponentialBackoffWithContext(ctx, backoff, func(ctx context.Context) (bool, error) {
		lastErr = kubeClient.Create(ctx, obj)
		switch {
		case lastErr == nil, errors.IsAlreadyExists(lastErr):
			return true, nil
		case errors.IsInternalError(lastErr), errors.IsServiceUnavailable(lastErr),
			errors.IsTooManyRequests(lastErr), errors.IsServerTimeout(lastErr), errors.IsTimeout(lastErr):
			return false, nil
		default:
			return false, lastErr
		}
	})
	if ctx.Err() == nil && wait.Interrupted(err) && lastErr != nil {
		return lastErr
	}
	return err
}
