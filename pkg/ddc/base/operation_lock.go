/*
Copyright 2022 The Fluid Authors.

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

package base

import (
	"context"
	"fmt"
	"reflect"

	"github.com/fluid-cloudnative/fluid/pkg/common"
	"github.com/fluid-cloudnative/fluid/pkg/dataoperation"
	cruntime "github.com/fluid-cloudnative/fluid/pkg/runtime"
	"github.com/fluid-cloudnative/fluid/pkg/utils"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func GetDataOperationRef(name, namespace string) string {
	// namespace may contain '-', use '/' as separator
	return fmt.Sprintf("%s/%s", namespace, name)
}

// LockTargetDataset locks the target dataset if it is not already locked by this data operation.
func LockTargetDataset(ctx cruntime.ReconcileRequestContext, object client.Object, operation dataoperation.OperationInterface, engine Engine) error {
	targetDataset := ctx.Dataset
	operationTypeName := string(operation.GetOperationType())

	// 1. Check if there's any conflict
	conflictingDataOpKey := targetDataset.GetLockedNameForOperation(operationTypeName)

	dataOpKey := types.NamespacedName{
		Namespace: object.GetNamespace(),
		Name:      object.GetName(),
	}

	// If the target dataset is already locked by this operation, return nil
	if dataOpKey.String() == conflictingDataOpKey {
		return nil
	}

	// If the target dataset is already locked by a different operation, return an error and requeue
	if len(conflictingDataOpKey) != 0 && conflictingDataOpKey != dataOpKey.String() {
		ctx.Log.Info(fmt.Sprintf("Found another %s operation that is in the 'Executing' phase, will back off", operationTypeName), "other", conflictingDataOpRef)
		ctx.Recorder.Eventf(object, v1.EventTypeNormal, common.DataOperationCollision,
			"Found another %s operation (%s) that is in the 'Executing' phase, will back off",
			operationTypeName, conflictingDataOpKey)
		return fmt.Errorf("found another %s operation that is in the 'Executing' phase, will back off", operationTypeName)
	}

	// 2. Check if the bounded runtime is ready
	if !engine.CheckRuntimeReady() {
		ctx.Log.V(1).Info("Bounded accelerate runtime not ready", "targetDataset", targetDataset)
		ctx.Recorder.Eventf(object, v1.EventTypeNormal, common.RuntimeNotReady, "Bounded accelerate runtime not ready")
		return fmt.Errorf("bounded accelerate runtime not ready")
	}

	ctx.Log.Info("No conflicts detected, attempting to lock the target dataset")

	// 3. Try to lock the target dataset
	datasetToUpdate := targetDataset.DeepCopy()
	datasetToUpdate.LockOperation(operationTypeName, dataOpKey.String())
	operation.LockTargetDatasetStatus(datasetToUpdate)

	if !reflect.DeepEqual(targetDataset.Status, datasetToUpdate.Status) {
		if err := ctx.Client.Status().Update(context.TODO(), datasetToUpdate); err != nil {
			ctx.Log.Info("Failed to update target dataset's lock, will requeue", "targetDatasetName", targetDataset.Name)
			return err
		}
	}

	return nil
}

// ReleaseTargetDataset release target dataset if locked by this data operation.
func ReleaseTargetDataset(ctx cruntime.ReconcileRequestContext, object client.Object,
	operation dataoperation.OperationInterface) error {
	// Note: ctx.Dataset may be nil, so use the `GetTargetDatasetNamespacedName`
	targetDatasetNamespacedName, err := operation.GetTargetDatasetNamespacedName(object)
	if err != nil {
		return err
	}

	operationTypeName := string(operation.GetOperationType())

	err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		dataset, err := utils.GetDataset(ctx.Client, targetDatasetNamespacedName.Name, targetDatasetNamespacedName.Namespace)
		if err != nil {
			if utils.IgnoreNotFound(err) == nil {
				ctx.Log.Info("can't find target dataset, won't release lock", "targetDataset", targetDatasetNamespacedName.Name)
				return nil
			}
			// other error
			return err
		}
		currentRef := dataset.GetLockedNameForOperation(operationTypeName)

		if currentRef != GetDataOperationRef(object.GetName(), object.GetNamespace()) {
			ctx.Log.Info("Found Ref inconsistent with the reconciling DataBack, won't release this lock, ignore it", "Operation", operationTypeName, "ref", currentRef)
			return nil
		}
		datasetToUpdate := dataset.DeepCopy()
		datasetToUpdate.ReleaseOperation(operationTypeName)

		// different operation may set other fields
		operation.ReleaseTargetDatasetStatus(datasetToUpdate)
		if !reflect.DeepEqual(datasetToUpdate.Status, dataset) {
			if err := ctx.Client.Status().Update(context.TODO(), datasetToUpdate); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		ctx.Log.Error(err, "can't release lock on target dataset", "targetDataset", targetDatasetNamespacedName)
	}
	return err
}
