package kubeclient

// Reviewer verification harness for PR #6217 / issue #6216.
//
// Claim under test: MergeNodeSelectorAndNodeAffinity emits its generated
// MatchExpressions in map-iteration order, which is randomized per iteration
// in Go, so multi-label nodeSelector maps produce a differently-ordered
// NodeAffinity on each reconciliation cycle. Callers (pkg/ddc/*/status.go)
// persist the result into status.CacheAffinity and gate the status write on
// reflect.DeepEqual(runtime.Status, runtimeToUpdate.Status) (see
// pkg/ddc/alluxio/status.go:120), so a fluctuating order causes a spurious
// status write on every cycle for multi-label workers.
//
// Polarity: all tests below are CONTRACT tests — they assert the intended
// correct behavior. They PASS on the fixed (sorted) code and FAIL on the base
// (map-iteration-order) code. TestPR6217ObserveOrderVariance is a diagnostic
// that never fails; it logs how many distinct orderings it observed so the
// base-vs-head asymmetry is visible in raw output.

import (
	"reflect"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func pr6217Selector() map[string]string {
	return map[string]string{
		"kubernetes.io/hostname":           "node-1",
		"topology.kubernetes.io/zone":      "zone-a",
		"fluid.io/dataset":                 "my-dataset",
		"node.kubernetes.io/instance-type": "m5.large",
		"beta.kubernetes.io/arch":          "amd64",
		"app.kubernetes.io/name":           "worker",
	}
}

func pr6217Keys(affinity *corev1.NodeAffinity) (keys []string) {
	terms := affinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
	for _, r := range terms[0].MatchExpressions {
		keys = append(keys, r.Key)
	}
	return
}

// F1 contract: generated MatchExpressions keys are in ascending order.
func TestPR6217SortedOrderContract(t *testing.T) {
	got := MergeNodeSelectorAndNodeAffinity(pr6217Selector(), nil)
	keys := pr6217Keys(got)
	for i := 1; i < len(keys); i++ {
		if !(keys[i-1] < keys[i]) {
			t.Fatalf("MatchExpressions keys not strictly ascending: %v", keys)
		}
	}
}

// F2 contract: repeated invocations are DeepEqual-stable. This is what makes
// the sorted output usable as status: the same input yields the same value.
func TestPR6217DeterministicAcrossInvocationsContract(t *testing.T) {
	first := MergeNodeSelectorAndNodeAffinity(pr6217Selector(), nil)
	for i := 0; i < 500; i++ {
		if !reflect.DeepEqual(first, MergeNodeSelectorAndNodeAffinity(pr6217Selector(), nil)) {
			t.Fatalf("invocation %d differs from the first under reflect.DeepEqual", i)
		}
	}
}

// P0 contract (the reported symptom, simulated at the controller's decision
// point): status persisted from one reconcile equals the freshly computed
// status of the next reconcile, so the DeepEqual gate in
// pkg/ddc/alluxio/status.go does not fire a spurious status write.
func TestPR6217StatusComparisonStableContract(t *testing.T) {
	// persisted simulates runtime.Status.CacheAffinity after a status write.
	persisted := MergeNodeSelectorAndNodeAffinity(pr6217Selector(), nil).DeepCopy()
	for i := 0; i < 500; i++ {
		// fresh simulates runtimeToUpdate.Status.CacheAffinity on the next cycle.
		fresh := MergeNodeSelectorAndNodeAffinity(pr6217Selector(), nil)
		if !reflect.DeepEqual(persisted, fresh) {
			t.Fatalf("spurious status diff on cycle %d:\npersisted keys: %v\nfresh keys:     %v",
				i, pr6217Keys(persisted), pr6217Keys(fresh))
		}
	}
}

// F3 contract: the append path (pod.go else-branch) is also deterministic
// when the worker spec already carries NodeSelectorTerms — the case for
// runtimes whose worker pods were mutated by the cache-affinity webhook.
func TestPR6217AppendPathDeterministicContract(t *testing.T) {
	existing := func() *corev1.Affinity {
		return &corev1.Affinity{
			NodeAffinity: &corev1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
					NodeSelectorTerms: []corev1.NodeSelectorTerm{
						{MatchExpressions: []corev1.NodeSelectorRequirement{
							{Key: "pre-existing.example.com/term", Operator: corev1.NodeSelectorOpIn, Values: []string{"x"}},
						}},
					},
				},
			},
		}
	}
	first := MergeNodeSelectorAndNodeAffinity(pr6217Selector(), existing())
	for i := 0; i < 500; i++ {
		if !reflect.DeepEqual(first, MergeNodeSelectorAndNodeAffinity(pr6217Selector(), existing())) {
			t.Fatalf("append path: invocation %d differs from the first under reflect.DeepEqual", i)
		}
	}
}

// Diagnostic (never fails): reports how many distinct key orderings appear
// across N invocations. Base: many distinct orderings. Fixed: exactly 1.
func TestPR6217ObserveOrderVariance(t *testing.T) {
	const n = 1000
	distinct := map[string]bool{}
	for i := 0; i < n; i++ {
		distinct[strings.Join(pr6217Keys(MergeNodeSelectorAndNodeAffinity(pr6217Selector(), nil)), ",")] = true
	}
	t.Logf("observed %d distinct key orderings across %d invocations", len(distinct), n)
}
