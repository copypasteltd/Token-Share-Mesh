# Original local result custody

`New(approvedNativeTextAdapter, store)` implements the existing once-only `InputAdapter` and a separate `TextOutputProvider`. Wrap it before binding the original `HostKernel`. The pump drains native output into per-result AES-256-GCM records; compute cleanup waits for custody, then delegates to the actual native cleanup receipt. Result reads never submit or renew work.

Use a new, empty, absolute project `runtime/…` directory with `Open(DefaultConfig(root))`. The exclusive marker and refusal of orphan files prevent silently restarting a lost-key store. Keys exist only in memory. Default reservation is 16 × 8 MiB, five-minute retention; the disk budget includes framing and tags. One reader per result; explicit close/reopen reads the same result. `Discard` or retention revokes and destroys it. Active readers or failed deletion retain their quota. Close the result store as well as the supervisor/native adapter you own.

This is **PLAINTEXT/STANDARD local storage protection**, not network E2E, LP, a Windows private-account ACL proof, real model/GPU qualification, trusted usage, buyer ACK or payment authority. Process-memory erasure is best effort. Storage calls may stall; no successful cleanup or terminal is fabricated. Missing original state stays UNKNOWN. Execution remains disabled in the normal node CLI.

[完整 RC-001—060 约束](../../../../docs/技术开发/附录-原生文本结果加密保管与标准协议交付.md) · [原生协议输出](../protocol/textoutput/README.md)。

## R026 DC-001—045 原结果传输与本地接收 / Original result transport and local receipt source

R026新增[DC-001—045](../../../../docs/技术开发/附录-原结果分块传输与本地接收源码.md)：PLAINTEXT/STANDARD原结果分块AEAD保存/完整核验、原提供方上传与原状态恢复、当前买家session绑定短时只读授权、原生加密接收和明确导出、React/Taro与Web显式本地核对签收、取消拒绝/过期退役及七HTTP源码。021/DATA082—084仅源码，当前115规划/150 schema/84平台表/75控制HTTP；40项新测试定义未编译/运行，021未迁移。平台能解密此档内容，不是E2E盲转；SDK/提供方守护编排、独立计量生产/真实资格、E2E/媒体/同卖家重派/持久超时、完整费用冲正/续租/到账对账、历史恢复与CK/GF继续待。全部56任务222子项开放，先全部生产源码/实际接线/测试源码，再集中验证和重录媒体；本次不是整轮完成。
