# Hourly rental rules / 小时租赁规则

**R026 · SOURCE_ONLY / UNVERIFIED · CODE_FIRST，未编译/测试。** These domain functions are not a public rental service and cannot start a GPU, authorize funds or free an owner.

`session.go` implements explicit original-device reservation, both readiness gates, one start transition, rational-budget duration caps, same-price paid-renewal terms, disconnect/hard-deadline stops and conservative cleanup transitions. The original C2C contract, seller/device/environment and start time remain fixed across a renewal. Preparation is unbilled. Unknown stopped-work/residue facts retain quarantine.

`ResourceSnapshot`、`Funding`、`ReadyProof`及清理事实只能由可信仓储/适配器解析，不能照搬请求正文。后续实际服务必须按order→session→resource加锁，并在同事务完成真实资格/资金重核、双围栏与原owner核验、状态/附约/授权撤销/安全事件。当前这些SQL、HTTP、端侧会话执行和物理清理接线仍未完成，不能将状态函数直接当执行票据。

`meter.go` requires the complete independently confirmed availability/fault history. It deduplicates exact retransmissions, rejects conflicting identities/sequences/overlaps, merges adjacent availability, subtracts fault unions and floors nanoseconds to milliseconds once. The exact NB hourly amount is rounded once and capped before narrowing to money; same-price renewals and media child invocations do not create extra rounding or token/per-image fees. Receipt hashes preserve the complete calculation inputs. A calculated refund is not a payment-channel refund result.

会话令牌与断线控制另见[rentalaccess](../../client/protocol/rentalaccess/README.md)。令牌、45秒控制确认、付费附约和物理会话owner分别核验；已有普通JOB存储不能改名冒充RENTAL_SESSION。原已发布SQL不改，实际仓储变更须新增前向迁移。

[完整编号细则与未接通边界 / Numbered rules and remaining integration](../../../../docs/技术开发/附录-小时租赁授权与累计计量源码细则.md) · [56任务/222子项 / Complete backlog](../../../全量代码优先实施清单.md)
