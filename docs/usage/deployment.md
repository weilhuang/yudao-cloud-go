# 部署方式与回滚思路

项目希望让 Java 与 Go 在同一芋道 Cloud 系统内共存，再按服务或接口逐步切换并能回滚。`deploy/docker-compose.yaml` 只启动本机依赖，不是生产清单。生产环境至少要单独决定密钥来源、数据库备份、内网边界、日志、监控、服务发现和回滚。当前代码仍处在开发版；本页说明现有入口如何工作，不等于动态切流已经实现或允许直接切线上流量。

## 单进程

`cmd/server` 对应 Java 单体 `yudao-server`。它在一个进程里注册 system 与 infra 的已实现路由，默认 `:48080`，不连接 Nacos。可以先在隔离环境里用它跑前端联调：

```bash
YUDAO_CONFIG=configs/config.yaml go run ./cmd/server
```

需要配置 `YUDAO_MYSQL_DSN`、`YUDAO_MYBATIS_ENCRYPTOR_PASSWORD` 和 Redis；端口、上传限制等见[配置说明](configuration.md)。不要让生产数据库与本机开发 Compose 共用口令。

## 拆分进程

`cmd/system` 与 `cmd/infra` 默认分别监听 `:48081`、`:48082`，注册名必须是 `system-server`、`infra-server`。Java Gateway 按现有路由发现它们；Go 项目目前不重写 Gateway。两个进程共享 MySQL/Redis，Nacos 的 namespace、group、注册 IP/端口必须能被网关实际访问。

```bash
YUDAO_CONFIG=configs/config-system.yaml go run ./cmd/system
YUDAO_CONFIG=configs/config-infra.yaml go run ./cmd/infra
```

先在预生产确认网关转发、`/admin-api/**` 响应、`/rpc-api/**` 的受信网络边界，以及 Java 模块对 Go 的调用。`tenant-id` 请求头不等于内部调用鉴权，不要把 RPC 端口直接开放到公网。

## 切流与回滚检查表

1. 冻结 Java/Go/Vben 的 commit SHA、数据库 schema、配置和密钥版本；完成备份与恢复演练。
2. 用同一套预生产数据跑 Java/Go 对照、浏览器 E2E、权限/租户/短信/文件负向用例。
3. 只在服务发现与网关可观察、能摘除实例的前提下做小流量切换，监看错误率、慢请求、MySQL/Redis/Nacos。
4. 回滚时先停止往 Go 实例分流，确认 Java 实例健康，再核对共享数据库写入与缓存失效。若 schema 或数据格式改变，必须有单独的数据回滚方案。

项目尚未完成这张表；[兼容性说明](compatibility.md)记录当前阻塞项。压测是功能与回滚确认后的步骤。
