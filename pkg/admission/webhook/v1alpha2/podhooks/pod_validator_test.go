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
	"errors"
	"testing"

	"github.com/kai-scheduler/KAI-scheduler/pkg/admission/plugins"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestPodValidator(t *testing.T) {
	RegisterFailHandler(Fail)
}

type validatorPlugin struct {
}

func (p *validatorPlugin) Name() string {
	return "foo-validator-plugin"
}

func (p *validatorPlugin) Mutate(pod *v1.Pod) error {
	return nil
}

func (p *validatorPlugin) Validate(pod *v1.Pod) error {
	// validate that the pod has a label "foo"
	if pod.Labels == nil {
		return errors.New("pod has no labels")
	}
	if _, ok := pod.Labels["foo"]; !ok {
		return errors.New("pod has no label 'foo'")
	}
	return nil
}

var _ = Describe("KaiAdmission Webhook", func() {
	var (
		validator *podValidator
	)

	BeforeEach(func() {
		plugin := &validatorPlugin{}
		testPlugins := plugins.New()
		testPlugins.RegisterPlugin(plugin)
		validator = &podValidator{
			kubeClient:    nil,
			plugins:       testPlugins,
			schedulerName: "test-scheduler",
		}
		Expect(validator).NotTo(BeNil(), "Expected validator to be initialized")
	})

	AfterEach(func() {
	})

	Context("When creating KaiAdmission under Defaulting Webhook", func() {
		It("Should deny creation if a required field is missing", func() {
			By("simulating an invalid creation scenario")
			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
					Labels:    map[string]string{},
				},
			}
			err := validator.plugins.Validate(pod)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("pod has no label 'foo'"))
		})
	})

})
