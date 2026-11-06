# Original rental media supervisor / 原租赁媒体会话监督

`SOURCE_ONLY_UNVERIFIED`：当前只完成代码与测试定义，未编译、未运行；不代表完整开发轮或可交付端到端验收。

本模块用独立媒体类型连接原 `comfyworker.RentalMediaAdapter`，管理原租赁的准备/授权/单调期限、一个活动子调用、结果托管、原子调用清理及最终本机父租赁清理。生产组合入口为 `node.ProviderRental.OpenComfy`；会话在加载前归原节点持有。节点通过 `InputOwner` 撤销未提交输入并等已进入接口退出，文本和 Comfy 会话共同参与原节点关闭。

调用顺序：原 ProviderRental/批准环境→OpenComfy→一次原 Authorize/ACK→SealMediaInput→Invoke→Query/Cancel/Output/DiscardResult→Close。Ready 只表示监督器已启动，EOF/Close 不表示买方 ACK，清理只释放本机物理占用。GPU 小时计费、原买卖双方/设备/合同/围栏及真实业务证据独立保持。

The production `Comfy` adapter wraps the actual typed native leaf, never a text job or network-supplied proof. Stop closes admission and requests native cancellation before metadata cleanup. `Done` waits for actual operations, control exchanges, readers, pipes, process and enclosing input-owner completion. Missing handles, unknown work, failed CLOSED, or a caller timeout retain the original owner. Output value copies share one opaque close state.

Public OpenAI/Anthropic media SDK delivery, independent readiness/hour meters/paid renewal/platform release, physical crash reconciliation, complete runtime archives, low-privilege and E2E/performance qualification remain open. All test fixtures are synthetic metadata/backends and are uncompiled/unexecuted; none is GPU, payment, stopped-work or security acceptance.

完整编号、时间/容量边界及生产持有顺序见 [MS-001—036](../../../../docs/技术开发/附录-原租赁媒体会话监督与停止回收源码.md)；全量 56/222 待办见 [代码阶段清单](../../../全量代码优先实施清单.md)。

## R026 MS-001—036 原媒体会话监督与停止回收源码 / Original media-session supervision source

R026新增[MS-001—036](../../../../docs/技术开发/附录-原租赁媒体会话监督与停止回收源码.md)：原节点持有的本机媒体会话监督/独立单调截止、真实Comfy叶子、一次原授权/单份输入及实际join、先停工后回收、两类会话共关闭、原媒体结果保留/撤销和本机会话清理CAS源码。27项新增测试定义均未编译/运行；公开媒体SDK/最终ACK、双方ready/可信小时水位/付费续租/平台资金释放、独立物理停工恢复及完整实际运行包/低权限/E2E/性能仍待。全部56任务222子项开放；本次无SQL变更或迁移。完整生产代码/实际依赖/测试源码齐备后统一验证，源码检查点不是开发轮完成。
