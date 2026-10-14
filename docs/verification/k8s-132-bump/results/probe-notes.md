# Probe tree evidence (scratch, never committed to the verify branch)

Probe = PR head 8534d282 with ONLY these changes:
1. sigs.k8s.io/structured-merge-diff/v4 pinned back to v4.4.1 (go.mod, vendor/modules.txt, vendor/ tree) — reverts the transitive bump dependabot pulled in
2. probe-only stub appended to pkg/csi/plugins/controller.go: ControllerModifyVolume returning codes.Unimplemented

## probe diff vs PR head (non-vendor portion; vendor portion is the v4.4.1 restore)
diff --git a/go.mod b/go.mod
index f090801b..dead96b6 100644
--- a/go.mod
+++ b/go.mod
@@ -152,7 +152,7 @@ require (
 	gopkg.in/yaml.v3 v3.0.1 // indirect
 	k8s.io/apiextensions-apiserver v0.29.2 // indirect
 	sigs.k8s.io/json v0.0.0-20241010143419-9aa6b5e7a4b3 // indirect
-	sigs.k8s.io/structured-merge-diff/v4 v4.4.2 // indirect
+	sigs.k8s.io/structured-merge-diff/v4 v4.4.1 // indirect
 )
 
 replace k8s.io/pod-security-admission => k8s.io/pod-security-admission v0.29.15
diff --git a/pkg/csi/plugins/controller.go b/pkg/csi/plugins/controller.go
index c8ccf6a1..0d32fc34 100644
--- a/pkg/csi/plugins/controller.go
+++ b/pkg/csi/plugins/controller.go
@@ -133,3 +133,8 @@ func sanitizeVolumeID(volumeID string) string {
 	}
 	return volumeID
 }
+
+// PROBE-ONLY stub (scratch worktree, never committed): satisfy csi v1.9 interface
+func (cs *controllerServer) ControllerModifyVolume(ctx context.Context, req *csi.ControllerModifyVolumeRequest) (*csi.ControllerModifyVolumeResponse, error) {
+	return nil, status.Error(codes.Unimplemented, "")
+}
diff --git a/vendor/modules.txt b/vendor/modules.txt
index 2c45b67b..2f682597 100644
--- a/vendor/modules.txt
+++ b/vendor/modules.txt
@@ -888,7 +888,7 @@ sigs.k8s.io/controller-runtime/pkg/webhook/internal/metrics
 ## explicit; go 1.21
 sigs.k8s.io/json
 sigs.k8s.io/json/internal/golang/encoding/json
-# sigs.k8s.io/structured-merge-diff/v4 v4.4.2
+# sigs.k8s.io/structured-merge-diff/v4 v4.4.1
 ## explicit; go 1.13
 sigs.k8s.io/structured-merge-diff/v4/fieldpath
 sigs.k8s.io/structured-merge-diff/v4/merge

## go build ./... on probe: exit 0 (no output)
## go vet ./... on probe: exit 0 (no output)
## go test -run TestVerify on probe: see probe-fixed-unit.out (all PASS)
