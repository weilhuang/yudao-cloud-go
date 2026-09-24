# 安全问题

如果发现可能泄露数据、绕过权限、伪造内部调用或读取其他租户数据的问题，请不要在公开 Issue、PR 或日志中贴利用步骤和真实凭据。

仓库公开前，维护者需在 GitHub 仓库设置中启用 **Private vulnerability reporting**。启用后请使用仓库的 **Report a vulnerability** 入口私下报告，写明受影响 commit、复现条件、影响范围和一个脱敏的最小复现。仓库仍为私有且该入口不可用时，请让有权限的维护者在仓库内创建私有 Security Advisory；不要将详情转到公开讨论。

维护者会先确认问题和影响，再协调修复、回归测试和披露时间。目前没有生产可用版本；`1.0.0` 平替声明尚未通过验收。支持范围以实际发布的 commit 与 [CHANGELOG.md](CHANGELOG.md) 为准。
