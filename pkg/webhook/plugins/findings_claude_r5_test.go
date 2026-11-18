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

package plugins

// Reviewer harness for PR #6197 (Claude), round 5 (head 9c8d0e9).
//
// The round-5 delta bcd497c7a switches the chart's clientless fallback gate from
// Go-template truthiness to hasKey, so an explicitly set clientless group reaches
// the rendered webhook-plugins profile verbatim, including falsey spellings.
// Which spellings a values file can actually deliver was established with the real
// helm CLI (results/chart-values-fallback.txt): `[]` sticks (a list replaces the map
// during helm coalescing), while `{}` and null keep the chart's default group through
// helm's map coalescing before the template ever runs.
//
// This file pins what the webhook does with each shape at startup
// (RegisterMutatingHandlers: yaml.Unmarshal errors abort the webhook with os.Exit(1)):
//
//   - clientless: []            -> unmarshal ERROR  (webhook fails to start; A-F7 canary)
//   - clientless: null          -> empty group      (injection off, no error)
//   - clientless: {}            -> empty group      (injection off, no error)
//   - clientless: {wD: [], woD: []} -> empty group  (the documented off switch)
//   - serverful: []             -> unmarshal ERROR  (control: the other groups behave
//     the same way, so the [] edge is not clientless-specific)
//
// A-F7 canary polarity: GREEN while the current behavior stands. If the webhook is
// made to tolerate a list-shaped group (or the chart rejects the spelling up front),
// the [] expectation flips and this must be inverted into a contract.
//
// Also pins the label rename of commit 9c8d0e9fc: the clientless routing label is
// clientless.fluid.io/inject (common.InjectClientless) and the old
// fluid.io/inject spelling is gone from the webhook's label gates.

import (
	"github.com/fluid-cloudnative/fluid/pkg/common"
	"github.com/fluid-cloudnative/fluid/pkg/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v2"
)

var _ = Describe("Reviewer findings (PR 6197), round 5: clientless group shapes in the plugins profile", func() {

	unmarshal := func(profileYAML string) (PluginsProfile, error) {
		profile := PluginsProfile{}
		err := yaml.Unmarshal([]byte(profileYAML), &profile)
		return profile, err
	}

	It("rejects a list-shaped clientless group (A-F7 canary: webhook startup fails)", func() {
		_, err := unmarshal("plugins:\n  clientless: []\n")
		Expect(err).To(HaveOccurred(), "a !!seq cannot unmarshal into the Clientless struct; RegisterMutatingHandlers returns the error and cmd/webhook exits 1")
	})

	It("rejects a list-shaped serverful group identically (control: not clientless-specific)", func() {
		_, err := unmarshal("plugins:\n  serverful: []\n")
		Expect(err).To(HaveOccurred(), "the other groups have no fallback either and fail the same way")
	})

	It("accepts a null clientless group as an empty group", func() {
		profile, err := unmarshal("plugins:\n  clientless: null\n")
		Expect(err).NotTo(HaveOccurred())
		Expect(profile.Plugins.Clientless.WithDataset).To(BeEmpty())
		Expect(profile.Plugins.Clientless.WithoutDataset).To(BeEmpty())
	})

	It("accepts an empty-map clientless group as an empty group", func() {
		profile, err := unmarshal("plugins:\n  clientless: {}\n")
		Expect(err).NotTo(HaveOccurred())
		Expect(profile.Plugins.Clientless.WithDataset).To(BeEmpty())
		Expect(profile.Plugins.Clientless.WithoutDataset).To(BeEmpty())
	})

	It("accepts the documented empty-group opt-out form", func() {
		profile, err := unmarshal("plugins:\n  clientless:\n    withDataset: []\n    withoutDataset: []\n")
		Expect(err).NotTo(HaveOccurred())
		Expect(profile.Plugins.Clientless.WithDataset).To(BeEmpty())
		Expect(profile.Plugins.Clientless.WithoutDataset).To(BeEmpty())
	})
})

var _ = Describe("Reviewer findings (PR 6197), round 5: the clientless label rename", func() {

	It("routes on clientless.fluid.io/inject", func() {
		Expect(utils.ClientlessEnabled(map[string]string{"clientless.fluid.io/inject": "true"})).To(BeTrue())
		Expect(utils.ClientlessEnabled(map[string]string{"clientless.fluid.io/inject": "false"})).To(BeFalse())
	})

	It("no longer routes on the old fluid.io/inject spelling", func() {
		Expect(utils.ClientlessEnabled(map[string]string{"fluid.io/inject": "true"})).To(BeFalse(),
			"the old spelling must not trigger injection after the rename (unreleased feature, no compat keep-alive)")
	})

	It("keeps the done marker and the fuse labels distinct from the clientless label", func() {
		// the completion marker is unchanged (done.sidecar.fluid.io/inject); only the
		// routing label was renamed, so a marked pod is not re-routed as clientless.
		Expect(utils.ClientlessEnabled(map[string]string{common.InjectSidecarDone: "true"})).To(BeFalse())
		Expect(common.InjectClientless).To(Equal("clientless.fluid.io/inject"))
	})
})
