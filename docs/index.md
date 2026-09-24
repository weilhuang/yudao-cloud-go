---
layout: home

hero:
  name: yudao-cloud-go
  text: Go 版本的后台管理基础服务
  tagline: 登录权限、租户、消息、文件和代码生成等 system / infra 能力，对接固定版本 Vben 管理端，与 Java 业务服务共存。
  image:
    src: /logo.svg
    alt: yudao-cloud-go 标志
  actions:
    - theme: brand
      text: 使用说明
      link: /usage/
    - theme: alt
      text: 开发说明
      link: /development/
    - theme: alt
      text: 重构过程
      link: /refactor/

features:
  - icon: 🧑‍💼
    title: 系统功能
    details: 登录、用户组织、角色权限、多租户、字典地区和消息通知。
    link: /usage/capability-comparison
  - icon: 🗄️
    title: 基础设施
    details: 文件、参数、数据源、代码生成、Redis 监控和 API 日志。
    link: /usage/capability-comparison
  - icon: ⚙️
    title: 使用说明
    details: 启动、配置、部署与回滚，先弄清当前版本能承担什么。
    link: /usage/
  - icon: 🧩
    title: 开发说明
    details: 架构、功能代码、测试和贡献流程，按实际接口推进。
    link: /development/
  - icon: 🛠️
    title: 重构过程
    details: 从冻结 Java 基线出发，按功能切片自己写出 Go 实现。
    link: /refactor/
---

<div class="home-status">
  <strong>项目状态：0.0.1 开发预览版</strong>
  <span>可本机运行并联调 Vben；线上切流仍需行为和回滚验收。<a href="/yudao-cloud-go/usage/capability-comparison.html">查看功能对照 →</a></span>
</div>
