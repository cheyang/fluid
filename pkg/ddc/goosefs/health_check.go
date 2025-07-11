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

package goosefs

import (
	"context"
	"fmt"
	"reflect"

	"github.com/fluid-cloudnative/fluid/pkg/common"
	"github.com/fluid-cloudnative/fluid/pkg/ctrl"
	fluiderrs "github.com/fluid-cloudnative/fluid/pkg/errors"
	"k8s.io/apimachinery/pkg/types"

	data "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/utils"
	"github.com/fluid-cloudnative/fluid/pkg/utils/kubeclient"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/util/retry"
)

// CheckRuntimeHealthy checks the healthy of the runtime
func (e *GooseFSEngine) CheckRuntimeHealthy() (err error) {

	// 1. Check the healthy of the master
	err = e.checkMasterHealthy()
	if err != nil {
		e.Log.Error(err, "The master is not healthy")
		updateErr := e.UpdateDatasetStatus(data.FailedDatasetPhase)
		if updateErr != nil {
			e.Log.Error(updateErr, "Failed to update dataset")
		}
		return
	}

	// 2. Check the healthy of the workers
	err = e.checkWorkersHealthy()
	if err != nil {
		e.Log.Error(err, "The workers are not healthy")
		updateErr := e.UpdateDatasetStatus(data.FailedDatasetPhase)
		if updateErr != nil {
			e.Log.Error(updateErr, "Failed to update dataset")
		}
		return
	}

	// 3. Check the healthy of the fuse
	err = e.checkFuseHealthy()
	if err != nil {
		e.Log.Error(err, "The fuse is not healthy")
		updateErr := e.UpdateDatasetStatus(data.FailedDatasetPhase)
		if updateErr != nil {
			e.Log.Error(updateErr, "Failed to update dataset")
		}
		return
	}

	// 4. If the status is fine, update dataset's status
	updateErr := e.UpdateDatasetStatus(data.BoundDatasetPhase)
	if updateErr != nil {
		e.Log.Error(updateErr, "Failed to update dataset")
	}

	return
}

// checkMasterHealthy checks whether the GooseFS master StatefulSet is healthy.
// It compares the desired and ready replica counts of the master StatefulSet.
// If the master is not healthy, it updates the runtime status to NotReady with the appropriate condition.
// If the master is healthy, it updates the status to Ready.
// The function uses a retry mechanism to handle conflicts when updating the runtime status.
//
// Parameters:
//   - none (uses fields from the GooseFSEngine receiver)
//
// Returns:
//   - error: returns an error if the master is not healthy or if the runtime status update fails.

func (e *GooseFSEngine) checkMasterHealthy() (err error) {
	masterName := e.getMasterName()

	healthy := false
	master, err := kubeclient.GetStatefulSet(e.Client, masterName, e.namespace)
	if err != nil {
		return err
	}

	err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		runtime, err := e.getRuntime()
		if err != nil {
			return err
		}

		runtimeToUpdate := runtime.DeepCopy()
		if master.Status.Replicas != master.Status.ReadyReplicas {
			if len(runtimeToUpdate.Status.Conditions) == 0 {
				runtimeToUpdate.Status.Conditions = []data.RuntimeCondition{}
			}
			cond := utils.NewRuntimeCondition(data.RuntimeMasterReady, "The master is not ready.",
				fmt.Sprintf("The master %s in %s is not ready.", master.Name, master.Namespace), corev1.ConditionFalse)
			_, oldCond := utils.GetRuntimeCondition(runtimeToUpdate.Status.Conditions, cond.Type)

			if oldCond == nil || oldCond.Type != cond.Type {
				runtimeToUpdate.Status.Conditions =
					utils.UpdateRuntimeCondition(runtimeToUpdate.Status.Conditions,
						cond)
			}
			runtimeToUpdate.Status.MasterPhase = data.RuntimePhaseNotReady

			return err
		} else {
			cond := utils.NewRuntimeCondition(data.RuntimeMasterReady, "The master is ready.",
				"The master is ready.", corev1.ConditionTrue)
			_, oldCond := utils.GetRuntimeCondition(runtimeToUpdate.Status.Conditions, cond.Type)

			if oldCond == nil || oldCond.Type != cond.Type {
				runtimeToUpdate.Status.Conditions =
					utils.UpdateRuntimeCondition(runtimeToUpdate.Status.Conditions,
						cond)
			}
			runtimeToUpdate.Status.MasterPhase = data.RuntimePhaseReady
			healthy = true
		}

		if !reflect.DeepEqual(runtime.Status, runtimeToUpdate.Status) {
			err = e.Client.Status().Update(context.TODO(), runtimeToUpdate)
			if err != nil {
				e.Log.Error(err, "Failed to update the runtime")
				return err
			}
		}

		return nil
	})

	if err != nil {
		e.Log.Error(err, "Failed update runtime")
		return err
	}

	if !healthy {
		err = fmt.Errorf("the master %s in %s is not ready. The expected number is %d, the actual number is %d",
			master.Name,
			master.Namespace,
			master.Status.Replicas,
			master.Status.ReadyReplicas)
	}

	return err

}

// checkWorkersHealthy checks the health status of workers in a GooseFSEngine runtime.
// It retrieves the worker statefulset and evaluates whether the workers are ready based on their status.
// If the workers are not ready, it updates the runtime's status with appropriate conditions and logs relevant information.
// If the runtime was created by a controller before v0.7.0, it logs a warning indicating that worker health checking is not supported for such runtimes.
func (e *GooseFSEngine) checkWorkersHealthy() (err error) {
	// Check the status of workers
	workers, err := ctrl.GetWorkersAsStatefulset(e.Client,
		types.NamespacedName{Namespace: e.namespace, Name: e.getWorkerName()})
	if err != nil {
		if fluiderrs.IsDeprecated(err) {
			e.Log.Info("Warning: the current runtime is created by runtime controller before v0.7.0, checking worker health state is not supported. To support these features, please create a new dataset", "details", err)
			e.Recorder.Event(e.runtime, corev1.EventTypeWarning, common.RuntimeDeprecated, "The runtime is created by controllers before v0.7.0, to fully enable latest capabilities, please delete the runtime and create a new one")
			return nil
		}
		return
	}

	healthy := false
	err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {

		runtime, err := e.getRuntime()
		if err != nil {
			return err
		}

		runtimeToUpdate := runtime.DeepCopy()

		if workers.Status.ReadyReplicas == 0 && *workers.Spec.Replicas > 0 {
			// if workers.Status.NumberReady != workers.Status.DesiredNumberScheduled {
			if len(runtimeToUpdate.Status.Conditions) == 0 {
				runtimeToUpdate.Status.Conditions = []data.RuntimeCondition{}
			}
			cond := utils.NewRuntimeCondition(data.RuntimeWorkersReady, "The workers are not ready.",
				fmt.Sprintf("The statefulset %s in %s are not ready, the Unavailable number is %d, please fix it.",
					workers.Name,
					workers.Namespace,
					*workers.Spec.Replicas-workers.Status.ReadyReplicas), corev1.ConditionFalse)

			_, oldCond := utils.GetRuntimeCondition(runtimeToUpdate.Status.Conditions, cond.Type)

			if oldCond == nil || oldCond.Type != cond.Type {
				runtimeToUpdate.Status.Conditions =
					utils.UpdateRuntimeCondition(runtimeToUpdate.Status.Conditions,
						cond)
			}

			runtimeToUpdate.Status.WorkerPhase = data.RuntimePhaseNotReady

			// runtimeToUpdate.Status.DesiredWorkerNumberScheduled
			// runtimeToUpdate.Status.WorkerPhase = data.RuntimePhaseNotReady

			e.Log.Error(err, "the workers are not ready")
		} else {
			healthy = true
			cond := utils.NewRuntimeCondition(data.RuntimeWorkersReady, "The workers are ready.",
				"The workers are ready", corev1.ConditionTrue)

			_, oldCond := utils.GetRuntimeCondition(runtimeToUpdate.Status.Conditions, cond.Type)

			if oldCond == nil || oldCond.Type != cond.Type {
				runtimeToUpdate.Status.Conditions =
					utils.UpdateRuntimeCondition(runtimeToUpdate.Status.Conditions,
						cond)
			}
			// runtimeToUpdate.Status.WorkerPhase = data.RuntimePhaseReady
		}
		// runtimeToUpdate.Status.DesiredWorkerNumberScheduled = int32(workers.Status.DesiredNumberScheduled)
		runtimeToUpdate.Status.WorkerNumberReady = int32(workers.Status.ReadyReplicas)
		runtimeToUpdate.Status.WorkerNumberAvailable = int32(workers.Status.CurrentReplicas)
		if !reflect.DeepEqual(runtime.Status, runtimeToUpdate.Status) {
			updateErr := e.Client.Status().Update(context.TODO(), runtimeToUpdate)
			if updateErr != nil {
				e.Log.Error(updateErr, "Failed to update the runtime")
				return updateErr
			}
		}

		return err
	})

	if err != nil {
		e.Log.Error(err, "Failed update runtime")
		return err
	}

	if !healthy {
		err = fmt.Errorf("the workers %s in %s are not ready, the unhealthy number %d",
			workers.Name,
			workers.Namespace,
			*workers.Spec.Replicas-workers.Status.ReadyReplicas)
	}

	return err
}

// checkFuseHealthy check fuses number changed
func (e *GooseFSEngine) checkFuseHealthy() error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() (err error) {
		runtime, err := e.getRuntime()
		if err != nil {
			e.Log.Error(err, "Failed to get Runtime", "runtimeName", e.name, "runtimeNamespace", e.namespace)
			return
		}
		err = e.Helper.CheckFuseHealthy(e.Recorder, runtime.DeepCopy(), e.getFuseName())
		if err != nil {
			e.Log.Error(err, "Failed to check runtimeFuse healthy")
		}
		return
	})
}
