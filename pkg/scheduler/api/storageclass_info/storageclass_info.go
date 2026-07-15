// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package storageclass_info

import (
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
)

type StorageClassInfo struct {
	ID          common_info.StorageClassID
	Provisioner string
}
