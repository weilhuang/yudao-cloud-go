# 开发手册

## 找到要改的地方

从一个 API 路径出发，在 `internal/platform/app/app.go` 找路由挂载，然后去对应的 `internal/system/<功能>` 或 `internal/infra/<功能>`。先读 `handler` 看输入输出，再读 `service/usecase` 看规则，最后读 `mysql/redis` 看持久化。`cmd/*` 只负责选入口；配置解析在 `internal/platform/config`。

开始前先看[兼容性说明](../usage/compatibility.md)：Java 返回字段、错误码、数据权限和缓存失效不一定能从 Go 类型推出来。确定冻结 Java 基线中的预期行为，再改 Go。

## 一次改一个小切片

1. 写明这次要支持的一个场景、失败场景和不在本次做的部分。
2. 把 HTTP/RPC 输入输出放在 handler，把规则放在用例，把 SQL/Redis 调用放在适配器；接口由用例按需要定义。
3. 为规则写单元测试；涉及 SQL、缓存、服务发现或真实 HTTP 时写有价值的集成测试。
4. 跑本页命令；在 PR 描述中写明改动、验证和剩余风险，提交时写清楚 Git message。公开协作不依赖维护者的本机工作区。

简单的接口不用为了分层而制造空壳。判断标准是：业务用例能不启动 Gin/MySQL 就被测试；持久化细节可以替换；同一个配置和路由装配没有散落在三个 `cmd` 里。

## 本地验证

```bash
gofmt -w <你改过的 Go 文件>
go mod verify
go test -count=1 ./...
go vet ./...
go build ./cmd/...
go test -tags=integration -p 1 -parallel 1 -count=1 -timeout 45m ./...
```

集成测试使用 Testcontainers，会真正启动容器。改 RPC 或 Java 兼容行为时，再按[测试手册](testing.md)跑混跑测试。CI 与本地单元测试通过只是进入评审的起点。

## 注释与提交

注释说“为什么这样做”和“踩过什么坑”，不要把代码逐行翻译成中文。公开 API、容易误用的配置、跨 Java/Go 的约定需要注释。文档也用能读懂的日常中文，技术标识符保持原样。

提交标题采用 `type(scope): 具体技术动作`，例如 `fix(config): 启动前校验数据源加密密钥` 或 `test(auth): 覆盖跨租户令牌刷新`。避免“补上”“补齐”等看不出行为边界的表述；正文写合同、测试和限制。不要把密钥、个人路径、生产日志或整库导出加入提交。仓库的贡献流程见 [CONTRIBUTING.md](https://github.com/weilhuang/yudao-cloud-go/blob/main/CONTRIBUTING.md)。

想看依赖方向如何从上游模板迁移到这里，先读[架构专题](architecture/01-principles.md)和[真实请求路径](architecture/03-request-walkthrough.md)；想跟着写一条切片，读[功能切片实战](../refactor/09-feature-slice.md)。
