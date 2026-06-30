// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package numa_placement_exporter

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	kaiv1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1"
)

func TestNumaPlacementExporter(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "NumaPlacementExporter operand Suite")
}

func fakeClient(objects ...client.Object) client.Client {
	testScheme := runtime.NewScheme()
	Expect(kaiv1.AddToScheme(testScheme)).To(Succeed())
	Expect(corev1.AddToScheme(testScheme)).To(Succeed())
	Expect(appsv1.AddToScheme(testScheme)).To(Succeed())
	return fake.NewClientBuilder().WithScheme(testScheme).WithObjects(objects...).Build()
}

func defaultedConfig() *kaiv1.Config {
	cfg := &kaiv1.Config{Spec: kaiv1.ConfigSpec{Namespace: "kai-scheduler"}}
	cfg.Spec.SetDefaultsWhereNeeded()
	return cfg
}

func numaShard(name string, enabled bool) *kaiv1.SchedulingShard {
	return &kaiv1.SchedulingShard{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: kaiv1.SchedulingShardSpec{
			Plugins: map[string]kaiv1.PluginConfig{
				numaPluginName: {Enabled: ptr.To(enabled)},
			},
		},
	}
}

func kinds(objects []client.Object) []string {
	out := make([]string, 0, len(objects))
	for _, o := range objects {
		out = append(out, o.GetObjectKind().GroupVersionKind().Kind)
	}
	return out
}

var _ = Describe("NumaPlacementExporter DesiredState", func() {
	var cfg *kaiv1.Config

	BeforeEach(func() {
		cfg = defaultedConfig()
	})

	It("auto-deploys when the numa plugin is enabled in a shard", func(ctx context.Context) {
		c := fakeClient(numaShard("default", true))
		objects, err := (&NumaPlacementExporter{}).DesiredState(ctx, c, cfg)
		Expect(err).To(BeNil())
		Expect(kinds(objects)).To(ConsistOf("DaemonSet", "ServiceAccount"))
	})

	It("does not deploy when no shard enables the numa plugin", func(ctx context.Context) {
		c := fakeClient(numaShard("default", false))
		objects, err := (&NumaPlacementExporter{}).DesiredState(ctx, c, cfg)
		Expect(err).To(BeNil())
		Expect(objects).To(BeEmpty())
	})

	It("does not deploy when there are no shards", func(ctx context.Context) {
		objects, err := (&NumaPlacementExporter{}).DesiredState(ctx, fakeClient(), cfg)
		Expect(err).To(BeNil())
		Expect(objects).To(BeEmpty())
	})

	It("explicit enabled=false overrides a numa-enabled shard", func(ctx context.Context) {
		cfg.Spec.NumaPlacementExporter.Service.Enabled = ptr.To(false)
		c := fakeClient(numaShard("default", true))
		objects, err := (&NumaPlacementExporter{}).DesiredState(ctx, c, cfg)
		Expect(err).To(BeNil())
		Expect(objects).To(BeEmpty())
	})

	It("explicit enabled=true deploys even with no shards", func(ctx context.Context) {
		cfg.Spec.NumaPlacementExporter.Service.Enabled = ptr.To(true)
		objects, err := (&NumaPlacementExporter{}).DesiredState(ctx, fakeClient(), cfg)
		Expect(err).To(BeNil())
		Expect(kinds(objects)).To(ConsistOf("DaemonSet", "ServiceAccount"))
	})

	It("builds the DaemonSet with hostPath mounts, root, NODE_NAME and args", func(ctx context.Context) {
		cfg.Spec.NumaPlacementExporter.Service.Enabled = ptr.To(true)
		objects, err := (&NumaPlacementExporter{}).DesiredState(ctx, fakeClient(), cfg)
		Expect(err).To(BeNil())

		var ds *appsv1.DaemonSet
		for _, o := range objects {
			if d, ok := o.(*appsv1.DaemonSet); ok {
				ds = d
			}
		}
		Expect(ds).ToNot(BeNil())
		Expect(ds.Spec.Template.Spec.NodeSelector).To(Equal(map[string]string{
			"feature.node.kubernetes.io/memory-numa": "true",
		}))

		container := ds.Spec.Template.Spec.Containers[0]
		Expect(*container.SecurityContext.RunAsUser).To(Equal(int64(0)))
		Expect(*container.SecurityContext.RunAsNonRoot).To(BeFalse())
		Expect(container.Args).To(ContainElement("--sysfs-root=" + sysfsMountPath))

		mountPaths := []string{}
		for _, m := range container.VolumeMounts {
			mountPaths = append(mountPaths, m.MountPath)
		}
		Expect(mountPaths).To(ConsistOf(podResourcesDir, sysfsMountPath))

		envNames := []string{}
		for _, e := range container.Env {
			envNames = append(envNames, e.Name)
		}
		Expect(envNames).To(ContainElement("NODE_NAME"))
	})

	It("is idempotent across reconciles (no duplicate volumes/mounts)", func(ctx context.Context) {
		cfg.Spec.NumaPlacementExporter.Service.Enabled = ptr.To(true)
		c := fakeClient()
		operand := &NumaPlacementExporter{}

		first, err := operand.DesiredState(ctx, c, cfg)
		Expect(err).To(BeNil())
		for _, o := range first {
			Expect(c.Create(ctx, o)).To(Succeed())
		}

		// Second reconcile builds from the now-existing object; appending would duplicate.
		second, err := operand.DesiredState(ctx, c, cfg)
		Expect(err).To(BeNil())

		var ds *appsv1.DaemonSet
		for _, o := range second {
			if d, ok := o.(*appsv1.DaemonSet); ok {
				ds = d
			}
		}
		Expect(ds).ToNot(BeNil())
		Expect(ds.Spec.Template.Spec.Volumes).To(HaveLen(2))
		Expect(ds.Spec.Template.Spec.Containers[0].VolumeMounts).To(HaveLen(2))
		Expect(ds.Spec.Template.Spec.Containers[0].Env).To(HaveLen(1))
	})
})
