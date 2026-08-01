// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package rd

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	runtimeClient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	operationAttemptsRetries = 10
	retryInterval            = 100 * time.Microsecond
)

// CreateObjectWithRetries creates an object using the scale-test retry behavior.
func CreateObjectWithRetries(
	ctx context.Context, kubeClient runtimeClient.Client, obj runtimeClient.Object,
) error {
	var err error
	for range operationAttemptsRetries {
		err = kubeClient.Create(ctx, obj)
		if err == nil || errors.IsAlreadyExists(err) {
			return nil
		}
		time.Sleep(retryInterval)
	}
	return err
}
