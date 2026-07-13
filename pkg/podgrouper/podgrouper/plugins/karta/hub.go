// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package karta

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kai-scheduler/KAI-scheduler/pkg/podgrouper/podgrouper/plugins/grouper"
	kartav1alpha1 "github.com/run-ai/karta/pkg/api/runai/v1alpha1"
	"github.com/run-ai/karta/pkg/instructions"
)

const missingCrdRetryTTL = time.Minute

type KartaHub struct {
	client            client.Client
	defaultGrouper    grouper.Grouper
	isKartaCrdMissing *missingCrd
}

var logger = log.FromContext(context.Background())

func NewKartaHub(client client.Client, defaultGrouper grouper.Grouper) *KartaHub {
	isKartaCrdMissing := missingCrd{}

	return &KartaHub{
		client:            client,
		defaultGrouper:    defaultGrouper,
		isKartaCrdMissing: &isKartaCrdMissing,
	}
}

func (g *KartaHub) GetPodGrouperPlugin(gvk metav1.GroupVersionKind) grouper.Grouper {
	if g.client == nil || g.isKartaCrdMissing.load() {
		return nil
	}

	kartaGrouper, err := g.getKartaGrouperForGvk(context.Background(), gvk)
	if err == nil {
		return kartaGrouper
	}
	if apimeta.IsNoMatchError(err) {
		logger.Info("Karta CRD is not installed")
		g.isKartaCrdMissing.mark()
		return nil
	}

	logger.Error(err, "Failed to create Karta grouper for gvk", "gvk", gvk.String())
	return nil
}

func (g *KartaHub) getKartaGrouperForGvk(ctx context.Context, gvk metav1.GroupVersionKind) (*KartaGrouper, error) {
	karta, err := getKartaForGvk(ctx, g.client, gvk)
	if err != nil || karta == nil {
		return nil, err
	}

	kartaSummary, err := instructions.NewStructureSummary(karta)
	if err != nil {
		return nil, fmt.Errorf("failed to create StructureSummary from Karta %s: %w", karta.Name, err)
	}

	return &KartaGrouper{
		kartaSummary:   kartaSummary,
		defaultGrouper: g.defaultGrouper,
	}, nil
}

func getKartaForGvk(ctx context.Context, kubeClient client.Client, gvk metav1.GroupVersionKind) (*kartav1alpha1.Karta, error) {
	kartas := &kartav1alpha1.KartaList{}
	if err := kubeClient.List(ctx, kartas, getKartaPerGvkLabelSelectors(gvk)); err != nil {
		return nil, err
	}

	matchingKartas := []*kartav1alpha1.Karta{}
	for index := range kartas.Items {
		karta := &kartas.Items[index]
		ktGvk := getGvkOfKarta(karta)
		if ktGvk == nil || *ktGvk != gvk || karta.GetDeletionTimestamp() != nil {
			continue
		}
		if isEmptyKarta(karta) || !hasGangSchedulingInstructions(karta) {
			continue
		}
		if err := kartav1alpha1.NewKartaValidator(karta).Validate(); err != nil {
			logger.Error(err, "Invalid Karta, skipping generic GVK fallback", "karta", karta.GetName())
			continue
		}
		matchingKartas = append(matchingKartas, karta)
	}

	if len(matchingKartas) > 1 {
		return nil, fmt.Errorf("found multiple Kartas with gang scheduling instructions for gvk %s", gvk.String())
	}
	if len(matchingKartas) == 0 {
		return nil, nil
	}

	return matchingKartas[0], nil
}

func getGvkOfKarta(kt *kartav1alpha1.Karta) *metav1.GroupVersionKind {
	gvk := kt.Spec.StructureDefinition.RootComponent.Kind
	if gvk == nil {
		return nil
	}
	return &metav1.GroupVersionKind{
		Group:   gvk.Group,
		Version: gvk.Version,
		Kind:    gvk.Kind,
	}
}

func getKartaPerGvkLabelSelectors(gvk metav1.GroupVersionKind) *client.ListOptions {
	listOpts := &client.ListOptions{}
	client.MatchingLabels{
		KartaGroupLabel:   gvk.Group,
		KartaVersionLabel: gvk.Version,
		KartaKindLabel:    gvk.Kind,
	}.ApplyToList(listOpts)
	return listOpts
}

func isEmptyKarta(kt *kartav1alpha1.Karta) bool {
	return kt.Spec.StructureDefinition.RootComponent.Name == "" &&
		len(kt.Spec.StructureDefinition.ChildComponents) == 0
}

func hasGangSchedulingInstructions(kt *kartav1alpha1.Karta) bool {
	gangScheduling := kt.Spec.Instructions.GangScheduling
	return gangScheduling != nil &&
		(gangScheduling.PodGroup != nil || len(gangScheduling.PodGroups) > 0)
}

type missingCrd struct {
	retryAfterUnixNano atomic.Int64 // Unix timestamp in nanoseconds
}

func (b *missingCrd) load() bool {
	return time.Now().UnixNano() < b.retryAfterUnixNano.Load()
}

func (b *missingCrd) mark() {
	b.retryAfterUnixNano.Store(time.Now().Add(missingCrdRetryTTL).UnixNano())
}
