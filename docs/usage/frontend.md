# 用固定 Vben 管理端联调

`ui/yudao-ui-admin-vben/` 是 [yudao-ui-admin-vben](https://github.com/yudaocode/yudao-ui-admin-vben) 的 Git 子模块，固定在 v2026.08 的 `486870dd8496d94b79aaa183069df8407de8cbf9`。这里是管理端源码；`website/` 是本项目的 VitePress 文档站，两个前端不要混用。

## 取到对应源码

已经克隆 Go 仓库的开发者，在仓库根目录运行：

```bash
git submodule update --init ui/yudao-ui-admin-vben
git -C ui/yudao-ui-admin-vben rev-parse HEAD
```

第二条命令应返回 `486870dd8496d94b79aaa183069df8407de8cbf9`。以后新克隆也可以使用 `git clone --recurse-submodules https://github.com/weilhuang/yudao-cloud-go.git`。普通 `git clone` 不会自动下载子模块内容；看到 `ui/yudao-ui-admin-vben/` 为空时，先执行上面的初始化命令。

管理端源码由上游仓库维护，遵循其中的 MIT `LICENSE`。本项目只记录固定提交指针，不把它的文件复制成 Go 仓库自己的代码。需要升级 Vben 时，先核对 Java/Go 接口差异和浏览器流程，再在单独的提交中更新子模块指针与兼容性文档。

## 本机启动

先按[快速开始](getting-started.md)启动 MySQL、Redis 和 `cmd/server`，确认 `http://127.0.0.1:48080/health` 可访问。Vben 基线的 `ui/yudao-ui-admin-vben/package.json` 要求 Node.js `^22.18.0` 或 `^24.12.0`，pnpm `>=11.0.0`，并固定 `pnpm@11.16.0`。准备好这些工具后：

```bash
cd ui/yudao-ui-admin-vben
pnpm install --frozen-lockfile
pnpm dev:antd
```

上游 `apps/web-antd/.env.development` 把管理端开发端口设为 `5666`，`apps/web-antd/vite.config.ts` 把 `/admin-api` 代理到本机 `48080`。浏览器打开命令输出的地址，通常是 `http://localhost:5666/`。若 Go 服务换了端口，要同步调整代理目标。上游文件里有演示登录值；只在隔离的本机数据上使用，不要把它当作生产凭据。

## 联调时看什么

先走登录，再看用户、角色、菜单、租户和文件等页面。浏览器开发工具里的 `/admin-api/**` 请求应打到正在运行的 Go 服务。页面能打开只证明前端资源加载成功；接口字段、权限、租户隔离、失败提示和副作用要按[测试与验收](../development/testing.md)逐项记录。当前 `0.0.1` 尚未完成固定 Vben 的全流程 E2E，不能据此宣称已可替换线上 Java。
