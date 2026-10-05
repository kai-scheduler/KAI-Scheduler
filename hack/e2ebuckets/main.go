// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

// Command e2ebuckets splits the E2E suites into balanced, seed-randomized CI buckets and
// verifies that every planned suite ran and passed.
//
//	e2ebuckets plan   [--buckets N] [--seed S] [--history-dir DIR]
//	e2ebuckets verify --packages JSON --reports-dir DIR
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultBuckets   = 3
	defaultSuitesDir = "test/e2e/suites"
	defaultDedicated = "test/e2e/suites/gitops,test/e2e/suites/upgrade"
)

func main() {
	if len(os.Args) < 2 {
		fail(fmt.Errorf("usage: e2ebuckets plan|verify [flags]"))
	}
	var err error
	switch os.Args[1] {
	case "plan":
		err = runPlan(os.Args[2:])
	case "verify":
		err = runVerify(os.Args[2:])
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}

type matrixEntry struct {
	Bucket   string `json:"bucket"`
	Seed     string `json:"seed"`
	Packages string `json:"packages"`
}

func runPlan(args []string) error {
	flags := flag.NewFlagSet("plan", flag.ExitOnError)
	bucketCount := flags.Int("buckets", defaultBuckets, "number of buckets, clamped to the number of packages")
	seed := flags.Int64("seed", 0, "random seed; derived from GITHUB_RUN_ID and GITHUB_RUN_ATTEMPT when 0")
	historyDir := flags.String("history-dir", "", "directory with e2e-report-*.json files from earlier runs")
	suitesRoot := flags.String("suites-root", defaultSuitesDir, "directory to scan for suites")
	dedicated := flags.String("dedicated", defaultDedicated, "comma-separated suites that have their own CI job")
	_ = flags.Parse(args)

	if *seed == 0 {
		*seed = seedFromRun(os.Getenv("GITHUB_RUN_ID"), os.Getenv("GITHUB_RUN_ATTEMPT"))
	}

	suites, err := DiscoverSuites(*suitesRoot)
	if err != nil {
		return err
	}
	packages, err := SplitDedicated(suites, splitList(*dedicated))
	if err != nil {
		return err
	}
	*bucketCount = min(*bucketCount, len(packages))

	var history []suiteReport
	if *historyDir != "" {
		var warnings []string
		history, warnings = ReadReports(*historyDir)
		for _, warning := range warnings {
			fmt.Println("warning:", warning)
		}
	}
	weights, missing := Weights(packages, history)
	if len(history) == 0 {
		fmt.Println("warning: no timing history found; every package gets the same weight")
	} else {
		for _, pkg := range missing {
			fmt.Printf("warning: no timing history for %s; using weight %d\n", pkg, weights[pkg])
		}
	}

	buckets, err := Plan(packages, weights, *bucketCount, *seed)
	if err != nil {
		return err
	}
	return writePlan(buckets, packages, *seed)
}

func writePlan(buckets []Bucket, packages []string, seed int64) error {
	matrix := struct {
		Include []matrixEntry `json:"include"`
	}{}
	summary := &strings.Builder{}
	fmt.Fprintf(summary, "### E2E buckets (seed `%d`)\n\n| Bucket | Weight | Packages |\n|---|---|---|\n", seed)
	for _, bucket := range buckets {
		paths := make([]string, len(bucket.Packages))
		for i, pkg := range bucket.Packages {
			paths[i] = "./" + pkg
		}
		matrix.Include = append(matrix.Include, matrixEntry{
			Bucket: bucket.Name, Seed: strconv.FormatInt(seed, 10), Packages: strings.Join(paths, " "),
		})
		fmt.Fprintf(summary, "| %s | %d | %s |\n", bucket.Name, bucket.Weight, strings.Join(bucket.Packages, "<br>"))
		fmt.Printf("%s (weight %d): %s\n", bucket.Name, bucket.Weight, strings.Join(bucket.Packages, " "))
	}

	matrixJSON, err := json.Marshal(matrix)
	if err != nil {
		return err
	}
	packagesJSON, err := json.Marshal(packages)
	if err != nil {
		return err
	}
	if err := appendToFile(os.Getenv("GITHUB_OUTPUT"), fmt.Sprintf("matrix=%s\npackages=%s\n", matrixJSON, packagesJSON)); err != nil {
		return err
	}
	return appendToFile(os.Getenv("GITHUB_STEP_SUMMARY"), summary.String())
}

func runVerify(args []string) error {
	flags := flag.NewFlagSet("verify", flag.ExitOnError)
	packagesJSON := flags.String("packages", "", "JSON array of packages that must have run")
	reportsDir := flags.String("reports-dir", "", "directory with the bucket e2e-report-*.json files")
	_ = flags.Parse(args)

	var expected []string
	if err := json.Unmarshal([]byte(*packagesJSON), &expected); err != nil || len(expected) == 0 {
		return fmt.Errorf("--packages must be a non-empty JSON array: %v", err)
	}
	reports, warnings := ReadReports(*reportsDir)
	if len(warnings) > 0 {
		return fmt.Errorf("unreadable reports: %v", warnings)
	}
	return VerifyReports(expected, reports)
}

// seedFromRun makes the seed unique per run and attempt while keeping it reproducible from the logs.
func seedFromRun(runID, attempt string) int64 {
	id, _ := strconv.ParseInt(runID, 10, 64)
	att, _ := strconv.ParseInt(attempt, 10, 64)
	if id == 0 {
		return time.Now().UnixNano() % 2147483647
	}
	return max((id*100+att)%2147483647, 1)
}

func splitList(value string) []string {
	var items []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			items = append(items, item)
		}
	}
	return items
}

func appendToFile(path, content string) error {
	if path == "" {
		return nil
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.WriteString(content)
	return err
}
