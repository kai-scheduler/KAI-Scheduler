// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package helmhooks

import (
	"context"
	"fmt"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	kaiv1alpha1 "github.com/kai-scheduler/KAI-scheduler/pkg/apis/kai/v1alpha1"
)

const (
	kueueGroup             = "kueue.x-k8s.io"
	kueueTopologyCRDName   = "topologies." + kueueGroup
	kueueTopologyListKind  = "TopologyList"
	migratedFromAnnotation = "kai.scheduler/migrated-from"
)

// MigrateTopologies copies Kueue Topologies into KAI Topologies with the same name and node
// labels. Existing KAI Topologies are left untouched, so the migration is idempotent.
func MigrateTopologies(ctx context.Context, c client.Client) error {
	logger := logf.FromContext(ctx)

	crd := &apiextensionsv1.CustomResourceDefinition{}
	if err := c.Get(ctx, client.ObjectKey{Name: kueueTopologyCRDName}, crd); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("Kueue Topology CRD not found, skipping migration")
			return nil
		}
		return fmt.Errorf("failed to get CRD %s: %w", kueueTopologyCRDName, err)
	}

	version, err := storageVersion(crd)
	if err != nil {
		return err
	}

	kueueTopologies := &unstructured.UnstructuredList{}
	kueueTopologies.SetGroupVersionKind(schema.GroupVersionKind{
		Group: kueueGroup, Version: version, Kind: kueueTopologyListKind,
	})
	if err := c.List(ctx, kueueTopologies); err != nil {
		return fmt.Errorf("failed to list Kueue topologies: %w", err)
	}
	if len(kueueTopologies.Items) == 0 {
		logger.Info("No Kueue Topologies found to migrate")
		return nil
	}

	for i := range kueueTopologies.Items {
		kueueTopology := &kueueTopologies.Items[i]
		name := kueueTopology.GetName()

		err := c.Get(ctx, client.ObjectKey{Name: name}, &kaiv1alpha1.Topology{})
		if err == nil {
			logger.Info("KAI Topology already exists, skipping", "name", name)
			continue
		}
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to get KAI Topology %s: %w", name, err)
		}

		kaiTopology, err := kaiTopologyFromKueue(kueueTopology)
		if err != nil {
			return err
		}
		if err := c.Create(ctx, kaiTopology); err != nil {
			if apierrors.IsAlreadyExists(err) {
				logger.Info("KAI Topology already exists, skipping", "name", name)
				continue
			}
			return fmt.Errorf("failed to create KAI Topology %s: %w", name, err)
		}
		logger.Info("Migrated topology", "name", name)
	}
	return nil
}

func storageVersion(crd *apiextensionsv1.CustomResourceDefinition) (string, error) {
	for _, version := range crd.Spec.Versions {
		if version.Storage {
			return version.Name, nil
		}
	}
	return "", fmt.Errorf("CRD %s has no storage version", crd.Name)
}

func kaiTopologyFromKueue(kueueTopology *unstructured.Unstructured) (*kaiv1alpha1.Topology, error) {
	name := kueueTopology.GetName()
	levels, found, err := unstructured.NestedSlice(kueueTopology.Object, "spec", "levels")
	if err != nil {
		return nil, fmt.Errorf("failed to read spec.levels of Kueue Topology %s: %w", name, err)
	}
	if !found {
		return nil, fmt.Errorf("kueue Topology %s has no spec.levels", name)
	}

	kaiTopology := &kaiv1alpha1.Topology{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Annotations: map[string]string{migratedFromAnnotation: kueueGroup},
		},
	}
	for _, level := range levels {
		levelMap, ok := level.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("kueue Topology %s has a malformed level: %v", name, level)
		}
		nodeLabel, _, _ := unstructured.NestedString(levelMap, "nodeLabel")
		if nodeLabel == "" {
			return nil, fmt.Errorf("kueue Topology %s has a level without nodeLabel", name)
		}
		kaiTopology.Spec.Levels = append(kaiTopology.Spec.Levels, kaiv1alpha1.TopologyLevel{NodeLabel: nodeLabel})
	}
	return kaiTopology, nil
}
