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

[完整字段/事件/限制/编号与接线细则](../../../../docs/技术开发/附录-固定路由协议转发核心实施细则.md)。生产准入、低权限引擎、SDK兼容矩阵、跨方言、工具、媒体、Sub2API抽取和内容加密仍待完成。
