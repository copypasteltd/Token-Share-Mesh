# Private ComfyUI host boundary / 私有Comfy宿主边界

**SOURCE_ONLY / 未编译、未运行。** `cipher.go` implements a bounded read-only AES-GCM media reader and single-use private receipt bound to an independently supplied original session/invocation/local generation/environment/recipe. `wire.go` bounds internal worker frames before allocation and provides separate content/control writers. This is not yet a process launcher, rental Backend, public endpoint or payment/cleanup qualifier.

See [MW-001—044](../../../../../docs/技术开发/附录-ComfyUI私有媒体执行与本地加密源码.md) and the [private Python worker](../../kernels/comfy-private/README.md). The local file format is neither BINARY_STREAM_1 nor network E2E; worker receipts contain private keys and must never enter provider UI, logs, databases or public API responses.

## Ownership / 所有权

`DecodeReceipt` receives only an authenticated original worker result frame, plus the host's independent expected scope/kind. It rejects extra/duplicate fields, noncanonical keys, foreign paths/scopes, bad record counts and unsupported media. `OpenCipher` consumes the receipt even on failure; copies share the same one-use state. It opens an existing basename in the original owned `os.Root`, authenticates the exact header/END and each requested data record, and caches one 64KiB block. Close revokes copied reader aliases and drops owned key/cache references. Format/JSON output is redacted.

原宿主必须持续持有原工作区根/权限、原节点OS锁和主密钥所有权，先登记工作区，再创建文件。该库不重建缺钥、创建新目录、重跑模型或替其他owner清文件。Python STOPPED/CLOSED、进程退出、取消ACK或kill均不能单独变成平台清理/计量/资金凭证；正式适配器还须核验原inbox/双围栏并等待实际process/pipe join。

从Frame取出的私有字节由原宿主负责在解码/取钥后及时clear；不得打印、落库或交给UI。库无法替调用者擦除另外保存的字节/string/native副本，Close也不承诺法证级RAM清除。

All authored tests remain unexecuted. Synthetic independent format fixtures are not ComfyUI/model/GPU, low-privilege, E2E, trusted metering or performance evidence. Full production integration and all 56/222 code/test sources precede batch validation. No SQL is changed.

## R026 测试定义编号索引 / Test source index

完整MW测试定义列表见[Python组件README](../../kernels/comfy-private/README.md#r026-测试定义编号索引--test-source-index)。全部只是源码，尚未执行。
