// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package rd

import (
	"context"
	"fmt"
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
	key := runtimeClient.ObjectKeyFromObject(obj)
	err := kubeClient.Get(ctx, key, obj)
	if err == nil {
		return fmt.Errorf("object %v already exists in the cluster", key)
	}

	for i := 0; i < operationAttemptsRetries; i++ {
		err = kubeClient.Create(ctx, obj)
		if err == nil || errors.IsAlreadyExists(err) {
			return nil
		}
		time.Sleep(retryInterval)
	}
	return err
}
