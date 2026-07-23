# Fluid 1.1.0 发布：CacheRuntime 通用缓存引擎框架，让任意缓存系统「零代码」接入

我们很高兴地宣布 **Fluid 1.1.0** 正式发布！

就在本周期内，Fluid 迎来重要里程碑——**2026 年 1 月 8 日正式成为 CNCF 孵化（Incubating）项目**。带着这份社区动能，自 v1.0.8 以来，社区历经约 9 个月、**485 个提交、125 位贡献者**的迭代，带来一次架构级的能力跃迁。

## 本次核心亮点

- 🌟 **CacheRuntime 通用缓存引擎框架（旗舰特性）**：全新的 `CacheRuntime` / `CacheRuntimeClass` CRD，把「如何编排一套缓存系统」抽象为一份**声明式模板**。接入新引擎不再需要编写专属 Controller，只需描述拓扑即可，原生支持 MasterSlave、P2P/DHT、ClientOnly 等主流缓存架构，并支持 Worker/Client 分层存储、基于 AdvancedStatefulSet 的原地升级，以及 DataLoad/DataProcess 数据流。

- 🚀 **Curvine 引擎首发接入**：基于 Rust 的高性能分布式缓存系统 Curvine 成为 CacheRuntime 框架的首个落地引擎。

- 🤖 **面向 AI 的数据加速**：模型 **Prefetch** 预热加速推理服务冷启动；通过 ThinRuntime 接入 **DeepSeek 3FS**、Curvine 等高性能存储，覆盖大模型训练与推理的数据访问场景。

- ⚡ **引擎增强**：JindoCache 新增 **RDMA** 支持与 Master 崩溃自愈；JuiceFS 支持 Worker `volumeClaimTemplates`；DataProcess 新增 **Cron 定时**与 **OnEvent 事件**触发策略。

- ✅ **稳定性大提升**：**240+ 项测试与 E2E 改进**，测试套件全面迁移 Ginkgo v2，关键路径引入冲突重试，修复并发数据竞争与控制器 panic。

- 🛡️ **安全加固**：控制面最小权限 RBAC、GitHub Actions 权限收敛、修复 addons 路径注入、基础镜像升级至 alpine 3.23.3、移除硬编码时区。

- 🗑️ **弃用**：GooseFSRuntime 正式废弃。

## 快速开始

```shell
helm repo add fluid https://fluid-cloudnative.github.io/charts
helm repo update
helm install fluid fluid/fluid --version 1.1.0 -n fluid-system
```

从「为每种缓存系统写一个控制器」到「用一份模板接入任意缓存系统」，Fluid 1.1.0 让云原生数据编排的可扩展性迈上新台阶。欢迎试用、反馈，并把你的缓存引擎接入 CacheRuntime 生态！🎉

> GitHub：[github.com/fluid-cloudnative/fluid](https://github.com/fluid-cloudnative/fluid) · 完整技术解读见《深度解读 Fluid 1.1.0》。
