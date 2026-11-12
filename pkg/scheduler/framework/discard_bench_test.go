// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package framework

import "testing"

func BenchmarkDiscardWithoutRecorder(b *testing.B) {
	stmt, task, node := buildEvictMarkerSession()
	ops := make([]Operation, 200)
	for i := range ops {
		ops[i] = evictOperation{
			taskInfo:     task,
			previousNode: node,
			reverseOperation: func() error {
				return nil
			},
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		stmt.operations = append([]Operation(nil), ops...)
		stmt.Discard()
	}
}
