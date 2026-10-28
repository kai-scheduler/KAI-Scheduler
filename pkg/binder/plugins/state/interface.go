// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package state

import "k8s.io/apimachinery/pkg/types"

// Forgetter discards only in-memory state matching a nonempty requestUID, without
// releasing resources or invoking PostBind. An empty podUID selects stored identities.
type Forgetter interface {
	ForgetBindingState(requestUID, podUID types.UID)
}
