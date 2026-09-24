# 怎么证明改动能用

不同测试回答不同问题。比如一个用例的判断条件可以在单元测试里验证；MySQL 事务和 Redis 缓存需要真实依赖；Java/Go 混跑、Vben 浏览器流程和回滚则要在目标拓扑里测。

```mermaid
flowchart LR
    U[单元测试：规则] --> I[集成测试：MySQL / Redis / Nacos]
    I --> M[混跑：Java + Go 共享依赖]
    M --> E[浏览器 E2E：固定 Vben]
    E --> P[预生产：配置 / 回滚 / 压测]
```

## 日常命令

```bash
go mod verify
go test -count=1 ./...
go vet ./...
go build ./cmd/...
```

`go test ./...` 默认不运行带 `integration` build tag 的文件。要跑 MySQL、Redis、Nacos 的真实集成测试，先启动 Docker，再执行：

```bash
go test -tags=integration -p 1 -parallel 1 -count=1 -timeout 45m ./...
```

`-p 1 -parallel 1` 避免本机同时拉起过多容器。测试自己创建和清理容器，不能把“能编译”当成“容器验证通过”。CI 也分开跑这两层。

## Java/Go 混跑

`internal/platform/app/java_mix_integration_test.go` 用独立的 MySQL/Redis 运行两种实现。它要求本机已有冻结基线构建出的 Java JAR 与 SQL，并设置 `YUDAO_JAVA_JAR`、`YUDAO_JAVA_SQL`、`YUDAO_JAVA_HOME`：

```bash
YUDAO_JAVA_JAR=/path/to/yudao-server.jar \
YUDAO_JAVA_SQL=/path/to/ruoyi-vue-pro.sql \
YUDAO_JAVA_HOME=/path/to/jdk25 \
go test -tags='integration java_integration' -p 1 -parallel 1 -count=1 -timeout 45m ./internal/platform/app
```

未设置这三个变量时测试会跳过，所以看最终输出时要检查是否有 `SKIP`。一次混跑测试也不能覆盖所有接口；新增的 Java 兼容功能要给出对应请求、数据和响应对照。

## 线上替换前还缺什么

- 用冻结的 Vben 前端走登录、权限、部门、文件、消息和失败流程，记录浏览器请求与页面结果。
- 核对仍在运行的 Java 模块通过 Feign/RPC 调用 Go 时的权限、租户、缓存失效和异常返回。
- 在与线上一致的网络、密钥、数据库版本和服务发现配置中演练切流与回滚。
- 功能正确率先过关，再在同样数据和负载下比较 Java/Go 的延迟、错误率与资源消耗。

这些项目没有证据前，版本不能宣称已平替或可上线。当前已知范围和缺口见[兼容性说明](../usage/compatibility.md)。

## 0.0.1 的检查记录怎样读

| 检查 | 能说明什么 | 不能说明什么 |
| --- | --- | --- |
| `go test -count=1 ./...` | 包内规则和 HTTP 测试通过 | 真实 MySQL/Redis/Nacos 一定兼容 |
| `go test -tags=integration ...` | 临时容器中的数据库、缓存、Nacos 行为 | Java Gateway、生产网络和旧数据可用 |
| `go test -race ...` | 被选中包的并发路径未检出竞态 | 全项目不存在任何并发问题 |
| 从 `website/` 运行 `npm run docs:build`，再用浏览器检查 `docs:dev` | VitePress 构建与本机页面可显示 | GitHub Pages 已部署并可访问 |
| Java 混跑、Vben、回滚 | 需单独给出命令、数据和结果 | 缺少证据时不能算通过 |

发 PR 或打 tag 时，记录具体 commit、命令、退出码、是否 `SKIP`、运行环境和没有覆盖的范围。尤其要区分“测试通过”和“测试文件因条件不足而跳过”。想自己练习一条合同的证据收集，可读[合同验收练习](../refactor/10-contract-lab.md)。
