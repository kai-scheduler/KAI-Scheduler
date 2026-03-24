// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package flags

import (
	"fmt"
	"strings"
)

type StringMapFlag map[string]string

func (m *StringMapFlag) Get() map[string]string {
	if *m == nil {
		return make(map[string]string)
	}
	return *m
}

func (m *StringMapFlag) String() string {
	// Convert map to string: key1=val1,key2=val2
	var pairs []string
	for k, v := range *m {
		pairs = append(pairs, fmt.Sprintf("%s=%s", k, v))
	}
	return strings.Join(pairs, ",")
}

func (m *StringMapFlag) Set(value string) error {
	// Convert string to map: key1=val1,key2=val2
	if *m == nil {
		*m = make(map[string]string)
	}
	if value == "" {
		return nil
	}
	pairs := strings.Split(value, ",")
	for _, pair := range pairs {
		kv := strings.SplitN(pair, "=", 2)
		if len(kv) != 2 {
			return fmt.Errorf("invalid map item: %q", pair)
		}
		(*m)[kv[0]] = kv[1]
	}
	return nil
}
