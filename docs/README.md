# 文档导航

`yudao-cloud-go` 面向芋道 Cloud 的 Go 后端重构。当前以 `yudao-cloud-mini` v2026.08 的 system、infra 为第一阶段对照范围；版本状态与缺口见[兼容范围](usage/compatibility.md)。这里的文档按读者要做的事分成三部分。

| 部分 | 你要解决的问题 | 入口 |
| --- | --- | --- |
| **1. 使用说明** | 启动、配置、部署、排错，评估能否接流量 | [进入使用说明](usage/index.md) |
| **2. 开发说明** | 找到功能代码，理解架构，补测试并参与项目 | [进入开发说明](development/index.md) |
| **3. 重构过程** | 按小任务从 Java 基线逐步写出 Go 实现 | [进入重构过程](refactor/index.md) |

第一次使用建议先完成[本机快速开始](usage/getting-started.md)，再按自己的目标进入开发或重构章节。重构过程提供一条可以跟做的路径，是现有服务之外的附加价值；项目本身的目标是形成能够通过线上替换验收的 Go 后端。

要在本机打开这份文档站，先进入仓库根目录的 `website/`，再运行 `npm ci` 和 `npm run docs:dev`，步骤见[文档站维护](development/site.md#本机预览)。要初始化 Go 服务的数据库，SQL 文件位置和导入命令见[快速开始](usage/getting-started.md)的第 3 步。

公开仓库的 `docs/` 保存 Markdown，`website/` 保存 VitePress 站点工程。维护者的逐轮审查、内部任务和阶段性测试记录保存在独立的重写工作区，不会复制进公开文档站。公开协作请在 PR 描述中说明改动和证据。
