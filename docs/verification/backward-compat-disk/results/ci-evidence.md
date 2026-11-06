# CI evidence (GitHub Actions API, observed 2026-11-06T01:55:57Z)

## PR head 27581121 — backward-compat run 27496321566 (2026-06-14)
backward-compat-test (v1.22.17): success
backward-compat-test (v1.30.13): success
backward-compat-test (v1.24.17): success
backward-compat-test (v1.33.2): success
backward-compat-test (v1.28.15): success

## PR head 27581121 — kind-e2e run 27496321576 (shared-script blast radius)
kind-e2e-test (v1.30.13): success
kind-e2e-test (v1.33.2): success
kind-e2e-test (v1.28.15): success
kind-e2e-test (v1.22.17): success
kind-e2e-test (v1.24.17): success

## Base-era failing run 27287867871 (branch 'policy', 2026-06-10) — per-leg failing step
backward-compat-test (v1.28.15): failure | failing step: Build current fluid docker images
backward-compat-test (v1.22.17): failure | failing step: Build current fluid docker images
backward-compat-test (v1.24.17): failure | failing step: Build current fluid docker images
backward-compat-test (v1.30.13): failure | failing step: Build current fluid docker images
backward-compat-test (v1.33.2): failure | failing step: Build current fluid docker images

## lint job (Project Check) across commits
merge-base da69206 (run 27493587937): failure
PR first commit 02be5ca3b (empty commit, no file changes): failure
PR head 27581121: failure (run 27496321559) — annotations are Node20 deprecation + go.mod cache warnings, lint job is Go-only (golangci-lint/CRD/openapi/gofmt), PR touches no Go code

## Nov 2026 failures of the same workflow (not ENOSPC)
run 41981311011 (dependabot go_modules, 2026-11-05): all 5 legs fail; log excerpt: 'go.mod requires go >= 1.26.0 (running go 1.25.12; GOTOOLCHAIN=local)' at make generate — Go-version mismatch, unrelated to disk.
June logs are HTTP 410 (expired) — literal 'no space left on device' from the triggering failure is no longer re-readable; step-level attribution is used instead.
