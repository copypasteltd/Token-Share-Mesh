# Update and publication workflow / 更新与推送流程

The user has authorized this project's ongoing updates to
`git@github.com:copypasteltd/Token-Share-Mesh.git`. Keep each meaningful change,
its bilingual explanation, evidence and numbered pending work in the same reviewable
history. This authorization applies to this repository; it does not authorize
force-pushing or replacing another contributor's work.

用户已授权本项目后续更新同步至上述仓库。每次有效修改与双语说明、证据、编号待办一起提交；授权不包括强推覆盖他人历史。

## Required sequence / 必要顺序

1. Implement under `dev/`; retain authoritative requirement/SQL/contract sources.
   在 dev 下实现，保留既有规范和契约的权威源。
2. Add the round's implemented-ID table and a complete 56-task snapshot in
   `dev/progress/implementation-status.json`. Keep historical snapshots intact.
   维护本轮实现编号和全部56任务快照，保留历史轮次。
3. Update both README templates in this directory and both root changelogs for
   changed behavior, usage or status. Run the README generator for both languages.
   使用逻辑/状态变化同时更新两种模板及中英更新日志，再统一生成。
4. Rebuild affected whitepaper/technical/development artifacts before validating.
   Do not hand-edit generated contracts/readers. Refresh actual media when its
   source HTML or illustrated interaction changes.
   先重建相关生成物，不能手改；来源或交互变化重录真实媒体。
5. Run document, repository and change-appropriate source/device checks. Source
   migrations must be forward-only. Static checks do not clear real-world gates.
   运行文档、仓库和适用源码/设备核验；迁移只增版本，静态通过不清真实门槛。
6. Check the remote, commit intended files and push. Inspect the actual remote
   commit after the push; a local commit alone is not publication evidence.
   检查远端、提交推送，再核远端实际提交号。

```powershell
python -X utf8 dev/tools/文档工具.py build
python -X utf8 dev/tools/构建仓库文档.py
python -X utf8 dev/tools/文档工具.py check
python -X utf8 dev/tools/核验仓库.py
python -X utf8 dev/tools/同步仓库.py check
python -X utf8 dev/tools/同步仓库.py publish --message "docs: explain the current change / 说明本次修改"
```

The publishing tool checks the expected remote and branch, rejects an unexpected
remote history, validates public artifacts, stages intended non-ignored project
files, commits with a file-based message and pushes without force. Both READMEs
and changelogs must be included in a meaningful publication. Its final local
receipt is saved under ignored `runtime/git/`; it records local and confirmed
remote SHA values without creating a self-referential tracked commit receipt.

同步工具核对远端和分支，拒绝意外历史，核验公开产物，暂存未忽略的本项目内容，用消息文件提交并普通推送；有效发布须含两份 README 和更新日志。最终回执写忽略的 runtime/git，记录本地/远端提交号，避免版本内记录自身提交号的循环。

## Files that never belong in Git / 不入库内容

Runtime databases, credentials, payment proofs, user prompts/results, local
browser profiles, caches, downloaded model/kernel/tool packages and compiled
binaries remain outside Git. The small approved README screenshots, MP4/GIF and
synthetic test evidence are versioned with provenance. `.gitattributes` preserves
content-addressed source bytes on checkout.

数据库、凭据、支付凭证、用户输入输出、浏览器配置、缓存、下载模型/内核/工具和编译产物不入库。README 小型演示媒体及合成测试证据连同来源入库；检出保留内容寻址文件原字节。


## 2026-11-06 全量代码阶段的源码检查点

用户要求先完成全部待办生产代码，之后统一开展编译、测试和调试。本阶段不得运行产品验证序列来推动局部收口。阶段内可把代码、双语说明和完整待办作为 **SOURCE_ONLY / UNVERIFIED** 检查点同步到 `codex/full-scope-code-first` 分支；主分支保留原已验证提交。检查点不代表本轮完成或可以发布，不能把历史R025证据改挂到新源码。全部在范围代码完成后再切换集中验证阶段，执行适用检查并合并交付。

The full-code phase may publish explicitly unverified source checkpoints to `codex/full-scope-code-first`. Keep the previously verified main revision intact. A checkpoint does not complete R026 or transfer historical R025 evidence to changed source. Complete every in-scope code item before batch validation, then integrate the verified delivery.
