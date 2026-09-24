# 04：当前实现的取舍与改进顺序

整洁架构不是把所有文件搬成五个顶层目录。它的收益来自：规则和外部细节能分开测试，换入口/存储时不用重写核心行为。这个项目还要与固定 Java 基线兼容，因而必须把**外部合同**与**内部设计**同时看清。

## 不能随意改的外部合同

| 合同 | 例子 | 改内部结构时的检查 |
| --- | --- | --- |
| HTTP | `/admin-api/**`、`code/msg/data`、长整数、时间 | 固定 Vben 请求和失败页面 |
| RPC | `/rpc-api/**`、`tenant-id`、Feign 返回 | Java→Go 混跑与网络隔离 |
| 数据 | 表结构、租户字段、软删除、AES 密文 | 旧数据读取与写后回滚 |
| 缓存 | Redis 令牌、角色/菜单失效 | Java/Go 共享缓存的并发修改 |
| 部署 | 单体、Nacos 模块名、Gateway 路由 | 三个入口和真实服务发现 |

这些合同可以成为 handler/adapter 层的输入输出；不需要把 Java DTO、SQL 行或 Gin 类型塞进每个用例。跨层的 DTO 尽量用简单结构表示业务需要的数据，再在边缘转换。

## 现在有哪些例外

| 包 | 已核对的现状 | 可验证的重构步骤 |
| --- | --- | --- |
| `internal/infra/demo01`～`demo03` | `Service` 直接持有 `*sql.DB` | 先为一个 CRUD 提取 Store 接口，保持事务与租户测试 |
| `internal/infra/codegen` | `Service` 处理表探查、事务和生成规则 | 分出元数据读取、定义仓储、模板生成；比较生成文件 |
| `internal/system/sendrpc` | 收件人查询有接口，发送编排仍在 handler | 为发送流程增加用例，不改变 RPC 路径和返回 |
| `internal/platform/app/app.go` | 装配点集中且较长 | 按功能抽注册函数，保持三个入口的路由差异测试 |

这些例外在 0.0.1 中存在。不能因为有 `handler.go` 和 `service.go` 就说整仓已严格满足原理；也不能在没有 Java 对照和测试时做大规模搬家。

## 建议的改进顺序

```mermaid
flowchart LR
    A[固定一个 Java 合同] --> B[补足失败与跨租户测试]
    B --> C[提取用例需要的小接口]
    C --> D[移动 SQL/SDK 细节到适配器]
    D --> E[单测 + 集成 + Java 对照]
    E --> F[更新文档和提交]
```

先处理会影响权限、安全或契约一致性的切片；再处理纯目录整洁。每次只改一个可核对的行为，查看 `git diff` 和测试结果。更完整的生产替换门槛见[兼容性说明](../../usage/compatibility.md)。架构资料来源：[上游模板](https://github.com/amitshekhariitbhu/go-backend-clean-architecture)、[作者讲解](https://outcomeschool.com/blog/go-backend-clean-architecture)、[Clean Architecture 原文](https://blog.cleancoder.com/uncle-bob/2012/08/13/the-clean-architecture.html)、[Go 模块布局](https://go.dev/doc/modules/layout)。
