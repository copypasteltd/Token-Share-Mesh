# Media provenance / 演示媒体来源

All screenshots and recordings show the real generated specification readers.
They are captured from a native, locally installed Chromium browser against a
loopback-only document preview. There are no fabricated marketplace transactions,
model responses or payment results in these assets.

全部截图和录制来自真实生成的规范阅读器，使用本机已安装原生 Chromium 和 loopback 文档入口。画面中没有伪造交易、模型结果或支付成功。

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
Downloads, browser profiles and intermediate frames stay in ignored `runtime/`.

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

