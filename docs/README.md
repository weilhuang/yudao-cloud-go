# 文档导航

`yudao-cloud-go` 是面向芋道 Vben 管理端的 Go 后台管理基础服务：`system` 负责登录、权限、租户和通知，`infra` 负责文件、配置、代码生成与运行日志。它们与继续运行的 Java 业务服务组成可受控切换、可回滚的异构系统。当前以 `yudao-cloud-mini` v2026.08 的两个基础模块为固定对照；[能力对照](usage/capability-comparison.md)按功能列出已有实现，[兼容范围](usage/compatibility.md)说明验收边界。这里的文档按读者要做的事分成三部分。

| 部分 | 你要解决的问题 | 入口 |
| --- | --- | --- |
| **1. 使用说明** | 启动、配置、部署、排错，评估能否接流量 | [进入使用说明](usage/index.md) |
| **2. 开发说明** | 找到功能代码，理解架构，补测试并参与项目 | [进入开发说明](development/index.md) |
| **3. 重构过程** | 按小任务从 Java 基线逐步写出 Go 实现 | [进入重构过程](refactor/index.md) |

第一次使用建议先完成[本机快速开始](usage/getting-started.md)，再按自己的目标进入开发或重构章节。重构过程提供一条可以跟做的路径，是现有基础服务之外的附加价值；项目本身的目标是让 Go 基础服务接入保留 Java 业务服务的系统。

可以直接阅读[在线文档](https://weilhuang.github.io/yudao-cloud-go/)。本机预览则进入仓库根目录的 `website/`，运行 `npm ci` 和 `npm run docs:dev`，步骤见[文档站维护](development/site.md#本机预览)。初始化 SQL 从固定 Java 基线获取，导入命令见[快速开始](usage/getting-started.md)的第 3 步。

公开仓库的 `docs/` 保存 Markdown，`website/` 保存 VitePress 站点工程。维护者的逐轮审查、内部任务和阶段性测试记录保存在独立的重写工作区，不会复制进公开文档站。公开协作请在 PR 描述中说明改动和证据。
