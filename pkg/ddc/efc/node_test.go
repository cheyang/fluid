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

package efc

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"

	"github.com/fluid-cloudnative/fluid/pkg/ddc/base"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ptr "k8s.io/utils/ptr"
)

func getTestEFCEngineNode(client client.Client, name string, namespace string, withRunTime bool) *EFCEngine {
	engine := &EFCEngine{
		runtime:     nil,
		name:        name,
		namespace:   namespace,
		Client:      client,
		runtimeInfo: nil,
		Log:         fake.NullLogger(),
	}
	if withRunTime {
		engine.runtime = &datav1alpha1.EFCRuntime{}
		engine.runtimeInfo, _ = base.BuildRuntimeInfo(name, namespace, common.EFCRuntime, datav1alpha1.TieredStore{})
	}
	return engine
}

func TestSyncScheduleInfoToCacheNodes(t *testing.T) {
	type fields struct {
		// runtime   *datav1alpha1.EFCRuntime
		worker    *appsv1.StatefulSet
		pods      []*v1.Pod
		ds        *appsv1.DaemonSet
		nodes     []*v1.Node
		name      string
		namespace string
	}
	testcases := []struct {
		name      string
		fields    fields
		nodeNames []string
	}{
		{
			name: "create",
			fields: fields{
				name:      "spark",
				namespace: "big-data",
				worker: &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "spark-worker",
						Namespace: "big-data",
						UID:       "uid1",
					},
					Spec: appsv1.StatefulSetSpec{
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"app":              "efc",
								"role":             "efc-worker",
								"fluid.io/dataset": "big-data-spark",
							},
						},
					},
				},
				pods: []*v1.Pod{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "spark-worker-0",
							Namespace: "big-data",
							OwnerReferences: []metav1.OwnerReference{{
								Kind:       "StatefulSet",
								APIVersion: "apps/v1",
								Name:       "spark-worker",
								UID:        "uid1",
								Controller: ptr.To(true),
							}},
							Labels: map[string]string{
								"app":              "efc",
								"role":             "efc-worker",
								"fluid.io/dataset": "big-data-spark",
							},
						},
						Spec: v1.PodSpec{
							NodeName: "node1",
						},
						Status: v1.PodStatus{
							Phase: v1.PodRunning,
							Conditions: []v1.PodCondition{
								{
									Type:   v1.PodReady,
									Status: v1.ConditionTrue,
								},
							},
						},
					},
				},
				nodes: []*v1.Node{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "node1",
						},
					},
				},
			},
			nodeNames: []string{"node1"},
		},
		{
			name: "add",
			fields: fields{
				name:      "hbase",
				namespace: "big-data",
				worker: &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "hbase-worker",
						Namespace: "big-data",
						UID:       "uid2",
					},
					Spec: appsv1.StatefulSetSpec{
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"app":              "efc",
								"role":             "efc-worker",
								"fluid.io/dataset": "big-data-hbase",
							},
						},
					},
				},
				pods: []*v1.Pod{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "hbase-worker-0",
							Namespace: "big-data",
							OwnerReferences: []metav1.OwnerReference{{
								Kind:       "StatefulSet",
								APIVersion: "apps/v1",
								Name:       "hbase-worker",
								UID:        "uid2",
								Controller: ptr.To(true),
							}},
							Labels: map[string]string{
								"app":              "efc",
								"role":             "efc-worker",
								"fluid.io/dataset": "big-data-hbase",
							},
						},
						Spec: v1.PodSpec{
							NodeName: "node3",
						},
						Status: v1.PodStatus{
							Phase: v1.PodRunning,
							Conditions: []v1.PodCondition{
								{
									Type:   v1.PodReady,
									Status: v1.ConditionTrue,
								},
							},
						},
					},
				},
				nodes: []*v1.Node{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "node3",
						},
					}, {
						ObjectMeta: metav1.ObjectMeta{
							Name: "node2",
							Labels: map[string]string{
								"fluid.io/s-big-data-hbase": "true",
							},
						},
					},
				},
			},
			nodeNames: []string{"node3"},
		},
		{
			name: "noController",
			fields: fields{
				name:      "hbase-a",
				namespace: "big-data",
				worker: &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "hbase-a-worker",
						Namespace: "big-data",
						UID:       "uid3",
					},
					Spec: appsv1.StatefulSetSpec{
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"app":              "efc",
								"role":             "efc-worker",
								"fluid.io/dataset": "big-data-hbase-a",
							},
						},
					},
				},
				pods: []*v1.Pod{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "hbase-a-worker-0",
							Namespace: "big-data",
							Labels: map[string]string{
								"app":              "efc",
								"role":             "efc-worker",
								"fluid.io/dataset": "big-data-hbase-a",
							},
						},
						Spec: v1.PodSpec{
							NodeName: "node5",
						},
					},
				},
				nodes: []*v1.Node{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "node5",
						},
					}, {
						ObjectMeta: metav1.ObjectMeta{
							Name: "node4",
							Labels: map[string]string{
								"fluid.io/s-big-data-hbase-a": "true",
							},
						},
					},
				},
			},
			nodeNames: []string{},
		},
	}

	runtimeObjs := []runtime.Object{}

	for _, testcase := range testcases {
		runtimeObjs = append(runtimeObjs, testcase.fields.worker)
		if testcase.fields.ds != nil {
			runtimeObjs = append(runtimeObjs, testcase.fields.ds)
		}
		for _, pod := range testcase.fields.pods {
			runtimeObjs = append(runtimeObjs, pod)
		}
		for _, node := range testcase.fields.nodes {
			runtimeObjs = append(runtimeObjs, node)
		}
	}
	c := fake.NewFakeClientWithScheme(testScheme, runtimeObjs...)

	for _, testcase := range testcases {
		engine := getTestEFCEngineNode(c, testcase.fields.name, testcase.fields.namespace, true)
		err := engine.SyncScheduleInfoToCacheNodes()
		if err != nil {
			t.Errorf("Got error %t.", err)
		}

		nodeList := &v1.NodeList{}
		datasetLabelsString := fmt.Sprintf("%s=true", engine.runtimeInfo.GetRuntimeLabelName())
		datasetLabels, err := labels.Parse(datasetLabelsString)
		if err != nil {
			return
		}

		err = c.List(context.TODO(), nodeList, &client.ListOptions{
			LabelSelector: datasetLabels,
		})

		if err != nil {
			t.Errorf("Got error %t.", err)
		}

		nodeNames := []string{}
		for _, node := range nodeList.Items {
			nodeNames = append(nodeNames, node.Name)
		}

		if len(testcase.nodeNames) == 0 && len(nodeNames) == 0 {
			continue
		}

		if !reflect.DeepEqual(testcase.nodeNames, nodeNames) {
			t.Errorf("test case %v fail to sync node labels %s, wanted %v, got %v", testcase.name, datasetLabelsString, testcase.nodeNames, nodeNames)
		}
	}
}
