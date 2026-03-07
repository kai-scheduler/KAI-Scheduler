// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package deployable

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"

	"golang.org/x/exp/slices"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	kaiv1 "github.com/kai-scheduler/kai-scheduler/pkg/apis/kai/v1"
	"github.com/kai-scheduler/kai-scheduler/pkg/operator/operands"
	"github.com/kai-scheduler/kai-scheduler/pkg/operator/operands/known_types"
)

var (
	objectsCreationOrder = []string{"ServiceAccount"}
)

type DeployableOperands struct {
	operands               []operands.Operand
	collectables           []*known_types.Collectable
	dataFieldsInheritFuncs map[interface{}]known_types.FieldInherit
}

func New(operands []operands.Operand, collectables []*known_types.Collectable) *DeployableOperands {
	return &DeployableOperands{
		operands,
		collectables,
		map[interface{}]known_types.FieldInherit{},
	}
}
func (d *DeployableOperands) RegisterFieldsInheritFromClusterObjects(
	objectType interface{}, fieldInheritFunc known_types.FieldInherit) {
	d.dataFieldsInheritFuncs[objectType] = fieldInheritFunc
}

func (d *DeployableOperands) Deploy(
	ctx context.Context, runtimeClient client.Client, kaiConfig *kaiv1.Config, owningObject client.Object,
) error {
	desiredState, err := d.getDesiredState(ctx, runtimeClient, kaiConfig, owningObject)
	if err != nil {
		return err
	}

	currentState, err := d.getCurrentState(ctx, runtimeClient, owningObject)
	if err != nil {
		return err
	}

	objectsToCreate, objectsToDelete, objectsToUpdate := d.calculateActionsOnObjects(desiredState, currentState)

	reconcilerAsOwnerReference := v1.OwnerReference{
		APIVersion: owningObject.GetObjectKind().GroupVersionKind().GroupVersion().String(),
		Kind:       owningObject.GetObjectKind().GroupVersionKind().Kind,
		Name:       owningObject.GetName(),
		UID:        owningObject.GetUID(),
		Controller: ptr.To(true),
	}

	if err := createObjectsInCluster(ctx, runtimeClient, reconcilerAsOwnerReference, objectsToCreate); err != nil {
		return err
	}

	if err := deleteObjectsInCluster(ctx, runtimeClient, objectsToDelete); err != nil {
		return err
	}

	if err := updateObjectsInCluster(ctx, runtimeClient, reconcilerAsOwnerReference, objectsToUpdate); err != nil {
		return err
	}
	return nil
}

func (d *DeployableOperands) IsDeployed(ctx context.Context, readerClient client.Reader) (bool, error) {
	var deployErrors error
	for _, operand := range d.operands {
		available, err := operand.IsDeployed(ctx, readerClient)
		if err != nil {
			deployErrors = errors.Join(deployErrors, fmt.Errorf("%s: %v", operand.Name(), err))
		}
		if !available {
			deployErrors = errors.Join(deployErrors, fmt.Errorf("%s: not deployed", operand.Name()))
		}
	}

	if deployErrors != nil {
		return false, deployErrors
	}
	return true, nil
}

func (d *DeployableOperands) IsAvailable(ctx context.Context, readerClient client.Reader) (bool, error) {
	var availableErrors error
	for _, operand := range d.operands {
		available, err := operand.IsAvailable(ctx, readerClient)
		if err != nil {
			availableErrors = errors.Join(availableErrors, fmt.Errorf("%s: %v", operand.Name(), err))
		}
		if !available {
			availableErrors = errors.Join(availableErrors, fmt.Errorf("%s: not available", operand.Name()))
		}
	}

	if availableErrors != nil {
		return false, availableErrors
	}
	return true, nil
}

func (d *DeployableOperands) getDesiredState(
	ctx context.Context, readerClient client.Reader, config *kaiv1.Config, owningObject client.Object,
) (map[string]client.Object, error) {
	logger := log.FromContext(ctx)
	result := map[string]client.Object{}
	for _, operand := range d.operands {
		objs, err := operand.DesiredState(ctx, readerClient, config)
		if err != nil {
			return nil, err
		}
		for _, obj := range objs {
			logger.Info("Desired state object", "Operand", reflect.TypeOf(operand),
				"Type", obj.GetObjectKind(), "Namespace", obj.GetNamespace(), "Name", obj.GetName())
			result[known_types.GetKey(obj.GetObjectKind().GroupVersionKind(), obj.GetNamespace(), obj.GetName())] = obj
		}
	}
	return result, nil
}

func (d *DeployableOperands) getCurrentState(
	ctx context.Context, runtimeClient client.Client, owningObject client.Object,
) (map[string]client.Object, error) {
	result := map[string]client.Object{}
	for _, collectable := range d.collectables {
		collectableResult, err := collectable.Collect(ctx, runtimeClient, owningObject)
		if err != nil {
			return nil, err
		}
		for k, v := range collectableResult {
			result[k] = v
		}
	}
	return result, nil
}

func (d *DeployableOperands) calculateActionsOnObjects(
	desiredState map[string]client.Object, currentState map[string]client.Object) (
	objectsToCreate []client.Object, objectsToDelete []client.Object, objectsToUpdate map[string]client.Object) {
	objectsToCreate = []client.Object{}
	objectsToDelete = []client.Object{}
	objectsToUpdate = map[string]client.Object{}
	objectsNotToChange := map[string]client.Object{}

	for key, obj := range desiredState {
		if _, found := currentState[key]; found {
			d.inheritFieldsFromCurrent(currentState[key], obj)
			if reflect.DeepEqual(currentState[key], obj) {
				objectsNotToChange[key] = obj
			} else {
				objectsToUpdate[key] = obj
			}
		} else {
			objectsToCreate = append(objectsToCreate, obj)
		}
	}

	for key, obj := range currentState {
		if _, found := objectsNotToChange[key]; !found {
			if _, found := objectsToUpdate[key]; !found {
				if isUnmanaged(obj) {
					continue
				}
				objectsToDelete = append(objectsToDelete, obj)
			}
		}
	}
	return objectsToCreate, objectsToDelete, objectsToUpdate
}

func (d *DeployableOperands) inheritFieldsFromCurrent(currentObj client.Object, desiredObj client.Object) {
	for objectType, fieldInheritFunc := range d.dataFieldsInheritFuncs {
		if reflect.TypeOf(desiredObj) == reflect.TypeOf(objectType) {
			fieldInheritFunc(currentObj, desiredObj)
		}
	}
}

func updateObjectsInCluster(
	ctx context.Context, runtimeClient client.Client, reconcilerAsOwnerReference v1.OwnerReference,
	objectsToUpdate map[string]client.Object) error {
	var err error

	for _, obj := range objectsToUpdate {
		if !isUnmanaged(obj) {
			obj.SetOwnerReferences([]v1.OwnerReference{reconcilerAsOwnerReference})
		}
		if err = runtimeClient.Update(ctx, obj); err != nil {
			return fmt.Errorf("failed updating %s %s/%s: %+v", obj.GetObjectKind().GroupVersionKind(),
				obj.GetNamespace(), obj.GetName(), err)
		}
	}
	return nil
}

func deleteObjectsInCluster(
	ctx context.Context, runtimeClient client.Client, objectsToDelete []client.Object) error {
	var err error
	for _, obj := range objectsToDelete {
		if err = runtimeClient.Delete(ctx, obj); err != nil {
			return fmt.Errorf("failed deleting %s %s/%s: %+v", obj.GetObjectKind().GroupVersionKind(),
				obj.GetNamespace(), obj.GetName(), err)
		}
	}
	return err
}

func createObjectsInCluster(
	ctx context.Context, runtimeClient client.Client, reconcilerAsOwnerReference v1.OwnerReference,
	objectsToCreate []client.Object) error {

	sortObjectByCreationOrder(objectsToCreate, objectsCreationOrder)

	for _, obj := range objectsToCreate {
		if err := createObjectForKAIConfig(ctx, runtimeClient, reconcilerAsOwnerReference, obj); err != nil {
			return err
		}
	}
	return nil
}

func sortObjectByCreationOrder(objectsToCreate []client.Object, kindsOrder []string) {
	customCreationOrderSort := func(i, j int) bool {
		iKind := objectsToCreate[i].GetObjectKind().GroupVersionKind().Kind
		jKind := objectsToCreate[j].GetObjectKind().GroupVersionKind().Kind
		iOrderIndex := slices.Index(kindsOrder, iKind)
		jOrderIndex := slices.Index(kindsOrder, jKind)
		if iOrderIndex == -1 {
			return false
		}
		if jOrderIndex == -1 {
			return true
		}
		return iOrderIndex < jOrderIndex
	}
	sort.Slice(objectsToCreate, customCreationOrderSort)
}

func isUnmanaged(obj client.Object) bool {
	gvk := obj.GetObjectKind().GroupVersionKind()
	if gvk.Kind == "CustomResourceDefinition" &&
		gvk.Group == "apiextensions.k8s.io" &&
		obj.GetName() == "queues.scheduling.run.ai" {
		return true
	}
	return false
}

func createObjectForKAIConfig(
	ctx context.Context, runtimeClient client.Client, reconcilerAsOwnerReference v1.OwnerReference, obj client.Object,
) error {
	obj.SetOwnerReferences([]v1.OwnerReference{reconcilerAsOwnerReference})

	if err := runtimeClient.Create(ctx, obj); err != nil {
		logger := log.FromContext(ctx)
		logger.Info(
			"Failed to create object, trying to update to take ownership",
			"Kind", obj.GetObjectKind().GroupVersionKind(), "Name", obj.GetName(), "Error", err,
		)

		if updateErr := runtimeClient.Update(ctx, obj); updateErr != nil {
			logger.Error(
				updateErr, "failed taking ownership on object",
				"GroupVersionKind", obj.GetObjectKind().GroupVersionKind(),
				"Name", obj.GetName(), "Namespace", obj.GetNamespace())

			return fmt.Errorf("failed creating %s %s/%s: %+v", obj.GetObjectKind().GroupVersionKind(),
				obj.GetNamespace(), obj.GetName(), err)
		}

		logger.Info(
			"Took ownership on object", "GroupVersionKind",
			obj.GetObjectKind().GroupVersionKind(), "Name", obj.GetName())
	}

	return nil
}

func (d *DeployableOperands) Monitor(ctx context.Context, runtimeReader client.Reader, kaiConfig *kaiv1.Config) error {
	for _, operand := range d.operands {
		if err := operand.Monitor(ctx, runtimeReader, kaiConfig); err != nil {
			return fmt.Errorf("failed monitoring %s: %v", operand.Name(), err)
		}
	}
	return nil
}

func (d *DeployableOperands) HasMissingDependencies(ctx context.Context, readerClient client.Reader, object client.Object) (string, error) {
	var missingDependencies string
	var err error

	kaiConfig, checkForKAIConfig := object.(*kaiv1.Config)
	if !checkForKAIConfig {
		return "", nil
	}

	for _, operand := range d.operands {
		// Assuming each operand has a HasDependencies method
		missing, e := operand.HasMissingDependencies(ctx, readerClient, kaiConfig)
		if e != nil {
			err = errors.Join(err, e)
		}
		if len(missing) > 0 {
			missingDependencies = missingDependencies + fmt.Sprintf("\n%s is missing %s", operand.Name(), missing)
		}
	}

	return missingDependencies, err
}
