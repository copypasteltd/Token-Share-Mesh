# Original-rental media gateway / 原租赁媒体入口

`CODE_FIRST / SOURCE_ONLY_UNVERIFIED`. This package opens no listener. The real
node binds it through `ComfyRental.OpenMediaGateway` after original preparation
and authorization. It uses the existing COMFYUI / GPU_RENTAL / STANDARD session,
seller, device, physical owner and fences. It does not create a text T1 job or
charge a media result, request or token fee; rental accounting remains hours.

Profiles cover approved single-PNG Images, explicitly selected **legacy** Videos,
WAV-only Audio speech, and a declared Responses background asset schema for GLB
or WAV. Videos references a retired API shape, not current Sora availability.
Anthropic Messages remains a text interface. Unsupported fields, remote URLs,
graphs, files, encoders and silent format/model/privacy fallbacks are rejected.

The handler owns bounded multipart staging, original-id HMAC replay, output
capacity reserved **before** Seal/Invoke, complete authenticated result reads,
encrypted RAM caches and authenticated single-range downloads. Each operation
checks its original scope. Export does not imply queue-management authority.
Queries do not extend retention or resubmit uncertain work. Actual HTTP calls,
body cancellation callbacks, cache keys and expiry work join before parent-node
cleanup. A write receipt is not final buyer ACK, usage, payment or stop proof.

本包只有未验证源码。完整技术细则、MG-001—036、请求示例、权限表、内存预算和全部
后续门槛见[技术附录](../../../../docs/技术开发/附录-原租赁媒体兼容入口与加密缓存源码.md)。
本地 AES-GCM 暂存不是网络端对端加密、硬件保密或低权限资格。真实 SDK/网络入口、
完整引擎配方、输出尺寸时长、双方 ready、可信小时计量、付费续租、最终 ACK、
平台资金释放和全量组合验收仍待。所有测试定义未编译、未运行。

## R026 MG-001—036 原租赁媒体入口与加密缓存源码 / Original-rental media gateway source

R026新增[MG-001—036](../../../../docs/技术开发/附录-原租赁媒体兼容入口与加密缓存源码.md)：原租赁本机STANDARD媒体入口、Images/明确旧版Videos/WAV speech/Responses资产映射、严格请求/独立scope、AES-GCM暂存/原HMAC幂等/提交前缓存配额、完整认证结果后加密缓存/本人单Range和原节点真实join源码。37项新增测试定义均未编译/运行。公开SDK/网络监听/E2E媒体传输/最终ACK、双方ready/可信小时计量/付费续租/物理停工对账/平台资金释放、完整实际配方/低权限/性能仍待。全部56任务222子项开放；无SQL变更或迁移。完整生产代码/实际依赖/测试源码齐备后统一验证，本次不是开发轮完成。
