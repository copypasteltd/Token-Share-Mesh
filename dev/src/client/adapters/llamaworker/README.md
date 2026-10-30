# Local pinned native LLM adapter / 本机批准原生 LLM 适配器

`LoadPackage` accepts a locally installed `P11_LLAMA_PACK_1` document with an independently supplied SHA-256, owner selection and approved environment digest. It rejects ambiguous JSON, escaping/nonportable paths, unlisted or changed libraries, changed weights/templates and mismatched environments. It starts no process. `PackageEnvironment` calculates an identity only; it grants no device, marketplace or execution permission.

本机批准参数包括具体原生显卡名、GPU 层数、上下文、批量和线程数。它们全部参与环境摘要；资源和能力由原 Host 规则再次核验。GPU 选择失败不能继承原批准退到 CPU；明确 CPU 选择产生另一份摘要。安装更新先停止旧进程、准备新目录并重新批准，不能在运行中换 DLL。

`Open` rechecks every pin, starts one owned process and uses `P11_LLAMA_PIPE_3`. INIT explicitly separates the llama main library, backend registry and backend core. The official frozen Windows build uses `llama.dll`, `ggml.dll` and `ggml-base.dll`; a synthetic combined DLL cannot establish that real packaging contract. The same exact library-directory allowlist contains CUDA dependencies and CPU support libraries; supporting CPU operators is not permission to silently replace the selected GPU.

`LocalInfo` exposes this owned PID and approved engine/model/template/environment/GPU metadata. It contains no prompt, generated content, IPC handles or key. Selected GPU metadata and loaded module observations do not prove physical resource mapping, all-layer offload or performance qualification.

The trusted embedding obtains original preparation authority before `Open`, registers the adapter through the matching `HostKernel`, wraps it in the encrypted result custodian, and submits only the original durable ticket. Use the original work for start/query/cancel/cleanup and the separate original result binding for output. Package loading and inspection grant none of those capabilities.

实现边界及可复现步骤见 [NF 约束](../../../../../docs/技术开发/附录-真实原生LLM包校验与本机接入.md)、[NK 管道](../../../../../docs/技术开发/附录-原生LLM受限管道与可替换worker.md) 和 [RC 结果保管](../../../../../docs/技术开发/附录-原生文本结果加密保管与标准协议交付.md)。真实模型测试为显式独立标签；常规测试不自动下载、启动模型或占用显卡。节点控制执行仍默认关闭，LP/E2E、可信 usage、最终 ACK 和资金资格分别验收。

## R024 current contract

Current PIPE_4 and environment v2 add independently selected GPU UUID/PCI. Metadata-only PROBE returns a bounded inventory through owned directed pipes and exits before any model/context load. Windows system CUDA Driver metadata is cross-checked against the selected GGML backend PCI before model loading, and READY is independently checked in Go. LocalInfo now contains observed physical identity; this is not remote attestation, performance or privacy qualification. Linux GPU identity is not implemented in this round. See [GI requirements](../../../../../docs/技术开发/附录-物理显卡身份探测与启动绑定.md). The older PIPE_3 wording above describes the R023 baseline; GI supersedes the current wire details.
