# Native execution ownership / 原生共资源门闩

**R026 · CODE_FIRST · SOURCE_ONLY / UNVERIFIED.** No compilation, SQL migration or test execution has been performed for this source. Complete all in-scope production integrations and test source before batch validation. Opening a ledger does not enable inference or establish payment, readiness, privacy or physical cleanup qualification.

本包以同一条 `resources` 记录管理受管个人使用、维护、普通 JOB 和小时 RENTAL。物理摘要唯一；平台资源代次与本机 owner 代次分别递增。租赁 owner 是原 `session_id`，不借普通任务的 `owner_attempt_id`。租内调用不另收图片、视频或 token 费用。

`schema.sql` 是已发布、原字节保留的五表基础 DDL。新增 `schema-002-rental.sql` 是端侧前向版本 2：保留原资源、占用、高水位和 inbox/receipt/fence，添加四张租赁元数据表。`Open` 在返回任何可用门闩前，于独占的同一连接中按 SQLite 官方表重建顺序迁移、检查外键、保存基础/前向源码与实际结构摘要并恢复外键检查。任何错误关闭；不会删库重建。版本 2 以后不得覆盖这两份已登记 DDL，应增加前向迁移。这里的九张端侧表不计入平台 DATA 编号。

The original node owns the OS process lock, protected directory, installation identity and ledger anchor. Missing/corrupt/mismatched metadata is not an empty database. Recovery quarantines occupied owners and stops current rental grants. It never reconstructs a live control deadline. An unknown commit closes admission and requires original-request recovery.

| 调用 / Call | 必需来源与作用 / Required source and effect |
|---|---|
| `ReceiveRentalPreparation` | 原本机独立批准的 `rentalaccess.Prepared`、原物理摘要与更高平台代次；占原共享槽，仅准备，无执行权 / Original independent approval reserves the same physical row without invoking. |
| `BindRentalGrant` | 独立原 DB 摘要、固定公钥 pin 验证的原 JWS 与完整 Expected；不变更已付 terms/硬截止，不产生 ALLOW / Bind the original verified grant without extending paid terms. |
| `ApplyRentalControl` + `CheckRental` | 原会话/环境/清单/物理摘要及 scope、最长45秒签名窗口同时成立；重复回复不续时 / Commit the original control proof and require the same current physical owner. |
| `BeginRentalInvocation` | 原加密输入摘要与批准 recipe；先存 SUBMITTING，已知提交成功后给一次 ticket；原 ID 重放只有 QUERY_ONLY / Persist before a single submission; never resubmit an unknown original invocation. |
| `MarkRentalSubmitted` / `RentalInvocation` | 绑定或读取原 backend ID；晚 ACK 不恢复已撤权限 / Retain exact original backend identity; a late acknowledgement cannot restore permission. |
| `StopRentalInvocation` + `ObserveRentalInvocationStopped` | 真实拥有的适配器停止并查原请求；已知 backend 不能改报 ABSENT / Actual original-key stopped-work query is required. |
| `FinishRentalInvocationCleanup` | 停工、禁止迟到启动、输入清除和输出钥处置齐全；仅清子调用，父会话继续占槽 / Child cleanup retains the parent's physical ownership. |
| `StopRental` + `ObserveRentalSessionStopped` | 先撤后续执行权；全部子调用完成原键清理后记录真实全会话停工 / Withdrawal retains the owner until actual complete stopped-work proof. |
| `FinishRentalSessionCleanup` | 当前原 owner/代次、全部 grant 已撤、工作区及输出钥清理齐全才释放本机槽 / Release only this original local owner after complete trusted cleanup. |
| `RentalHistory` | 释放后仍读原安全回执；不能借下一 owner 的权限查询/取消/提交 / Original history cannot control a subsequent physical owner. |

Internal cleanup observations are supplied only by trusted owned native adapters. They are not buyer/provider JSON acknowledgements or evidence that a physical engine has stopped merely because a process/HTTP connection closed. Generic JOB/local cleanup entry points explicitly reject RENTAL. Stop/expiry alone releases neither the physical owner nor the original platform budget.

当前 `node.ProviderRental` 已有本机加密批准记录、原平台摘要/控制 HTTP 与本门闩的接线源码。实际租赁内核适配、受认证输入消费、参考素材/密文分块、独立停工 watchdog、原平台清理证明/解除占用以及 Android 会话执行仍待完成。CLI 继续不开租赁执行。这里的本机元数据 SQLite 不保存 prompt、输出、内容钥或明文内容指纹；加密批准 journal 也不等于客户内容 E2E 或硬件增强证明。

[RN-071—090 and test-source inventory / 编号细则](../../../../docs/技术开发/附录-小时租赁授权与累计计量源码细则.md) · [Full 56-task / 222-step backlog / 全部待办](../../../全量代码优先实施清单.md) · [SQLite documented rebuild procedure](https://www.sqlite.org/lang_altertable.html#making_other_kinds_of_table_schema_changes)
