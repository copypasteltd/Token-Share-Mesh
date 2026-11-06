# Original rental media workspaces

SOURCE_ONLY_UNVERIFIED. This package owns metadata-reserved, anchored local
media directories under the same native node root and execution ledger. It
does not run an engine, grant a rental, restore a lost key or release a GPU.

生产入口是原 node.Store.InitMediaWorkspaces/OpenMediaWorkspaces；目录创建前提交
身份和130 MiB容量预留，原调用重放只查状态。工作区与读取引用在真实join前持有
原根目录，删除需要原停工证据、CLEANING、退役和一次删除tombstone。

See [WS-001—030](../../../../docs/技术开发/附录-租内媒体工作区与结果托管源码.md)
for exact constraints, the recovery boundary and outstanding production work.
All test definitions remain uncompiled and unexecuted during CODE_FIRST.

## R026 WS-001—030 原媒体工作区与结果托管源码 / Original media workspace and custody source

R026新增[WS-001—030](../../../../docs/技术开发/附录-租内媒体工作区与结果托管源码.md)：原租赁媒体工作区/native003元数据预留、原节点目录持有/退役删除/清理专用恢复、认证RAM媒体输入、专用子调用适配器和结果钥托管/读取撤销。23项新测试定义均未编译/运行；公开SDK、完整媒体会话监督/独立停工对账/ready/续租/可信水位/平台释放、完整实际包/低权限/E2E/性能仍待。全56任务/222子项开放，只登记native003源，未迁移/真实执行；完整代码后统一验证，源码检查点不是开发轮完成。
