<p align="center"><img src="website/public/logo.svg" width="76" height="76" alt="yudao-cloud-go 标志"></p>
<h1 align="center">yudao-cloud-go</h1>
<p align="center"><strong>让芋道 Cloud 的 Java 与 Go 服务共存，并逐步实现可动态替换的异构系统。</strong></p>
<p align="center">先对齐接口与数据契约，再按服务切换流量、观察结果并保留回滚路径。</p>

<p align="center">
  <a href="https://github.com/weilhuang/yudao-cloud-go/actions/workflows/ci.yml"><img alt="Go CI" src="https://github.com/weilhuang/yudao-cloud-go/actions/workflows/ci.yml/badge.svg"></a>
  <a href="https://github.com/weilhuang/yudao-cloud-go/actions/workflows/docs.yml"><img alt="文档站" src="https://github.com/weilhuang/yudao-cloud-go/actions/workflows/docs.yml/badge.svg"></a>
  <a href="LICENSE"><img alt="Apache-2.0" src="https://img.shields.io/badge/license-Apache--2.0-blue"></a>
  <a href="CHANGELOG.md"><img alt="0.0.1 开发预览版" src="https://img.shields.io/badge/version-0.0.1-orange"></a>
</p>

<p align="center">
  <a href="https://weilhuang.github.io/yudao-cloud-go/"><strong>在线文档</strong></a> ·
  <a href="docs/usage/index.md">使用说明</a> ·
  <a href="docs/development/index.md">开发说明</a> ·
  <a href="docs/refactor/index.md">重构过程</a>
</p>

## 项目要解决什么

[yudao-cloud](https://github.com/YunaiV/yudao-cloud) 已有成熟的 Java 服务和管理端。这个项目用 Go 逐步实现兼容的服务能力，目标是在同一系统中让 Java 与 Go **共存、受控切换、能够回滚**：可以先替换一个服务或一组接口，确认契约与数据一致后再扩大范围，而不是要求一次性迁走整个系统。

第一阶段固定 [yudao-cloud-mini](https://github.com/yudaocode/yudao-cloud-mini) **v2026.08 / JDK 25** 的 `system`、`infra` 和对应 [Vben 管理端](https://github.com/yudaocode/yudao-ui-admin-vben)作为对照。运行方式有单进程 `cmd/server`，也有接入现有 Java Gateway 与 Nacos 的 `cmd/system`、`cmd/infra`。Go 侧目前不重写 Gateway。

> **当前状态：`0.0.1` 开发预览版。** 已实现部分接口和两种运行入口，适合本机开发与隔离环境联调。Java/Go 受控切流、完整混跑、Vben 端到端流程、目标环境回滚等还缺验收证据；**目前不能替换线上 Java 服务**。范围和缺口见[兼容性说明](docs/usage/compatibility.md)。

| 目标能力 | 当前进展 | 下一步验收 |
| --- | --- | --- |
| Java 与 Go 服务共存 | 已提供拆分运行入口，可接入现有 Java Gateway 和 Nacos | 验证真实网关、服务发现及跨语言 RPC |
| 按服务或接口切换 | 已实现部分 Java 风格的 HTTP/RPC 接口与数据访问 | 验证路由、权限、租户、事务、缓存和错误行为，再演练小流量切换 |
| 出问题能回滚 | [部署文档](docs/usage/deployment.md)列出切流与回滚检查项 | 在目标环境完成流量回切与共享数据恢复演练 |

## 从哪里开始

| 想做的事 | 入口 |
| --- | --- |
| 了解项目、在线阅读 | [GitHub Pages 文档站](https://weilhuang.github.io/yudao-cloud-go/) |
| 本机运行 Go 服务 | [快速开始](docs/usage/getting-started.md) |
| 选择单进程或拆分部署 | [部署与回滚](docs/usage/deployment.md) |
| 修改代码、了解架构和测试 | [开发说明](docs/development/index.md) |
| 跟着 Java 基线自己写一遍 | [重构过程](docs/refactor/index.md) |

### 本机运行

需要 Go 1.24.3、Docker Desktop、Docker Compose、Git 和 OpenSSL。以下是启动顺序；**数据库 SQL 含 `DROP TABLE`，只能按[快速开始](docs/usage/getting-started.md)导入新建的本机空库**。SQL 使用固定 Java 提交的 [`sql/mysql/ruoyi-vue-pro.sql`](https://github.com/yudaocode/yudao-cloud-mini/blob/712fc7c2528e434217833bf04e438bf18a9c96e4/sql/mysql/ruoyi-vue-pro.sql)，本仓库没有复制整库文件。

```bash
git clone https://github.com/weilhuang/yudao-cloud-go.git
cd yudao-cloud-go
./scripts/generate-dev-env.sh
set -a; . ./.env; set +a
docker compose -f deploy/docker-compose.yaml up -d --wait mysql redis
# 现在按快速开始的第 3 步导入固定版本的 SQL，再启动服务
YUDAO_CONFIG=configs/config.yaml go run ./cmd/server
```

另开终端检查 `curl --fail http://127.0.0.1:48080/health`。健康检查只证明进程和启动依赖可用；接口兼容性要继续按[测试与验收](docs/development/testing.md)验证。

管理端是 `ui/yudao-ui-admin-vben` Git 子模块，固定在上游 v2026.08 的 `486870dd8496d94b79aaa183069df8407de8cbf9`。需要联调时运行 `git submodule update --init ui/yudao-ui-admin-vben`，再按[前端联调说明](docs/usage/frontend.md)启动。在线文档由独立的 `website/` 工程生成，与管理端不同。

## 运行方式与替换边界

```mermaid
flowchart LR
    UI[Vben 管理端] --> MODE{运行方式}
    MODE -->|单进程联调| ONE[cmd/server]
    MODE -->|拆分部署| GW[现有 Java Gateway]
    GW --> SYS[cmd/system]
    GW --> INF[cmd/infra]
    SYS --> NACOS[Nacos]
    INF --> NACOS
    ONE --> DATA[(MySQL / Redis)]
    SYS --> DATA
    INF --> DATA
```

拆分运行时，Go 服务以 `system-server`、`infra-server` 注册；替换路径需要继续验证 Java Gateway 的路由、跨语言调用和共享数据行为。`/rpc-api/**` 只应在受信网络中使用。**图中是当前运行入口，不表示 Java/Go 动态切流已经验收。**切流条件和回滚步骤见[部署说明](docs/usage/deployment.md)。

## 文档与贡献

文档按三条路径组织：[使用说明](docs/usage/index.md)回答怎么运行和部署，[开发说明](docs/development/index.md)解释代码与测试，[重构过程](docs/refactor/index.md)提供可以跟做的功能拆解。Markdown 放在 `docs/`，VitePress 工程放在 `website/`；可直接访问[在线文档](https://weilhuang.github.io/yudao-cloud-go/)，也可按[文档站维护](docs/development/site.md)在本机预览。

欢迎提交可复现的问题和有测试证据的改动。请先看[贡献指南](CONTRIBUTING.md)；安全问题按 [SECURITY.md](SECURITY.md) 私密报告。版本变化见 [CHANGELOG.md](CHANGELOG.md)，代码采用 [Apache-2.0](LICENSE)。

## 致谢与来源

感谢芋道开源团队提供[完整后端 yudao-cloud](https://github.com/YunaiV/yudao-cloud)、[yudao-cloud-mini](https://github.com/yudaocode/yudao-cloud-mini) 和 [yudao-ui-admin-vben](https://github.com/yudaocode/yudao-ui-admin-vben)。本项目参考它们的源码与行为，也借鉴 [go-backend-clean-architecture](https://github.com/amitshekhariitbhu/go-backend-clean-architecture) 的职责划分。具体基线、第三方来源与许可见[兼容性说明](docs/usage/compatibility.md)和 [NOTICE](NOTICE)。本项目是独立的 Go 重构项目，不代表上游项目。
