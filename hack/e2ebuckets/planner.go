// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"math/rand"
	"slices"
	"sort"
)

const (
	candidateAttempts = 200
	// Accept the first candidate whose heaviest bucket is within this percent of a perfect split.
	tolerancePercent = 110
)

// Bucket is a group of E2E packages that run in one CI job.
type Bucket struct {
	Name     string
	Packages []string
	Weight   int
}

// Plan splits packages into buckets. The split is a pure function of its inputs: the seed
// shuffles package order and breaks ties, so each seed yields a different but balanced grouping.
func Plan(packages []string, weights map[string]int, bucketCount int, seed int64) ([]Bucket, error) {
	if bucketCount < 1 {
		return nil, fmt.Errorf("bucket count must be at least 1, got %d", bucketCount)
	}
	if len(packages) < bucketCount {
		return nil, fmt.Errorf("%d packages cannot fill %d buckets", len(packages), bucketCount)
	}

	target := targetLoad(packages, weights, bucketCount)
	var best []Bucket
	bestMax := -1
	for candidate := 0; candidate < candidateAttempts; candidate++ {
		buckets := assign(packages, weights, bucketCount, rand.New(rand.NewSource(seed+int64(candidate))))
		maxLoad := heaviest(buckets)
		if bestMax == -1 || maxLoad < bestMax {
			best, bestMax = buckets, maxLoad
		}
		if maxLoad <= target {
			break
		}
	}

	for i := range best {
		sort.Strings(best[i].Packages)
	}
	return best, validate(best, packages)
}

func assign(packages []string, weights map[string]int, bucketCount int, rng *rand.Rand) []Bucket {
	shuffled := slices.Clone(packages)
	rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })

	buckets := make([]Bucket, bucketCount)
	for i := range buckets {
		buckets[i].Name = fmt.Sprintf("bucket-%d", i+1)
	}
	for _, pkg := range shuffled {
		lightest := []int{0}
		for i := 1; i < bucketCount; i++ {
			switch {
			case buckets[i].Weight < buckets[lightest[0]].Weight:
				lightest = []int{i}
			case buckets[i].Weight == buckets[lightest[0]].Weight:
				lightest = append(lightest, i)
			}
		}
		target := &buckets[lightest[rng.Intn(len(lightest))]]
		target.Packages = append(target.Packages, pkg)
		target.Weight += weights[pkg]
	}
	return buckets
}

func targetLoad(packages []string, weights map[string]int, bucketCount int) int {
	total, largest := 0, 0
	for _, pkg := range packages {
		total += weights[pkg]
		largest = max(largest, weights[pkg])
	}
	ideal := (total + bucketCount - 1) / bucketCount
	return max((ideal*tolerancePercent+99)/100, largest)
}

func heaviest(buckets []Bucket) int {
	maxLoad := 0
	for _, bucket := range buckets {
		maxLoad = max(maxLoad, bucket.Weight)
	}
	return maxLoad
}

// validate guarantees that every package is assigned exactly once and no bucket is empty.
func validate(buckets []Bucket, packages []string) error {
	count := map[string]int{}
	for _, bucket := range buckets {
		if len(bucket.Packages) == 0 {
			return fmt.Errorf("%s has no packages", bucket.Name)
		}
		for _, pkg := range bucket.Packages {
			count[pkg]++
		}
	}
	for _, pkg := range packages {
		if count[pkg] != 1 {
			return fmt.Errorf("package %s assigned %d times, want exactly once", pkg, count[pkg])
		}
	}
	return nil
}
