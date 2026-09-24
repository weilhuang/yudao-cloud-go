# 文档站工程

这里放 VitePress 的依赖、配置、主题和静态资源。公开文档正文仍在仓库根目录的 [`docs/`](../docs/README.md)；改使用说明或开发说明时，直接编辑那里的 Markdown。

在线站点：[https://weilhuang.github.io/yudao-cloud-go/](https://weilhuang.github.io/yudao-cloud-go/)。

## 本机启动

需要 Node.js 24 和 npm。在仓库根目录执行：

```bash
cd website
npm ci
npm run docs:dev
```

浏览器打开终端输出的地址。构建和预览静态结果也在 `website/` 中执行：

```bash
npm run docs:build
npm run docs:preview
```

`srcDir: '../docs'` 让 VitePress 读取相邻目录的 Markdown；生成物位于 `.vitepress/dist/`，不会进入 Git。目录分工、导航修改和 GitHub Pages 流水线见[文档站维护](../docs/development/site.md)。
