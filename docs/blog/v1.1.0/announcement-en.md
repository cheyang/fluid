# Announcing Fluid 1.1.0: The CacheRuntime Framework — Onboard Any Cache System with Zero Controller Code

We're excited to announce the release of **Fluid 1.1.0**!

This cycle also brought a major community milestone: on **January 8, 2026, Fluid was accepted as a CNCF Incubating project** (having joined the CNCF Sandbox back in April 2021). Carried by that momentum, and spanning roughly 9 months since v1.0.8, this release lands **485 commits from 125 contributors** — and marks an architectural leap for the project.

## Highlights

- 🌟 **CacheRuntime — a generic cache-engine framework (flagship)**: Two new CRDs, `CacheRuntime` and `CacheRuntimeClass`, turn "how to orchestrate a cache system" into a **declarative template**. Onboarding a new engine no longer requires writing a dedicated controller — you just describe the topology. It natively supports the mainstream cache topologies (MasterSlave, P2P/DHT, ClientOnly), tiered store for both worker and client, in-place upgrades via AdvancedStatefulSet, and the DataLoad/DataProcess data-flow.

- 🚀 **Curvine as the first engine**: Curvine — a high-performance, Rust-based distributed cache — is the first engine to land on the CacheRuntime framework.

- 🤖 **Data acceleration for AI**: model **prefetch** warms caches to cut inference cold-start time; high-performance stores such as **DeepSeek 3FS** and Curvine integrate through ThinRuntime, covering both LLM training and inference data paths.

- ⚡ **Engine enhancements**: **RDMA** support and master crash-recovery for JindoCache; `volumeClaimTemplates` for JuiceFS workers; **Cron** and **OnEvent** trigger policies for DataProcess.

- ✅ **Reliability**: **240+ testing/E2E improvements**, a full migration of the test suite to Ginkgo v2, retry-on-conflict on critical paths, and fixes for data races and controller panics.

- 🛡️ **Security hardening**: least-privilege RBAC on the control plane, tightened GitHub Actions permissions, a path-injection fix in addons, base image bumped to alpine 3.23.3, and removal of a hardcoded timezone.

- 🗑️ **Deprecation**: GooseFSRuntime is now deprecated.

## Get started

```shell
helm repo add fluid https://fluid-cloudnative.github.io/charts
helm repo update
helm install fluid fluid/fluid --version 1.1.0 -n fluid-system
```

From "write a controller for every cache system" to "onboard any cache system with a single template," Fluid 1.1.0 raises the bar for extensibility in cloud-native data orchestration. Try it out, share feedback, and bring your own cache engine to the CacheRuntime ecosystem! 🎉

> GitHub: [github.com/fluid-cloudnative/fluid](https://github.com/fluid-cloudnative/fluid)
