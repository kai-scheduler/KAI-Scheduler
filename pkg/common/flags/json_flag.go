// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package flags

import (
	"encoding/json"
	"fmt"
)

// JSONFlag implements pflag.Value for JSON-serialized K8s types.
type JSONFlag[T any] struct {
	Value *T
}

func (f *JSONFlag[T]) String() string {
	if f.Value == nil {
		return ""
	}
	b, err := json.Marshal(f.Value)
	if err != nil {
		return fmt.Sprintf("<marshal error: %v>", err)
	}
	return string(b)
}

func (f *JSONFlag[T]) Set(s string) error {
	if s == "" {
		f.Value = nil
		return nil
	}
	v := new(T)
	if err := json.Unmarshal([]byte(s), v); err != nil {
		return fmt.Errorf("failed to parse JSON for %T: %w", v, err)
	}
	f.Value = v
	return nil
}

func (f *JSONFlag[T]) Type() string {
	return "json"
}
