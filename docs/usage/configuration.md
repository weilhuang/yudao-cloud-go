# 配置一览

加载顺序是进程内默认值 → `YUDAO_CONFIG` 指定的 YAML → 环境变量。入口分别为 `cmd/server`、`cmd/system`、`cmd/infra`。没有传 `YUDAO_CONFIG` 时仍要提供必填环境变量；推荐显式指定对应的 YAML，便于读代码时知道用了哪套端口和服务名。

| 变量 | 用途 | 本机例子 |
| --- | --- | --- |
| `YUDAO_CONFIG` | 选择 YAML 文件 | `configs/config.yaml` |
| `YUDAO_MYSQL_DSN` | Go MySQL DSN，必填 | 由 `generate-dev-env.sh` 生成 |
| `YUDAO_MYBATIS_ENCRYPTOR_PASSWORD` | 兼容 Java 加密字段的 AES 密钥，必填，16/24/32 字节 | 本机随机生成 |
| `YUDAO_REDIS_ADDR` / `YUDAO_REDIS_PASSWORD` / `YUDAO_REDIS_DB` | 缓存与令牌数据 | `127.0.0.1:6379` / 空 / `0` |
| `YUDAO_HTTP_ADDR` | HTTP 监听地址 | `:48080` |
| `YUDAO_NACOS_ENABLED` / `YUDAO_NACOS_ADDR` | 模块部署的服务注册 | `true` / `127.0.0.1:8848` |
| `YUDAO_NACOS_NAMESPACE` / `YUDAO_NACOS_GROUP` | 服务发现范围 | `dev` / `DEFAULT_GROUP` |
| `YUDAO_NACOS_SERVICE_NAME` | Java 网关查找的服务名 | `system-server` 或 `infra-server` |
| `YUDAO_NACOS_REGISTER_IP` / `YUDAO_NACOS_REGISTER_PORT` | 对外通告的地址 | 部署时填网关可达地址 |
| `YUDAO_JAVA_CACHE_PREFIX` | Java Spring Cache 键的可选前缀 | 与同库 Java 实例一致 |

## 环境变量完整索引

下面按 `internal/platform/config/config.go` 的 `applyEnv` 整理。未设置时先用入口默认值，再由所选 YAML 覆盖；显式设置空字符串的字段可能覆盖 YAML，因此生产环境要把配置来源收敛到一处。表中的“默认”只指本机代码/示例，不能直接理解为安全生产配置。

| 组 | 变量 | 说明与校验 |
| --- | --- | --- |
| 进程 | `YUDAO_APP_NAME`、`YUDAO_HTTP_ADDR` | 服务名和监听地址；三个入口分别有默认名/端口 |
| HTTP | `YUDAO_HTTP_UPLOAD_MAX_FILE_BYTES`、`YUDAO_HTTP_UPLOAD_MAX_REQUEST_BYTES` | 默认 16/32 MiB；整请求不能小于单文件 |
| HTTP | `YUDAO_HTTP_READ_TIMEOUT_SECONDS` | 默认 120 秒，覆盖请求头与请求体 |
| MySQL | `YUDAO_MYSQL_DSN` | 必填；不要把真实 DSN 写进 YAML 或日志 |
| Redis | `YUDAO_REDIS_ADDR`、`YUDAO_REDIS_PASSWORD`、`YUDAO_REDIS_DB` | 默认本机地址和 DB 0；DB 必须是非负整数 |
| Nacos | `YUDAO_NACOS_ENABLED`、`YUDAO_NACOS_ADDR`、`YUDAO_NACOS_GRPC_PORT` | 模块入口默认启用；映射端口不是 8848+1000 时显式给 gRPC 端口 |
| Nacos | `YUDAO_NACOS_NAMESPACE`、`YUDAO_NACOS_GROUP`、`YUDAO_NACOS_SERVICE_NAME` | 网关发现范围与服务名；打开时 group、serviceName 必填 |
| Nacos | `YUDAO_NACOS_REGISTER_IP`、`YUDAO_NACOS_REGISTER_PORT` | 注册给网关的地址与端口；不能只填容器内可达地址 |
| Nacos | `YUDAO_NACOS_USERNAME`、`YUDAO_NACOS_PASSWORD` | 有鉴权的 Nacos 环境使用；本机 Compose 不启用鉴权 |
| Nacos | `YUDAO_NACOS_VERSION`、`YUDAO_NACOS_TAG` | 服务发现元数据，不是 Git release 版本号；与现有网关灰度规则核对 |
| 登录 | `YUDAO_CAPTCHA_ENABLE`、`YUDAO_BCRYPT_COST` | 验证码开关；成本 0 表示使用 bcrypt 默认成本 |
| 短信 | `YUDAO_SMS_CODE_BEGIN`、`YUDAO_SMS_CODE_END` | 两项要一起设；均为 0 时生成 100000–999999 的随机六位码 |
| 加密 | `YUDAO_MYBATIS_ENCRYPTOR_PASSWORD` | 必填；16/24/32 字节，读取旧 Java 密文须与原密钥一致 |
| 缓存 | `YUDAO_JAVA_CACHE_PREFIX` | `platform/app` 读取，用于与 Java Spring Cache 的前缀对齐 |

`Config.Validate()` 会在连接数据库前拦住明显错误，但不会证明线上拓扑安全。配置加载测试在 `internal/platform/config/config_test.go`；部署时还要实际检查网关能否访问注册 IP、Redis 键是否与 Java 一致、密钥是否能解旧密文。

## 一个配置怎样生效

```mermaid
flowchart LR
    Entry[cmd/server 或 cmd/system 或 cmd/infra] --> Default[入口默认值]
    Default --> YAML[YUDAO_CONFIG 指向的 YAML]
    YAML --> Env[环境变量覆盖]
    Env --> Validate[Config.Validate]
    Validate --> Start[app.Start 连接依赖]
```

本机切换到 `cmd/system` 时，不要继续给 `configs/config.yaml`；应使用 `configs/config-system.yaml`，并先让 Nacos 的 `dev` 命名空间存在。配置示例里的 `127.0.0.1` 只对本机可达，容器或多机部署必须按实际网络填写。

**别把 `.env`、生产 DSN、AES 密钥或 Nacos 口令提交进仓库。** 本机 Compose 的密码只供练习。与 Java 混跑时，数据库、Redis、加密密钥、租户语义和缓存前缀必须逐项对齐。生产服务应通过受控的密钥管理或部署环境注入，并限制 `/rpc-api/**` 只能被可信服务访问。

如果已有 Java 数据使用的是公开示例密钥，替换成新密钥前要先规划重加密和回滚；直接改配置会让旧密文无法读取。这个仓库不会自动替你迁移生产密文。
