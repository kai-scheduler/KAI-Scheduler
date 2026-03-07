// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package childqueues_updater

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v2 "github.com/kai-scheduler/kai-scheduler/pkg/apis/scheduling/v2"
)

func TestUpdateQueueChildren(t *testing.T) {
	objects := []client.Object{
		&v2.Queue{
			ObjectMeta: v1.ObjectMeta{
				Name: "parent",
			},
		},
		&v2.Queue{
			ObjectMeta: v1.ObjectMeta{
				Name: "child1",
			},
			Spec: v2.QueueSpec{
				ParentQueue: "parent",
			},
		},
		&v2.Queue{
			ObjectMeta: v1.ObjectMeta{
				Name: "child2",
			},
			Spec: v2.QueueSpec{
				ParentQueue: "parent",
			},
		},
		&v2.Queue{
			ObjectMeta: v1.ObjectMeta{
				Name: "not-a-child",
			},
		},
	}

	scheme := runtime.NewScheme()
	err := v2.AddToScheme(scheme)
	assert.Nil(t, err)

	updater := ChildQueuesUpdater{
		Client: fake.NewClientBuilder().
			WithScheme(scheme).
			WithIndex(&v2.Queue{}, ".spec.parentQueue", func(object client.Object) []string {
				queue := object.(*v2.Queue)
				if queue.Spec.ParentQueue == "" {
					return []string{}
				}
				return []string{queue.Spec.ParentQueue}
			}).
			WithObjects(objects...).Build(),
	}

	parent := &v2.Queue{
		ObjectMeta: v1.ObjectMeta{
			Name: "parent",
		},
	}

	err = updater.UpdateQueue(context.Background(), parent)
	assert.Nil(t, err)

	assert.Len(t, parent.Status.ChildQueues, 2)
	assert.ElementsMatch(t, parent.Status.ChildQueues, []string{"child1", "child2"})
}
