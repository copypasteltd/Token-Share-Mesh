# Usage / 使用指南

## Existing runnable software / 当前可运行内容

Use a native Windows amd64 development machine, Python 3.13 and Node.js 22+.
Execute the following commands from the clone root. Linux is a target platform;
the current Windows bootstrap is not a Linux installer.

使用 Windows amd64 原生开发环境、Python3.13、Node.js22+，命令从克隆根执行。Linux 为目标平台，当前 Windows 工具入口不是 Linux 安装器。

```powershell
python -X utf8 dev/tools/文档工具.py bootstrap
python -X utf8 dev/tools/文档工具.py build
python -X utf8 dev/tools/文档工具.py check
python -X utf8 dev/tools/预览文档.py --port 8765
```

Visit the printed local URL. Search by `P11-F74`, `端对端` or another keyword;
click a result to reach the original chapter. Escape clears a query. Printing
uses the reader's print view. Stop the preview with Ctrl+C. An occupied port can
be avoided with `--port 0`, which prints the chosen loopback port.

访问打印出的本机地址，搜索编号/关键词，点击结果跳转，Escape 清空；阅读器支持打印视图。Ctrl+C 停止。端口占用时用 `--port 0` 自动选择并打印本机端口。

```powershell
python -X utf8 dev/tools/获取原生工具链.py
python -X utf8 dev/tools/原生开发.py mod download
python -X utf8 dev/tools/原生数据库验证.py --round R003
python -X utf8 dev/tools/核验原生源码.py --round R003
python -X utf8 dev/tools/整理原生验证证据.py --round R003
```

The database command creates its own random-credential, loopback-only PostgreSQL17
instance inside `runtime/db/integration/`. It runs actual migrations and tests and
confirms shutdown. It does not connect to your other databases. For non-ASCII
Windows paths it temporarily aliases this project path and removes only its own
alias after shutdown confirmation. If cleanup fails, recover that specific
instance before removing the retained alias. Never stop every PostgreSQL process
or delete another project's drive mapping.

数据库入口在项目内创建随机凭据、仅 loopback 的 PostgreSQL17 实例，执行真实迁移/测试并确认关闭，不连接其他数据库。中文路径通过临时路径别名处理，停库确认后只撤销自己创建的映射。失败时先恢复、停止明确的本次实例，不能批量停所有 PostgreSQL 或删除其他项目映射。

## Existing database CLI / 已有数据库命令

For a PostgreSQL17 development database you explicitly selected, provide
`P11_DATABASE_URL` through your local environment. Do not commit a connection
string or put a password in an example command. The command names are:

对本人明确选择的 PostgreSQL17 开发库，通过本机环境提供 `P11_DATABASE_URL`，不把连接串提交或把密码写入示例命令。

```powershell
python -X utf8 dev/tools/原生开发.py run ./api/cmd/p11-admin migrate
python -X utf8 dev/tools/原生开发.py run ./api/cmd/p11-admin check-schema
```

`migrate` uses a transaction advisory lock and immutable migration hashes.
`check-schema` checks the presence/count of domain tables and migration metadata;
it is not a full schema-diff, permission or recovery audit. No destructive down
migration or database-reset command is provided. Production role separation and
full restart/upgrade recovery remain in DEV-02.

`migrate` 有同事务互斥和不可变摘要；`check-schema` 只检查领域表数量及迁移元数据，不等于完整差异、权限或恢复审计。没有 down/清库命令；正式权限分离和完整升级、恢复仍在 DEV-02。

## Intended buyer journey / 规划买方流程

| Step / 步骤 | Buyer decision and system behavior / 买方决策及系统动作 |
| --- | --- |
| Discover / 发现 | Select an actual seller and approved capability / 选择实际卖家及获准能力 |
| Confirm / 确认 | Read model/workflow, processors, mode/privacy, total fees, budget, limits and deadline / 查看模型、处理方、模式隐私、总费、预算限额和期限 |
| Contract / 成约 | Accept an immutable quote/order; no silent seller replacement / 确认不可变报价订单，不静默换卖家 |
| Pay / 支付 | Complete authorized channel interaction; verify proof under the original intent / 完成授权交互，按原意图验付 |
| Pair / 配对 | E2E requires buyer-local or explicitly paired trusted-LAN gateway and verified recipient / 加密需本机或已配对可信LAN网关及接收端核验 |
| Invoke / 调用 | Configure the contract route and standard SDK base URL; unsupported profiles reject / 配置合同路由和标准SDK入口，不支持的档位拒绝 |
| Accept / 接受 | Authenticate result, confirm acceptance or submit bounded rejection evidence / 认证结果并接受或提交有界拒绝证据 |
| Resolve / 售后 | Follow order/refund/dispute state without disclosing unnecessary encrypted content / 按订单退款争议状态处理，不额外披露加密正文 |

This journey is not an instruction to call a currently running marketplace API;
there is no completed HTTP trading/model service through R003.

此表为产品使用规格，R003仍无已完成的HTTP交易/模型服务可直接调用。当前可测试的交易入口是`internal/market.Catalog.List`与`internal/trade.Service`的CreateQuote/GetQuote/CreateOrder/GetOrder；只有已验证凭据适配器能构造Principal，不能从普通请求自报角色。

R003 creates bounded quotes and WAITING_PAYMENT orders only. It does not reserve GPU capacity or channel funds. Hourly rental sessions/renewals, budget transactions and trusted usage remain pending. See [the exact subset and historical price boundaries](../../docs/技术开发/附录-NB价版与交易服务实施约束.md).

R003仅产生有界报价与WAITING_PAYMENT订单，不占GPU或冻结渠道资金。小时会话/续租、预算事务与可信usage仍待完成；[实施子集与旧价格边界](../../docs/技术开发/附录-NB价版与交易服务实施约束.md)逐项说明。

## Intended provider journey / 规划卖方流程

Pair the enrolled device, confirm software/model/node installation, set available
time and resource limits, pass capability/permission checks, set a listing price
and opt in to work. The provider approves what may run; the scheduler selects a
permitted task within the accepted contract. Switching kernels stops new sales,
drains current owners, validates the replacement environment and switches only
under the shared resource lock.

设备配对后确认软件/模型/节点安装，设置时段和资源上限，通过能力/许可检查，自主定价并开接单。卖方批准可运行内容，调度器只在合同内选择。换内核先停新售、排空当前任务、验证新环境，再在共享资源锁下切换。

Providers see approved versions, resource/stage and earnings information, and can
stop new work. Privacy tasks omit prompt/output browsing. Android participation
requires real-device lifecycle, charging, network, temperature and consent checks;
an emulator or a claimed SoC capability is not proof of eligible supply.

卖方可见批准版本、资源、阶段和收益，能停新单；隐私任务不提供正文浏览。Android 必须真机验证生命周期、充电、网络、温度和同意；模拟器或芯片型号不能证明可售供给。

## Evidence / 验证口径

[R001 evidence](../tests/evidence/R001-验证报告.md) covers 10 native foundation cases.
[Implementation progress](../progress/README.md) covers every pending task. No
real OAuth/payment, physical GPU/Android, E2E interoperability or whole-platform
acceptance is implied by a successful document build or CLI cross-compilation.

[R001证据](../tests/evidence/R001-验证报告.md)覆盖10项原生基础场景；[实施进度](../progress/README.md)列全部缺口。文档构建或 CLI 交叉编译不代表真实登录/支付、GPU/手机、加密互通或完整平台验收通过。

[R003 evidence](../tests/evidence/R003-验证报告.md) covers 22 native cases, including pricing, qualification, quote/order concurrency and revoked-role replay. Temporary DB approvals are synthetic fixtures and cannot qualify real supply. The scripts default to R003 and retain the historical R001 evidence.

[R003证据](../tests/evidence/R003-验证报告.md)覆盖22项原生场景，含计价、资格、报价/订单并发和撤销角色回放。临时库审批是合成夹具，不解锁真实供给。工具默认R003，保留R001历史证据。

