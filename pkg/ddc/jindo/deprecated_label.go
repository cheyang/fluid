/*
Copyright 2023 The Fluid Authors.

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

package jindo

import (
	"github.com/fluid-cloudnative/fluid/pkg/utils"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
)

func (e *JindoEngine) HasDeprecatedCommonLabelName() (deprecated bool, err error) {
	// return deprecated.LabelAnnotationStorageCapacityPrefix + e.namespace + "-" + e.name

	var (
		workerName string = e.getWorkerName()
		namespace  string = e.namespace
	)

	// runtime, err := e.getRuntime()
	// if err != nil {
	// 	return
	// }

	workers, err := e.getDaemonset(workerName, namespace)
	if err != nil {
		if apierrs.IsNotFound(err) {
			e.Log.Info("Workers with deprecated label not found")
			deprecated = false
			err = nil
			return
		}
		e.Log.Error(err, "Failed to get worker", "workerName", workerName)
		return deprecated, err
	}

	nodeSelectors := workers.Spec.Template.Spec.NodeSelector
	e.Log.Info("The current node selectors for worker", "workerName", workerName, "nodeSelector", nodeSelectors)

	deprecatedCommonLabelName := utils.GetCommonLabelName(true, e.namespace, e.name, e.runtimeInfo.GetOwnerDatasetUID())
	if _, deprecated = nodeSelectors[deprecatedCommonLabelName]; deprecated {
		//
		e.Log.Info("the deprecated node selector exists", "nodeSelector", deprecatedCommonLabelName)
	} else {
		e.Log.Info("The deprecated node selector doesn't exist", "nodeSelector", deprecatedCommonLabelName)
	}

	return
}
