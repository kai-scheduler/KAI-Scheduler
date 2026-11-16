// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package status_updater

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	"github.com/kai-scheduler/KAI-scheduler/deployments/kai-scheduler/crds"
	kai "github.com/kai-scheduler/api/client/clientset/versioned"
	enginev2alpha2 "github.com/kai-scheduler/api/scheduling/v2alpha2"
)

func TestPodGroupStatusApply(t *testing.T) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("requires envtest assets (make envtest)")
	}
	ctx := context.Background()
	definitions, err := crds.LoadEmbeddedCRDs()
	require.NoError(t, err)
	var definition *apiextensionsv1.CustomResourceDefinition
	for _, crd := range definitions {
		if crd.Name == "podgroups.scheduling.run.ai" {
			definition = crd
			break
		}
	}
	require.NotNil(t, definition)
	oldDefinition := definition.DeepCopy()
	for i := range oldDefinition.Spec.Versions {
		properties := oldDefinition.Spec.Versions[i].Schema.OpenAPIV3Schema.Properties["status"].Properties
		conditions, ok := properties["schedulingConditions"]
		if !ok {
			continue
		}
		conditions.XListType = nil
		conditions.XListMapKeys = nil
		conditions.Items.Schema.Required = nil
		conditions.Items.Schema.XMapType = nil
		properties["schedulingConditions"] = conditions
		state := properties["schedulingState"]
		state.XMapType = nil
		properties["schedulingState"] = state
	}
	testEnv := &envtest.Environment{CRDs: []*apiextensionsv1.CustomResourceDefinition{oldDefinition}}
	config, err := testEnv.Start()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, testEnv.Stop()) })
	client, err := kai.NewForConfig(config)
	require.NoError(t, err)
	core, err := kubernetes.NewForConfig(config)
	require.NoError(t, err)
	_, err = core.CoreV1().Namespaces().Create(ctx, &v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ssa-test"}}, metav1.CreateOptions{})
	require.NoError(t, err)
	groups := client.SchedulingV2alpha2().PodGroups("ssa-test")
	create := func(t *testing.T, name, pool string) *enginev2alpha2.PodGroup {
		t.Helper()
		pg, err := groups.Create(ctx, &enginev2alpha2.PodGroup{
			ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{nodePoolLabelKey: pool}},
			Spec:       enginev2alpha2.PodGroupSpec{MinMember: ptr.To(int32(1)), Queue: "queue"},
		}, metav1.CreateOptions{})
		require.NoError(t, err)
		return pg
	}
	condition := func(pool, message string) enginev2alpha2.SchedulingCondition {
		return enginev2alpha2.SchedulingCondition{NodePool: pool, Type: enginev2alpha2.UnschedulableOnNodePool, Message: message, TransitionID: "1", Status: v1.ConditionTrue}
	}
	legacy := create(t, "legacy", "pool-a")
	legacy.Status.SchedulingConditions = []enginev2alpha2.SchedulingCondition{condition("pool-a", "old"), condition("pool-b", "preserve")}
	legacy.Status.ResourcesStatus.Requested = v1.ResourceList{v1.ResourceCPU: resource.MustParse("2")}
	legacy, err = groups.UpdateStatus(ctx, legacy, metav1.UpdateOptions{FieldManager: "old-scheduler"})
	require.NoError(t, err)
	legacyClear := create(t, "legacy-clear", "pool-a")
	legacyClear.Status = *legacy.Status.DeepCopy()
	legacyClear, err = groups.UpdateStatus(ctx, legacyClear, metav1.UpdateOptions{FieldManager: "old-scheduler"})
	require.NoError(t, err)
	legacyState := create(t, "legacy-state", "pool-a")
	legacyState.Status.SchedulingState = &enginev2alpha2.PodGroupSchedulingState{CorePods: []string{"old-core"}}
	legacyState, err = groups.UpdateStatus(ctx, legacyState, metav1.UpdateOptions{FieldManager: "old-scheduler"})
	require.NoError(t, err)
	legacyDuplicate := create(t, "legacy-duplicate", "default")
	duplicate := condition("pool-b", "newest")
	duplicate.TransitionID = "3"
	legacyDuplicate.Status.SchedulingConditions = []enginev2alpha2.SchedulingCondition{condition("", "default"), condition("pool-b", "old"), duplicate}
	legacyDuplicate, err = groups.UpdateStatus(ctx, legacyDuplicate, metav1.UpdateOptions{FieldManager: "old-scheduler"})
	require.NoError(t, err)
	t.Run("refuses an atomic schema without mutating status", func(t *testing.T) {
		updater := &defaultStatusUpdater{kaiClient: client, nodePoolLabelKey: nodePoolLabelKey}
		_, err := updater.applyPodGroupStatus(ctx, legacy)
		require.True(t, apierrors.IsBadRequest(err), "%v", err)
		unchanged, err := groups.Get(ctx, legacy.Name, metav1.GetOptions{})
		require.NoError(t, err)
		require.Equal(t, legacy.Status, unchanged.Status)
	})
	extensions, err := apiextensionsclient.NewForConfig(config)
	require.NoError(t, err)
	installed, err := extensions.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, definition.Name, metav1.GetOptions{})
	require.NoError(t, err)
	installed.Spec = definition.Spec
	_, err = extensions.ApiextensionsV1().CustomResourceDefinitions().Update(ctx, installed, metav1.UpdateOptions{})
	require.NoError(t, err)
	su := &defaultStatusUpdater{kaiClient: client, nodePoolLabelKey: nodePoolLabelKey}
	// Wait for the updated schema to reach the field manager before exercising apply.
	probe := create(t, "schema-probe", "pool-a")
	require.Eventually(t, func() bool {
		probe.Status.SchedulingConditions = []enginev2alpha2.SchedulingCondition{condition("pool-a", "probe")}
		written, err := su.sendSchedulingStatus(ctx, probe, "pool-a", schedulingStatusManagerPrefix+"pool-a", "")
		if err != nil {
			t.Logf("schema probe: %v", err)
			return false
		}
		if !ownsSchedulingCondition(written, schedulingStatusManagerPrefix+"pool-a", "pool-a") {
			t.Logf("schema probe fields: %+v", written.ManagedFields)
			return false
		}
		return true
	}, 10*time.Second, 100*time.Millisecond)

	t.Run("atomic list migration preserves other status and ownership", func(t *testing.T) {
		desired := legacy.DeepCopy()
		desired.Status.SchedulingConditions[0].Message = "new"
		updated, err := su.applyPodGroupStatus(ctx, desired)
		require.NoError(t, err)
		require.Equal(t, "new", conditionForNodePool(updated, "pool-a").Message)
		require.Equal(t, "preserve", conditionForNodePool(updated, "pool-b").Message)
		require.Equal(t, legacy.Status.ResourcesStatus, updated.Status.ResourcesStatus)
		require.True(t, ownsSchedulingCondition(updated, schedulingStatusManagerPrefix+"pool-a", "pool-a"))
		for _, entry := range updated.ManagedFields {
			if entry.Manager == "old-scheduler" {
				_, fields, err := statusFields(entry)
				require.NoError(t, err)
				require.Contains(t, fields, "f:resourcesStatus")
				var conditions map[string]json.RawMessage
				require.NoError(t, json.Unmarshal(fields["f:schedulingConditions"], &conditions))
				require.Contains(t, conditions, schedulingConditionKey("pool-b"))
				require.NotContains(t, conditions, schedulingConditionKey("pool-a"))
			}
		}
	})
	t.Run("first apply can clear a legacy entry", func(t *testing.T) {
		desired := legacyClear.DeepCopy()
		removePodGroupSchedulingCondition(desired, "pool-a")
		updated, err := su.applyPodGroupStatus(ctx, desired)
		require.NoError(t, err)
		require.Nil(t, conditionForNodePool(updated, "pool-a"))
		require.NotNil(t, conditionForNodePool(updated, "pool-b"))
		require.Equal(t, legacyClear.Status.ResourcesStatus, updated.Status.ResourcesStatus)
	})
	t.Run("migrates legacy scheduling state ownership", func(t *testing.T) {
		desired := legacyState.DeepCopy()
		desired.Status.SchedulingState.CorePods = []string{"new-core"}
		updated, err := su.applyPodGroupStatus(ctx, desired)
		require.NoError(t, err)
		require.Equal(t, []string{"new-core"}, updated.Status.SchedulingState.CorePods)
		for _, entry := range updated.ManagedFields {
			if entry.Manager == "old-scheduler" {
				_, status, err := statusFields(entry)
				require.NoError(t, err)
				require.NotContains(t, status, "f:schedulingState")
			}
		}
	})
	t.Run("normalizes missing and duplicate legacy keys", func(t *testing.T) {
		updater := &defaultStatusUpdater{kaiClient: client, nodePoolLabelKey: nodePoolLabelKey}
		updated, err := updater.applyPodGroupStatus(ctx, legacyDuplicate)
		require.NoError(t, err)
		require.Len(t, updated.Status.SchedulingConditions, 2)
		require.Equal(t, "default", conditionForNodePool(updated, "default").Message)
		require.Equal(t, "newest", conditionForNodePool(updated, "pool-b").Message)
	})
	t.Run("independent shard applies and resource updates coexist", func(t *testing.T) {
		pg := create(t, "parallel", "pool-a")
		var wg sync.WaitGroup
		errors := make(chan error, 3)
		for _, pool := range []string{"pool-a", "pool-b"} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				desired := pg.DeepCopy()
				desired.Status.SchedulingConditions = []enginev2alpha2.SchedulingCondition{condition(pool, pool)}
				_, err := su.sendSchedulingStatus(ctx, desired, pool, schedulingStatusManagerPrefix+pool, "")
				errors <- err
			}()
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := groups.Patch(ctx, pg.Name, types.MergePatchType, []byte(`{"status":{"resourcesStatus":{"requested":{"cpu":"3"}}}}`), metav1.PatchOptions{FieldManager: "podgroup-controller"}, "status")
			errors <- err
		}()
		wg.Wait()
		close(errors)
		for err := range errors {
			require.NoError(t, err)
		}
		updated, err := groups.Get(ctx, pg.Name, metav1.GetOptions{})
		require.NoError(t, err)
		require.Len(t, updated.Status.SchedulingConditions, 2)
		require.Equal(t, "3", updated.Status.ResourcesStatus.Requested.Cpu().String())
		removePodGroupSchedulingCondition(updated, "pool-a")
		updated, err = su.applyPodGroupStatus(ctx, updated)
		require.NoError(t, err)
		require.Nil(t, conditionForNodePool(updated, "pool-a"))
		require.NotNil(t, conditionForNodePool(updated, "pool-b"))
	})
	t.Run("state takeover replaces the core set and retains other conditions", func(t *testing.T) {
		pg := create(t, "takeover", "pool-a")
		pg.Status.SchedulingConditions = []enginev2alpha2.SchedulingCondition{condition("pool-a", "old")}
		pg.Status.SchedulingState = &enginev2alpha2.PodGroupSchedulingState{CorePods: []string{"old-core"}}
		pg, err = su.applyPodGroupStatus(ctx, pg)
		require.NoError(t, err)
		stale := pg.DeepCopy()
		pg.Labels[nodePoolLabelKey] = "pool-b"
		pg, err = groups.Update(ctx, pg, metav1.UpdateOptions{})
		require.NoError(t, err)
		pg.Status.SchedulingState.CorePods = []string{"new-core"}
		pg.Status.SchedulingConditions = append(pg.Status.SchedulingConditions, condition("pool-b", "new"))
		updated, err := su.applyPodGroupStatus(ctx, pg)
		require.NoError(t, err)
		require.Equal(t, []string{"new-core"}, updated.Status.SchedulingState.CorePods)
		require.NotNil(t, conditionForNodePool(updated, "pool-a"))
		_, err = su.applyPodGroupStatus(ctx, stale)
		require.True(t, apierrors.IsConflict(err), "%v", err)
		updated.Status.SchedulingState.CorePods = nil
		updated, err = su.applyPodGroupStatus(ctx, updated)
		require.NoError(t, err)
		require.Empty(t, updated.Status.SchedulingState.CorePods)
	})
	for _, changeSpec := range []bool{false, true} {
		name := "resources-race"
		if changeSpec {
			name = "spec-race"
		}
		t.Run(name, func(t *testing.T) {
			desired := create(t, name, "pool-a")
			desired.Status.SchedulingConditions = []enginev2alpha2.SchedulingCondition{condition("pool-a", "verdict")}
			var applyCalls atomic.Int32
			racingConfig := rest.CopyConfig(config)
			racingConfig.WrapTransport = func(transport http.RoundTripper) http.RoundTripper {
				return roundTripperFunc(func(request *http.Request) (*http.Response, error) {
					if request.Method == http.MethodPatch && request.URL.Query().Get("dryRun") == "" && strings.HasSuffix(request.URL.Path, "/"+name+"/status") && strings.Contains(request.Header.Get("Content-Type"), "apply-patch") {
						if applyCalls.Add(1) == 1 {
							patch := []byte(`{"status":{"resourcesStatus":{"requested":{"cpu":"7"}}}}`)
							subresources := []string{"status"}
							if changeSpec {
								patch = []byte(`{"spec":{"minMember":2}}`)
								subresources = nil
							}
							if _, err := groups.Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{FieldManager: "external-controller"}, subresources...); err != nil {
								return nil, err
							}
						}
					}
					return transport.RoundTrip(request)
				})
			}
			racingClient, err := kai.NewForConfig(racingConfig)
			require.NoError(t, err)
			racingUpdater := &defaultStatusUpdater{kaiClient: racingClient, nodePoolLabelKey: nodePoolLabelKey}
			updated, err := racingUpdater.applyPodGroupStatus(ctx, desired)
			if changeSpec {
				require.True(t, apierrors.IsConflict(err), "%v", err)
				updated, err = groups.Get(ctx, name, metav1.GetOptions{})
				require.NoError(t, err)
				require.Empty(t, updated.Status.SchedulingConditions)
				require.EqualValues(t, 1, applyCalls.Load())
			} else {
				require.NoError(t, err)
				require.EqualValues(t, 2, applyCalls.Load())
				require.Equal(t, "7", updated.Status.ResourcesStatus.Requested.Cpu().String())
				require.Equal(t, "verdict", conditionForNodePool(updated, "pool-a").Message)
			}
		})
	}

	t.Run("rejects stale specs and recreated objects", func(t *testing.T) {
		pg := create(t, "stale", "pool-a")
		stale := pg.DeepCopy()
		pg.Spec.MinMember = ptr.To(int32(2))
		_, err := groups.Update(ctx, pg, metav1.UpdateOptions{})
		require.NoError(t, err)
		_, err = su.applyPodGroupStatus(ctx, stale)
		require.True(t, apierrors.IsConflict(err), "%v", err)
		require.NoError(t, groups.Delete(ctx, pg.Name, metav1.DeleteOptions{}))
		replacement := create(t, pg.Name, "pool-a")
		_, err = su.applyPodGroupStatus(ctx, stale)
		require.True(t, apierrors.IsConflict(err), "%v", err)
		updated, err := groups.Get(ctx, pg.Name, metav1.GetOptions{})
		require.NoError(t, err)
		require.Equal(t, replacement.UID, updated.UID)
		require.Empty(t, updated.Status.SchedulingConditions)
	})
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }
