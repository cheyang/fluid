# Fluid CacheRuntime 架构图

下图展示 Fluid 1.1.0 **CacheRuntime 通用缓存引擎框架**的整体架构：从声明式 API（`CacheRuntimeClass` 模板 + `CacheRuntime` 实例 + `Dataset`），经由 Fluid 控制器 Reconcile 与「Spec → Pod」转换，编排出 Master / Worker / Client 三类组件，并打通应用 Pod → FUSE 客户端 → Worker 缓存 → 底层存储（UFS）的数据链路。

```mermaid
graph TB
    subgraph API["① 声明式 API 层"]
        direction LR
        Class["CacheRuntimeClass<br/><i>引擎方定义的模板</i><br/>拓扑 · 挂载入口 · 数据操作规格"]
        CR["CacheRuntime<br/><i>使用方定义的实例</i><br/>副本 · 分层存储 · 资源"]
        DS["Dataset<br/>指向 UFS 挂载点"]
    end

    Ctrl["② Fluid CacheRuntime Controller<br/>Reconcile　+　Spec → Pod 转换"]

    subgraph Data["③ 数据面 · Workloads"]
        direction LR
        Master["Master · StatefulSet<br/>元数据 / Headless Service"]
        Worker["Worker · AdvancedStatefulSet<br/>分层存储 Mem/SSD/HDD<br/>原地升级 (in-place)"]
        Client["Client · DaemonSet<br/>FUSE · POSIX 挂载"]
    end

    App["应用 Pod<br/>训练 / 推理 / 分析"]
    UFS[("底层存储 UFS<br/>S3 · OSS · HDFS · MinIO")]
    DL["DataLoad / DataProcess<br/>预热 · 数据处理"]

    Class --> Ctrl
    CR --> Ctrl
    DS --> Ctrl
    Ctrl ==> Master
    Ctrl ==> Worker
    Ctrl ==> Client

    Master -. 元数据协调 .-> Worker
    App -->|POSIX 读写| Client
    Client -->|缓存命中| Worker
    Worker -->|未命中回源| UFS
    DL -->|预热写入| Worker
    DL -. 拉取 .-> UFS
    Worker -. 缓存位置<br/>亲和调度 .-> App

    classDef api fill:#E8F0FE,stroke:#4285F4,stroke-width:1px,color:#1a1a1a;
    classDef ctrl fill:#FEF7E0,stroke:#F9AB00,stroke-width:2px,color:#1a1a1a;
    classDef data fill:#E6F4EA,stroke:#34A853,stroke-width:1px,color:#1a1a1a;
    classDef store fill:#F1F3F4,stroke:#5F6368,stroke-width:1px,color:#1a1a1a;
    classDef app fill:#FCE8E6,stroke:#EA4335,stroke-width:1px,color:#1a1a1a;

    class Class,CR,DS api;
    class Ctrl ctrl;
    class Master,Worker,Client data;
    class UFS store;
    class App,DL app;
```

## 图例说明

| 层 | 角色 | 说明 |
| --- | --- | --- |
| **① 声明式 API** | `CacheRuntimeClass` | 由**引擎/平台方**定义一次的模板：文件系统类型、组件拓扑、UFS 挂载与状态上报入口、数据操作规格 |
| | `CacheRuntime` | 由**使用方**创建的实例：引用某个 Class，填入副本数、分层存储、资源等运行时参数 |
| | `Dataset` | 声明底层存储挂载点与访问凭据，与 CacheRuntime 绑定 |
| **② 控制面** | CacheRuntime Controller | Reconcile 上述 CRD，并将 Spec 声明式转换为各组件工作负载；最小权限 RBAC |
| **③ 数据面** | Master | 元数据与协调，StatefulSet + Headless Service |
| | Worker | 缓存数据存储，AdvancedStatefulSet 支持原地升级；支持内存/SSD/HDD 分层存储 |
| | Client | FUSE 客户端，DaemonSet 形式提供 POSIX 挂载 |
| **数据链路** | App → Client → Worker → UFS | 应用经 FUSE 读写，命中走 Worker 缓存，未命中回源 UFS；缓存位置信息用于数据亲和调度 |
| | DataLoad / DataProcess | 将 UFS 数据预热进 Worker 缓存或执行数据处理 |

> 本图为 Mermaid 源码，可在 GitHub、VS Code、mermaid.live 等直接渲染，也可导出为 SVG/PNG 作为文章配图。
