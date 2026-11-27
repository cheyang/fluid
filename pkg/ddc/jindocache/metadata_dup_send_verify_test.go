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

package jindocache

// Reviewer verification harness for PR #6298 / issue #6297 (Reviewer A, Claude).
//
// POLARITY: contract test. It asserts the intended correct behavior:
// after syncMetadataInternal() spawns the async metadata-sync goroutine and
// the single result is consumed, the goroutine must exit and the channel must
// be closed by its deferred base.SafeClose. On the buggy code (duplicate
// unguarded `resultChan <- result` after base.SafeSend) this test FAILS.
// After the fix it PASSES.

import (
	goruntime "runtime"
	"testing"
	"time"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestVerifySyncMetadataSingleSendNoLeak(t *testing.T) {
	dataset := &datav1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "spark",
			Namespace: "fluid",
		},
	}
	client := fake.NewFakeClientWithScheme(testScheme, dataset)
	engine := &JindoCacheEngine{
		name:               "spark",
		namespace:          "fluid",
		Client:             client,
		Log:                fake.NullLogger(),
		MetadataSyncDoneCh: nil,
		runtime:            &datav1alpha1.JindoRuntime{},
	}

	before := goruntime.NumGoroutine()

	// First call: MetadataSyncDoneCh == nil -> else branch, channel created,
	// sync goroutine launched.
	if err := engine.syncMetadataInternal(); err != nil {
		t.Fatalf("first syncMetadataInternal() failed: %v", err)
	}
	ch := engine.MetadataSyncDoneCh
	if ch == nil {
		t.Fatal("expected MetadataSyncDoneCh to be initialized after first sync call")
	}

	// Poll the receive path until the single result is consumed and the
	// engine clears e.MetadataSyncDoneCh.
	deadline := time.Now().Add(5 * time.Second)
	for engine.MetadataSyncDoneCh != nil && time.Now().Before(deadline) {
		if err := engine.syncMetadataInternal(); err != nil {
			t.Fatalf("follow-up syncMetadataInternal() failed: %v", err)
		}
	}
	if engine.MetadataSyncDoneCh != nil {
		t.Fatal("timed out waiting for the metadata sync result to be consumed")
	}

	// The channel must close promptly: this proves the goroutine reached its
	// deferred base.SafeClose instead of blocking forever on a second send.
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("unexpected second value sent on MetadataSyncDoneCh")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("MetadataSyncDoneCh never closed: goroutine likely blocked on duplicate send (issue #6297)")
	}

	// No leaked goroutine: count must come back down to the baseline.
	leakDeadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(leakDeadline) {
		if goruntime.NumGoroutine() <= before {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("goroutine leaked: before=%d, after=%d (goroutine blocked on duplicate send)", before, goruntime.NumGoroutine())
}
