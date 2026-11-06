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

// Package gomegacontract is a reviewer verification harness for
// https://github.com/fluid-cloudnative/fluid/pull/6231 (gomega 1.38.2 -> 1.44.0).
// It pins the vendored gomega's post-1.44 matcher semantics as executable
// contract tests: every assertion below is RED on gomega 1.38.2 and GREEN on
// 1.44.0, so the suite doubles as a guard against accidental downgrades.
// It contains no production code and is not imported anywhere.
package gomegacontract
