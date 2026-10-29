"""供给端内核与节点白名单设计源；仅生成文档，不安装或执行第三方组件。"""
from __future__ import annotations


SOURCE = "docs/技术开发/附录-供给端内核封装与替换规范.md"


def kernel_documents() -> dict[str, dict]:
    ident = {"type": "string", "pattern": "^[A-Za-z][A-Za-z0-9_-]{2,127}$"}
    sha = {"type": "string", "pattern": "^[a-f0-9]{64}$"}
    version = {"type": "string", "minLength": 1, "maxLength": 128,
               "not": {"enum": ["latest", "main", "master", "HEAD", "*", "UNFROZEN"]}}

    def obj(properties, required=None, **extra):
        return {"type": "object", "additionalProperties": False, "properties": properties,
                "required": list(properties) if required is None else required, **extra}

    def arr(items, minimum=0, maximum=128):
        return {"type": "array", "items": items, "minItems": minimum, "maxItems": maximum,
                "uniqueItems": True}

    artifact = obj({"component_id": ident, "role": {"enum": [
        "LM_CLI", "LM_DAEMON", "LM_RUNTIME", "COMFY_CORE", "PYTHON", "PYTHON_DEPENDENCY",
        "NATIVE_DEPENDENCY", "CUSTOM_NODE", "MEDIA_TOOL", "LMA_SOURCE", "ANDROID_NATIVE", "PLATFORM_BRIDGE",
        "PRIVATE_WORKER", "LLAMA_LIBRARY", "GGML_BACKEND"]},
        "version": version, "source_url": {"type": "string", "format": "uri", "pattern": "^https://", "maxLength": 2048},
        "sha256": sha, "license_evidence_ref": ident})
    environment = {
        "$schema": "https://json-schema.org/draft/2020-12/schema",
        "$id": "https://p11.invalid/design/kernel-environment-v1.0.schema.json",
        "title": "P11 immutable kernel environment payload draft",
        **obj({"schema_version": {"const": "1.0-draft"}, "environment_id": ident,
            "engine_kind": {"enum": ["LM_STUDIO", "LLAMA_CPP_NATIVE", "COMFYUI", "LMA_LOCAL", "AUTHORIZED_UPSTREAM"]},
            "platform": {"enum": ["WINDOWS", "LINUX", "ANDROID"]},
            "arch": {"enum": ["X86_64", "ARM64"]}, "adapter_contract_version": version,
            "adapter_version": version, "components": arr(artifact, 1),
            "model_digests": arr(sha), "recipe_digests": arr(sha, 1),
            "protocol_profile_ids": arr(ident, 1),
            "modalities": arr({"enum": ["TEXT", "IMAGE", "VIDEO", "AUDIO", "THREED"]}, 1, 5),
            "node_allowlist_revision": ident, "node_allowlist_digest": sha,
            "owner_consent_ref": ident, "approval_key_id": ident},
            required=["schema_version", "environment_id", "engine_kind", "platform", "arch",
                "adapter_contract_version", "adapter_version", "components", "model_digests",
                "recipe_digests", "protocol_profile_ids", "modalities", "owner_consent_ref",
                "approval_key_id"]),
        "allOf": [
            {"if": {"properties": {"engine_kind": {"const": "LLAMA_CPP_NATIVE"}}}, "then": {
                "properties": {"platform": {"enum": ["WINDOWS", "LINUX"]}, "components": {
                    "allOf": [{"contains": {"properties": {"role": {"const": role}}, "required": ["role"]}}
                              for role in ("PRIVATE_WORKER", "LLAMA_LIBRARY", "GGML_BACKEND")]}}}},
            {"if": {"properties": {"engine_kind": {"const": "LM_STUDIO"}}}, "then": {
                "properties": {"platform": {"enum": ["WINDOWS", "LINUX"]}, "components": {
                    "allOf": [{"contains": {"properties": {"role": {"const": role}}, "required": ["role"]}}
                              for role in ("LM_CLI", "LM_DAEMON", "LM_RUNTIME")]}}}},
            {"if": {"properties": {"engine_kind": {"const": "COMFYUI"}}}, "then": {
                "required": ["node_allowlist_revision", "node_allowlist_digest"],
                "properties": {"platform": {"enum": ["WINDOWS", "LINUX"]}, "components": {
                    "allOf": [{"contains": {"properties": {"role": {"const": role}}, "required": ["role"]}}
                              for role in ("COMFY_CORE", "PYTHON")]}}}},
            {"if": {"properties": {"engine_kind": {"const": "LMA_LOCAL"}}}, "then": {
                "properties": {"platform": {"const": "ANDROID"}, "components": {
                    "allOf": [{"contains": {"properties": {"role": {"const": role}}, "required": ["role"]}}
                              for role in ("LMA_SOURCE", "ANDROID_NATIVE", "PLATFORM_BRIDGE")]}}}}
        ],
        "$comment": "Hash/sign detached outside payload; use exact approved UTF-8 bytes, not reserialization. Schema does not prove signatures, binaries, dependency closure, consent, licenses or sale qualification. Never include prompt/body/key/host paths."
    }
    node_pack = obj({"node_id": ident, "source_url": {"type": "string", "format": "uri", "pattern": "^https://", "maxLength": 2048},
        "exact_revision": version, "code_sha256": sha,
        "class_types": arr({"type": "string", "pattern": "^[A-Za-z][A-Za-z0-9_]{0,127}$"}, 1),
        "dependency_artifacts": arr(artifact), "reviewed_install_step_ids": arr(ident),
        "license_evidence_refs": arr(ident, 1), "file_scopes": arr({"enum": ["TASK_WORKSPACE", "APPROVED_MODELS_READONLY"]}, 0, 2),
        "install_download_hosts": arr({"type": "string", "format": "hostname"}, 1, 30),
        "execution_network": {"enum": ["NONE", "APPROVED_HOSTS"]},
        "execution_allowed_hosts": arr({"type": "string", "format": "hostname"}, 0, 30),
        "runtime_code_download_allowed": {"const": False}, "runtime_package_install_allowed": {"const": False}},
        allOf=[{"if": {"properties": {"execution_network": {"const": "NONE"}}},
                "then": {"properties": {"execution_allowed_hosts": {"maxItems": 0}}},
                "else": {"properties": {"execution_allowed_hosts": {"minItems": 1}}}}])
    allowlist = {
        "$schema": "https://json-schema.org/draft/2020-12/schema",
        "$id": "https://p11.invalid/design/node-allowlist-v1.0.schema.json",
        "title": "P11 finite ComfyUI node allowlist payload draft",
        **obj({"schema_version": {"const": "1.0-draft"}, "allowlist_revision": ident,
            "comfy_environment_ref": ident, "approval_key_id": ident,
            "core_class_types": arr({"type": "string", "pattern": "^[A-Za-z][A-Za-z0-9_]{0,127}$"}, 1),
            "node_packs": arr(node_pack, 0, 64),
            "valid_until": {"type": "string", "format": "date-time"}, "revoked": {"type": "boolean"},
            "unlisted_enabled_node_policy": {"const": "REJECT_BEFORE_IMPORT"},
            "buyer_install_permission": {"const": False}}),
        "$comment": "Finite 64-pack representation limit, not 64 trusted packs or an installation target. Requires code and transitive dependency review, owner consent, enabled-directory verification before imports, and recipe requalification. Not an OS sandbox."
    }
    policy = {
        "version": "1.0-draft", "date": "2026-10-29", "status": "DESIGN_UNIMPLEMENTED",
        "test_status": "NOT_EXECUTED", "sale_enabled": False,
        "routes": [
            {"platform": "WINDOWS", "purpose": "PRIVATE_LLM", "engine": "LLAMA_CPP_NATIVE",
             "implementation": "NATIVE_CPP_WORKER_EMBEDDED_C_API", "transport": "INHERITED_PIPE_NO_TCP_CONTENT",
             "frozen_versions": None, "test_status": "NOT_EXECUTED", "sale_enabled": False},
            {"platform": "LINUX", "purpose": "PRIVATE_LLM", "engine": "LLAMA_CPP_NATIVE",
             "implementation": "NATIVE_CPP_WORKER_EMBEDDED_C_API", "transport": "INHERITED_PIPE_NO_TCP_CONTENT",
             "frozen_versions": None, "test_status": "NOT_EXECUTED", "sale_enabled": False},
            {"platform": "WINDOWS", "purpose": "LLM", "engine": "LM_STUDIO",
             "implementation": "LMS_CLI_LLMSTER_RUNTIME_WRAPPER", "replaceable_layers": ["CLI", "DAEMON", "INFERENCE_RUNTIME"],
             "frozen_versions": None, "private_profile_validation": "NOT_EXECUTED", "license_status": "UNVERIFIED"},
            {"platform": "WINDOWS", "purpose": "MEDIA", "engine": "COMFYUI",
             "implementation": "COMPLETE_NATIVE_ENVIRONMENT", "modalities": ["IMAGE", "VIDEO", "AUDIO", "THREED"],
             "node_install_policy": "PLATFORM_ALLOWLIST_AND_OWNER_CONSENT", "frozen_versions": None},
            {"platform": "ANDROID", "purpose": "LOCAL_INFERENCE", "engine": "LMA_LOCAL",
             "implementation": "LMA_SOURCE_DERIVATIVE", "source_commit": "5d9e81d59273f2a656763ea3ee3401b2846a18cc",
             "upstream_language_fact": "JAVA_SERVICE_AND_WEBVIEW", "new_bridge_candidate": "KOTLIN_JAVA_INTEROP",
             "executable_replacement": "SIGNED_APK_NATIVE_COMPONENTS", "downloaded_executable_hotload": False},
            {"platform": "LINUX", "purpose": "RETAINED_NATIVE_DUAL_ENGINE", "engine": "LM_STUDIO_AND_COMFYUI",
             "windows_evidence_reused_as_linux_pass": False}
        ],
        "replacement": {"immutable_environment": True, "replace_active_paid_environment": False,
            "unreviewed_latest_activation": False, "owner_confirmation_required": True,
            "same_resource_maintenance_gate": True, "requalify_before_new_sale": True,
            "rollback_to_revoked_environment": False, "preserve_fences_inbox_and_financial_facts": True},
        "binding_field": "kernel_environment_digest",
        "binding_scopes": ["CAPABILITY", "QUOTE_CONTRACT", "EXECUTION_MANIFEST_OR_RENTAL_GRANT", "ADAPTER_COMMAND", "LOCAL_TASK_RECORD"],
        "whitelist": {"before_import_inventory_check": True, "exact_revision_and_hash": True,
            "transitive_dependencies_reviewed": True, "buyer_manager_access": False,
            "runtime_package_install_allowed": False, "native_venv_is_security_sandbox": False},
        "provider_controls": ["INSTALL", "MODEL_AND_RECIPE_SELECTION", "VERSION_CONFIRMATION", "SHARE_CONDITIONS", "PRICE", "DRAIN", "EMERGENCY_STOP"],
        "provider_visibility": ["COMPONENT_VERSIONS", "ENVIRONMENT_DIGEST", "NODE_INVENTORY", "UPDATE_DIFF", "TASK_AND_RESOURCE_STATE", "EARNINGS_STATE"],
        "local_content_protection": {
            "status": "DESIGN_UNIMPLEMENTED", "test_status": "NOT_EXECUTED",
            "target": "RAISE_LOCAL_CONTENT_ACCESS_COST_FOR_COMMON_TOOLS",
            "required_checks": ["PROVIDER_UI", "TASK_FILES", "ENGINE_HISTORY_AND_APIS", "MODEL_LOGS",
                                "LOCAL_NETWORK_LAST_HOP", "MEDIA_SAVE_PATHS", "SIMPLE_NODE_OR_PACKAGE_CHANGES"],
            "advanced_memory_analysis_required": False,
            "recommended_first_route": "NATIVE_SEPARATE_IDENTITY_ENCRYPTED_WORKSPACE_CLOSED_CONTENT_ENDPOINTS",
            "private_llm_engine": "LLAMA_CPP_NATIVE", "lm_studio_compatibility_retained": True,
            "complete_plan": "private-execution-plan-v1.0.json",
            "new_service_mode": False, "existing_privacy_profile_enum_changed": False,
            "ordinary_native_or_vm_is_attested_confidential": False,
            "ordinary_vm": "OPTIONAL_PRODUCT_EVALUATION_ONLY_NO_LOCAL_EXECUTION",
            "lm_studio_log_and_http_last_hop_validation": "NOT_EXECUTED",
            "comfyui_requires_native_route_auth_and_prewrite_encryption": True,
            "lma_requires_internal_engine_path_and_private_encrypted_task_storage": True,
            "qualification_bound_to_environment_digest": True,
            "whole_malicious_client_resisted_by_self_hash": False,
            "sale_enabled": False,
            "conformance_cases": [f"LP-{i:02d}" for i in range(1, 15)],
            "source_document": "docs/技术开发/附录-本地内容隐私防护与分级验收.md"
        },
        "protocol": {"private_data_json_fields_required": False, "audio": "OPENAI_APPROVED_SUBSETS",
            "music_and_3d": "DECLARED_RESPONSES_STRUCTURED_ASSET_PROFILE", "fake_vendor_3d_endpoint": False,
            "unverified_profile_sale_enabled": False},
        "conformance_cases": [f"K{i:02d}" for i in range(1, 21)],
        "deferred": ["SERVER_DEPLOYMENT", "HTTPS_CERTIFICATES", "OBSERVABILITY"],
        "source_document": SOURCE,
        "source_check": "docs/资料/供给端内核封装核查-2026-10-29.md"
    }
    execution_plan = {
        "version": "1.0-draft", "date": "2026-10-29", "status": "DESIGN_UNIMPLEMENTED",
        "test_status": "NOT_EXECUTED", "sale_enabled": False,
        "source_document": "docs/技术开发/附录-低开销隐私执行完整方案.md",
        "authorized_decisions": {"additional_private_llm_engine": True, "lm_studio_compatibility_retained": True,
            "local_virtualization_execution": False, "deploy_and_observability_planning": False},
        "execution": {"llm": "LLAMA_CPP_NATIVE_EMBEDDED_C_API", "media": "COMFYUI_PRIVATE_PIPE_BRIDGE",
            "android": "LMA_INPROCESS_JNI", "content_tcp_listener": False, "persistent_native_workers": True,
            "extra_model_copies_for_privacy": 0, "crypto_gpu_jobs": 0, "task_files_only_encrypted": True},
        "wire": {"profile_id": "BINARY_STREAM_1", "status": "DRAFT_NOT_INTEROPERABILITY_TESTED",
            "separate_from_legacy_json_frame_schema": True, "requires_signed_profile_binding": True,
            "crypto_suite": "LIBSODIUM_SEALED_BOX_SECRETSTREAM_PLUS_IDENTITY_SIGNATURE",
            "library_version_frozen": False, "stream_header_max_bytes": 16384,
            "header_length_prefix_bytes": 4, "end_record_tag": "TAG_FINAL",
            "extra_empty_final_record": False,
            "binary_channel_negotiation_contract_frozen": False,
            "length_prefix_bytes": 4, "sequence_bytes": 8, "decrypted_inner_header_bytes": 8,
            "decrypted_record_max_bytes": 65536, "data_payload_max_bytes": 65528,
            "ciphertext_max_bytes": 65553, "body_length_max_bytes": 65561, "wire_record_max_bytes": 65565,
            "text_coalesce_max_ms": 8, "shared_stream_buffer_max_bytes": 4 * 1024 * 1024,
            "reorder_max_frames": 32, "reorder_max_bytes": 2 * 1024 * 1024,
            "media_base64_required": False, "silent_profile_fallback": False},
        "storage": {"profile_id": "LOCAL_SEGMENTED_1", "status": "DRAFT_REQUIRES_CRYPTO_REVIEW",
            "segment_plaintext_bytes": 1024 * 1024, "fresh_independent_secretstream_header_per_segment": True,
            "per_object_random_key": True, "segment_aad_identity_and_index_required": True,
            "encrypted_authenticated_final_manifest": True, "manifest_max_bytes": 65536,
            "immutable_completed_object_read_seek": True,
            "inplace_overwrite": False, "desktop_decrypted_segment_cache_max": 4,
            "android_decrypted_segment_cache_max": 2, "media_plaintext_disk_output": False,
            "ram_disk": False, "model_file_encryption": False, "keys_in_ordinary_database": False},
        "scheduling": {"shared_physical_resource_gate": True, "input_preparation_holds_gpu": False,
            "result_upload_or_buyer_receipt_holds_gpu": False,
            "delivery_scope_distinct_from_compute_owner": True,
            "active_encoding_using_gpu_holds_gate": True, "release_requires_actual_stop_and_cleanup": True,
            "old_attempt_cannot_clean_next_owner": True, "separate_control_channel": True,
            "rental_subtask_completion_releases_session_gate": False},
        "budgets": {"classification": "UNMEASURED_ACCEPTANCE_TARGETS",
            "desktop_added_idle_mib": 64, "desktop_added_active_mib": 96,
            "android_added_idle_mib": 12, "android_added_active_mib": 24,
            "buyer_added_idle_mib": 48, "buyer_added_active_mib": 80,
            "desktop_content_buffer_mib_per_resource": 16, "desktop_process_pool_mib": 32,
            "android_content_buffer_mib": 8, "buyer_content_buffer_mib": 16,
            "desktop_inference_throughput_regression_pct": 3, "desktop_e2e_p95_regression_pct": 5,
            "android_e2e_p95_regression_pct": 8, "desktop_first_chunk_extra_p95_ms": 20,
            "android_first_chunk_extra_p95_ms": 30, "control_dispatch_p95_ms": 50,
            "default_spool_mib_per_active_resource": 256, "max_media_object_mib": 64,
            "max_cipher_media_object_mib": 66},
        "acceptance": {"local_privacy_cases": [f"LP-{i:02d}" for i in range(1, 15)],
            "work_packages": [f"PE-{i:02d}" for i in range(1, 9)], "ab_repetitions_per_case_min": 30,
            "same_model_backend_precision_context_and_recipe": True, "runtime_evidence_available": False}
    }
    return {"provider-kernel-policy-v1.0.json": policy,
            "private-execution-plan-v1.0.json": execution_plan,
            "kernel-environment-v1.0.schema.json": environment,
            "node-allowlist-v1.0.schema.json": allowlist}
