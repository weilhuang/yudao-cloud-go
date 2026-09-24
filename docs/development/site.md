# 维护文档网站

这站点读取仓库根目录 `docs/` 里的 Markdown；VitePress 工程独立放在 `website/`。主页是 `docs/index.md`，导航在 `website/.vitepress/config.ts`，主题在 `website/.vitepress/theme/`，logo 在 `website/public/`。普通文档只需改 `docs/` 中的 Markdown；新增一章后把入口加到侧边栏，别让读者只能靠搜索找到它。

## 本机预览

需要 Node.js 24 与 npm。从仓库根目录进入站点工程，再安装依赖、启动开发服务：

```bash
cd website
npm ci
npm run docs:dev
```

本机打开命令输出的地址，通常是 `http://localhost:5173/yudao-cloud-go/`。从 `website/` 运行 `npm run docs:build`；构建产物在 `website/.vitepress/dist`，已被 Git 忽略。亮色和暗色主题都要看一眼，Mermaid 图、代码块与手机窄屏也要检查。

Mermaid 围栏代码块直接写在 Markdown 里，由 `vitepress-plugin-mermaid` 渲染。主题会在宽图上保留可读的文字大小，窄屏可横向滚动。`website/package.json` 锁定经过构建、预览和审计验证的 Vite 6.4.3；升级 VitePress 或 Mermaid 时，请从 `website/` 重跑 `npm ci`、`npm audit`、构建和本机页面检查。

`website/.vitepress/config.ts` 使用 `srcDir: '../docs'` 读取 Markdown。VitePress 的开发服务器以 `docs/` 为模块解析起点，而依赖装在 `website/node_modules/`。`npm run docs:dev` 和 `npm run docs:build` 会先运行 `website/scripts/link-docs-deps.mjs`，建立被 Git 忽略的 `docs/node_modules` 软链接，指向站点的同一套依赖。脚本碰到已有的其他文件或目录会报错，避免覆盖开发者数据。不要在 `docs/` 下另装一套依赖。静态资源仍从 `website/public/` 复制到站点根目录。

如果页面打开后只剩空白，先看浏览器控制台是否报模块导出错误，再确认软链接指向 `website/node_modules/`，以及 `npm ci` 是否已完成。只检查 HTTP 200 不够；浏览器还要能显示正文、Mermaid 图和搜索入口。

## GitHub Pages

仓库名为 `yudao-cloud-go`，所以 VitePress 的 `base` 是 `/yudao-cloud-go/`。仓库若改名或改自定义域名，要同步改 `website/.vitepress/config.ts` 的 `base` 和图标路径。计划地址是 `https://weilhuang.github.io/yudao-cloud-go/`；**只有 Pages 真正发布且访问成功后才把它当成可用地址**。

维护者在 GitHub 仓库的 **Settings → Pages → Build and deployment → Source** 选择 **GitHub Actions**。`.github/workflows/docs.yml` 监听 `docs/**` 和 `website/**`，在 PR 上只构建，在 `main` 的相关变更或手动运行时构建并发布 `website/.vitepress/dist`。工作流在 `website/` 安装和构建，缓存使用 `website/package-lock.json`；它只部署文档，不部署 Go 服务。仓库已经公开，但 Pages 的实际访问仍要等新代码推送并运行工作流后验证。

这份说明只配置流水线，没有替用户推送代码或开通 Pages。发布前还要检查 GitHub 仓库可见性、Pages 设置、构建日志和实际网页，尤其是内部链接、搜索与移动端布局。
