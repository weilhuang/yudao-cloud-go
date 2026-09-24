# 排错手册

先判断问题发生在哪一步：配置解析、依赖连接、HTTP 请求、Java/Go 契约，还是文档构建。下面的命令都在仓库根目录执行；不要把包含口令的完整环境变量或 DSN 贴到公开 Issue。

## 进程启动失败

| 报错或现象 | 常见原因 | 先做什么 |
| --- | --- | --- |
| `mysql.dsn 不能为空` | 当前终端没有加载 `.env` | 检查文件存在后运行 `set -a; . ./.env; set +a`；只显示变量名，别打印口令 |
| `mybatis.encryptorPassword 不能为空` | 未设置 AES 密钥 | 本机运行 `./scripts/generate-dev-env.sh`；已有 `.env` 时不要覆盖 |
| MySQL/Redis 连接失败 | 容器未健康、端口被占、DSN 指向别的实例 | 运行 `docker compose -f deploy/docker-compose.yaml ps` 与 `docker info` |
| `bind: address already in use` | `48080`、`48081` 或 `48082` 已被占用 | 查看本机端口占用，再调整 `YUDAO_HTTP_ADDR` |
| Nacos 注册失败 | `dev` 命名空间不存在、8848/9848 不通、注册 IP 不可达 | 单体先确认能运行；拆分时核对 Nacos 命名空间、gRPC 端口和网关可达地址 |

启动前的 `Config.Validate()` 会拒绝缺少 DSN、密钥和明显错误的端口/大小配置。进程不提供 `/health` 是有意的：依赖没有连通时不应报告健康。

## 健康检查成功，页面仍然报错

`/health` 只检查进程启动与依赖连接。先从浏览器 Network 面板记下实际路径、方法、状态、`code/msg` 和租户请求头，再到 `internal/platform/app/app.go` 查对应路由是否挂在当前入口。单体用 `cmd/server`；拆分时 `system` 与 `infra` 路由不能打错端口。

如果是菜单/按钮看不到，核对用户权限、角色菜单缓存和前端固定版本。若是列表与导出结果不一致，重点核对数据范围、租户条件和 Java 基线，不要只数接口路径。

## 集成测试没有真正执行

普通 `go test ./...` 不包含 `integration` build tag。运行：

```bash
docker info
go test -tags=integration -p 1 -parallel 1 -count=1 -timeout 45m ./...
```

若想验证 Java/Go 混跑，再给 [测试手册](../development/testing.md)中的三个 Java 路径变量，并加 `-v` 看有没有 `SKIP`。Testcontainers 拉镜像失败时先确认 Docker Desktop 和网络，不能把“跳过”记成“通过混跑”。

## VitePress 页面或链接不对

从仓库根目录进入 `website/` 后运行：

```bash
cd website
npm ci
npm run docs:build
npm run docs:preview
```

仓库名改变时同时改 `website/.vitepress/config.ts` 中的 `base` 与图标路径。新文章要加入侧边栏；本地预览检查亮色、暗色和窄屏。构建通过只证明静态资源可生成，GitHub Pages 仍要看 Actions 和实际网页。

## 发 Issue 前

提供 Go commit、运行入口、配置变量名（不要值）、最小请求、预期与实际、测试命令和是否跳过。日志、截图和数据库样本要先脱敏；安全漏洞走[私密报告](https://github.com/weilhuang/yudao-cloud-go/blob/main/SECURITY.md)。
