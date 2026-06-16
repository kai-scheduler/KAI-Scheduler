// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// Package cputopology resolves logical CPU IDs to their NUMA node by reading sysfs.
// The kubelet podresources API reports a container's exclusive cpu_ids but not which NUMA
// node each belongs to; this package supplies that mapping.
package cputopology

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// CPUToNUMA maps a logical CPU ID to its NUMA node ID.
type CPUToNUMA map[int64]int

// Load reads <sysfsRoot>/devices/system/node/node<N>/cpulist for every NUMA node and builds
// the CPU -> NUMA node mapping.
func Load(sysfsRoot string) (CPUToNUMA, error) {
	nodeRoot := filepath.Join(sysfsRoot, "devices", "system", "node")
	entries, err := os.ReadDir(nodeRoot)
	if err != nil {
		return nil, fmt.Errorf("reading NUMA node directory %q: %w", nodeRoot, err)
	}

	mapping := CPUToNUMA{}
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "node") {
			continue
		}
		nodeID, err := strconv.Atoi(strings.TrimPrefix(entry.Name(), "node"))
		if err != nil {
			continue
		}

		cpuListPath := filepath.Join(nodeRoot, entry.Name(), "cpulist")
		raw, err := os.ReadFile(cpuListPath)
		if err != nil {
			return nil, fmt.Errorf("reading %q: %w", cpuListPath, err)
		}

		cpus, err := parseCPUList(string(raw))
		if err != nil {
			return nil, fmt.Errorf("parsing %q: %w", cpuListPath, err)
		}
		for _, cpu := range cpus {
			mapping[cpu] = nodeID
		}
	}

	if len(mapping) == 0 {
		return nil, fmt.Errorf("no CPUs found under %q", nodeRoot)
	}
	return mapping, nil
}

// parseCPUList parses a Linux cpulist string such as "0-3,8,12-13" into individual CPU IDs.
func parseCPUList(raw string) ([]int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	var cpus []int64
	for _, token := range strings.Split(raw, ",") {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}

		lo, hi, isRange := strings.Cut(token, "-")
		start, err := strconv.ParseInt(strings.TrimSpace(lo), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid CPU id %q: %w", lo, err)
		}
		end := start
		if isRange {
			end, err = strconv.ParseInt(strings.TrimSpace(hi), 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid CPU id %q: %w", hi, err)
			}
		}
		for cpu := start; cpu <= end; cpu++ {
			cpus = append(cpus, cpu)
		}
	}
	return cpus, nil
}
