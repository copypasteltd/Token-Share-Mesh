# Original native text SDK ingress

SOURCE_ONLY, uncompiled and unexecuted. R026 stays CODE_FIRST until all 56 tasks / 222 steps have complete production wiring and test source. See [SI-001–040](../../../../../docs/技术开发/附录-标准SDK原订单文本入口源码.md) and [round rules](../../../../开发轮次执行规则.md).

Three exact POST endpoints serve bounded non-streaming native JSON: `/v1/chat/completions`, `/v1/responses`, `/v1/messages`. They require a stable original `Idempotency-Key`, an authenticated original route-bound key and a previously confirmed/funded C2C contract. The module never picks a fallback seller, accepts a buyer result, signs trusted usage or creates a payment.

The production `invocation.SDK` performs atomic budget/input/first-lease admission and rechecks the current original key, route and buyer for each result chunk and before first response bytes. It uses the existing native text renderer after full content verification. API-host configuration is opt-in and must match independently provisioned issuer pins. Unsupported streaming and beta/tool/media requests fail before admission.

Limits: 256 KiB request, 1 MiB original result text, 8 MiB encoded response, four concurrent requests, 120-second maximum wait. These are explicit budgets, not measurements. No new result files, listening subprocesses or SQL migrations are introduced. Host closure cancels and joins owned callers before erasing signer copies.

SDK auto-payment, provider-daemon orchestration, live SSE, E2E/media data-plane delivery, independently qualified meter production and complete original-channel settlement remain open. A 200 native response is read-only delivery; explicit buyer decision remains required through the control plane. Tests here are synthetic source definitions, not end-to-end certification.
