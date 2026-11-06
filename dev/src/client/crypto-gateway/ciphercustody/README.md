# Original ciphertext custody / 原密文托管

**SOURCE_ONLY_UNVERIFIED — not compiled or executed.** This package keeps the original signed BINARY_STREAM_1 ciphertext under its enclosing native OS root and single-writer lock. It is not LOCAL_SEGMENTED_1, plaintext storage, execution authorization, trusted metering or buyer acceptance. See the [IC technical specification](../../../../../docs/技术开发/附录-原密文托管与不可变补传源码.md).

首次调用先按完整 wire 上限预留容量并持久化 WRITING，再执行 producer。READY 重试只返回原 handle/manifest，不再读取 source、加密或执行模型。中断或提交未知保留原状态；重开将 WRITING 记为 INTERRUPTED，不猜测 push 状态、不隐式新建 epoch。未知原文件或丢失已锚定日志均失败关闭。

`Produce` creates one original outbound object. `Import` retains one original inbound manifest and its bounded ciphertext. `WithOriginal`/`CopyRange` authenticate the complete opaque signed object before exposing original cipher bytes. Their opaque checks prove neither private AEAD/FINAL nor semantic INPUT validity; the parent `ConsumeStored` barrier remains required. Failed transport preserves READY for an exact-byte retry.

`Expire` journals RETIRING before cancel/join/delete, keeps conservative quota on uncertain removal and compacts only expired tombstones. Two payload owners share explicit capacity; one reserved serial cleanup owner can cancel them even at full capacity. `BeginClose` signals cancellation; `Close` joins actual owners before clearing the metadata wrapping key. Parent roots must cancel both vault and spool before joining either nested owner.

Capacity is an explicit 66–512 MiB policy with 1–64 objects, a 2 MiB AEAD journal and 64 KiB copy buffers. These are design budgets, not measurements or the still-pending complete 4 MiB flow/reorder budget. No listener, content CLI, plaintext export, caller ACK retirement or automatic model loop is included.

现有测试定义仅源码：结构性签名假密文夹具只覆盖托管/边界/恢复；真实 native secretstream 组合另有 build-tag 测试源码。二者都未运行，不形成设备、内存性能、隐私、签收或渠道资格。R026 的 56/222 全量代码齐备后才统一验证。
