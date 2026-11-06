# Private ComfyUI host boundary / 私有Comfy宿主边界

**SOURCE_ONLY / 未编译、未运行。** `cipher.go` implements a bounded read-only AES-GCM media reader and single-use original-scope receipt. `config.go` and `process*.go` add the original environment reference, fixed native Python/three pipes, PIPE_2 preparation/one-shot grant ACK, independent control deadlines and actual process/pipe join. The rental-media Backend, authenticated workspace/input, public delivery, complete runtime archives and payment/cleanup qualification remain open.

See [MW-001—044](../../../../../docs/技术开发/附录-ComfyUI私有媒体执行与本地加密源码.md) and the [private Python worker](../../kernels/comfy-private/README.md). The local file format is neither BINARY_STREAM_1 nor network E2E; worker receipts contain private keys and must never enter provider UI, logs, databases or public API responses.

## Ownership / 所有权

`DecodeReceipt` receives only an authenticated original worker result frame, plus the host's independent expected scope/kind. It rejects extra/duplicate fields, noncanonical keys, foreign paths/scopes, bad record counts and unsupported media. `OpenCipher` consumes the receipt even on failure; copies share the same one-use state. It opens an existing basename in the original owned `os.Root`, authenticates the exact header/END and each requested data record, and caches one 64KiB block. Close revokes copied reader aliases and drops owned key/cache references. Format/JSON output is redacted.

原宿主必须持续持有原工作区根/权限、原节点OS锁和主密钥所有权，先登记工作区，再创建文件。该库不重建缺钥、创建新目录、重跑模型或替其他owner清文件。Python STOPPED/CLOSED、进程退出、取消ACK或kill均不能单独变成平台清理/计量/资金凭证；正式适配器还须核验原inbox/双围栏并等待实际process/pipe join。

从Frame取出的私有字节由原宿主负责在解码/取钥后及时clear；不得打印、落库或交给UI。库无法替调用者擦除另外保存的字节/string/native副本，Close也不承诺法证级RAM清除。

All authored tests remain unexecuted. Synthetic independent format fixtures are not ComfyUI/model/GPU, low-privilege, E2E, trusted metering or performance evidence. Full production integration and all 56/222 code/test sources precede batch validation. No SQL is changed.

## R026 测试定义编号索引 / Test source index

完整MW测试定义列表见[Python组件README](../../kernels/comfy-private/README.md#r026-测试定义编号索引--test-source-index)。全部只是源码，尚未执行。

## R026 CH-001—036 原环境与宿主源码继续 / Original environment and host source

R026新增[CH-001—036](../../../../../docs/技术开发/附录-ComfyUI原环境持有与本机进程接线源码.md)：原Comfy环境运行持有、固定原生进程/三管道、PIPE_2准备/一次原授权ACK/单调截止和原ProviderRental准备接线。29项新测试定义未编译/运行；CH-033—036正式租内媒体Backend、认证工作区/恢复清理、公开交付、完整包/低权限/E2E、独立ready/计量/付费续租/平台释放仍待。全56任务/222子项开放，无SQL变更、迁移或真实执行；完整代码后统一验证，源码检查点不是开发轮完成。

## R026 WS-001—030 原媒体工作区与结果托管源码 / Original media workspace and custody source

R026新增[WS-001—030](../../../../../docs/技术开发/附录-租内媒体工作区与结果托管源码.md)：原租赁媒体工作区/native003元数据预留、原节点目录持有/退役删除/清理专用恢复、认证RAM媒体输入、专用子调用适配器和结果钥托管/读取撤销。23项新测试定义均未编译/运行；公开SDK、完整媒体会话监督/独立停工对账/ready/续租/可信水位/平台释放、完整实际包/低权限/E2E/性能仍待。全56任务/222子项开放，只登记native003源，未迁移/真实执行；完整代码后统一验证，源码检查点不是开发轮完成。

## R026 MS-001—036 原媒体会话监督与停止回收源码 / Original media-session supervision source

R026新增[MS-001—036](../../../../../docs/技术开发/附录-原租赁媒体会话监督与停止回收源码.md)：原节点持有的本机媒体会话监督/独立单调截止、真实Comfy叶子、一次原授权/单份输入及实际join、先停工后回收、两类会话共关闭、原媒体结果保留/撤销和本机会话清理CAS源码。27项新增测试定义均未编译/运行；公开媒体SDK/最终ACK、双方ready/可信小时水位/付费续租/平台资金释放、独立物理停工恢复及完整实际运行包/低权限/E2E/性能仍待。全部56任务222子项开放；本次无SQL变更或迁移。完整生产代码/实际依赖/测试源码齐备后统一验证，源码检查点不是开发轮完成。
