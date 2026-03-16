/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package utils

import (
	"math/rand"
	"sync"
	"time"
)

var generatedNames sync.Map

func GenerateRandomK8sName(l int) string {
	str := "abcdefghijklmnopqrstuvwxyz"
	chars := []byte(str)
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	for {
		result := make([]byte, l)
		for i := range l {
			result[i] = chars[r.Intn(len(chars))]
		}
		name := string(result)

		if _, loaded := generatedNames.LoadOrStore(name, true); !loaded {
			return name
		}
	}
}
