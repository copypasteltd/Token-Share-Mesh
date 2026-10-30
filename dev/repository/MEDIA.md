# Media provenance / 演示媒体来源

The historical reader screenshots and recordings show real generated specification readers.
They are captured from a native, locally installed Chromium browser against a
loopback-only document preview. There are no fabricated marketplace transactions,
model responses or payment results in these assets.

历史阅读器截图和录制来自真实生成的规范阅读器，使用本机原生 Chromium 和 loopback 文档入口。R006另增实际产品页面媒体，详见下一节；两类来源独立登记，均没有真实设备/资金执行。

## R008 actual product UI / 实际产品页面

React/Vite Web and React/Taro H5 are captured from their compiled source, served by the native Go test harness alongside the real control HTTP handlers and disposable PostgreSQL. OAuth authorization, qualification and device heartbeats are synthetic and visibly labelled. RSA2 exchange verification, quote/order persistence and original-command recovery are real test operations. Native Alipay is compiled but not rendered or authorized in these captures.

Web与Taro H5截图/录屏为真正编译页面，实际连接HTTP/临时原生数据库；授权/卖家资格/设备心跳为带标识的合成夹具，仅在测试二进制启用，不在产品中加入假登录。心跳只刷新本用例的合成设备，不代表设备客户端已经实现。H5不冒称支付宝原生容器；没有支付成功、模型结果或GPU/手机任务。

| Product asset / 产品媒体 | Content / 内容 |
| --- | --- |
| [Web market](../media/R008/screenshots/web-market.png) / [Taro H5 market](../media/R008/screenshots/miniapp-market.png) | Concrete model/seller, dual prices and privacy / 具体型号、卖家、单位与隐私 |
| [Web E2E detail](../media/R008/screenshots/web-llm-detail.png) / [Taro H5 detail](../media/R008/screenshots/miniapp-llm-detail.png) | Buyer gateway unavailable; no prompt collection / 买方可信网关尚未连接，不收Prompt |
| [Web unknown reply](../media/R008/screenshots/web-order-pending.png) / [Taro H5 unknown reply](../media/R008/screenshots/miniapp-order-pending.png) | A committed order reply is deliberately lost; original key/body retained / 后台已提交，故意丢回执，保留原键/参数 |
| [Web order](../media/R008/screenshots/web-order.png) / [Taro H5 order](../media/R008/screenshots/miniapp-order.png) | Recovered WAITING_PAYMENT; no active billing / 恢复原单，未付款未计时 |
| [Web sharing](../media/R008/screenshots/web-share.png) / [Taro H5 sharing](../media/R008/screenshots/miniapp-share.png) | Real account projection, no fabricated income or device connection / 本人投影，不虚构收入或已配对设备 |
| [Web MP4](../media/R008/recordings/web-transaction.mp4) / [GIF](../media/R008/recordings/web-transaction.gif) | Actual browser compositor frames and original timing, silent / 实际浏览器帧与原时间，无声 |
| [Taro H5 MP4](../media/R008/recordings/miniapp-transaction.mp4) / [GIF](../media/R008/recordings/miniapp-transaction.gif) | Real H5 actions, not a sequence of design PNGs / 实际页面操作，不是设计图轮播 |
| [Product manifest](../media/frontend-manifest.json) / [Web evidence](../tests/evidence/R008-web-browser.json) / [H5 evidence](../tests/evidence/R008-miniapp-browser.json) | Source/output hashes, frame provenance and browser closure / 源码/产物摘要、逐帧来源和浏览器关闭 |

After `前端开发.py bootstrap` and `build`, run `原生数据库验证.py --round R008`, then `核验前端源码.py --round R008`. Existing native Edge/Chrome and FFmpeg/FFprobe are required. The test automatically closes its owned browser; the Go harness and database runner close their own HTTP/DB resources. Keep failed attempts rather than relabeling them as passes. Regenerating assets requires refreshing their manifests and the README together.

先构建再实际联调/录制；所有依赖/临时配置/原始帧在runtime。媒体来源不等于真机/真实渠道或完整产品验收；历史阅读器媒体继续按下列独立清单维护。

| Asset / 产物 | Content / 内容 |
| --- | --- |
| [Architecture SVG](../media/architecture.svg) | Authored system design illustration; not a screenshot / 人工编写系统示意，不是截图 |
| [Whitepaper desktop](../media/screenshots/whitepaper-desktop.png) | Real 1440×900 reader overview / 实际桌面阅读器 |
| [Whitepaper search](../media/screenshots/whitepaper-search.png) | Actual full-text query and matching chapter links / 实际全文搜索及章节结果 |
| [Technical desktop](../media/screenshots/technical-desktop.png) | Real technical implementation reader / 实际技术阅读器 |
| [Mobile width](../media/screenshots/whitepaper-mobile.png) | 390×844 document viewport; not an Alipay miniapp screenshot / 窄屏文档，不是支付宝小程序截图 |
| [MP4](../media/recordings/readers-tour.mp4) | Silent recording of actual browser compositor frames, original action timing / 真实浏览器合成帧，无声、原操作时序 |
| [GIF](../media/recordings/readers-tour.gif) | Full recorded tour at 1.5× playback, 8 fps, 960 px wide / 完整录制1.5倍播放、8fps、宽960像素 |
| [Manifest](../media/media-manifest.json) | Source/artifact SHA-256, capture method, assertions and cleanup evidence / 来源及产物摘要、录制方式、交互验证和清理证据 |

## Reproduce / 复现

Install Node.js22+, a native Chromium/Edge/Chrome browser, and FFmpeg/FFprobe on
PATH. Media dependencies are pinned in `dev/tools/media/package-lock.json`.
Reader dependencies stay in `runtime/media/workspace`; browser profiles and intermediate frames also stay in ignored `runtime/`.

先准备 Node.js22+、原生 Chromium/Edge/Chrome、PATH 中的 FFmpeg/FFprobe。依赖由锁文件固定，缓存、浏览器配置和原帧均在忽略的 runtime 内。

```powershell
python -X utf8 dev/tools/文档工具.py build
python -X utf8 dev/tools/媒体工具.py bootstrap
python -X utf8 dev/tools/媒体工具.py capture
python -X utf8 dev/tools/构建仓库文档.py
python -X utf8 dev/tools/核验仓库.py
```

Optional `P11_BROWSER_PATH`, `P11_FFMPEG_PATH` and `P11_FFPROBE_PATH` select existing
native executables. No browser/engine installer or virtualization is launched by
capture. Only this project's browser contexts and preview process are closed in
`finally`; other user tabs and processes are not touched. The MP4 is probed and
decoded after capture. Source HTML changes invalidate the manifest until a real
new capture is made.

可用三个 `P11_*_PATH` 选择已安装可执行文件。录制不启动浏览器/模型安装器或虚拟化；finally 只关闭本次所有页面、浏览器和文档预览进程，不操作其他用户标签或进程。MP4 实际探测并解码。HTML 改动后必须重新真实录制，否则来源摘要核验失败。


R008 reuses build/test receipts only when their hashes match the unchanged frontend source; its actual browser replay/media use the current backend. R006/R007 captures and evidence remain independent. / R008仅在摘要一致时复用未改前端的编译/TS记录，浏览器与媒体针对当前后台重跑，R006/R007原证据保留。

R008 adds a native foreground control agent with separate [actual Windows CLI evidence](../tests/evidence/R008-native-node-cli.json); these UI heartbeats remain labelled synthetic fixtures and do not prove GPU execution or Linux keyring operation. / R008新增原生控制代理，另保留实际Windows CLI证据；当前页面心跳仍是标注的合成夹具，不证明GPU执行或Linux钥库。
