// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package evictor

import (
	v1 "k8s.io/api/core/v1"
)

type Interface interface {
	Evict(pod *v1.Pod, message string) error
}
