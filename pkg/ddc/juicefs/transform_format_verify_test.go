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

// This file is the review-finding-verifier harness (additive). It pins the
// downstream consequence of the edition classification done in transform():
// not just that Edition is right, but that genFormatCmd therefore takes the
// matching branch and produces a non-empty community `format` command (or an
// enterprise `auth` command) — i.e. the "filesystem never formatted" half of
// the bug is resolved, not just the label. Production code is untouched here.
package juicefs

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/net"

	"github.com/fluid-cloudnative/fluid/pkg/common"
	"github.com/fluid-cloudnative/fluid/pkg/ddc/base"
	"github.com/fluid-cloudnative/fluid/pkg/ddc/base/portallocator"
	"github.com/fluid-cloudnative/fluid/pkg/utils/fake"

	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
)

var _ = Describe("JuiceFSEngine transform format-cmd (verify harness)", func() {
	BeforeEach(func() {
		pr := net.ParsePortRangeOrDie("14000-15999")
		Expect(portallocator.SetupRuntimePortAllocator(nil, pr, "bitmap", dummy)).To(Succeed())
	})

	// B1/B2 contract: metaurl in SharedEncryptOptions -> community edition AND a
	// non-empty community `juicefs format` command (the bug left FormatCmd empty
	// because genFormatCmd took the enterprise early-return branch).
	It("community: metaurl in SharedEncryptOptions yields edition=community and a non-empty ce format cmd", func() {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "fluid"},
			Data:       map[string][]byte{"metaurl": []byte("redis://127.0.0.1:6379/0")},
		}
		dataset := &datav1alpha1.Dataset{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "fluid"},
			Spec: datav1alpha1.DatasetSpec{
				SharedEncryptOptions: []datav1alpha1.EncryptOption{{
					Name: JuiceMetaUrl,
					ValueFrom: datav1alpha1.EncryptOptionSource{
						SecretKeyRef: datav1alpha1.SecretKeySelector{Name: "test", Key: "metaurl"},
					},
				}},
				Mounts: []datav1alpha1.Mount{{
					MountPoint: "juicefs:///mnt/test",
					Name:        "test",
				}},
			},
		}
		fakeClient := fake.NewFakeClientWithScheme(testScheme, secret.DeepCopy(), dataset.DeepCopy())
		runtimeInfo, err := base.BuildRuntimeInfo("test", "fluid", "juicefs")
		Expect(err).NotTo(HaveOccurred())

		engine := JuiceFSEngine{
			name:        "test",
			namespace:   "fluid",
			Client:      fakeClient,
			Log:         fake.NullLogger(),
			runtime:     &datav1alpha1.JuiceFSRuntime{},
			runtimeInfo: runtimeInfo,
		}
		runtime := &datav1alpha1.JuiceFSRuntime{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "fluid"},
			Spec: datav1alpha1.JuiceFSRuntimeSpec{
				Fuse:   datav1alpha1.JuiceFSFuseSpec{},
				Worker: datav1alpha1.JuiceFSCompTemplateSpec{Replicas: 1},
			},
		}

		value, err := engine.transform(runtime)
		Expect(err).NotTo(HaveOccurred())
		Expect(value.Edition).To(Equal(CommunityEdition), "edition must be community when metaurl is in SharedEncryptOptions")
		Expect(value.Configs.FormatCmd).NotTo(BeEmpty(), "FormatCmd must be non-empty so the fs actually gets formatted")
		Expect(value.Configs.FormatCmd).To(ContainSubstring(common.JuiceCeCliPath), "FormatCmd must use the community ce cli path")
		Expect(value.Configs.FormatCmd).To(ContainSubstring("format"), "FormatCmd must be a community format command")
		Expect(strings.Contains(value.Configs.FormatCmd, " auth ")).To(BeFalse(), "FormatCmd must NOT take the enterprise auth branch")
	})

	// B4 regression contract: enterprise (token in SharedEncryptOptions) must stay
	// enterprise and produce the ee `juicefs auth` command. The PR only touches
	// the metaurl classification path; this pins that enterprise is unaffected.
	It("enterprise: token in SharedEncryptOptions yields edition=enterprise and an ee auth cmd", func() {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "fluid"},
			Data:       map[string][]byte{"token": []byte("dummy-enterprise-token")},
		}
		dataset := &datav1alpha1.Dataset{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "fluid"},
			Spec: datav1alpha1.DatasetSpec{
				SharedEncryptOptions: []datav1alpha1.EncryptOption{{
					Name: "token",
					ValueFrom: datav1alpha1.EncryptOptionSource{
						SecretKeyRef: datav1alpha1.SecretKeySelector{Name: "test", Key: "token"},
					},
				}},
				Mounts: []datav1alpha1.Mount{{
					MountPoint: "juicefs:///mnt/test",
					Name:       "test",
				}},
			},
		}
		fakeClient := fake.NewFakeClientWithScheme(testScheme, secret.DeepCopy(), dataset.DeepCopy())
		runtimeInfo, err := base.BuildRuntimeInfo("test", "fluid", "juicefs")
		Expect(err).NotTo(HaveOccurred())

		engine := JuiceFSEngine{
			name:        "test",
			namespace:   "fluid",
			Client:      fakeClient,
			Log:         fake.NullLogger(),
			runtime:     &datav1alpha1.JuiceFSRuntime{},
			runtimeInfo: runtimeInfo,
		}
		runtime := &datav1alpha1.JuiceFSRuntime{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "fluid"},
			Spec: datav1alpha1.JuiceFSRuntimeSpec{
				Fuse:   datav1alpha1.JuiceFSFuseSpec{},
				Worker: datav1alpha1.JuiceFSCompTemplateSpec{Replicas: 1},
			},
		}

		value, err := engine.transform(runtime)
		Expect(err).NotTo(HaveOccurred())
		Expect(value.Edition).To(Equal(EnterpriseEdition), "token-only dataset stays enterprise (no regression)")
		Expect(value.Configs.FormatCmd).To(ContainSubstring(common.JuiceCliPath), "ee FormatCmd uses the enterprise ee cli path")
		Expect(value.Configs.FormatCmd).To(ContainSubstring(" auth "), "ee FormatCmd is an auth command")
		Expect(value.Configs.FormatCmd).To(ContainSubstring("--token=${TOKEN}"), "ee FormatCmd carries the token arg")
	})
})
