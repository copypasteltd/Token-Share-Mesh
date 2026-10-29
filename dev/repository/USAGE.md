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
python -X utf8 dev/tools/原生数据库验证.py --round R004
python -X utf8 dev/tools/核验原生源码.py --round R004
python -X utf8 dev/tools/整理原生验证证据.py --round R004
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

## Native identity/control API / 原生身份控制接口

Configure these values explicitly in your local environment. All *_FILE values point to files you own inside the ignored runtime area; no real secret belongs in source or example commands. HMAC files contain at least 32 bytes and must stay stable across restarts. The identity digest key requires a migration before replacement. RSA material uses one PEM key per file, at least 2048 bits. This adapter supports RSA public-key mode, not certificate mode.

在本机环境显式配置下表。*_FILE指向runtime内本人持有的文件，不将真实秘密放入示例命令/源码。HMAC文件至少32字节，重启不能随意重生；更换身份摘要密钥须先做迁移。RSA每文件一把PEM密钥、至少2048位；当前为公钥模式，未实现证书模式。

| Variable / 变量 | Meaning / 含义 |
| --- | --- |
| P11_DATABASE_URL | Explicitly selected migrated development DB / 已选且已迁移的开发库 |
| P11_ALIPAY_APP_ID | Authorized application ID / 本应用ID |
| P11_ALIPAY_SUBJECT_FIELD | Explicit OPEN_ID or USER_ID; no fallback / 明确选字段，不回退 |
| P11_ALIPAY_PRIVATE_KEY_FILE | Application RSA private key path / 应用RSA私钥路径 |
| P11_ALIPAY_PUBLIC_KEY_FILE | Trusted Alipay RSA public key path / 可信支付宝RSA公钥路径 |
| P11_ALIPAY_OAUTH_RETURN_URL | Registered WEB callback; omit only if WEB login stays closed / Web已登记回调，未配则Web登录关闭 |
| P11_AUTH_PEPPER_FILE | Stable session/CSRF HMAC secret / 稳定会话摘要密钥 |
| P11_IDENTITY_PEPPER_FILE | Stable identity/code/browser-binding HMAC secret / 稳定身份、授权码和浏览器绑定摘要密钥 |
| P11_API_KEY_PEPPER_FILE | Stable API-key HMAC secret / 稳定API密钥摘要密钥 |
| P11_CURSOR_KEY_FILE | Stable signed-page cursor secret / 稳定分页签名密钥 |
| P11_BROWSER_ORIGIN | One exact allowed browser origin, no path / 唯一明确允许的浏览器源，无路径 |
| P11_API_ADDR | Optional numeric loopback, default 127.0.0.1:8787 / 可选数字本机地址 |
| P11_INSECURE_LOOPBACK | Set 1 explicitly for HTTP loopback development cookies / 本机HTTP开发显式为1 |

```powershell
python -X utf8 dev/tools/原生开发.py run ./api/cmd/p11-api
```

The command requires migration version 3 and listens only on loopback. It does not migrate, create test identities or execute models/payments. No server deployment, TLS setup or observability work is introduced. For local HTTP development use an exact loopback browser origin and explicit insecure-loopback flag; development cookies have separate names. Default Cookie policy remains Secure/HttpOnly/Strict.

命令要求迁移版本3，只监听loopback，不自动迁移、造测试身份或执行模型/支付。不增加部署/TLS/可观测性工作。本机HTTP使用精确loopback浏览器源和显式开发标志，Cookie名称独立；默认策略仍为Secure/HttpOnly/Strict。

| Step / 步骤 | Miniapp / 小程序 | Web / 浏览器 |
| --- | --- | --- |
| Bind / 绑定 | In-memory verifier, POST /auth/challenges with MINIAPP and base64url SHA-256 / 内存verifier与挑战 | Same with WEB; HttpOnly login binding and state-bound authorization URL / WEB挑战及只读绑定Cookie |
| Authorize / 授权 | my.getAuthCode returns a fresh auth code / 取得新授权码 | Authorized callback checks state; scrub temporary code from address / 回调核state并清临时地址 |
| Exchange / 兑换 | POST /auth/sessions with challenge_id/auth_code/code_verifier / POST兑换 | Same plus web_state, exact Origin and binding Cookie / 另带state、Origin和Cookie |
| Use / 使用 | In-memory once-returned access_token, Bearer / 仅内存持有会话令牌 | HttpOnly Cookie; csrf_token only in memory; Origin and X-CSRF-Token on writes / 只读Cookie及内存CSRF |
| Read / 查询 | GET /auth/session never redisplays token / 不重发令牌 | GET /auth/session restores current CSRF / 可读取本会话CSRF |
| Exit / 退出 | DELETE /auth/session and clear memory / 注销清内存 | DELETE with CSRF then expired Cookie / 带CSRF注销并清Cookie |

These are backend contracts; Taro/Web frontends and the callback window remain to be built. A full-page redirect would lose an in-memory verifier, so WEB integration must keep the initiating SPA alive and receive an origin-checked same-origin callback result rather than persist session secrets in localStorage. These bindings are our own flow, not a claim that Alipay accepts PKCE parameters.

以上为后端合同；Taro/Web前端及授权回调窗口尚待构建。整页跳转会丢失内存verifier，Web须保留原SPA并接收核源的同源回调窗口结果，不把会话秘密写入localStorage。这是本项目绑定流程，不宣称支付宝提供PKCE参数。

All paths above have /v1/p11 prefix. Current routes cover 13 of 93 control operations: auth challenge/session create/read/revoke; profile; key list/create/revoke; services; quote create/read; order create/read. A valid account session is required for purchase; API keys never authorize it. Unknown authorization outcomes require fresh authorization. Signed synthetic OAuth and native HTTP tests passed; real application/channel integration has not.

上述路径前缀均为/v1/p11。当前13/93操作涵盖挑战/会话、本人、密钥、市场、报价和订单；买单必须本人会话，API key不能代购。授权UNKNOWN重新发起授权，不重复兑原码。签名合成渠道响应及本机HTTP已验证，真实应用/渠道联调尚未执行。详见[实际源码路由](../src/api/internal/http/routes.json)和[实施约束](../../docs/技术开发/附录-身份登录与HTTP交易实施约束.md)。

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

R004 exposes a limited native control API, including market/quote/order operations.
The full buyer journey, model invocation and payments are still pending.

R004已接入市场/报价/建单的有限原生HTTP子集；此表完整使用流程、模型调用与资金链仍待完成。只有已验证凭据适配器可构造Principal，普通请求不能自报角色。

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

