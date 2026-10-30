# Shared frontend / 共享前端

This package contains the implemented **control UI subset**, not an inference gateway. React/Taro Alipay pages and React/Vite Web share validated HTTP DTOs, exact BigInt price previews, auth transports, an owner-partitioned pending-command journal and an asynchronous view model. The server's immutable contract remains authoritative. No prompts, inference results, payment credentials or decryption keys are collected here.

本目录为两端共享控制逻辑；小程序优先，Web同合同。计价只为小时、输入/输出分别M token；资源明确个人卖家和具体型号。端对端服务在可信本地网关配对完成前保持关闭。当前实际建单只到WAITING_PAYMENT，不付款、不计时、不执行设备。

Run from the repository root / 从仓库根执行（原生Node22.14.0、npm10.9.2）：

```powershell
python -X utf8 dev/tools/前端开发.py bootstrap
python -X utf8 dev/tools/前端开发.py check
python -X utf8 dev/tools/前端开发.py test
python -X utf8 dev/tools/前端开发.py build
```

The wrapper stages these tracked sources in `runtime/frontend/workspace`; all dependencies/cache/builds remain under `runtime/`. `bootstrap` uses the committed lockfile with install scripts disabled. `lock` is an explicit dependency-update action. Individual `alipay`, `h5`, `web` commands are available. Build outputs: `runtime/frontend/builds/miniapp-alipay`, `miniapp-h5`, `web`. Do not run npm directly in `dev/src` or commit generated dependencies.

原生小程序构建前明确提供控制服务地址；此选项是编译配置，不规划部署。空地址会阻止原生登录；Web/H5空地址代表与控制面同源。

```powershell
$env:P11_FRONTEND_API_BASE = 'https://your-authorized-control.example'
python -X utf8 dev/tools/前端开发.py alipay
Remove-Item Env:P11_FRONTEND_API_BASE
```

A real application ID, registered OAuth callback and native Alipay developer-tool/device checks are still required. No application ID or real channel credentials are supplied by this repository. H5 is a browser preview and uses Web cookie auth; it does not validate native `my.getAuthCode`. Windows native development is covered by the current wrapper; Linux frontend runtime is not claimed.

浏览器使用WebCrypto S256、一次性state/绑定Cookie，回调后删授权参数及临时证明；会话HttpOnly Cookie、写请求CSRF。原生小程序显式选`ALIPAY_NATIVE_NONCE_1`服务端一次性随机证明，`my.getAuthCode`后仅内存保留Bearer；不假设支付宝支持WebCrypto、不用Math.random。命令号使用独立内存种子与单调HMAC计数，重启重新授权。该证明不称客户端PKCE或内容加密。

Usage / 使用顺序：

1. Authorize, browse/filter personal sellers, read concrete models and the privacy boundary / 授权后查看个人设备与卖家。
2. Select a resource, enter bounded duration/usage and budget, request an authoritative quote / 填用量和预算，取得正式合同。
3. Explicitly check disclosure and create the order / 主动勾选确认，建单后仍等待付款。
4. If the reply is lost, reload and use “核对原请求”; the original key and body are reused / 未知不换参数、不新购，后台再次核验当前权限与原单。
5. Read the current account's orders/profile/listings. Local recent IDs are not complete history. Seller write flows and device pairing remain pending / 本人只读资料不能冒充审核或开售。

`core/tests/core.test.ts` is the actual 13-case unit entry. The Go browser integration harness installs synthetic OAuth/qualification only in the test binary, serves compiled real UI, exercises the real HTTP/DB chain and closes its native browser. Run `python -X utf8 dev/tools/原生数据库验证.py --round R006` **after building**; it creates/stops a disposable native PostgreSQL instance and uses an existing Edge/Chrome plus native FFmpeg. It performs no payment or device execution. Never use Docker/WSL/emulators.

See [implementation constraints / 完整实施细则](../../../docs/技术开发/附录-双端交易页面与授权恢复实施约束.md), [source progress / 编号进度](../../progress/README.md) and [actual media / 真实媒体来源](../../repository/MEDIA.md). Full product acceptance, true Alipay authorization, GPU/Android, crypto interoperability and performance remain unexecuted.
