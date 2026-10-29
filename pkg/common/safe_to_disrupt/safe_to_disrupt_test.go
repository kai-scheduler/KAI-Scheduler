// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package safe_to_disrupt

import "testing"

func TestFromAnnotations(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		want        bool
	}{
		{name: "no annotations", want: true},
		{name: "no annotation", annotations: map[string]string{"other": "false"}, want: true},
		{name: "false", annotations: map[string]string{AnnotationKey: "false"}, want: false},
		{name: "true", annotations: map[string]string{AnnotationKey: "true"}, want: true},
		{name: "invalid value is ignored", annotations: map[string]string{AnnotationKey: "never"}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FromAnnotations(tt.annotations); got != tt.want {
				t.Errorf("FromAnnotations(%v) = %v, want %v", tt.annotations, got, tt.want)
			}
		})
	}
}
