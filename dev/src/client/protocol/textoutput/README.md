# Native text protocol output

`Render(ctx, sink, originalOutput, DefaultLimits())` consumes an already authorized `supervisor.TextOutput`. It renders original OpenAI Chat, OpenAI Responses or Anthropic Messages as JSON/SSE. It creates no listener, route grant, execution permission or credential. The caller supplies a bounded context-aware Sink; `NewHTTPSink` requires real write-deadline support on an already authorized response writer.

Split UTF-8 is reassembled; invalid/truncated bytes, missing authenticated END, invalid counts/stop metadata, cancellation, writer failure and size overflow cannot produce a successful terminal sequence. Chat honors original include_usage. Messages starts with actual input count and returns the actual matched stop string. Responses preserves sequence/item/content identity, store=false and completed/incomplete output.

Defaults: JSON 512 KiB; each SSE JSON 64 KiB; aggregate plaintext 512 KiB. Responses repeats full text in final events, so its serialized event limit can fail earlier. Capability admission must explicitly budget this before public execution; that production wiring remains pending. All final events are encoded/bounded before any terminal event is emitted. A Receipt confirms local writes only; counts are protocol candidates, not trusted billing, final buyer ACK or settlement.

Read handles always close. Result lifetime and compute lifetime remain separately managed. Current tests use real controlled HTTP and the existing strict relay plus separate synthetic native ABI integration; independent official SDK/real model/GPU/privacy qualification remains pending.

[完整 RC 规范](../../../../../docs/技术开发/附录-原生文本结果加密保管与标准协议交付.md)。
