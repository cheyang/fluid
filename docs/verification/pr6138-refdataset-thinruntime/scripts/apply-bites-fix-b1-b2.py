#!/usr/bin/env python3
# HARNESS-BITES helper: temporarily apply the proposed fixes for B1/B2 to
# pkg/utils/dataset_runtime.go, so the contract tests can be shown to discriminate.
# Reverted with `git checkout -- pkg/utils/dataset_runtime.go` afterwards.
import sys

p = "pkg/utils/dataset_runtime.go"
src = open(p).read()

old = """			runtimeToUpdate := runtime.DeepCopy()
			runtimeToUpdate.SetOwnerReferences([]metav1.OwnerReference{
				datasetControllerOwnerReference(dataset)})
"""

new = """			runtimeToUpdate := runtime.DeepCopy()
			desired := datasetControllerOwnerReference(dataset)
			merged := []metav1.OwnerReference{desired}
			for _, ref := range runtimeToUpdate.GetOwnerReferences() {
				if ref.Kind == desired.Kind && ref.APIVersion == desired.APIVersion {
					continue
				}
				merged = append(merged, ref)
			}
			runtimeToUpdate.SetOwnerReferences(merged)
			labelsToUpdate := runtimeToUpdate.GetLabels()
			if labelsToUpdate == nil {
				labelsToUpdate = map[string]string{}
			}
			labelsToUpdate[common.LabelAnnotationDatasetId] = GetDatasetId(dataset.GetNamespace(), dataset.GetName(), string(dataset.GetUID()))
			runtimeToUpdate.SetLabels(labelsToUpdate)
"""

if old not in src:
    sys.exit("anchor not found in %s" % p)

open(p, "w").write(src.replace(old, new, 1))
print("applied bites fix to %s" % p)
