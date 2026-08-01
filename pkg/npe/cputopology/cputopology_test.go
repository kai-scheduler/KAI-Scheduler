// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package cputopology

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseCPUList(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    []int64
		wantErr bool
	}{
		{name: "empty", raw: "\n", want: nil},
		{name: "single", raw: "3", want: []int64{3}},
		{name: "range", raw: "0-3", want: []int64{0, 1, 2, 3}},
		{name: "mixed", raw: "0-2,5,8-9\n", want: []int64{0, 1, 2, 5, 8, 9}},
		{name: "invalid", raw: "a-b", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseCPUList(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLoad(t *testing.T) {
	root := t.TempDir()
	writeNode(t, root, "node0", "0-1,4")
	writeNode(t, root, "node1", "2-3")
	// A non-node directory should be ignored.
	if err := os.MkdirAll(filepath.Join(root, "devices", "system", "node", "has_cpu"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := Load(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := CPUToNUMA{0: 0, 1: 0, 4: 0, 2: 1, 3: 1}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestLoadNoNodes(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "devices", "system", "node"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root); err == nil {
		t.Fatalf("expected error for empty topology")
	}
}

func writeNode(t *testing.T, root, node, cpulist string) {
	t.Helper()
	dir := filepath.Join(root, "devices", "system", "node", node)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cpulist"), []byte(cpulist), 0o644); err != nil {
		t.Fatal(err)
	}
}
