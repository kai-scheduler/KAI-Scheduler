// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package featuregates

import (
	"errors"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	resourcev1 "k8s.io/api/resource/v1"
	resourcev1alhpa3 "k8s.io/api/resource/v1alpha3"
	resourcev1beta1 "k8s.io/api/resource/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	version "k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/discovery"
	fakediscovery "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/kubernetes/fake"
)

// erroringDiscovery fails the requested discovery call, standing in for an API
// server that is briefly unreachable while a component starts up.
type erroringDiscovery struct {
	discovery.DiscoveryInterface
	versionErr error
	groupsErr  error
}

func (e *erroringDiscovery) ServerVersion() (*version.Info, error) {
	if e.versionErr != nil {
		return nil, e.versionErr
	}
	return e.DiscoveryInterface.ServerVersion()
}

func (e *erroringDiscovery) ServerGroups() (*metav1.APIGroupList, error) {
	if e.groupsErr != nil {
		return nil, e.groupsErr
	}
	return e.DiscoveryInterface.ServerGroups()
}

func discoveryForVersion(major, minor string) discovery.DiscoveryInterface {
	fakeClient := fake.NewClientset()
	fakeClient.Discovery().(*fakediscovery.FakeDiscovery).FakedServerVersion = &version.Info{
		Major: major,
		Minor: minor,
	}
	return fakeClient.Discovery()
}

func TestCache(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Test cache")
}

var _ = Describe("New", func() {
	Context("DRA Feature Gate", func() {
		DescribeTable("should report DRA availability based on Kubernetes version and resource API availability",
			func(serverMajor, serverMinor string, resourceGroupVersions []string, expectDRAAvailable bool) {
				fakeClient := fake.NewClientset()
				fakeClient.Discovery().(*fakediscovery.FakeDiscovery).FakedServerVersion = &version.Info{
					Major: serverMajor,
					Minor: serverMinor,
				}

				for _, groupVersion := range resourceGroupVersions {
					fakeClient.Resources = append(fakeClient.Resources, &metav1.APIResourceList{GroupVersion: groupVersion})
				}

				Expect(IsDynamicResourcesEnabled(fakeClient.Discovery())).To(Equal(expectDRAAvailable))
			},
			Entry("compatible version (1.32) with resource API should enable DRA", "1", "32", []string{resourcev1beta1.SchemeGroupVersion.String()}, true),
			Entry("compatible version (1.32+) with resource API should enable DRA", "1", "32+", []string{resourcev1beta1.SchemeGroupVersion.String()}, true),
			Entry("compatible version (1.32) without resource API should not enable DRA", "1", "32", []string{}, false),
			Entry("incompatible version (1.25) with resource API should not enable DRA", "1", "25", []string{resourcev1beta1.SchemeGroupVersion.String()}, false),
			Entry("incompatible version (1.25) without resource API should not enable DRA", "1", "25", []string{}, false),
			Entry("edge case version (1.31) with resource API should not enable DRA", "1", "31", []string{resourcev1alhpa3.SchemeGroupVersion.String()}, false),
			Entry("higher compatible version (1.35) with resource API should enable DRA", "1", "34", []string{resourcev1.SchemeGroupVersion.String()}, true),
		)

		It("should return an error when the server version cannot be retrieved", func() {
			discoveryClient := &erroringDiscovery{
				DiscoveryInterface: discoveryForVersion("1", "34"),
				versionErr:         errors.New("connection refused"),
			}

			_, err := IsDynamicResourcesEnabled(discoveryClient)
			Expect(err).To(HaveOccurred())
		})

		It("should return an error when the server groups cannot be retrieved", func() {
			discoveryClient := &erroringDiscovery{
				DiscoveryInterface: discoveryForVersion("1", "34"),
				groupsErr:          errors.New("connection refused"),
			}

			_, err := IsDynamicResourcesEnabled(discoveryClient)
			Expect(err).To(HaveOccurred())
		})

		It("should leave the gate untouched when discovery fails", func() {
			SetDynamicResourcesEnabledForTest(true)
			DeferCleanup(SetDynamicResourcesEnabledForTest, false)

			discoveryClient := &erroringDiscovery{
				DiscoveryInterface: discoveryForVersion("1", "34"),
				versionErr:         errors.New("connection refused"),
			}

			Expect(SetDRAFeatureGate(discoveryClient)).NotTo(Succeed())
			Expect(DynamicResourcesEnabled()).To(BeTrue())
		})
	})

	Context("NodeResourceTopology Feature Gate", func() {
		It("should report availability when the API group is served", func() {
			fakeClient := fake.NewClientset()
			fakeClient.Resources = append(fakeClient.Resources,
				&metav1.APIResourceList{GroupVersion: nodeResourceTopologyGroup + "/v1alpha2"})

			Expect(IsNodeResourceTopologyEnabled(fakeClient.Discovery())).To(BeTrue())
		})

		It("should report unavailability when the API group is absent", func() {
			Expect(IsNodeResourceTopologyEnabled(fake.NewClientset().Discovery())).To(BeFalse())
		})

		It("should return an error when the server groups cannot be retrieved", func() {
			discoveryClient := &erroringDiscovery{
				DiscoveryInterface: fake.NewClientset().Discovery(),
				groupsErr:          errors.New("connection refused"),
			}

			_, err := IsNodeResourceTopologyEnabled(discoveryClient)
			Expect(err).To(HaveOccurred())
		})

		It("should leave the gate untouched when discovery fails", func() {
			SetNodeResourceTopologyEnabledForTest(true)
			DeferCleanup(SetNodeResourceTopologyEnabledForTest, false)

			discoveryClient := &erroringDiscovery{
				DiscoveryInterface: fake.NewClientset().Discovery(),
				groupsErr:          errors.New("connection refused"),
			}

			Expect(SetNodeResourceTopologyFeatureGate(discoveryClient)).NotTo(Succeed())
			Expect(NodeResourceTopologyEnabled()).To(BeTrue())
		})
	})
})
