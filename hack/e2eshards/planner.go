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
	// Accept the first candidate whose heaviest shard is within this percent of a perfect split.
	tolerancePercent = 110
)

// Shard is a group of E2E packages that run in one CI job.
type Shard struct {
	Name     string
	Packages []string
	Weight   int
}

// Plan splits packages into shards. The split is a pure function of its inputs: the seed
// shuffles package order and breaks ties, so each seed yields a different but balanced grouping.
func Plan(packages []string, weights map[string]int, shardCount int, seed int64) ([]Shard, error) {
	if shardCount < 1 {
		return nil, fmt.Errorf("shard count must be at least 1, got %d", shardCount)
	}
	if len(packages) < shardCount {
		return nil, fmt.Errorf("%d packages cannot fill %d shards", len(packages), shardCount)
	}

	target := targetLoad(packages, weights, shardCount)
	var best []Shard
	bestMax := -1
	for candidate := 0; candidate < candidateAttempts; candidate++ {
		shards := assign(packages, weights, shardCount, rand.New(rand.NewSource(seed+int64(candidate))))
		maxLoad := heaviest(shards)
		if bestMax == -1 || maxLoad < bestMax {
			best, bestMax = shards, maxLoad
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

func assign(packages []string, weights map[string]int, shardCount int, rng *rand.Rand) []Shard {
	shuffled := slices.Clone(packages)
	rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })

	shards := make([]Shard, shardCount)
	for i := range shards {
		shards[i].Name = fmt.Sprintf("shard-%d", i+1)
	}
	for _, pkg := range shuffled {
		lightest := []int{0}
		for i := 1; i < shardCount; i++ {
			switch {
			case shards[i].Weight < shards[lightest[0]].Weight:
				lightest = []int{i}
			case shards[i].Weight == shards[lightest[0]].Weight:
				lightest = append(lightest, i)
			}
		}
		target := &shards[lightest[rng.Intn(len(lightest))]]
		target.Packages = append(target.Packages, pkg)
		target.Weight += weights[pkg]
	}
	return shards
}

func targetLoad(packages []string, weights map[string]int, shardCount int) int {
	total, largest := 0, 0
	for _, pkg := range packages {
		total += weights[pkg]
		largest = max(largest, weights[pkg])
	}
	ideal := (total + shardCount - 1) / shardCount
	return max((ideal*tolerancePercent+99)/100, largest)
}

func heaviest(shards []Shard) int {
	maxLoad := 0
	for _, shard := range shards {
		maxLoad = max(maxLoad, shard.Weight)
	}
	return maxLoad
}

// validate guarantees that every package is assigned exactly once and no shard is empty.
func validate(shards []Shard, packages []string) error {
	count := map[string]int{}
	for _, shard := range shards {
		if len(shard.Packages) == 0 {
			return fmt.Errorf("%s has no packages", shard.Name)
		}
		for _, pkg := range shard.Packages {
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
