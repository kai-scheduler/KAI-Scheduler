/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package utils

import (
	"context"
)

type ClientGoWarningHandler struct {
	Messages []string
}

func (h *ClientGoWarningHandler) HandleWarningHeaderWithContext(ctx context.Context, code int, agent string, text string) {
	if code == 299 && text != "" {
		h.Messages = append(h.Messages, text)
	}
}
