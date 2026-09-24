# 本机跑起来

以下命令在仓库根目录执行，使用一套**空的本机开发数据库**。SQL 里有 `DROP TABLE`，绝不能导入已有业务数据的库。

## 1. 准备工具

需要 Go 1.24.3、Git、OpenSSL、Docker Desktop 和 Docker Compose。先确认 `go version`、`docker compose version`、`docker info` 都能正常返回。MySQL 的 `3306`、Redis 的 `6379` 和 Go 的 `48080` 端口要空闲。代码暂未提供生产数据库迁移器；本机用冻结 Java 基线的 SQL 初始化。

## 2. 生成本机配置并启动依赖

```bash
./scripts/generate-dev-env.sh
set -a; . ./.env; set +a
docker compose -f deploy/docker-compose.yaml up -d --wait mysql redis
```

脚本只创建新的 `.env`，不会覆盖已有文件。MySQL 与 Redis 只绑定 `127.0.0.1`。如果还要练习拆分部署，再启动 `nacos`：

```bash
docker compose -f deploy/docker-compose.yaml up -d nacos
```

在 Nacos 控制台创建 ID 和名称均为 `dev` 的命名空间，然后再运行 `cmd/system` 或 `cmd/infra`。Nacos Compose 配置仅限本机实验，默认没有认证。

## 3. 导入冻结 Java 基线 SQL

**SQL 文件在上游仓库的 [`sql/mysql/ruoyi-vue-pro.sql`](https://github.com/yudaocode/yudao-cloud-mini/blob/712fc7c2528e434217833bf04e438bf18a9c96e4/sql/mysql/ruoyi-vue-pro.sql)，不在本 Go 仓库。**它包含演示用户、手机号、邮件等样例数据，所以本仓库没有复制整库 SQL。下面从上游的固定提交取文件。目标库必须是第 2 步新建的空库；如果你已用这套容器保存了自己的数据，先不要执行导入。

```bash
git clone --branch master-jdk25 https://github.com/yudaocode/yudao-cloud-mini.git ../yudao-cloud-mini-baseline
git -C ../yudao-cloud-mini-baseline checkout 712fc7c2528e434217833bf04e438bf18a9c96e4
docker compose -f deploy/docker-compose.yaml exec -T -e MYSQL_PWD="$YUDAO_DEV_MYSQL_PASSWORD" mysql \
  mysql -uroot ruoyi-vue-pro < ../yudao-cloud-mini-baseline/sql/mysql/ruoyi-vue-pro.sql
```

可用 `docker compose -f deploy/docker-compose.yaml ps` 查看依赖状态。若已有上游仓库，可直接读取同一 SHA 的 SQL，不必重新 clone。

## 4. 启动 Go

```bash
YUDAO_CONFIG=configs/config.yaml go run ./cmd/server
```

另开终端验证：

```bash
curl --fail http://127.0.0.1:48080/health
```

没有数据库或 Redis 时进程会直接报错退出。`/health` 成功表示进程与启动依赖已连通，不表示所有 Java 接口和线上数据都通过验收。

你可以把这个流程当成三道检查：`docker compose ps` 证明依赖容器健康，`go run` 证明配置与装配能启动，`/health` 证明 HTTP 可访问。接下来若要验证登录、权限和页面，请按[测试手册](../development/testing.md)准备固定 Vben 前端与隔离数据；不要用健康检查替代业务验收。

管理端源码在 `ui/yudao-ui-admin-vben/` Git 子模块中。初始化命令、Node.js/pnpm 要求和本机代理地址见[前端联调](frontend.md)。只跑 Go 服务时不需要下载子模块。

本机脚本会随机生成 `YUDAO_MYBATIS_ENCRYPTOR_PASSWORD`。它适合新建的开发数据；如果要读取**已有 Java 加密的数据源密码**，必须从你自己管理的安全配置中提供与那套 Java 数据一致的 AES 密钥，不要把密钥写进 Git。

## 常见卡点

| 现象 | 先看哪里 |
| --- | --- |
| `mysql.dsn 不能为空` | 当前终端是否执行了 `set -a; . ./.env; set +a` |
| `mybatis.encryptorPassword 不能为空` | `.env` 是否由脚本生成，密钥是否被导入环境 |
| `bind: address already in use` | 本机对应端口是否被现有 MySQL/Redis/Go 占用；改 Compose 端口时也要改 DSN |
| Nacos 注册失败 | 命名空间 `dev` 是否存在，`8848/9848` 是否可用 |
| Docker 无法拉镜像 | 先确认 Docker Desktop 本身可用，再检查网络和镜像源 |

更多按现象排查的步骤见[排错手册](troubleshooting.md)。如果你已有本机 MySQL/Redis，可以不启动 Compose，但必须让 DSN、Redis 地址和密钥指向你明确准备的隔离环境。

停止开发容器用 `docker compose -f deploy/docker-compose.yaml stop`。MySQL 使用命名卷保存数据；清理数据卷会丢失本机数据，这份指南不自动做这一步。
