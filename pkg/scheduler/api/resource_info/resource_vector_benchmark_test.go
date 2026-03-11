// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resource_info

import (
	"fmt"
	"testing"
)

func BenchmarkResourceVectorLessEqual(b *testing.B) {
	benchmarks := []struct {
		name string
		size int
	}{
		{"100resources", 100},
		{"500resources", 500},
		{"1000resources", 1000},
		{"2000resources", 2000},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			indexMap := ResourceVectorMap{
				resourceNames: make([]string, 0, bm.size),
				namesToIndex:  make(map[string]int, bm.size),
			}
			for i := 0; i < bm.size; i++ {
				indexMap.AddResource(fmt.Sprintf("resource-%d", i))
			}

			vec1 := NewResourceVector(&indexMap)
			vec2 := NewResourceVector(&indexMap)
			for i := 0; i < len(vec1); i++ {
				vec1[i] = float64(i+1) * 100
				vec2[i] = float64(i+2) * 100
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = vec1.LessEqual(vec2)
			}
		})
	}
}

func BenchmarkResourceVectorAdd(b *testing.B) {
	benchmarks := []struct {
		name string
		size int
	}{
		{"100resources", 100},
		{"500resources", 500},
		{"1000resources", 1000},
		{"2000resources", 2000},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			indexMap := ResourceVectorMap{
				resourceNames: make([]string, 0, bm.size),
				namesToIndex:  make(map[string]int, bm.size),
			}
			for i := 0; i < bm.size; i++ {
				indexMap.AddResource(fmt.Sprintf("resource-%d", i))
			}

			vec1 := NewResourceVector(&indexMap)
			vec2 := NewResourceVector(&indexMap)
			for i := 0; i < len(vec1); i++ {
				vec1[i] = float64(i+1) * 100
				vec2[i] = float64(i+2) * 100
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				vecCopy := vec1.Clone()
				vecCopy.Add(vec2)
			}
		})
	}
}

func BenchmarkResourceVectorSub(b *testing.B) {
	benchmarks := []struct {
		name string
		size int
	}{
		{"100resources", 100},
		{"500resources", 500},
		{"1000resources", 1000},
		{"2000resources", 2000},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			indexMap := ResourceVectorMap{
				resourceNames: make([]string, 0, bm.size),
				namesToIndex:  make(map[string]int, bm.size),
			}
			for i := 0; i < bm.size; i++ {
				indexMap.AddResource(fmt.Sprintf("resource-%d", i))
			}

			vec1 := NewResourceVector(&indexMap)
			vec2 := NewResourceVector(&indexMap)
			for i := 0; i < len(vec1); i++ {
				vec1[i] = float64(i+2) * 100
				vec2[i] = float64(i+1) * 100
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				vecCopy := vec1.Clone()
				vecCopy.Sub(vec2)
			}
		})
	}
}
