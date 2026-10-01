// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

const suitesMarker = "test/e2e/suites/"

// DiscoverSuites returns the directories under root (slash-separated, relative to the
// working directory) that contain a Ginkgo entrypoint.
func DiscoverSuites(root string) ([]string, error) {
	found := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(content, []byte("RunSpecs(")) {
			found[filepath.ToSlash(filepath.Dir(path))] = true
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	suites := make([]string, 0, len(found))
	for suite := range found {
		suites = append(suites, suite)
	}
	sort.Strings(suites)
	return suites, nil
}

// SplitDedicated separates suites that have their own CI job from the ones to shard.
func SplitDedicated(suites, dedicated []string) (main []string, err error) {
	for _, name := range dedicated {
		if !slices.Contains(suites, name) {
			return nil, fmt.Errorf("dedicated E2E package was not discovered: %s", name)
		}
	}
	for _, suite := range suites {
		if !slices.Contains(dedicated, suite) {
			main = append(main, suite)
		}
	}
	return main, nil
}

// suiteReport is the subset of a Ginkgo JSON report entry used here.
type suiteReport struct {
	SuitePath      string
	RunTime        int64
	SuiteSucceeded bool
}

func (r suiteReport) pkg() (string, bool) {
	_, after, found := strings.Cut(filepath.ToSlash(r.SuitePath), suitesMarker)
	if !found {
		return "", false
	}
	return suitesMarker + after, true
}

// ReadReports loads every e2e-report-*.json under dir. Unreadable reports are returned as
// warnings so a corrupt artifact degrades balancing instead of failing the run.
func ReadReports(dir string) (reports []suiteReport, warnings []string) {
	_ = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		name := filepath.Base(path)
		if err != nil || entry.IsDir() || !strings.HasPrefix(name, "e2e-report-") || !strings.HasSuffix(name, ".json") {
			return nil
		}
		content, err := os.ReadFile(path)
		if err == nil {
			var parsed []suiteReport
			if err = json.Unmarshal(content, &parsed); err == nil {
				reports = append(reports, parsed...)
				return nil
			}
		}
		warnings = append(warnings, fmt.Sprintf("ignoring %s: %v", path, err))
		return nil
	})
	return reports, warnings
}

// Weights estimates each package's runtime in seconds from history. Packages with no history
// get twice the median so an unknown suite is not packed alongside a slow one.
func Weights(packages []string, history []suiteReport) (weights map[string]int, missing []string) {
	sums, counts := map[string]int{}, map[string]int{}
	for _, report := range history {
		if pkg, ok := report.pkg(); ok {
			sums[pkg] += int((report.RunTime + 1e9 - 1) / 1e9)
			counts[pkg]++
		}
	}

	weights = map[string]int{}
	var observed []int
	for _, pkg := range packages {
		if counts[pkg] > 0 {
			weights[pkg] = (sums[pkg]+counts[pkg]-1)/counts[pkg] + 1
			observed = append(observed, weights[pkg])
		}
	}

	fallback := 1
	if len(observed) > 0 {
		sort.Ints(observed)
		fallback = observed[len(observed)/2]
	}
	for _, pkg := range packages {
		if _, ok := weights[pkg]; !ok {
			weights[pkg] = fallback * 2
			missing = append(missing, pkg)
		}
	}
	return weights, missing
}

// VerifyReports fails unless every expected package appears in the reports and all suites passed.
func VerifyReports(expected []string, reports []suiteReport) error {
	ran := map[string]bool{}
	var failed []string
	for _, report := range reports {
		pkg, ok := report.pkg()
		if !ok {
			continue
		}
		ran[pkg] = true
		if !report.SuiteSucceeded {
			failed = append(failed, pkg)
		}
	}

	var missing []string
	for _, pkg := range expected {
		if !ran[pkg] {
			missing = append(missing, pkg)
		}
	}
	if len(missing) > 0 || len(failed) > 0 {
		return fmt.Errorf("E2E packages missing from reports: %v; failed: %v", missing, failed)
	}
	return nil
}
