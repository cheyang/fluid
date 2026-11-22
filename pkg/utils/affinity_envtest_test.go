/*
Verification harness for PR #6276 (reviewer: codex) — integration layer.
Additive test file only — production code is untouched.

Runs a real kube-apiserver (envtest, KUBEBUILDER_ASSETS required) and checks:

1. CONTRACT: the affinity produced by InjectNodeSelectorRequirements on the PR
   head (requirement merged into an existing term) is accepted by API-server
   validation when used in a Pod spec, and the persisted pod really carries the
   injected requirement.
2. CONTRACT: the shape the BASE code leaves behind for an empty-but-non-nil
   NodeSelectorTerms (requirement dropped, empty selector unchanged) is
   REJECTED by API-server validation — i.e. the empty-terms path was not merely
   a silent no-op, it produced a pod the scheduler would never see.

Skips when KUBEBUILDER_ASSETS is unset.
*/

package utils

import (
	"context"
	"os"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

func TestVerifyInjectedAffinityAPIValidation(t *testing.T) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("KUBEBUILDER_ASSETS not set; skipping envtest integration layer")
	}
	env := &envtest.Environment{}
	cfg, err := env.Start()
	if err != nil {
		t.Fatalf("failed to start envtest: %v", err)
	}
	defer func() {
		_ = env.Stop()
	}()

	c, err := client.New(cfg, client.Options{})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ns := &v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "verify-pr6276"}}
	if err := c.Create(ctx, ns); err != nil {
		t.Fatalf("failed to create namespace: %v", err)
	}

	newPod := func(name string, affinity *v1.Affinity) *v1.Pod {
		return &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns.Name},
			Spec: v1.PodSpec{
				Containers: []v1.Container{{Name: "c", Image: "busybox"}},
				Affinity:   affinity,
			},
		}
	}

	// Case 1: requirement merged into an existing term (head behavior).
	merged := InjectNodeSelectorRequirements([]v1.NodeSelectorRequirement{{
		Key:      "kubernetes.io/hostname",
		Operator: v1.NodeSelectorOpIn,
		Values:   []string{"node01"},
	}}, &v1.Affinity{
		NodeAffinity: &v1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
				NodeSelectorTerms: []v1.NodeSelectorTerm{
					{MatchExpressions: []v1.NodeSelectorRequirement{
						{Key: "node.kubernetes.io/instance-type", Operator: v1.NodeSelectorOpIn, Values: []string{"GPU"}},
					}},
				},
			},
		},
	})
	if err := c.Create(ctx, newPod("merged-affinity", merged)); err != nil {
		t.Fatalf("apiserver rejected merged affinity produced on head: %v", err)
	}
	persisted := &v1.Pod{}
	if err := c.Get(ctx, client.ObjectKey{Name: "merged-affinity", Namespace: ns.Name}, persisted); err != nil {
		t.Fatalf("failed to read back pod: %v", err)
	}
	terms := persisted.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
	found := false
	for _, term := range terms {
		for _, e := range term.MatchExpressions {
			if e.Key == "kubernetes.io/hostname" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("persisted pod lost the injected requirement: %+v", terms)
	}

	// Case 2: what base produces for an empty-but-non-nil NodeSelectorTerms —
	// the requirement is dropped and the empty selector is left as-is. The API
	// server must reject that pod, proving the base path yields an invalid spec.
	baseLeftover := &v1.Affinity{
		NodeAffinity: &v1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
				NodeSelectorTerms: []v1.NodeSelectorTerm{},
			},
		},
	}
	err = c.Create(ctx, newPod("empty-terms-affinity", baseLeftover))
	if err == nil {
		t.Fatalf("expected apiserver to reject a pod with empty nodeSelectorTerms, but it was accepted")
	}
	t.Logf("empty-terms pod rejected as expected: %v", err)
}
