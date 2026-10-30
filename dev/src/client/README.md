# Native provider control / 原生提供方控制

R008 implements the p11-node control foundation in the shared Go module. Windows current-user DPAPI is locally exercised; Linux Secret Service is implemented but has protocol-fixture/cross-build evidence only. This is not an inference engine, resource scheduler, content encryption gateway or complete provider release.

R008实现原生控制基座：本人显式初始化/登记/前台运行，加密本地状态及原命令恢复。当前心跳只报UNKNOWN，execution_enabled=false，不自授可售资格，不接受任务、下载模型、启动内核或打开监听端口。

| Path / 路径 | Responsibility / 职责 |
| --- | --- |
| cmd/p11-node | Foreground CLI; bounded stdin, safe output and cancellation / 前台命令、受控标准输入、安全状态和取消 |
| node | OS key adapters, AEAD journal, process locks, recovery and heartbeat / 系统钥库、加密日志、进程锁、恢复及心跳 |
| protocol/devicecontrol | Shared API/native DTOs, enrollment transport and request proofs / 服务端共用DTO、登记传输及签名证明 |

Build from the repository root / 从项目根构建：

```text
python -X utf8 dev/tools/核验原生源码.py --round R008
```

Outputs remain under runtime/bin. Explicit commands are init, enroll, recover, status, heartbeat and run, all requiring --state with an absolute private directory. init consumes public JSON on piped stdin; enroll consumes the once-only owner pairing token on piped stdin. No secret arguments/environment or plaintext fallback are supported. Never initialize over existing state after an unknown result.

输出在runtime/bin。所有命令均显式指定本人绝对状态目录；新建目录的父目录须已存在。Windows用本人/SYSTEM ACL和DPAPI；Linux需本人已解锁且运行中的Secret Service。缺钥、坏状态、重复写进程均停止，不静默重建身份。对外只输出不含秘密的型号/设备/版本/序号投影。临时数据、模型及凭据不能进入本源码目录。

See [bilingual usage](../../repository/USAGE.md), [complete recovery specification](../../../docs/技术开发/附录-原生提供方控制代理与本地恢复.md) and [R008 evidence](../../tests/evidence/R008-验证报告.md). Future Windows/Linux engine adapters and LMA-derived Android execution remain separate pending work. No local virtualization is used or required.
