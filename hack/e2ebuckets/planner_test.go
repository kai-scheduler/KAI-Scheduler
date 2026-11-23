// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestE2EBuckets(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "E2E bucket planner")
}

func pkgName(i int) string { return fmt.Sprintf("test/e2e/suites/p%02d", i) }

func fixture() ([]string, map[string]int) {
	weights := map[string]int{}
	var packages []string
	for i, weight := range []int{300, 250, 200, 120, 100, 90, 60, 50, 40, 30, 20, 10} {
		packages = append(packages, pkgName(i))
		weights[pkgName(i)] = weight
	}
	return packages, weights
}

var _ = Describe("Plan", func() {
	It("assigns every package exactly once with no empty bucket", func() {
		packages, weights := fixture()
		for seed := int64(1); seed <= 25; seed++ {
			buckets, err := Plan(packages, weights, 3, seed)
			Expect(err).NotTo(HaveOccurred())
			Expect(buckets).To(HaveLen(3))
			var all []string
			for _, bucket := range buckets {
				Expect(bucket.Packages).NotTo(BeEmpty())
				all = append(all, bucket.Packages...)
			}
			Expect(all).To(ConsistOf(packages))
		}
	})

	It("is deterministic for a seed and varies across seeds", func() {
		packages, weights := fixture()
		first, _ := Plan(packages, weights, 3, 7)
		again, _ := Plan(packages, weights, 3, 7)
		Expect(again).To(Equal(first))

		distinct := map[string]bool{}
		for seed := int64(1); seed <= 20; seed++ {
			buckets, _ := Plan(packages, weights, 3, seed)
			distinct[fmt.Sprint(buckets[0].Packages)] = true
		}
		Expect(len(distinct)).To(BeNumerically(">", 1))
	})

	It("keeps the heaviest bucket near the ideal split", func() {
		packages, weights := fixture()
		total := 0
		for _, weight := range weights {
			total += weight
		}
		for seed := int64(1); seed <= 25; seed++ {
			buckets, err := Plan(packages, weights, 3, seed)
			Expect(err).NotTo(HaveOccurred())
			Expect(heaviest(buckets)).To(BeNumerically("<=", total/3*115/100))
		}
	})

	It("supports any bucket count up to the package count", func() {
		packages, weights := fixture()
		for count := 1; count <= len(packages); count++ {
			buckets, err := Plan(packages, weights, count, 3)
			Expect(err).NotTo(HaveOccurred())
			Expect(buckets).To(HaveLen(count))
		}
	})

	It("rejects more buckets than packages and non-positive counts", func() {
		packages, weights := fixture()
		_, err := Plan(packages, weights, len(packages)+1, 1)
		Expect(err).To(HaveOccurred())
		_, err = Plan(packages, weights, 0, 1)
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("Weights", func() {
	report := func(pkg string, seconds int64, ok bool) suiteReport {
		return suiteReport{SuitePath: "/runner/work/repo/" + pkg, RunTime: seconds * 1e9, SuiteSucceeded: ok}
	}

	It("averages history and gives unknown packages twice the median", func() {
		history := []suiteReport{
			report("test/e2e/suites/a", 100, true), report("test/e2e/suites/a", 200, true),
			report("test/e2e/suites/b", 50, true), report("test/e2e/suites/c", 10, true),
		}
		weights, missing := Weights([]string{"test/e2e/suites/a", "test/e2e/suites/b", "test/e2e/suites/c", "test/e2e/suites/new"}, history)
		Expect(weights["test/e2e/suites/a"]).To(Equal(151))
		Expect(weights["test/e2e/suites/new"]).To(Equal(2 * 51))
		Expect(missing).To(ConsistOf("test/e2e/suites/new"))
	})

	It("falls back to uniform weights without history", func() {
		weights, missing := Weights([]string{"test/e2e/suites/a", "test/e2e/suites/b"}, nil)
		Expect(weights).To(HaveKeyWithValue("test/e2e/suites/a", 2))
		Expect(missing).To(HaveLen(2))
	})
})

var _ = Describe("VerifyReports", func() {
	expected := []string{"test/e2e/suites/a", "test/e2e/suites/b"}
	suite := func(pkg string, ok bool) suiteReport {
		return suiteReport{SuitePath: "/w/" + pkg, SuiteSucceeded: ok}
	}

	It("passes when every package ran and succeeded", func() {
		Expect(VerifyReports(expected, []suiteReport{suite(expected[0], true), suite(expected[1], true)})).To(Succeed())
	})
	It("fails when a package is missing", func() {
		Expect(VerifyReports(expected, []suiteReport{suite(expected[0], true)})).To(MatchError(ContainSubstring(expected[1])))
	})
	It("fails when a suite failed", func() {
		Expect(VerifyReports(expected, []suiteReport{suite(expected[0], true), suite(expected[1], false)})).NotTo(Succeed())
	})
})

var _ = Describe("Discovery", func() {
	It("finds suites with RunSpecs and separates dedicated ones", func() {
		root := GinkgoT().TempDir()
		write := func(rel, content string) {
			path := filepath.Join(root, rel)
			Expect(os.MkdirAll(filepath.Dir(path), 0o755)).To(Succeed())
			Expect(os.WriteFile(path, []byte(content), 0o644)).To(Succeed())
		}
		write("a/a_suite_test.go", "RunSpecs(t, \"a\")")
		write("b/b_test.go", "no entrypoint")
		write("upgrade/u_suite_test.go", "RunSpecs(t, \"u\")")

		suites, err := DiscoverSuites(root)
		Expect(err).NotTo(HaveOccurred())
		Expect(suites).To(Equal([]string{filepath.ToSlash(filepath.Join(root, "a")), filepath.ToSlash(filepath.Join(root, "upgrade"))}))

		main, err := SplitDedicated(suites, []string{suites[1]})
		Expect(err).NotTo(HaveOccurred())
		Expect(main).To(Equal([]string{suites[0]}))
		_, err = SplitDedicated(suites, []string{"missing"})
		Expect(err).To(HaveOccurred())
	})
})
