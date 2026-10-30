# Bound native relay core / 固定路由原生转发核

R012 implements a Go library for three bounded, same-dialect text profiles: OpenAI Chat Completions, stateless Responses and Anthropic Messages (2026-10-21). It has no new dependencies and contains no copied Sub2API code. It is not mounted by p11-api or p11-node.

配置一份不可变卖家/合同/路由/档位/模型/精确后端地址，以及显式Authorizer、SecretSource后，通过New创建Core。将其作为受控http.Handler，或用Forward取得Outcome；结束Close释放空闲连接。没有默认放行准入/密钥实现，不提供可启动的公开模型监听命令。

The trusted Authorizer must durably validate and reserve the original route, funding/budget and current execution owner before returning a bound, revocable Grant. Unknown submission must query the original backend. Forward closes HTTP on cancellation and reports protocol outcomes; it cannot establish real engine stop, clear an owner or turn upstream usage into billable evidence. Trusted ports must honor context.

STANDARD只允许平台明文域，E2E_BASIC只允许未来卖方认证解密后的执行域；本库没有完成加解密。端点/凭据不受买方JSON控制，拒绝未支持字段和模式，不自动重放、重定向或跨卖家兜底。ResponseWriter必须支持读写截止与flush。

Run from repository root:

```powershell
python -B -X utf8 dev/tools/原生开发.py test -count=1 ./gateway/core
```

Tests use explicit synthetic admission/secret ports and owned native loopback HTTP servers. They do not call paid models or qualify SDKs, GPUs, Android, payments, privacy performance or E2E interoperability.

The exact-field follow-up passes all 20 relay cases. The latest full rerun failed seven existing short-deadline cases; 114 unchanged-package cases retain only byte-verified evidence from the initial published full pass. A serialized diagnostic also failed six cases. See the [current limited evidence](../../../tests/evidence/R012-验证报告.md); this is not a current full-suite pass. / 字段修复后转发20项通过，末次全量7项失败，114项仅复用初次完整通过的相同源码对照；未放宽时限/清理规则。

[完整字段/事件/限制/编号与接线细则](../../../../docs/技术开发/附录-固定路由协议转发核心实施细则.md)。生产准入、低权限引擎、SDK兼容矩阵、跨方言、工具、媒体、Sub2API抽取和内容加密仍待完成。


R015 update: the original failed R012 attempts remain historical evidence; the latest complete source-validation report is [R015](../../../../dev/tests/evidence/R015-验证报告.md). The new confirmed-order resolver supplies configuration only, not this core's Authorizer/Grant. Production invocation intent, funds, call-count and lease integration remain pending.

## R016 integration boundary / 原调用准备边界

R016 adds original-call preparation: current scoped-key, original order and same-order funding facts are rechecked, then INPUT_PENDING, the full per-call budget hold, one call slot and a safe event commit atomically. Original-key recovery does not reserve twice; changed parameters or keys conflict. Cancellation, expiry and unknown results retain the original intent and hold. Only a versioned HMAC of the transient request digest is persisted. Forward migration 008 adds six columns; 001–007 and native SQLite DDL retain their exact bytes. The scope remains 34/93 operations and 65 tables, with three native text profiles under PLAINTEXT/STANDARD. There is no model listener, Lease/Grant issuer or inference. Input-ready/resource admission, trusted usage, real payments, E2E and route UI remain pending. See [RI specification](../../../../docs/技术开发/附录-原调用准备与次数预算原子接线.md). The preparation result is never this core’s Authorizer/Grant.

R016新增原调用准备库：当前scoped key/原订单/同单资金事实重新核对后，将INPUT_PENDING、整笔单次预算、次数占用及安全事件在同一事务提交。原键恢复不重复占用；改参数/key冲突；取消/过期/未知保留原记录和占用。请求摘要只以版本化HMAC持久保存。前向008追加六列，001—007和端侧DDL原字节不改；仍34/93操作、65表。仅三个原生文本档位的PLAINTEXT/STANDARD；没有挂载模型入口、签发Lease/Grant或运行模型。输入就绪/资源原子准入、可信usage、真实支付、E2E和路由UI仍待。当前PrepareTx仅SQL组合，不能在提交前触发本转发核或任何后端请求。
