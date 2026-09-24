# 使用说明

这里回答三个实际问题：怎样把服务跑起来、怎样选择部署入口、怎样判断它能否接你的流量。当前 `0.0.1` 适合本机开发和隔离环境联调；接入线上前必须完成[兼容性验收](compatibility.md)。

## 从哪里开始

| 你要做的事 | 读什么 | 完成后的检查 |
| --- | --- | --- |
| 第一次启动 | [快速开始](getting-started.md) | MySQL/Redis 健康、Go 启动、`/health` 返回成功 |
| 用管理端联调 | [固定 Vben 前端](frontend.md) | `ui/yudao-ui-admin-vben/` 位于固定提交，页面请求到本机 Go 服务 |
| 接自己的数据库与缓存 | [配置说明](configuration.md) | 必填环境变量齐全，密钥与 Java 旧数据匹配 |
| 选择单进程或拆分部署 | [部署与回滚](deployment.md) | Gateway/Nacos 服务名、网络和回滚路径已核对 |
| 遇到启动或接口问题 | [排错手册](troubleshooting.md) | 能复现并定位到配置、依赖或契约 |
| 评估 Java 替换 | [兼容范围](compatibility.md)、[安全边界](security-model.md) | Java/Go 混跑、Vben 页面、权限与回滚留下证据 |

## 当前服务范围

本仓库先以 `yudao-cloud-mini` v2026.08（JDK 25）的 system、infra 为固定 Java 对照，沿用官方 Vben 管理端。它提供单进程入口 `cmd/server`，也提供 `cmd/system`、`cmd/infra` 两个拆分入口；拆分模式继续使用现有 Java Gateway。完整芋道 Cloud 的其他业务模块还不在本阶段范围内。

```mermaid
flowchart LR
    A[本机试用] --> B[快速开始]
    B --> C[配置和单进程]
    C --> D[接口联调]
    D --> E[拆分部署与混跑]
    E --> F[回滚演练]
    F --> G[是否允许切流]
```

`/health` 成功只是第一步。要判断能否接流量，还要核对响应字段、租户与数据权限、缓存副作用、Java Feign 调用和失败路径。测试层级与命令放在[开发说明的测试章节](../development/testing.md)。
