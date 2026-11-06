# Complete ComfyUI environment source / 完整环境源码

SOURCE_ONLY / 未编译、未运行。该模块安装完整固定软件包和有限白名单节点，管理原节点下的不可变候选与本机选择；没有执行ComfyUI、模型、pip/git或网络下载。后续MW已新增私有worker、四类编码及本地首写加密源码；正式原宿主启动/原RENTAL_SESSION媒体/完整运行包和公开交付仍待接线。所有56任务222子项保持开放，代码齐备后统一测试。

Read the [CE-001—036 specification](../../../../docs/技术开发/附录-ComfyUI完整环境安装与原节点登记源码.md) and [current source progress](../../../progress/R026-全量代码阶段进展.md). The [source pin](upstream.lock.json) identifies official v0.44.0 commit `2c75dc1b379a59e0880619d029607af5169a62ef`; it is not a shipped Python/Torch runtime bundle or hardware qualification.

## 本人选择及安装

独立本机issuer/pins/revocation文件与独立Consent定义精确目录摘要、节点、许可及磁盘上限。`Verifier.Verify`生成不可变Plan；`Registry.Install`只接受原节点根下已下载ZIP的精确artifact映射。先加密登记原stage，再解压复核、提交目录、再次复核；失败保留QUARANTINED，不自动安装脚本、重试新身份、切换或开售。没有模型权重下载。

节点客户端 `comfy-init/status/install/verify/delete --state <原节点绝对路径>` 提供本机管理。install/verify额外要求独立 `--trust` / `--consent` 绝对路径。install的stdin为 `signed_catalog` 和 `local_archives`。成功操作全部带 `execution_enabled=false / qualification=NOT_GRANTED`。具体信封和字段见CE文档，命令当前不执行。

## Ownership and replacement

`node.Store` anchors a random KernelID before creating the registry. Its separate AES-256-GCM key is derived from the original OS-protected master and original installation/device/seller/control binding. Missing or ambiguous anchored metadata is never recreated. Close cancels installation IO and joins entered operations before the parent releases its OS lock, root or keys.

`Store.ActivateComfy` uses that node's original execution ledger. Selection requires the same physical MAINTENANCE/PREPARING owner, a full directory check and a metadata commit while holding the shared gate. It starts no engine, grants no qualification and leaves the maintenance owner occupied. Explicit rollback follows the same approval/checks; it cannot reset identity, fences, money or prices. Tombstones precede deletion, active candidates cannot be deleted and history remains bounded/countable.

Full hashes run on installation/selection and before future imports, not once per generated token/frame. Installer memory is bounded by catalog/central-directory limits and streaming buffers; actual memory/performance remains unmeasured. Finite approved Python nodes are trusted code, not an isolation boundary. Registry encryption is not network E2E, provider blindness, LP or TEE.

## 测试源码

微小合成包覆盖独立信任/同意、可选节点依赖、路径/大小写/脚本/类覆盖拒绝、ZIP限额/符号链接、精确全树、AEAD与异根、半提交隔离、墓碑、关闭join、维护共槽和CLI输入边界。**全部未编译、未运行**；没有真实Python/ComfyUI/GPU或媒体验收。编号索引由R026文档整理器同步，禁止把未来测试定义计入历史通过数量。

This component adds no SQL and changes none of the published platform/native DDL. Docker/WSL/QEMU and other local virtualization remain prohibited. Continue complete production code and test-source development across all 56/222 before batch validation.

## R026 测试定义编号索引

| 编号 | 定义 | 源码 | 状态 |
|---|---|---|---|
| CE-T001 | TestComfyCLIRejectsAmbiguousOwnerOptionsBeforeOpeningNode | dev/src/client/cmd/p11-node/comfy_test.go | 未编译/未运行 |
| CE-T002 | TestComfyOwnerFilesStrictBoundedAndSelectedByAbsolutePath | dev/src/client/cmd/p11-node/comfy_test.go | 未编译/未运行 |
| CE-T003 | TestComfyActivationAndExplicitRollbackUseOriginalPhysicalMaintenanceGate | dev/src/client/comfyenv/activation_test.go | 未编译/未运行 |
| CE-T004 | TestComfyActivationRejectsLocalStoppedAndForeignMaintenanceHolds | dev/src/client/comfyenv/activation_test.go | 未编译/未运行 |
| CE-T005 | TestComfyArchiveRejectsSymlinkBeforeFollowingItsTarget | dev/src/client/comfyenv/archive_test.go | 未编译/未运行 |
| CE-T006 | TestComfyInstallCancelledAdmissionCreatesNoCandidateOrStage | dev/src/client/comfyenv/archive_test.go | 未编译/未运行 |
| CE-T007 | TestComfyCatalogRequiresIndependentPinsConsentAndCurrentRevocations | dev/src/client/comfyenv/catalog_test.go | 未编译/未运行 |
| CE-T008 | TestComfyCatalogSelectsFiniteNodesAndOnlyTheirOptionalDependencies | dev/src/client/comfyenv/catalog_test.go | 未编译/未运行 |
| CE-T009 | TestComfyCatalogRejectsPathsCaseAliasesHooksAndCrossRoleOverrides | dev/src/client/comfyenv/catalog_test.go | 未编译/未运行 |
| CE-T010 | TestComfyCatalogImmutablePlanCopiesAndExactExpiry | dev/src/client/comfyenv/catalog_test.go | 未编译/未运行 |
| CE-T011 | TestComfyCatalogStrictJSONAndDiskBudget | dev/src/client/comfyenv/catalog_test.go | 未编译/未运行 |
| CE-T012 | TestComfyInstallExactFullTreeAndFreshPinsReadback | dev/src/client/comfyenv/registry_test.go | 未编译/未运行 |
| CE-T013 | TestComfyInstallDirectoryDriftQuarantinesWithoutFallback | dev/src/client/comfyenv/registry_test.go | 未编译/未运行 |
| CE-T014 | TestComfyInstallDamagedOriginalArchiveKeepsStageAndCandidate | dev/src/client/comfyenv/registry_test.go | 未编译/未运行 |
| CE-T015 | TestComfyArchiveRejectsExtraTraversalAndCaseEntries | dev/src/client/comfyenv/registry_test.go | 未编译/未运行 |
| CE-T016 | TestComfyArchiveBoundsRejectBeforeEntryAllocation | dev/src/client/comfyenv/registry_test.go | 未编译/未运行 |
| CE-T017 | TestComfyRegistryInterruptedInstallationNeverAdoptsTree | dev/src/client/comfyenv/registry_test.go | 未编译/未运行 |
| CE-T018 | TestComfyRegistryMissingCorruptOrDifferentBindingNeverRecreated | dev/src/client/comfyenv/registry_test.go | 未编译/未运行 |
| CE-T019 | TestComfyRegistryCommitFailurePoisonsOriginalMetadata | dev/src/client/comfyenv/registry_test.go | 未编译/未运行 |
| CE-T020 | TestComfyDeleteTombstoneRetainsHistoryAndOtherOwnedFiles | dev/src/client/comfyenv/registry_test.go | 未编译/未运行 |
| CE-T021 | TestComfyDeleteUncertainTombstoneNeverRemovesOriginalTree | dev/src/client/comfyenv/registry_test.go | 未编译/未运行 |
| CE-T022 | TestComfyRegistryCloseCancelsAndJoinsEnteredOperation | dev/src/client/comfyenv/registry_test.go | 未编译/未运行 |
| CE-T023 | TestComfyRegistryRejectsAlteredAuthenticatedMetadata | dev/src/client/comfyenv/registry_test.go | 未编译/未运行 |
| CE-T024 | TestComfyMetadataIncludesExactConsentWithoutContentOrExecutionGrant | dev/src/client/comfyenv/registry_test.go | 未编译/未运行 |
| CE-T025 | TestMaintenanceRequiresOriginalPreparingOwnerAndNeverAcquiresAnotherMode | dev/src/client/execution/maintenance_test.go | 未编译/未运行 |
| CE-T026 | TestMaintenanceHoldsPhysicalGateAcrossLocalMetadataCommit | dev/src/client/execution/maintenance_test.go | 未编译/未运行 |
| CE-T027 | TestMaintenanceMetadataFailureDoesNotManufactureCleanupOrFree | dev/src/client/execution/maintenance_test.go | 未编译/未运行 |
| CE-T028 | TestMaintenanceCannotBorrowRentalSessionPhysicalOwner | dev/src/client/execution/maintenance_test.go | 未编译/未运行 |
| CE-T029 | TestNodeComfyRegistryOriginalAnchorEncryptedReopenAndIndependentKey | dev/src/client/node/kernels_test.go | 未编译/未运行 |
| CE-T030 | TestNodeComfyRegistryMissingAnchoredJournalCannotBeReinitialized | dev/src/client/node/kernels_test.go | 未编译/未运行 |
| CE-T031 | TestNodeComfyRegistryRejectsForeignRootAndPreservesOSLockUntilClose | dev/src/client/node/kernels_test.go | 未编译/未运行 |

## R026 MW-001—044 私有媒体源码继续 / Private media source

R026新增[MW-001—044](../../../../docs/技术开发/附录-ComfyUI私有媒体执行与本地加密源码.md)：固定原生私有Comfy执行器、GPU UUID/PCI复核、有限配方、PNG/分片MP4/PCM WAV/几何GLB编码、原锚/加密预登记/分块GCM及Go一次原scope凭据/私有帧源码。44项新测试定义未编译/运行；正式原宿主启动器/租内媒体/完整运行包/公开交付/低权限/E2E与性能仍待。CE31/RN67及全部56任务222子项仍未收口，代码齐备后统一验证；本次无SQL变更。

## R026 CH-001—036 原环境与宿主源码继续 / Original environment and host source

R026新增[CH-001—036](../../../../docs/技术开发/附录-ComfyUI原环境持有与本机进程接线源码.md)：原Comfy环境运行持有、固定原生进程/三管道、PIPE_2准备/一次原授权ACK/单调截止和原ProviderRental准备接线。29项新测试定义未编译/运行；CH-033—036正式租内媒体Backend、认证工作区/恢复清理、公开交付、完整包/低权限/E2E、独立ready/计量/付费续租/平台释放仍待。全56任务/222子项开放，无SQL变更、迁移或真实执行；完整代码后统一验证，源码检查点不是开发轮完成。

## R026 MS-001—036 原媒体会话监督与停止回收源码 / Original media-session supervision source

R026新增[MS-001—036](../../../../docs/技术开发/附录-原租赁媒体会话监督与停止回收源码.md)：原节点持有的本机媒体会话监督/独立单调截止、真实Comfy叶子、一次原授权/单份输入及实际join、先停工后回收、两类会话共关闭、原媒体结果保留/撤销和本机会话清理CAS源码。27项新增测试定义均未编译/运行；公开媒体SDK/最终ACK、双方ready/可信小时水位/付费续租/平台资金释放、独立物理停工恢复及完整实际运行包/低权限/E2E/性能仍待。全部56任务222子项开放；本次无SQL变更或迁移。完整生产代码/实际依赖/测试源码齐备后统一验证，源码检查点不是开发轮完成。
