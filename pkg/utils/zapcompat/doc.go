// Package zapcompat holds the reviewer verification harness for the
// go.uber.org/zap v1.27.0 -> v1.28.0 dependency bump (fluid PR #6241).
//
// It contains no production code: only tests that pin the logging contracts
// fluid's binaries and its dependency graph (sigs.k8s.io/controller-runtime)
// rely on across the upgrade. See docs/verification/zap-bump/.
package zapcompat
