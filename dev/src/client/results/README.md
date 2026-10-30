# Original local result custody

`New(approvedNativeTextAdapter, store)` implements the existing once-only `InputAdapter` and a separate `TextOutputProvider`. Wrap it before binding the original `HostKernel`. The pump drains native output into per-result AES-256-GCM records; compute cleanup waits for custody, then delegates to the actual native cleanup receipt. Result reads never submit or renew work.

Use a new, empty, absolute project `runtime/…` directory with `Open(DefaultConfig(root))`. The exclusive marker and refusal of orphan files prevent silently restarting a lost-key store. Keys exist only in memory. Default reservation is 16 × 8 MiB, five-minute retention; the disk budget includes framing and tags. One reader per result; explicit close/reopen reads the same result. `Discard` or retention revokes and destroys it. Active readers or failed deletion retain their quota. Close the result store as well as the supervisor/native adapter you own.

This is **PLAINTEXT/STANDARD local storage protection**, not network E2E, LP, a Windows private-account ACL proof, real model/GPU qualification, trusted usage, buyer ACK or payment authority. Process-memory erasure is best effort. Storage calls may stall; no successful cleanup or terminal is fabricated. Missing original state stays UNKNOWN. Execution remains disabled in the normal node CLI.

[完整 RC-001—060 约束](../../../../docs/技术开发/附录-原生文本结果加密保管与标准协议交付.md) · [原生协议输出](../protocol/textoutput/README.md)。
