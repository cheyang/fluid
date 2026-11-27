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

// Verification harness for PR #6298 / issue #6297 (reviewer: codex).
// Additive test file only; production code is untouched.
//
// Polarity: CONTRACT tests — they assert the intended correct behavior.
// On the pre-fix (base) code they FAIL (duplicate send observed / goroutine leaked);
// on the fixed code they PASS.

import (
	"runtime"
	"testing"
	"time"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/ddc/base"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func newDupSendVerifyEngine(t *testing.T) *JindoCacheEngine {
	t.Helper()
	dataset := &datav1alpha1.Dataset{
		ObjectMeta: metav1.ObjectMeta{Name: "verify-spark", Namespace: "fluid"},
	}
	client := fake.NewFakeClientWithScheme(testScheme, dataset)
	return &JindoCacheEngine{
		name:               "verify-spark",
		namespace:          "fluid",
		Client:             client,
		Log:                fake.NullLogger(),
		MetadataSyncDoneCh: nil,
		runtime:            &datav1alpha1.JindoRuntime{},
	}
}

// startAndConsumeOneSync runs the async metadata sync once: the first call spawns the
// sender goroutine, follow-up calls consume the single result until the engine clears
// MetadataSyncDoneCh (mirrors how the production caller drives it).
func startAndConsumeOneSync(t *testing.T, e *JindoCacheEngine) chan base.MetadataSyncResult {
	t.Helper()
	if err := e.syncMetadataInternal(); err != nil {
		t.Fatalf("first syncMetadataInternal() failed: %v", err)
	}
	ch := e.MetadataSyncDoneCh
	if ch == nil {
		t.Fatal("expected MetadataSyncDoneCh to be initialized after first call")
	}
	deadline := time.Now().Add(10 * time.Second)
	for e.MetadataSyncDoneCh != nil {
		if err := e.syncMetadataInternal(); err != nil {
			t.Fatalf("follow-up syncMetadataInternal() failed: %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for the sync result to be consumed")
		}
	}
	return ch
}

// TestVerifyMetadataSyncDuplicateSend proves that one completed metadata sync produces
// exactly one value on MetadataSyncDoneCh and that the channel is then closed.
// On base code the goroutine executes `resultChan <- result` after base.SafeSend, so
// this third receive observes a duplicate VALUE (ok == true). In production no third
// receiver ever exists (the field is nil'ed; the next sync creates a NEW channel), so
// that send blocks the goroutine forever.
func TestVerifyMetadataSyncDuplicateSend(t *testing.T) {
	e := newDupSendVerifyEngine(t)
	ch := startAndConsumeOneSync(t, e)

	select {
	case v, ok := <-ch:
		if ok {
			t.Fatalf("duplicate send: received an unexpected second value %#v; "+
				"in production this send has no receiver and blocks the goroutine forever", v)
		}
		// channel closed without a second value: correct behavior
	case <-time.After(3 * time.Second):
		t.Fatal("channel was neither closed nor sent on: sender goroutine stuck unexpectedly")
	}
}

// TestVerifyMetadataSyncGoroutineLeak proves the sender goroutine exits after its
// single result is consumed, without any further receive on the channel (which is what
// happens in production). On base code the goroutine parks forever on the duplicate
// send, so the goroutine count never returns to baseline.
func TestVerifyMetadataSyncGoroutineLeak(t *testing.T) {
	before := runtime.NumGoroutine()
	e := newDupSendVerifyEngine(t)
	_ = startAndConsumeOneSync(t, e)

	deadline := time.Now().Add(3 * time.Second)
	for {
		now := runtime.NumGoroutine()
		if now <= before {
			return // sender goroutine exited: correct behavior
		}
		if time.Now().After(deadline) {
			t.Fatalf("goroutine leaked: before=%d, after=%d (sender goroutine blocked on duplicate send)", before, now)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
