/*
Copyright 2021 The Fluid Authors.

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

package transformer

import (
	datav1alpha1 "github.com/fluid-cloudnative/fluid/api/v1alpha1"
	"github.com/fluid-cloudnative/fluid/pkg/common"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

// fluidScheme knows the fluid API types. It recovers the GroupVersionKind of an object whose TypeMeta is
// empty, which a typed client is allowed to hand back: an ownerReference without kind/apiVersion is rejected
// by the API server and can not be resolved back to its owner by an owner based watch.
var fluidScheme = runtime.NewScheme()

func init() {
	utilruntime.Must(datav1alpha1.AddToScheme(fluidScheme))
}

func GenerateOwnerReferenceFromObject(obj client.Object) *common.OwnerReference {
	gvk := obj.GetObjectKind().GroupVersionKind()
	if gvk.Empty() {
		if resolved, err := apiutil.GVKForObject(obj, fluidScheme); err == nil {
			gvk = resolved
		}
	}

	ref := &common.OwnerReference{
		APIVersion:         gvk.GroupVersion().String(),
		Kind:               gvk.Kind,
		UID:                string(obj.GetUID()),
		Enabled:            true,
		Name:               obj.GetName(),
		BlockOwnerDeletion: false,
		Controller:         true,
	}

	return ref

}

func FilterOwnerByKind(ownerReferences []metav1.OwnerReference, ownerKind string) []metav1.OwnerReference {
	ret := []metav1.OwnerReference{}

	for _, owner := range ownerReferences {
		if owner.Kind == ownerKind {
			ret = append(ret, owner)
		}
	}

	return ret
}
