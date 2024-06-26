/*
Copyright 2024 The Fluid Authors.
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

package vineyard

const (
	wokrerPodRole = "vineyard-worker"

	MasterPeerName = "peer"

	MasterPeerPort = 2380

	MasterClientName = "client"

	MasterClientPort = 2379

	WorkerRPCName = "rpc"

	WorkerRPCPort = 9600

	WorkerExporterName = "exporter"

	WorkerExporterPort = 9144

	// whether to reserve memory for vineyardd
	WorkerReserveMemory = "vineyardd.reserve.memory"

	DefaultWorkerReserveMemoryValue = "true"

	// the prefix of etcd key for vineyard objects
	WorkerEtcdPrefix = "etcd.prefix"

	DefaultWorkerEtcdPrefixValue = "/vineyard"

	// the size of vineyardd in vineyard-fuse
	VineyarddSize = "size"

	// the etcd endpoint of vineyardd in vineyard-fuse
	EtcdEndpoint = "etcd_endpoint"

	// the etcd prefix of vineyardd in vineyard-fuse
	EtcdPrefix = "etcd_prefix"

	DefaultSize = "0"

	DefaultEtcdPrefix = "/vineyard"
)
