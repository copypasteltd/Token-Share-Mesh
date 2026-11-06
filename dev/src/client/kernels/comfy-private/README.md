# Private ComfyUI worker / ComfyUI私有媒体执行器

**SOURCE_ONLY / 未导入、未运行。** 本目录实现固定原生包内的独立Python执行器：选择性节点导入、GPU UUID/PCI检查、有限配方、私有内容/控制通道，以及PNG/分片MP4/WAV/几何GLB直接写分块GCM密文。当前为PIPE_2/comfy-private-2，准备只能加载，须原宿主独立授权并收到控制ACK才可推理。Go原节点准备启动器源码已接入；正式原租内媒体Backend、认证工作区/输入、完整运行包和公开协议交付仍待。

Read [MW-001—044](../../../../../docs/技术开发/附录-ComfyUI私有媒体执行与本地加密源码.md), [complete environment CE](../../comfyenv/README.md) and [Go boundary](../../adapters/comfyworker/README.md). All 56 tasks/222 steps remain open. Complete full-scope production code and test sources before any batch compilation/testing/debugging.

## Source layout / 源码分工

| Source | Responsibility |
|---|---|
| `worker.py` | Fixed packaged Python only, `-I -B -S`, independent original control handle, muted diagnostics |
| `mesh_comfy/environment.py` | Whole-tree/model pins, exact CUDA identity, approved selective imports; no server startup |
| `recipe.py` / `private_nodes.py` | One approved graph/output and finite typed slots; PNG/JPEG encrypted input |
| `encoders.py` | PNG, fragmented libx264 MP4, PCM16 WAV and geometry-only GLB, without plaintext temporary media |
| `spool.py` / `workspace.py` | Per-file random RAM keys, 64KiB GCM records/seek, original anchor, encrypted pre-create reservations |
| `wire.py` / `worker.py` | Separate content/control, persistent invocation tombstones, monotonic deadlines and synchronized stop facts |
| `tests/` | Future batch definitions for ciphertext, frames, recipes, cancellation and real codecs on synthetic CPU tensors |

## Host contract / 原宿主合同

Only the independently authorized original native host may launch this worker under its original OS lock, physical RENTAL owner, current local consent and exact contract/environment/model/recipe. The host must durably reserve the whole private workspace before creating its anchor or launching the process. It must own the input/output/control pipes and actual process join. INIT/READY cannot create authority; `qualification=NOT_GRANTED` remains explicit.

The packaged layout is `environment/{python,comfy,bridge,tools}`; this directory becomes `bridge/`, with every shipped file pinned by the independently signed catalog. Current source pins ComfyUI v0.44.0 / `2c75dc1b379a59e0880619d029607af5169a62ef`. No full Python/Torch/codec/node runtime archive ships here. Do not run this repository directory as a production substitute or auto-install Python packages/models.

双管道传入解密后的买方参数；提供方UI只应获阶段、占用、环境/许可及收益元数据。worker本地文件用新随机AES-256-GCM钥逐块加密，RESULT只交给私有宿主。当前源码没有完成OS低权限身份、网络E2E或设备所有者不可见证明，不授LP/TEE/严格增强标签。No Docker/WSL/QEMU/VM, RAM disk, public ComfyUI server, automatic node installation or seller/device/model/privacy fallback.

Actual codec availability, supported recipes/models, Windows/Linux GPU binding, RAM/VRAM overhead and performance are unverified. MP4 is capped at 240 frames/30 seconds; WAV at 120 seconds; GLB contains geometry only. Media remains inside the original hourly STANDARD rental, with no per-file fee. LLM prices remain separate input/output M token.

ComfyUI retains its upstream GPL license, copied under `dev/licenses/` and pinned by CE. Shipped combined environments and every dependency require their own exact license/source inventory; the repository's top-level license does not replace upstream terms.

## R026 测试定义编号索引 / Test source index

| 编号 | 定义 | 源码 | 状态 |
|---|---|---|---|
| MW-T001 | TestComfyCipherIndependentFixtureAndBoundedCrossRecordSeek | dev/src/client/adapters/comfyworker/cipher_test.go | 未编译/未运行 |
| MW-T002 | TestComfyCipherRejectsTruncationTrailingBytesAndMissingEnd | dev/src/client/adapters/comfyworker/cipher_test.go | 未编译/未运行 |
| MW-T003 | TestComfyCipherAuthenticatesHeaderEndAndIndividualData | dev/src/client/adapters/comfyworker/cipher_test.go | 未编译/未运行 |
| MW-T004 | TestComfyReceiptCannotSelectAnotherOriginalScopeKindOrPath | dev/src/client/adapters/comfyworker/cipher_test.go | 未编译/未运行 |
| MW-T005 | TestComfyReceiptIsConsumedAcrossCopiedAliasesIncludingFailure | dev/src/client/adapters/comfyworker/cipher_test.go | 未编译/未运行 |
| MW-T006 | TestComfyReceiptCloseRevokesAllAliasesAndFormattingRedacts | dev/src/client/adapters/comfyworker/cipher_test.go | 未编译/未运行 |
| MW-T007 | TestComfyCipherContextCancellationAndCloseRevokeCopiedReaders | dev/src/client/adapters/comfyworker/cipher_test.go | 未编译/未运行 |
| MW-T008 | TestComfyReceiptRejectsDuplicateUnknownAndNonCanonicalKeyFields | dev/src/client/adapters/comfyworker/cipher_test.go | 未编译/未运行 |
| MW-T009 | TestComfyWorkerFramesBoundLengthBeforePayloadAndRejectInputKinds | dev/src/client/adapters/comfyworker/wire_test.go | 未编译/未运行 |
| MW-T010 | TestComfyPrivateFramePartialReadsSingleUseAliasesAndRedaction | dev/src/client/adapters/comfyworker/wire_test.go | 未编译/未运行 |
| MW-T011 | TestComfyPrivateFrameCloseRevokesAliasesAndRejectsTruncation | dev/src/client/adapters/comfyworker/wire_test.go | 未编译/未运行 |
| MW-T012 | TestComfyIndependentControlWriterNotBlockedByContentMutex | dev/src/client/adapters/comfyworker/wire_test.go | 未编译/未运行 |
| MW-T013 | TestComfyPrivateWriterRejectsEmptyOversizeCancelledAndWrongKinds | dev/src/client/adapters/comfyworker/wire_test.go | 未编译/未运行 |
| MW-T014 | test_original_scope_roundtrip_cross_record_seek_and_no_plaintext_file | dev/src/client/kernels/comfy-private/tests/test_cipher.py | 未编译/未运行 |
| MW-T015 | test_independent_aead_decrypts_end_and_first_data_record | dev/src/client/kernels/comfy-private/tests/test_cipher.py | 未编译/未运行 |
| MW-T016 | test_reservation_failure_creates_no_payload | dev/src/client/kernels/comfy-private/tests/test_cipher.py | 未编译/未运行 |
| MW-T017 | test_tampered_data_record_rejected_before_exposing_plaintext | dev/src/client/kernels/comfy-private/tests/test_cipher.py | 未编译/未运行 |
| MW-T018 | test_missing_or_appended_authenticated_end_refuses_reader | dev/src/client/kernels/comfy-private/tests/test_cipher.py | 未编译/未运行 |
| MW-T019 | test_one_reader_readall_and_out_of_range_seek_limits | dev/src/client/kernels/comfy-private/tests/test_cipher.py | 未编译/未运行 |
| MW-T020 | test_limit_and_cancel_close_clear_owned_keys_without_deleting_cipher | dev/src/client/kernels/comfy-private/tests/test_cipher.py | 未编译/未运行 |
| MW-T021 | test_empty_spool_and_noncanonical_scope_cannot_produce_result | dev/src/client/kernels/comfy-private/tests/test_cipher.py | 未编译/未运行 |
| MW-T022 | test_workspace_anchor_and_encrypted_reservation_precede_payload | dev/src/client/kernels/comfy-private/tests/test_cipher.py | 未编译/未运行 |
| MW-T023 | test_foreign_workspace_anchor_or_scope_is_not_adopted | dev/src/client/kernels/comfy-private/tests/test_cipher.py | 未编译/未运行 |
| MW-T024 | test_cancel_before_start_is_a_permanent_original_tombstone | dev/src/client/kernels/comfy-private/tests/test_control.py | 未编译/未运行 |
| MW-T025 | test_finish_never_allows_reentering_the_original_invocation | dev/src/client/kernels/comfy-private/tests/test_control.py | 未编译/未运行 |
| MW-T026 | test_foreign_child_cancel_does_not_interrupt_current_original_work | dev/src/client/kernels/comfy-private/tests/test_control.py | 未编译/未运行 |
| MW-T027 | test_native_interrupt_binding_preserves_already_cancelled_session | dev/src/client/kernels/comfy-private/tests/test_control.py | 未编译/未运行 |
| MW-T028 | test_monotonic_child_and_session_deadlines_survive_wall_clock_rollback | dev/src/client/kernels/comfy-private/tests/test_control.py | 未编译/未运行 |
| MW-T029 | test_foreign_control_identity_or_lost_control_pipe_stops_original_session | dev/src/client/kernels/comfy-private/tests/test_control.py | 未编译/未运行 |
| MW-T030 | test_png_real_codec_roundtrip_contains_pixels_without_prompt_metadata | dev/src/client/kernels/comfy-private/tests/test_encoders.py | 未编译/未运行 |
| MW-T031 | test_png_refuses_multiple_outputs_and_nonfinite_pixels | dev/src/client/kernels/comfy-private/tests/test_encoders.py | 未编译/未运行 |
| MW-T032 | test_fragmented_mp4_real_codec_roundtrip_has_exact_frame_count | dev/src/client/kernels/comfy-private/tests/test_encoders.py | 未编译/未运行 |
| MW-T033 | test_mp4_limits_do_not_silently_resize_or_select_another_codec | dev/src/client/kernels/comfy-private/tests/test_encoders.py | 未编译/未运行 |
| MW-T034 | test_pcm_wav_real_codec_roundtrip_uses_exact_known_frames | dev/src/client/kernels/comfy-private/tests/test_encoders.py | 未编译/未运行 |
| MW-T035 | test_glb_geometry_roundtrip_has_bounded_positions_and_indices | dev/src/client/kernels/comfy-private/tests/test_encoders.py | 未编译/未运行 |
| MW-T036 | test_glb_refuses_bad_indices_and_encoder_cancellation | dev/src/client/kernels/comfy-private/tests/test_encoders.py | 未编译/未运行 |
| MW-T037 | test_parameters_bind_only_approved_slots_and_do_not_mutate_next_call | dev/src/client/kernels/comfy-private/tests/test_recipe_wire.py | 未编译/未运行 |
| MW-T038 | test_native_savers_unknown_nodes_and_second_sink_are_refused | dev/src/client/kernels/comfy-private/tests/test_recipe_wire.py | 未编译/未运行 |
| MW-T039 | test_path_model_and_duplicate_parameter_targets_are_immutable | dev/src/client/kernels/comfy-private/tests/test_recipe_wire.py | 未编译/未运行 |
| MW-T040 | test_strict_json_rejects_duplicate_keys_nan_surrogates_and_utf8 | dev/src/client/kernels/comfy-private/tests/test_recipe_wire.py | 未编译/未运行 |
| MW-T041 | test_private_wire_partial_reads_truncation_and_preallocation_limit | dev/src/client/kernels/comfy-private/tests/test_recipe_wire.py | 未编译/未运行 |
| MW-T042 | test_private_output_writer_accepts_only_bounded_worker_metadata | dev/src/client/kernels/comfy-private/tests/test_recipe_wire.py | 未编译/未运行 |
| MW-T043 | test_accidental_io_guard_denies_network_subprocess_and_plain_file_writes | dev/src/client/kernels/comfy-private/tests/test_recipe_wire.py | 未编译/未运行 |
| MW-T044 | test_cancelled_owned_pipe_returns_before_waiting_for_content | dev/src/client/kernels/comfy-private/tests/test_recipe_wire.py | 未编译/未运行 |

## R026 CH-001—036 原环境与宿主源码继续 / Original environment and host source

R026新增[CH-001—036](../../../../../docs/技术开发/附录-ComfyUI原环境持有与本机进程接线源码.md)：原Comfy环境运行持有、固定原生进程/三管道、PIPE_2准备/一次原授权ACK/单调截止和原ProviderRental准备接线。29项新测试定义未编译/运行；CH-033—036正式租内媒体Backend、认证工作区/恢复清理、公开交付、完整包/低权限/E2E、独立ready/计量/付费续租/平台释放仍待。全56任务/222子项开放，无SQL变更、迁移或真实执行；完整代码后统一验证，源码检查点不是开发轮完成。
