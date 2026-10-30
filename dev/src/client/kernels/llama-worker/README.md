# Native llama.cpp worker / 原生 LLM 内核

`worker.cpp` implements the private `P11_LLAMA_PIPE_3` protocol over directed inherited pipes. It dynamically calls the frozen llama.cpp C API; it starts no content listener and executes no task-supplied command. The Go owner is [`adapters/llamaworker`](../../adapters/llamaworker/). The [42 numbered constraints](../../../../../docs/技术开发/附录-原生LLM受限管道与可替换worker.md) define authority, IPC, tokenizer, cancellation and acceptance boundaries.

本目录是原生内核源码，不供应真实 llama 动态库或模型。`dependencies.lock.json` 固定头文件和 Windows 原生编译器；构建缓存、第三方头文件、编译器、模型和临时文件都在忽略的 `runtime/`。第三方 MIT 文本见 [许可](../../../../licenses/llama.cpp-LICENSE)。

From the project root / 在项目根目录：

```powershell
python -B -X utf8 dev/tools/构建原生LLM内核.py --fetch --fixture
python -B -X utf8 dev/tools/核验原生LLM内核.py
```

The first command explicitly prepares pinned native build inputs and compiles the worker plus a **synthetic C ABI DLL**. The second runs six tagged tests against that actual Windows process. It does not load real weights, perform real inference, schedule a GPU, qualify LP/E2E, or enable node execution. Linux native C++ compilation/runtime acceptance remains open. No Docker, WSL or other local virtualization is used.

实际集成由可信本机装配程序提供 `Config`，先取得物理资源准备权，再 `Open` 暖机并注册原 `HostKernel`。包、单文件GGUF、内置模板、GPU设备名和资源参数只由本机批准决定；任务只能使用原批准。调用原持久 ticket 的一次提交入口，授权内容 sink 通过 `Read` 接收并清除返回的字节；`Result` 是本地计数，不能直接记账。

Cancellation uses its own pipe. The worker waits for `llama_synchronize`, clears KV and frees per-task context before DONE. A process exit, timeout or kill leaves ambiguous work UNKNOWN. Cleanup additionally wipes the adapter-owned output and returns only the original request/backend/generation/manifest binding. Model weights may remain loaded; context/KV are not shared across buyers.

当前进程使用调用者账户；Windows Job Object/匿名管道并不构成独立低权限服务或硬件保密。真实上游运行库/模型许可与ABI、具体GPU映射、显存/性能、三SDK输出/SSE/ACK、可信usage及长期宿主执行入口仍需实施与验收。更新任何原生内核必须重新锁定版本、包摘要和资格，不得热换DLL后继承旧批准。

R022 adds actual input count to STARTED and the matched original stop index to DONE (UINT32_MAX otherwise). The result custodian drains into local encrypted storage before compute cleanup; [RC-001—060](../../../../../docs/技术开发/附录-原生文本结果加密保管与标准协议交付.md) covers its separate lifetime and three protocol renderers. This remains caller-account STANDARD code, not LP/E2E or real GPU qualification.

R023 separates the pinned main library, backend registry and backend core in INIT. The official Windows package uses llama.dll / ggml.dll / ggml-base.dll. Explicit frozen CUDA/model preparation: `python -B -X utf8 dev/tools/准备真实LLM验证包.py --fetch --model-mirror hf-mirror`; explicit actual model test: `python -B -X utf8 dev/tools/核验真实LLM内核.py --attempt 1` (choose a fresh attempt number if retained evidence exists). These inputs are not included in Git or an automatic install. See [NF constraints](../../../../../docs/技术开发/附录-真实原生LLM包校验与本机接入.md) for package pinning, no-execution inspection and retained failures.
