// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package v2

type QueueResources struct {
	// GPU resources in fractions. 0.7 = 70% of a gpu
	GPU QueueResource `json:"gpu,omitempty"`

	// CPU resources in millicpus. 1000 = 1 cpu
	CPU QueueResource `json:"cpu,omitempty"`

	// Memory resources in megabytes. 1 = 10^6  (1000*1000) bytes
	Memory QueueResource `json:"memory,omitempty"`
}

type QueueResource struct {
	// +optional
	Quota float64 `json:"quota"`
	// +optional
	OverQuotaWeight float64 `json:"overQuotaWeight"`
	// +optional
	Limit float64 `json:"limit"`
	// LendingLimit is the maximum amount of unused Quota that sibling queues may
	// borrow. When unset, there is no lending limit (all unused Quota may enter
	// the over-quota pool) — the historical behavior.
	// When set to 0, unused Quota is not lent: Quota stays exclusively reserved
	// even while idle. When set to N, at most N of unused Quota is lendable;
	// the remainder (Quota - N) is exclusive.
	// Must be non-negative, and must not exceed Quota when Quota is finite.
	// +optional
	LendingLimit *float64 `json:"lendingLimit,omitempty"`
}
