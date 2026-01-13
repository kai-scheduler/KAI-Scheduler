// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package v2

import (
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// log is for logging in this package.
var queuelog = logf.Log.WithName("queue-resource")

const missingResourcesError = "resources must be specified"

var enableQuotaValidation bool

// SetEnableQuotaValidation sets the quota validation flag
func SetEnableQuotaValidation(enabled bool) {
	enableQuotaValidation = enabled
}

func (r *Queue) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		WithValidator(NewQueueValidator(mgr.GetClient(), enableQuotaValidation)).
		Complete()
}
