/*
Copyright 2026 The Fluid Authors.

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

// Package dataflowenvtest is the L2 (integration) layer of the verification
// harness for fluid-cloudnative/fluid PR #6254 (reviewer: codex). It runs the
// real DataFlowReconciler against a real API server (envtest) with the shipped
// CRDs installed, proving the release path works through the status subresource
// rather than only against the fake client. Additive only.
package dataflowenvtest

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	dataflow "github.com/fluid-cloudnative/fluid/pkg/controllers/v1alpha1/dataflow"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	testEnv   *envtest.Environment
	k8sClient client.Client
)

func TestDataFlowEnvtest(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "DataFlow envtest Suite (PR6254 verification)")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}
	cfg, err := testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	scheme := runtime.NewScheme()
	Expect(datav1alpha1.AddToScheme(scheme)).To(Succeed())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme})
	Expect(err).NotTo(HaveOccurred())
})

var _ = AfterSuite(func() {
	if testEnv != nil {
		Expect(testEnv.Stop()).To(Succeed())
	}
})

var _ = Describe("PR6254 L2: release a waiting operation whose runAfter is gone (real API server)", func() {
	const namespace = "default"

	It("P0-contract: waiting DataLoad without runAfter is released via the status subresource, no panic", func() {
		name := "l2-p0"
		load := &datav1alpha1.DataLoad{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}}
		Expect(k8sClient.Create(context.TODO(), load)).To(Succeed())

		load.Status = datav1alpha1.OperationStatus{
			Phase:      common.PhasePending,
			WaitingFor: datav1alpha1.WaitingStatus{OperationComplete: ptr.To(true)},
			Conditions: []datav1alpha1.Condition{},
		}
		Expect(k8sClient.Status().Update(context.TODO(), load)).To(Succeed())

		r := dataflow.NewDataFlowReconciler(k8sClient, logf.Log, record.NewFakeRecorder(32), 5*time.Second)
		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: name, Namespace: namespace}}

		var (
			result ctrl.Result
			err    error
		)
		Expect(func() { result, err = r.Reconcile(context.TODO(), req) }).NotTo(Panic())
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal(ctrl.Result{}))

		updated := &datav1alpha1.DataLoad{}
		Expect(k8sClient.Get(context.TODO(), req.NamespacedName, updated)).To(Succeed())
		Expect(updated.Status.WaitingFor.OperationComplete).To(HaveValue(BeFalse()))
		Expect(updated.Status.Phase).To(Equal(common.PhasePending))
	})

	It("P0-xkind + F1-canary: waiting DataLoad keeps waiting; non-waiting same-name DataBackup gets the spurious write", func() {
		name := "l2-xkind"
		preceding := &datav1alpha1.DataLoad{ObjectMeta: metav1.ObjectMeta{Name: "l2-preceding", Namespace: namespace}}
		Expect(k8sClient.Create(context.TODO(), preceding)).To(Succeed())
		preceding.Status = datav1alpha1.OperationStatus{Phase: common.PhaseExecuting, Conditions: []datav1alpha1.Condition{}}
		Expect(k8sClient.Status().Update(context.TODO(), preceding)).To(Succeed())

		waitingLoad := &datav1alpha1.DataLoad{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
			Spec: datav1alpha1.DataLoadSpec{
				RunAfter: &datav1alpha1.OperationRef{
					ObjectRef: datav1alpha1.ObjectRef{Kind: "DataLoad", Name: "l2-preceding"},
				},
			},
		}
		Expect(k8sClient.Create(context.TODO(), waitingLoad)).To(Succeed())
		waitingLoad.Status = datav1alpha1.OperationStatus{
			Phase:      common.PhasePending,
			WaitingFor: datav1alpha1.WaitingStatus{OperationComplete: ptr.To(true)},
			Conditions: []datav1alpha1.Condition{},
		}
		Expect(k8sClient.Status().Update(context.TODO(), waitingLoad)).To(Succeed())

		idleBackup := &datav1alpha1.DataBackup{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}}
		Expect(k8sClient.Create(context.TODO(), idleBackup)).To(Succeed())
		idleBackup.Status = datav1alpha1.OperationStatus{Phase: common.PhaseComplete, Conditions: []datav1alpha1.Condition{}}
		Expect(k8sClient.Status().Update(context.TODO(), idleBackup)).To(Succeed())

		r := dataflow.NewDataFlowReconciler(k8sClient, logf.Log, record.NewFakeRecorder(32), 5*time.Second)
		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: name, Namespace: namespace}}

		Expect(func() { _, _ = r.Reconcile(context.TODO(), req) }).NotTo(Panic())

		updatedLoad := &datav1alpha1.DataLoad{}
		Expect(k8sClient.Get(context.TODO(), req.NamespacedName, updatedLoad)).To(Succeed())
		Expect(updatedLoad.Status.WaitingFor.OperationComplete).To(HaveValue(BeTrue()))

		updatedBackup := &datav1alpha1.DataBackup{}
		Expect(k8sClient.Get(context.TODO(), req.NamespacedName, updatedBackup)).To(Succeed())
		// canary: spurious write on the non-waiting object (F1)
		Expect(updatedBackup.Status.WaitingFor.OperationComplete).To(HaveValue(BeFalse()),
			"canary: expected the spurious status write to still happen on a real API server")
	})
})
