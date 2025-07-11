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
	"fmt"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/ddc/goosefs/operations"
	"github.com/fluid-cloudnative/fluid/pkg/utils/kubeclient"
)

// GetHCFSStatus retrieves the status information of HCFS (Hadoop Compatible File System)
// This function queries the HCFS endpoint and the underlying filesystem compatible version
//
// Process flow:
//  1. First calls queryHCFSEndpoint() to get the HCFS access endpoint
//  2. If endpoint retrieval fails, logs the error and returns the error
//  3. Calls queryCompatibleUFSVersion() to get the underlying filesystem compatible version
//  4. If version retrieval fails, logs the error and returns the error
//  5. Constructs the HCFSStatus struct and populates it with the retrieved information
//  6. Returns the status information and nil error
//
// Returns:
//   - *datav1alpha1.HCFSStatus: Pointer to a struct containing HCFS status information,
//     including endpoint and version details
//   - error: Returns an error if any issues occur during the query process
//
// Query the hcfs status
func (e *GooseFSEngine) GetHCFSStatus() (status *datav1alpha1.HCFSStatus, err error) {
	endpoint, err := e.queryHCFSEndpoint()
	if err != nil {
		e.Log.Error(err, "Failed to get HCFS Endpoint")
		return status, err
	}

	version, err := e.queryCompatibleUFSVersion()
	if err != nil {
		e.Log.Error(err, "Failed to get Compatible Endpoint")
		return status, err
	}

	status = &datav1alpha1.HCFSStatus{
		Endpoint:                    endpoint,
		UnderlayerFileSystemVersion: version,
	}
	return
}

// query the hcfs endpoint
func (e *GooseFSEngine) queryHCFSEndpoint() (endpoint string, err error) {

	var (
		serviceName = fmt.Sprintf("%s-master-0", e.name)
		host        = fmt.Sprintf("%s.%s", serviceName, e.namespace)
	)

	svc, err := kubeclient.GetServiceByName(e.Client, serviceName, e.namespace)
	if err != nil {
		e.Log.Error(err, "Failed to get Endpoint")
		return endpoint, err
	}

	if svc == nil {
		e.Log.Error(fmt.Errorf("failed to find the svc %s in %s", e.name, e.namespace), "failed to find the svc, it's nil")
		return
	}

	for _, port := range svc.Spec.Ports {
		if port.Name == "rpc" {
			endpoint = fmt.Sprintf("goosefs://%s:%d", host, port.Port)
			return
		}
	}

	return
}

// query the compatible version of UFS
func (e *GooseFSEngine) queryCompatibleUFSVersion() (version string, err error) {
	podName, containerName := e.getMasterPodInfo()
	fileUtils := operations.NewGooseFSFileUtils(podName, containerName, e.namespace, e.Log)
	version, err = fileUtils.GetConf("goosefs.underfs.version")
	if err != nil {
		e.Log.Error(err, "Failed to getConf")
		return
	}
	return
}
