/*
Verification harness for PR https://github.com/fluid-cloudnative/fluid/pull/6284
Reviewer B (Codex). ADDITIVE ONLY — does not modify production code.

Claims under test (function: AlluxioFileUtils.MasterPodName):

	P0 (premise, runs against BASE): on the base branch, when `alluxio fsadmin
	   report` succeeds (nil error) but its output lacks the master address on
	   the second line, MasterPodName panics with "index out of range" instead
	   of returning an error. This file is a CONTRACT test: it FAILS (panics) on
	   buggy code and PASSES on fixed code, so a red run on base IS the premise
	   reproduction.

	C1 (contract): malformed-but-successful outputs return an error, never panic.
	C2 (contract): well-formed output still parses to the master address
	   (regression guard: the fix must not change the happy path).
	C3 (contract): exec failure still propagates the exec error.
	O1 (observation/canary, pre-existing): a second line with >= 3 fields that
	   is NOT "Master Address:" (e.g. "Rpc Port: 19998") is silently accepted
	   and its third field returned as the address. This documents pre-existing
	   lenient parsing outside this PR's scope; it PASSES on both base and head
	   while the behavior stays lenient, and FLIPS if parsing is ever tightened.

Run with inlining disabled (gomonkey requirement):

	go test -gcflags=all=-l ./pkg/ddc/alluxio/operations/ -run 'TestVerifyMasterPodName' -v
*/
package operations

import (
	"errors"
	"reflect"
	"testing"

	"github.com/agiledragon/gomonkey/v2"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"
)

func TestVerifyMasterPodNameMalformedOutputReturnsError(t *testing.T) {
	cases := map[string]string{
		"empty output":                "",
		"single line no newline":      "Alluxio cluster summary: ",
		"single line with newline":    "Alluxio cluster summary: \n",
		"only newline":                "\n",
		"second line missing value":   "Alluxio cluster summary: \n    Master Address:",
		"second line two fields only": "Alluxio cluster summary: \n    Rpc Port:",
		"second line empty":           "Alluxio cluster summary: \n\n    Web Port: 20010",
	}
	patches := gomonkey.NewPatches()
	defer patches.Reset()

	a := &AlluxioFileUtils{log: fake.NullLogger()}
	for name, stdout := range cases {
		stdout := stdout
		execFn := func(a AlluxioFileUtils, command []string, verbose bool) (string, string, error) {
			return stdout, "", nil
		}
		patches.ApplyPrivateMethod(reflect.TypeOf(AlluxioFileUtils{}), "exec", execFn)
		// On buggy code the next line panics with index out of range (P0 reproduction).
		if _, err := a.MasterPodName(); err == nil {
			t.Errorf("case %q (stdout %q): want err, got nil", name, stdout)
		}
	}
}

func TestVerifyMasterPodNameWellFormedStillParses(t *testing.T) {
	execFn := func(a AlluxioFileUtils, command []string, verbose bool) (string, string, error) {
		return "Alluxio cluster summary: \n    Master Address: 192.168.0.193:20009\n    Web Port: 20010", "", nil
	}
	patches := gomonkey.ApplyPrivateMethod(reflect.TypeOf(AlluxioFileUtils{}), "exec", execFn)
	defer patches.Reset()

	a := &AlluxioFileUtils{log: fake.NullLogger()}
	address, err := a.MasterPodName()
	if err != nil {
		t.Fatalf("well-formed output: want nil err, got %v", err)
	}
	if address != "192.168.0.193" {
		t.Errorf("well-formed output: want address %q, got %q", "192.168.0.193", address)
	}
}

func TestVerifyMasterPodNameExecErrorPropagates(t *testing.T) {
	execFn := func(a AlluxioFileUtils, command []string, verbose bool) (string, string, error) {
		return "", "", errors.New("fail to run the command")
	}
	patches := gomonkey.ApplyPrivateMethod(reflect.TypeOf(AlluxioFileUtils{}), "exec", execFn)
	defer patches.Reset()

	a := &AlluxioFileUtils{log: fake.NullLogger()}
	if _, err := a.MasterPodName(); err == nil {
		t.Error("exec failure: want err, got nil")
	}
}

// TestVerifyMasterPodNameLenientParsingObservation documents (does not judge)
// the pre-existing lenient parsing: a >=3-field second line that is not the
// "Master Address:" line is silently accepted. Polarity: canary — passes on
// both base and head today; flips if parsing is ever tightened.
func TestVerifyMasterPodNameLenientParsingObservation(t *testing.T) {
	execFn := func(a AlluxioFileUtils, command []string, verbose bool) (string, string, error) {
		return "Alluxio cluster summary: \n    Rpc Port: 19998\n    Master Address: 10.0.0.1:19998", "", nil
	}
	patches := gomonkey.ApplyPrivateMethod(reflect.TypeOf(AlluxioFileUtils{}), "exec", execFn)
	defer patches.Reset()

	a := &AlluxioFileUtils{log: fake.NullLogger()}
	address, err := a.MasterPodName()
	if err != nil {
		t.Fatalf("lenient-parsing observation changed: got err %v (parsing tightened?)", err)
	}
	if address != "19998" {
		t.Fatalf("lenient-parsing observation changed: got address %q, want %q", address, "19998")
	}
	t.Logf("observed: non-Master-Address second line parsed leniently, address=%q", address)
}
