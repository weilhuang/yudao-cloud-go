import { fileURLToPath } from 'node:url'
import { defineConfig } from 'vitepress'
import { withMermaid } from 'vitepress-plugin-mermaid'

export default withMermaid(defineConfig({
  lang: 'zh-CN',
  title: 'yudao-cloud-go',
  description: '芋道 Cloud Go 后端重构的使用、开发与重构过程文档',
  // 文档正文仍在仓库根目录的 docs/，站点依赖和主题放在 website/。
  srcDir: '../docs',
  base: '/yudao-cloud-go/',
  mermaid: { securityLevel: 'strict' },
  lastUpdated: true,
  head: [
    ['link', { rel: 'icon', type: 'image/svg+xml', href: '/yudao-cloud-go/logo.svg' }],
    ['meta', { name: 'theme-color', content: '#1677e8' }],
  ],
  vite: {
    publicDir: fileURLToPath(new URL('../public', import.meta.url)),
    // npm 脚本会把 docs/node_modules 指向站点依赖，供这里的 Vite 根目录解析。
    // Mermaid 依赖的 fastdom 是 CommonJS；开发服务器需要先转换它。
    optimizeDeps: { include: ['fastdom', 'fastdom/extensions/fastdom-promised.js'] },
  },
  themeConfig: {
    logo: '/logo.svg',
    siteTitle: 'yudao-cloud-go',
    nav: [
      { text: '使用说明', link: '/usage/' },
      { text: '开发说明', link: '/development/' },
      { text: '重构过程', link: '/refactor/' },
      { text: 'GitHub', link: 'https://github.com/weilhuang/yudao-cloud-go' },
    ],
    sidebar: [
      {
        text: '1. 使用说明',
        items: [
          { text: '阅读入口', link: '/usage/' },
          { text: '快速开始', link: '/usage/getting-started' },
          { text: '前端联调', link: '/usage/frontend' },
          { text: '配置说明', link: '/usage/configuration' },
          { text: '部署与回滚', link: '/usage/deployment' },
          { text: '兼容范围', link: '/usage/compatibility' },
          { text: '安全边界', link: '/usage/security-model' },
          { text: '排错手册', link: '/usage/troubleshooting' },
        ],
      },
      {
        text: '2. 开发说明',
        items: [
          { text: '开发入口', link: '/development/' },
          { text: '开发手册', link: '/development/workflow' },
          { text: '架构总览', link: '/development/architecture' },
          { text: '上游模板与依赖规则', link: '/development/architecture/01-principles' },
          { text: '本项目的落地方式', link: '/development/architecture/02-project-mapping' },
          { text: '真实请求路径', link: '/development/architecture/03-request-walkthrough' },
          { text: '架构取舍与演进', link: '/development/architecture/04-tradeoffs' },
          { text: '测试与验收', link: '/development/testing' },
          { text: '文档站维护', link: '/development/site' },
          { text: '开源发布检查', link: '/development/release' },
        ],
      },
      {
        text: '3. 重构过程',
        items: [
          { text: '重构路线', link: '/refactor/' },
          { text: '01 · 进程与健康检查', link: '/refactor/01-server' },
          { text: '02 · 数据与缓存', link: '/refactor/02-data' },
          { text: '03 · 登录和令牌', link: '/refactor/03-auth' },
          { text: '04 · 用户和权限', link: '/refactor/04-directory' },
          { text: '05 · 文件与基础设施', link: '/refactor/05-infra' },
          { text: '06 · 拆分与验收', link: '/refactor/06-distributed' },
          { text: '07 · 功能拆解清单', link: '/refactor/07-feature-map' },
          { text: '08 · Java 到 Go', link: '/refactor/08-java-to-go' },
          { text: '09 · 功能切片实战', link: '/refactor/09-feature-slice' },
          { text: '10 · 合同验收练习', link: '/refactor/10-contract-lab' },
        ],
      },
    ],
    outline: { level: [2, 3], label: '本页内容' },
    search: {
      provider: 'local',
      options: {
        locales: {
          root: {
            translations: {
              button: { buttonText: '搜索文档', buttonAriaLabel: '搜索文档' },
              modal: {
                displayDetails: '显示详情',
                resetButtonTitle: '清空搜索',
                backButtonTitle: '关闭搜索',
                noResultsText: '没有找到结果',
                footer: {
                  selectText: '选择',
                  selectKeyAriaLabel: '选择',
                  navigateText: '切换',
                  navigateUpKeyAriaLabel: '上移',
                  navigateDownKeyAriaLabel: '下移',
                  closeText: '关闭',
                  closeKeyAriaLabel: '关闭',
                },
              },
            },
          },
        },
      },
    },
    editLink: {
      pattern: 'https://github.com/weilhuang/yudao-cloud-go/edit/main/docs/:path',
      text: '帮忙改进这页',
    },
    socialLinks: [{ icon: 'github', link: 'https://github.com/weilhuang/yudao-cloud-go' }],
    footer: {
      message: '代码采用 Apache-2.0 协议。文档与代码一起接受改进。',
      copyright: 'Copyright © 2026 weilhuang and contributors',
    },
    darkModeSwitchLabel: '主题',
    lightModeSwitchTitle: '切换到浅色模式',
    darkModeSwitchTitle: '切换到深色模式',
    sidebarMenuLabel: '菜单',
    returnToTopLabel: '回到顶部',
    skipToContentLabel: '跳转到内容',
  },
}))
