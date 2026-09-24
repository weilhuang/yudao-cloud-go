---
layout: home

hero:
  name: yudao-cloud-go
  text: 迈向可动态替换的 Java / Go 异构系统
  tagline: 从 system、infra 开始对齐契约，让两种服务逐步具备共存、受控切换和回滚能力。
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
  <span>当前实现可供开发与隔离环境联调；线上替换仍需完整验收。<a href="/yudao-cloud-go/usage/compatibility.html">查看兼容范围 →</a></span>
</div>
