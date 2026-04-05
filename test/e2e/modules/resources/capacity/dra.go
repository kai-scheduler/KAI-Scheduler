/*
Copyright 2025 NVIDIA CORPORATION
SPDX-License-Identifier: Apache-2.0
*/
package capacity

import (
	"context"
	"fmt"

	"github.com/kai-scheduler/KAI-scheduler/pkg/common/constants"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// SkipIfInsufficientDynamicResources checks if there are enough NodeName-type resourceSlices that have at least devicePerNode devices
func SkipIfInsufficientDynamicResources(clientset kubernetes.Interface, deviceClassName string, nodes, devicePerNode int) {
	devicesByNode := ListDevicesByNode(clientset, deviceClassName)

	fittingNodes := 0
	for _, devices := range devicesByNode {
		if devices >= devicePerNode {
			fittingNodes++
		}
	}

	if fittingNodes < nodes {
		ginkgo.Skip(fmt.Sprintf("Expected at least %d nodes with %d devices of deviceClass %s, found %d",
			nodes, devicePerNode, deviceClassName, fittingNodes))
	}
}

func ListDevicesByNode(clientset kubernetes.Interface, deviceClass string) map[string]int {
	resourceSlices, err := clientset.ResourceV1().ResourceSlices().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			ginkgo.Skip("DRA is not enabled in the cluster, skipping")
		}
		gomega.Expect(err).NotTo(gomega.HaveOccurred(), "Failed to list resource slices")
	}

	devicesByNode := map[string]int{}
	for _, slice := range resourceSlices.Items {
		if slice.Spec.NodeName == nil || *slice.Spec.NodeName == "" {
			continue
		}

		// TODO: This is NOT the proper way to filter devices: the correct way is to check for each device if it matches
		// the device class CEL selector. We'll need to address it if we want more complicated tests.
		if slice.Spec.Driver != deviceClass {
			continue
		}

		if _, ok := devicesByNode[*slice.Spec.NodeName]; !ok {
			devicesByNode[*slice.Spec.NodeName] = 0
		}

		devicesByNode[*slice.Spec.NodeName] = devicesByNode[*slice.Spec.NodeName] + len(slice.Spec.Devices)
	}

	return devicesByNode
}

func CleanupResourceClaims(ctx context.Context, clientset kubernetes.Interface, namespace string) {
	err := clientset.ResourceV1().ResourceClaims(namespace).
		DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{
			LabelSelector: fmt.Sprintf("%s=engine-e2e", constants.AppLabelName),
		})
	if err != nil {
		if !errors.IsNotFound(err) {
			gomega.Expect(err).To(gomega.Succeed(), "Failed to delete resource claim")
		}
	}
}
