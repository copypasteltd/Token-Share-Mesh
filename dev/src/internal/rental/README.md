# Hourly rental rules / 小时租赁规则

**R026 · SOURCE_ONLY / UNVERIFIED · CODE_FIRST，未编译/迁移/测试。** Domain rules plus the platform transaction/HTTP source now exist. Native rental execution, actual channel/evidence production and physical cleanup remain open.

`session.go` implements explicit original-device reservation, both readiness gates, one start transition, rational-budget duration caps, same-price paid-renewal terms, disconnect/hard-deadline stops and conservative cleanup transitions. The original C2C contract, seller/device/environment and start time remain fixed across a renewal. Preparation is unbilled. Unknown stopped-work/residue facts retain quarantine.

`service.go/store.go/readiness.go/access.go`按原order→session→resource取锁，接通占位/准备领取/设备ready/买方ready/停止/一次grant及历史查询源码。实际资金只读取原持久SETTLED Alipay记录，不承认请求里的成功布尔；原资格与完整预算重核。市场另核原会话占用的BUSY资源，不能改选设备。独立就绪证据生产者仍待，没有已独立批准证据时ready关闭。

Forward014 stores the raw original contract/manifest, rational hourly price, original session owner and generation, immutable version history and metadata outbox. The legacy integer minute price is NULL for this profile. Access grants persist only safe claims and token digests; the first committed HTTP response consumes the Bearer once. Original-key replay returns NOT_REPLAYABLE metadata. An explicit new grant/current version permanently revokes the previous credential. Stop revokes grants and retains the original physical owner/full budget until actual native cleanup is independently confirmed; 014 intentionally admits neither physical release nor paid-term mutation.

启动可选租赁服务需要显式`P11_RENTAL_SIGNER_FILE`、独立`P11_RENTAL_VERIFIERS_FILE`及`P11_PAYMENT_ENVIRONMENT`；缺两文件时服务关闭，半份/错pin配置启动失败。注册的7项接口及安全调用顺序见RN-031—050。公共准备清单不等于执行票据；新接口仍未验证，不应作为可用交易服务发布。

`meter.go` requires the complete independently confirmed availability/fault history. It deduplicates exact retransmissions, rejects conflicting identities/sequences/overlaps, merges adjacent availability, subtracts fault unions and floors nanoseconds to milliseconds once. The exact NB hourly amount is rounded once and capped before narrowing to money; same-price renewals and media child invocations do not create extra rounding or token/per-image fees. Receipt hashes preserve the complete calculation inputs. A calculated refund is not a payment-channel refund result.

会话令牌与断线控制另见[rentalaccess](../../client/protocol/rentalaccess/README.md)。令牌、45秒控制确认、付费附约和物理会话owner分别核验；已有普通JOB存储不能改名冒充RENTAL_SESSION。原加密journal/控制传输、付费续租资金/附约、独立区间计量落库、端侧执行/停止/导出/清理释放仍须完成。原已发布SQL不改，实际仓储变更须新增前向迁移。

[完整编号细则与未接通边界 / Numbered rules and remaining integration](../../../../docs/技术开发/附录-小时租赁授权与累计计量源码细则.md) · [56任务/222子项 / Complete backlog](../../../全量代码优先实施清单.md)
