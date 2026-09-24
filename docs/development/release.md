# 版本发布检查

这页是维护者准备后续版本时的检查清单。**发布源码**和**允许 Java/Go 受控切换线上流量**是两道不同的门：0.0.1 只表示开发预览版；异构共存、动态切换和回滚还要完成[兼容性门槛](../usage/compatibility.md)。文档站已上线：[在线文档](https://weilhuang.github.io/yudao-cloud-go/)。

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

发布前确认工作树干净、准备发布的分支和 tag 指向预期提交。现有 `0.0.1` 是开发预览版标签，后续文档提交不应移动这个已发布标签。`ui/yudao-ui-admin-vben/` 子模块应固定在 `486870dd8496d94b79aaa183069df8407de8cbf9`，URL 应为 `https://github.com/yudaocode/yudao-ui-admin-vben.git`。检查目标分支**整条可达历史**，不只检查当前文件：文件名、提交正文、作者/提交者、旧代码和二进制都可能携带信息。重点查密钥、真实 DSN、内部 IP/域名、个人路径、客户数据、数据库导出和不属于项目的模板文件。测试中的 `example.com`、本机回环地址和随机开发口令要与真实凭据区分。

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

## 发布前核对远端历史

公开 `main` 保留了原始的 `85f246c` 初始化提交，后续已发布 0.0.1 预览内容。**每次发布都先核对实时远端 SHA**；本机的 `origin/main` 可能已经过期，不能只凭它决定是否推送。若远端已被别人更新，先看差异，不要强推覆盖。

```bash
git ls-remote --heads origin main
git fetch origin main
git log --oneline --left-right --graph HEAD...origin/main
```

确认差异后只推这次要发布的分支和新版本 tag；不要用 `--all` 或 `--mirror` 把本机旧历史备份带到远端。原始初始化提交中的模板资料仍可从 Git 历史看到，这正是保留其 SHA 的结果；以前公开过的对象、PR、fork 和缓存也不会因后续提交而消失。任何曾真实使用的密钥都应轮换。

## 推送后再做

1. 看 Go CI 与 VitePress 工作流是否全部通过；失败时查具体日志，不把本地通过当成远端通过。
2. 确认 GitHub Pages 工作流发布了这次提交，访问实际站点的首页、三个文档入口、Mermaid 图、搜索和窄屏布局。
3. 核对新版本 tag 指向预期提交；如果仍是 0.0.1，Release 描述继续标为开发预览版。
4. 公开 README 的 CI 徽章显示的是新分支运行结果，再处理分支保护和后续贡献。

本地准备过程不会自动推送或发布站点。遇到 GitHub 网络或凭据故障时，先修连接并重新读远端状态，再决定是否执行上面的手动命令。
