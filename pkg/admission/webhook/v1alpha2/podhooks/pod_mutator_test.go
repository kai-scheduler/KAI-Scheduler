/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package podhooks

import (
	"testing"

	"github.com/kai-scheduler/kai-scheduler/pkg/admission/plugins"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestPodMutator(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Pod Mutator Suite")
}

type fooLabelPlugin struct {
}

func (p *fooLabelPlugin) Name() string {
	return "foo-label-plugin"
}

func (p *fooLabelPlugin) Mutate(pod *v1.Pod) error {
	if pod.Labels == nil {
		pod.Labels = map[string]string{}
	}
	pod.Labels["foo"] = "default_value"
	return nil
}

func (p *fooLabelPlugin) Validate(pod *v1.Pod) error {
	return nil
}

var _ = Describe("KaiAdmission Webhook", func() {
	var (
		defaulter *podMutator
	)

	BeforeEach(func() {
		// create a simple plugin that adds a label to the pod
		plugin := &fooLabelPlugin{}
		testPlugins := plugins.New()
		testPlugins.RegisterPlugin(plugin)
		defaulter = &podMutator{
			kubeClient:    nil,
			plugins:       testPlugins,
			schedulerName: "test-scheduler",
		}
		Expect(defaulter).NotTo(BeNil(), "Expected defaulter to be initialized")
	})

	AfterEach(func() {
	})

	Context("When creating KaiAdmission under Defaulting Webhook", func() {

		It("Should apply defaults when a required field is empty", func() {
			By("simulating a scenario where defaults should be applied")
			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
					Labels:    map[string]string{},
				},
				Spec: v1.PodSpec{
					SchedulerName: "test-scheduler",
					Containers: []v1.Container{
						{
							Name: "test-container",
						},
					},
				},
			}

			// Test the plugin directly instead of going through the webhook
			err := defaulter.plugins.Mutate(pod)
			Expect(err).To(BeNil())
			Expect(pod.Labels).To(HaveKeyWithValue("foo", "default_value"))
		})
	})
})
