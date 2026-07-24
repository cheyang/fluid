# Fluid 1.1.0 发布博客素材

本目录包含 Fluid 1.1.0 的对外宣传文章与配图。

## 文章

| 文件 | 说明 |
| --- | --- |
| [announcement-zh.md](./announcement-zh.md) | 发布摘要 / 公告（中文） |
| [announcement-en.md](./announcement-en.md) | Release announcement（English） |
| [deep-dive-zh.md](./deep-dive-zh.md) | 功能详解 · 技术博客（中文，含内嵌架构图） |
| [architecture-zh.md](./architecture-zh.md) | CacheRuntime 架构图 + 图例说明 |

## 配图

每张图均提供 `.mmd`（Mermaid 源码，可编辑重渲染）、`.svg`（矢量，网页/文档）、`.png`（高清 3× 缩放，PPT / 公众号 / 社媒）三种格式。

| 图 | 用途 | 文件前缀 |
| --- | --- | --- |
| CacheRuntime 架构（竖版，中文） | 博客内嵌 | `cacheruntime-arch` |
| CacheRuntime 架构（English） | 国际社区 / CNCF Blog | `cacheruntime-arch-en` |
| CacheRuntime 架构（横版 16:9） | PPT 幻灯片 | `cacheruntime-arch-landscape` |
| AI 数据加速链路 | 博客 §2 / 演讲 | `ai-data-pipeline-zh` |
| 2026 Roadmap | 博客 §8 / 演讲 | `roadmap-2026-zh` |

## 重新渲染

修改 `.mmd` 源码后，用 [mermaid-cli](https://github.com/mermaid-js/mermaid-cli) 重新导出：

```shell
# SVG（透明背景）
npx -y @mermaid-js/mermaid-cli -i <name>.mmd -o <name>.svg -b transparent
# PNG（白底，3× 高清）
npx -y @mermaid-js/mermaid-cli -i <name>.mmd -o <name>.png -b white -s 3
```

也可直接把 `.mmd` 内容粘贴到 [mermaid.live](https://mermaid.live) 在线预览与导出。

## 配色约定

| 层 / 角色 | 颜色 |
| --- | --- |
| 声明式 API（CRD） | 蓝 `#E8F0FE` / `#4285F4` |
| 控制器 Controller | 黄 `#FEF7E0` / `#F9AB00` |
| 数据面 Workloads / Fluid 缓存 | 绿 `#E6F4EA` / `#34A853` |
| 底层存储 UFS | 灰 `#F1F3F4` / `#5F6368` |
| 应用 / 数据操作 | 红 `#FCE8E6` / `#EA4335` |
| Roadmap / 未来项 | 紫 `#F3E8FD` / `#A142F4` |
