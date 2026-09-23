# WeKnora 协作规则

## Fork 与上游同步

- `origin` 是个人 fork，仅将个人功能、修复和本地定制推送到 `origin`。
- `upstream` 是官方仓库，只允许用于 `fetch`、查看和合并官方代码；禁止向 `upstream` 推送。
- 个人改动必须在 fork 的分支上开发，不直接改写官方 `main` 的历史。
- 同步官方修复前，先保存并检查个人改动，再执行 `git fetch upstream`，将 `upstream/main` 安全合并或变基到个人分支。
- 合并冲突涉及个人功能时，保留个人需求并根据上游最新实现调整；不得为了同步而丢弃个人改动。
- 推送时只使用 `git push origin <个人分支>`；需要贡献官方项目时，由用户明确决定是否创建 Pull Request。

## 当前约束

LDAP/Active Directory 支持属于个人 fork 的定制功能。实现代码不得推送到 `upstream`，但必须持续吸收 `upstream/main` 的 bug 修复和安全更新。
