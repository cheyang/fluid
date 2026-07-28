#!/usr/bin/env python3
# HARNESS-BITES helper for B3: temporarily reorder reconcileDataset so the
# NoneDatasetPhase -> NotBound status update (step 4) happens BEFORE the reference-runtime
# creation (step 3). The B3 canary must FLIP TO RED under this change.
# Reverted with `git checkout -- pkg/controllers/v1alpha1/dataset/dataset_controller.go`.
import re, sys

p = "pkg/controllers/v1alpha1/dataset/dataset_controller.go"
src = open(p).read()

step3_start = src.index("\t// 3. Create Runtime if it's reference dataset")
step4_start = src.index("\t// 4. Update the phase to NotBoundDatasetPhase")
step5_start = src.index("\t// 5. Check if needRequeue")

step3 = src[step3_start:step4_start]
step4 = src[step4_start:step5_start]

if not step3.strip() or not step4.strip():
    sys.exit("failed to slice steps 3/4")

swapped = src[:step3_start] + step4 + step3 + src[step5_start:]
open(p, "w").write(swapped)
print("swapped steps 3 and 4 in %s" % p)
