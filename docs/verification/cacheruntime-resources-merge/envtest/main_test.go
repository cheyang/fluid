package main

import (
	"context"
	"os"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

// Proves the apiserver rule behind F9: a pod whose hugepages request != limit
// (the shape PR #6177's merge can synthesize and whose head validator accepts)
// is rejected by a real API server.
func TestApiserverRejectsHugepagesRequestBelowLimit(t *testing.T) {
	env := &envtest.Environment{}
	cfg, err := env.Start()
	if err != nil {
		t.Fatalf("start envtest: %v", err)
	}
	defer env.Stop()

	c, err := client.New(cfg, client.Options{})
	if err != nil {
		t.Fatalf("client: %v", err)
	}

	hp := corev1.ResourceName("hugepages-2Mi")
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "f9-hugepages", Namespace: "default"},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{
			Name:  "c",
			Image: "busybox",
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{hp: resource.MustParse("1Gi")},
				Limits:   corev1.ResourceList{hp: resource.MustParse("2Gi")},
			},
		}}},
	}
	err = c.Create(context.Background(), pod)
	if err == nil {
		t.Fatalf("UNEXPECTED: apiserver accepted hugepages request 1Gi with limit 2Gi")
	}
	t.Logf("apiserver rejected the pod as expected: %v", err)

	// Control: request > limit on an overcommittable resource (the F6 case).
	pod2 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "f6-cpu", Namespace: "default"},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{
			Name:  "c",
			Image: "busybox",
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1")},
				Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m")},
			},
		}}},
	}
	if err := c.Create(context.Background(), pod2); err == nil {
		t.Fatalf("UNEXPECTED: apiserver accepted cpu request 1 with limit 500m")
	} else {
		t.Logf("apiserver rejected the cpu pod as expected: %v", err)
	}

	// The enriched F9 case: cpu IS present (so "HugePages require cpu or memory"
	// does not apply) and only the must-be-equal rule is tripped. This is the
	// exact shape the merge synthesizes from the enriched fixture: template
	// {cpu, hugepages 1Gi/1Gi} + CacheRuntime {cpu, hugepages limit 2Gi}.
	podEnriched := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "f9-hugepages-with-cpu", Namespace: "default"},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{
			Name:  "c",
			Image: "busybox",
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{hp: resource.MustParse("1Gi"), corev1.ResourceCPU: resource.MustParse("1")},
				Limits:   corev1.ResourceList{hp: resource.MustParse("2Gi"), corev1.ResourceCPU: resource.MustParse("2")},
			},
		}}},
	}
	err = c.Create(context.Background(), podEnriched)
	if err == nil {
		t.Fatalf("UNEXPECTED: apiserver accepted hugepages request 1Gi with limit 2Gi (cpu present)")
	}
	if strings.Contains(err.Error(), "HugePages require cpu or memory") {
		t.Fatalf("rejected for the wrong reason (hugepages need cpu/memory, which is present): %v", err)
	}
	t.Logf("apiserver rejected the enriched pod for the must-be-equal rule only: %v", err)

	// Control: equal hugepages request/limit must be ACCEPTED (template alone is valid).
	pod3 := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "f9-hugepages-ok", Namespace: "default"},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{
			Name:  "c",
			Image: "busybox",
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{hp: resource.MustParse("1Gi"), corev1.ResourceCPU: resource.MustParse("1")},
				Limits:   corev1.ResourceList{hp: resource.MustParse("1Gi"), corev1.ResourceCPU: resource.MustParse("1")},
			},
		}}},
	}
	if err := c.Create(context.Background(), pod3); err != nil {
		t.Fatalf("UNEXPECTED: apiserver rejected equal hugepages request/limit: %v", err)
	} else {
		t.Logf("apiserver accepted equal hugepages request=limit=1Gi (control)")
	}
	_ = os.Stdout
}
