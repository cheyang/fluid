/*

Copyright 2020 The Fluid Authors.

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

package alluxio

import (
	"context"
	"fmt"
	"reflect"
	"time"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	"github.com/fluid-cloudnative/fluid/pkg/utils"
	"github.com/fluid-cloudnative/fluid/pkg/utils/tieredstore"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/util/retry"
)

// transformResourcesForMaster transforms the resource requirements for the Alluxio master and job master
// based on the provided AlluxioRuntime specification. It updates the resource configuration in the
// Alluxio value object if the corresponding resource limits or requests are specified in the runtime.
//
// Parameters:
//   - runtime: A pointer to the AlluxioRuntime object containing the resource specifications.
//   - value: A pointer to the Alluxio object where the transformed resource configurations will be stored.
func (e *AlluxioEngine) transformResourcesForMaster(runtime *datav1alpha1.AlluxioRuntime, value *Alluxio) {
	// If the runtime object is nil, return immediately as there is nothing to transform.
	if runtime == nil {
		return
	}

	// Check if resource limits or requests are specified for the Alluxio master.
	// If so, transform the resource requirements and update the Alluxio value object.
	if len(runtime.Spec.Master.Resources.Limits) > 0 || len(runtime.Spec.Master.Resources.Requests) > 0 {
		value.Master.Resources = utils.TransformRequirementsToResources(runtime.Spec.Master.Resources)
	}

	// Check if resource limits or requests are specified for the Alluxio job master.
	// If so, transform the resource requirements and update the Alluxio value object.
	if len(runtime.Spec.JobMaster.Resources.Limits) > 0 || len(runtime.Spec.JobMaster.Resources.Requests) > 0 {
		value.JobMaster.Resources = utils.TransformRequirementsToResources(runtime.Spec.JobMaster.Resources)
	}
}

// transformResourcesForWorker is responsible for transforming and setting resource limits for the Alluxio Worker component.
// This function updates the resource requirements for the Worker and JobWorker based on the runtime configuration
// and ensures that memory requests meet the required constraints.
//
// Parameters:
//   - runtime: *datav1alpha1.AlluxioRuntime, the runtime configuration of Alluxio, including resource definitions
//     for Worker and JobWorker.
//   - value: *Alluxio, the Alluxio runtime instance used to store the transformed resource information.
//
// Return value:
//   - error: Returns an error if any issue occurs during resource transformation; otherwise, returns nil.
func (e *AlluxioEngine) transformResourcesForWorker(runtime *datav1alpha1.AlluxioRuntime, value *Alluxio) error {

	//for worker
	value.Worker.Resources = utils.TransformRequirementsToResources(runtime.Spec.Worker.Resources)

	// for job worker
	if len(runtime.Spec.JobWorker.Resources.Limits) > 0 || len(runtime.Spec.JobWorker.Resources.Requests) > 0 {
		value.JobWorker.Resources = utils.TransformRequirementsToResources(runtime.Spec.JobWorker.Resources)
	}

	runtimeInfo, err := e.getRuntimeInfo()
	if err != nil {
		e.Log.Error(err, "failed to transformResourcesForWorker")
		return err
	}
	storageMap := tieredstore.GetLevelStorageMap(runtimeInfo)

	e.Log.Info("transformResourcesForWorker", "storageMap", storageMap)

	// mem set request
	needUpdated := false
	var needSetMem resource.Quantity

	for key, requirement := range storageMap {
		if key == common.MemoryCacheStore {
			req := requirement.DeepCopy()

			if runtime.Spec.Worker.Resources.Requests == nil ||
				runtime.Spec.Worker.Resources.Requests.Memory() == nil ||
				runtime.Spec.Worker.Resources.Requests.Memory().IsZero() ||
				req.Cmp(*runtime.Spec.Worker.Resources.Requests.Memory()) > 0 {
				needUpdated = true
				needSetMem.Add(req)
			}

			if !runtime.Spec.Worker.Resources.Limits.Memory().IsZero() &&
				req.Cmp(*runtime.Spec.Worker.Resources.Limits.Memory()) > 0 {
				err = fmt.Errorf("the memory tierdStore's size %v is greater than worker limits memory %v", req, runtime.Spec.Worker.Resources.Limits.Memory())
				e.Log.Error(err, "the memory tierdStore's size is is greater than worker limits memory")
				return err
			}

		}

	}

	if needUpdated {
		if value.Worker.Resources.Requests == nil {
			value.Worker.Resources.Requests = make(common.ResourceList)
		}
		value.Worker.Resources.Requests[corev1.ResourceMemory] = needSetMem.String()
		err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			runtime, err := e.getRuntime()
			if err != nil {
				return err
			}
			runtimeToUpdate := runtime.DeepCopy()
			if len(runtimeToUpdate.Spec.Worker.Resources.Requests) == 0 {
				runtimeToUpdate.Spec.Worker.Resources.Requests = make(corev1.ResourceList)
			}
			runtimeToUpdate.Spec.Worker.Resources.Requests[corev1.ResourceMemory] = needSetMem
			if !reflect.DeepEqual(runtimeToUpdate, runtime) {
				err = e.Client.Update(context.TODO(), runtimeToUpdate)
				if err != nil {
					if apierrors.IsConflict(err) {
						time.Sleep(3 * time.Second)
					}
					return err
				}
				time.Sleep(1 * time.Second)
			}

			return nil
		})

		if err != nil {
			return err
		}
	}

	return nil
}

// transformResourcesForFuse adjusts the FUSE container resource requirements
// based on the memory limits specified in the AlluxioRuntime spec and the tiered store configuration.
//
// Logic:
// 1. Skip if no resource limits or memory limit is defined in the runtime spec.
// 2. Convert the resource requirements from runtime to internal representation.
// 3. Retrieve runtime info and get tiered storage requirements.
// 4. If MemoryCacheStore is configured, add its requirement to the memory limit.
// 5. Update the final memory limit in the FUSE resource configuration.
//
// Note: disk cache resources are not handled yet (commented out).
// TODO(iluoeli): Final memory limit should consider both heap (Xmx) and direct memory.
func (e *AlluxioEngine) transformResourcesForFuse(runtime *datav1alpha1.AlluxioRuntime, value *Alluxio) {

	if runtime.Spec.Fuse.Resources.Limits == nil {
		e.Log.Info("skip setting memory limit")
		return
	}

	if _, found := runtime.Spec.Fuse.Resources.Limits[corev1.ResourceMemory]; !found {
		e.Log.Info("skip setting memory limit")
		return
	}

	value.Fuse.Resources = utils.TransformRequirementsToResources(runtime.Spec.Fuse.Resources)

	runtimeInfo, err := e.getRuntimeInfo()
	if err != nil {
		e.Log.Error(err, "failed to transformResourcesForFuse")
	}
	storageMap := tieredstore.GetLevelStorageMap(runtimeInfo)

	e.Log.Info("transformFuse", "storageMap", storageMap)

	// TODO(iluoeli): it should be xmx + direct memory
	memLimit := resource.MustParse("50Gi")
	if quantity, exists := runtime.Spec.Fuse.Resources.Limits[corev1.ResourceMemory]; exists && !quantity.IsZero() {
		memLimit = quantity
	}

	for key, requirement := range storageMap {
		if value.Fuse.Resources.Limits == nil {
			value.Fuse.Resources.Limits = make(common.ResourceList)
		}
		if key == common.MemoryCacheStore {
			req := requirement.DeepCopy()

			memLimit.Add(req)

			e.Log.Info("update the requiremnet for memory", "requirement", memLimit)

		}
		// } else if key == common.DiskCacheStore {
		// 	req := requirement.DeepCopy()
		// 	e.Log.Info("update the requiremnet for disk", "requirement", req)
		// 	value.Fuse.Resources.Limits[corev1.ResourceEphemeralStorage] = req.String()
		// }
	}
	if value.Fuse.Resources.Limits != nil {
		value.Fuse.Resources.Limits[corev1.ResourceMemory] = memLimit.String()
	}

}

// transformTolerations updates the Alluxio configuration with tolerations specified in the dataset.
// It iterates over each toleration in dataset.Spec.Tolerations, clears the TolerationSeconds field,
// and appends the modified toleration to value.Tolerations. This ensures that only the necessary toleration
// settings (without expiration) are transferred to the Alluxio value.
func (e *AlluxioEngine) transformTolerations(dataset *datav1alpha1.Dataset, value *Alluxio) {
	if len(dataset.Spec.Tolerations) > 0 {
		// value.Tolerations = dataset.Spec.Tolerations
		value.Tolerations = []corev1.Toleration{}
		for _, toleration := range dataset.Spec.Tolerations {
			toleration.TolerationSeconds = nil
			value.Tolerations = append(value.Tolerations, toleration)
		}
	}
}
