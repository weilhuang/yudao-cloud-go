# 0.0.1 开源发布检查

这页是给维护者手动发布 GitHub 仓库用的清单。**发布源码**和**允许替换线上 Java**是两道不同的门：0.0.1 只表示开发预览版；生产替换还要完成[兼容性门槛](../usage/compatibility.md)。

## 先检查准备推送的对象

```bash
git status --short
git branch --list
git tag --list
git log --reverse --format='%h %s' main
git ls-tree -r --name-only main
git submodule status
git config -f .gitmodules --get submodule.vben.url
git ls-remote --heads origin main
```

预期是本地只有 `main`、版本 tag 为 `0.0.1`、工作树干净，`0.0.1` 指向你打算发布的提交。`ui/yudao-ui-admin-vben/` 子模块应固定在 `486870dd8496d94b79aaa183069df8407de8cbf9`，URL 应为 `https://github.com/yudaocode/yudao-ui-admin-vben.git`。检查 `main` **整条可达历史**，不只检查当前文件：文件名、提交正文、作者/提交者、旧代码和二进制都可能携带信息。重点查密钥、真实 DSN、内部 IP/域名、个人路径、客户数据、数据库导出和不属于项目的模板文件。测试中的 `example.com`、本机回环地址和随机开发口令要与真实凭据区分。

建议用独立的历史扫描工具再扫一遍；任何自动扫描都有漏报，人工还要查看命中项和许可证来源。发现真实凭据时，先轮换，再重写待推送历史并复查。不要指望删掉当前文件就能清掉旧提交。

仓库自带的高置信度检查可运行 `python3 scripts/audit-public-history.py main`。它扫描从 `main` 可达的提交和 blob，发现问题只打印文件名，不把疑似秘密写进日志。`ui/yudao-ui-admin-vben/` 是 Git 指针，脚本只放行预期路径和提交号，**不会扫描上游子模块仓库的历史**。原始 `85f246c` 是已公开的上游模板；脚本只对该提交中四个已复核的固定 blob 放行模板作者联系信息、测试邮箱及带格式占位符的 MongoDB URI。它有意不判定测试里的示例手机号、二进制地区库和未知格式的凭据；仍需人工复核。旧模板 `.env.example` 中的 JWT 示例值不能用于实际部署。

## 再检查能否使用

| 项目 | 本机命令或证据 |
| --- | --- |
| 格式、依赖、单测、静态检查、构建 | `gofmt`、`go mod verify`、`go test -count=1 ./...`、`go vet ./...`、`go build ./cmd/...` |
| MySQL/Redis/Nacos | `go test -tags=integration -p 1 -parallel 1 -count=1 -timeout 45m ./...`，确认没有意外跳过 |
| 文档站 | 从 `website/` 运行 `npm ci`、`npm audit --audit-level=high`、`npm run docs:build`；本机预览亮暗主题和窄屏 |
| 文档与链接 | README 的命令可执行，章节有入口，Markdown 本地链接无缺失 |
| 许可 | `LICENSE`、`NOTICE`、内嵌地区数据与第三方源码的许可来源一致 |
| GitHub 设置 | Pages 发布源为 GitHub Actions；启用私密漏洞报告；检查分支保护与 CI 权限 |

Java/Go 真实混跑、Vben E2E、目标环境回滚和压测尚未成为 0.0.1 的通过证据。README、CHANGELOG 和 GitHub Release 描述应如实说明。

## 推送前核对远端历史

公开 `main` 保留了 GitHub 上原始的 `85f246c` 初始化提交，后面接一条整理后的 Go 重构与开源预览提交。如果远端 `main` 仍指向初始化提交，本地 `main` 是它的后代，可以普通快进推送。**先核对实时远端 SHA**；本机的 `origin/main` 可能已经过期，不能只凭它决定是否推送。若远端已被别人更新，先看差异，不要强推覆盖。

```bash
git ls-remote --heads origin main
# 确认上面返回的 SHA 与 85f246cb237caebf6d6cb7618b3f22d56b022bf1 相同，再运行：
git push origin main:main
git push origin refs/tags/0.0.1
```

只推 `main` 与本次版本 tag；不要用 `--all` 或 `--mirror` 把本机旧历史备份带到远端。原始初始化提交中的模板资料仍可从 Git 历史看到，这正是保留其 SHA 的结果；以前公开过的对象、PR、fork 和缓存也不会因后续提交而消失。任何曾真实使用的密钥都应轮换。

## 推送后再做

1. 看 Go CI 与 VitePress 工作流是否全部通过；失败时查具体日志，不把本地通过当成远端通过。
2. 在 GitHub Pages 设置中选 GitHub Actions 发布源，访问实际站点的首页、三个文档入口、Mermaid 图、搜索和窄屏布局。
3. 核对 GitHub `main` 与 `0.0.1` tag 指向同一预期提交，Release 描述标为开发预览版。
4. 公开 README 的 CI 徽章显示的是新分支运行结果，再处理分支保护和后续贡献。

本地准备过程不会自动推送或发布站点。遇到 GitHub 网络或凭据故障时，先修连接并重新读远端状态，再决定是否执行上面的手动命令。
