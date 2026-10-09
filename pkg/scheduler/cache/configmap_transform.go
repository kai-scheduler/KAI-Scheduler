// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
)

func setConfigMapTransform(informer cache.SharedIndexInformer) error {
	return informer.SetTransform(compactConfigMap)
}

func compactConfigMap(obj any) (any, error) {
	configMap, ok := obj.(*v1.ConfigMap)
	if !ok {
		return obj, nil
	}

	return &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:            configMap.Name,
			Namespace:       configMap.Namespace,
			UID:             configMap.UID,
			ResourceVersion: configMap.ResourceVersion,
		},
	}, nil
}
