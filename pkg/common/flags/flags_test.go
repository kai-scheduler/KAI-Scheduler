// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package flags

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/exp/slices"
	"strings"
	"testing"
)

func TestCommonFlags(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "common flags tests")
}

var _ = Describe("StringMapFlag", func() {
	It("parses an empty string as an empty map", func() {
		var m StringMapFlag
		Expect(m.Set("")).To(Succeed())
		Expect(m.Get()).To(BeEmpty())
	})

	It("parses a single key=value pair", func() {
		var m StringMapFlag
		Expect(m.Set("foo=bar")).To(Succeed())
		Expect(m.Get()).To(HaveKeyWithValue("foo", "bar"))
		Expect(len(m.Get())).To(Equal(1))
	})

	It("parses multiple key=value pairs", func() {
		var m StringMapFlag
		Expect(m.Set("foo=bar,baz=qux")).To(Succeed())
		Expect(m.Get()).To(HaveKeyWithValue("foo", "bar"))
		Expect(m.Get()).To(HaveKeyWithValue("baz", "qux"))
		Expect(len(m.Get())).To(Equal(2))
	})

	It("overwrites duplicate keys with the last value", func() {
		var m StringMapFlag
		Expect(m.Set("foo=bar,foo=baz")).To(Succeed())
		Expect(m.Get()).To(HaveKeyWithValue("foo", "baz"))
		Expect(len(m.Get())).To(Equal(1))
	})

	It("returns an error for invalid input", func() {
		var m StringMapFlag
		err := m.Set("foo,bar=baz")
		Expect(err).To(HaveOccurred())
	})

	It("String() returns the correct string representation", func() {
		var m StringMapFlag
		err := m.Set("foo=bar,baz=qux")
		Expect(err).ToNot(HaveOccurred())
		str := m.String()
		// Accept either order
		Expect([]string{str, reversePairs(str)}).To(ContainElement("foo=bar,baz=qux"))
	})
})

type testStruct struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

var _ = Describe("JSONFlag", func() {
	It("Value is nil by default", func() {
		var f JSONFlag[testStruct]
		Expect(f.Value).To(BeNil())
		Expect(f.String()).To(Equal(""))
	})

	It("parses valid JSON", func() {
		var f JSONFlag[testStruct]
		Expect(f.Set(`{"name":"test","count":42}`)).To(Succeed())
		Expect(f.Value).ToNot(BeNil())
		Expect(f.Value.Name).To(Equal("test"))
		Expect(f.Value.Count).To(Equal(42))
	})

	It("returns error for invalid JSON", func() {
		var f JSONFlag[testStruct]
		Expect(f.Set("not-json")).To(HaveOccurred())
		Expect(f.Value).To(BeNil())
	})

	It("sets Value to nil for empty string", func() {
		var f JSONFlag[testStruct]
		Expect(f.Set(`{"name":"test"}`)).To(Succeed())
		Expect(f.Value).ToNot(BeNil())
		Expect(f.Set("")).To(Succeed())
		Expect(f.Value).To(BeNil())
	})

	It("String() returns JSON representation", func() {
		var f JSONFlag[testStruct]
		Expect(f.Set(`{"name":"test","count":1}`)).To(Succeed())
		Expect(f.String()).To(Equal(`{"name":"test","count":1}`))
	})

	It("Type() returns json", func() {
		var f JSONFlag[testStruct]
		Expect(f.Type()).To(Equal("json"))
	})
})

// Helper to reverse the order of pairs in a comma-separated string
func reversePairs(s string) string {
	pairs := strings.Split(s, ",")
	slices.Reverse(pairs)
	return strings.Join(pairs, ",")
}
