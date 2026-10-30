"""生成自有控制面与传输设计契约；不是运行中的服务，也非厂商协议替代品。"""
from __future__ import annotations

from copy import deepcopy

DATE = "2026-10-29"
SCHEMA = "https://json-schema.org/draft/2020-12/schema"


def obj(properties: dict, required: list[str] | None = None, **extra) -> dict:
    return {"type": "object", "additionalProperties": False, "properties": properties,
            "required": list(properties) if required is None else required, **extra}


def ref(name: str) -> dict:
    return {"$ref": "#/components/schemas/" + name}


def arr(items: dict, maximum: int = 100, minimum: int = 0) -> dict:
    return {"type": "array", "items": items, "minItems": minimum, "maxItems": maximum}


def enum(*values: str) -> dict:
    return {"type": "string", "enum": list(values)}


ID = {"type": "string", "pattern": "^[A-Za-z][A-Za-z0-9_-]{2,127}$", "maxLength": 128}
TEXT = {"type": "string", "minLength": 1, "maxLength": 512}
HASH = {"type": "string", "pattern": "^[a-f0-9]{64}$"}
UINT = {"type": "integer", "minimum": 0, "maximum": 9007199254740991}
VERSION = {"type": "integer", "minimum": 1, "maximum": 2147483647}
COUNTER = {"type": "string", "pattern": "^(0|[1-9][0-9]{0,19})$"}
TIME = {"type": "string", "format": "date-time"}
BOOL = {"type": "boolean"}
TOKEN = {"type": "string", "minLength": 32, "maxLength": 4096, "writeOnly": True}
SIG = {"type": "string", "minLength": 64, "maxLength": 4096, "pattern": "^[A-Za-z0-9_-]+$"}
MODE = enum("PLAINTEXT", "E2E", "GPU_RENTAL")
PRIVACY = enum("STANDARD", "PLATFORM_BLIND", "ATTESTED_CONFIDENTIAL")


def make_schemas() -> dict:
    s: dict[str, dict] = {}
    s["SafeError"] = obj({"error": obj({"code": TEXT, "message": TEXT, "retryable": BOOL,
        "request_id": ID, "details": obj({"resource_id": ID, "state": TEXT, "retry_after_sec": UINT}, [])})})
    s["Ack"] = obj({"id": ID, "version": VERSION, "status": TEXT, "recorded_at": TIME})
    s["EvidenceRef"] = obj({"evidence_id": ID, "kind": enum("LICENSE", "PAYMENT", "PHYSICAL_TEST", "SDK", "CRYPTO", "ATTESTATION", "CLEANUP", "USAGE", "DISPUTE"),
        "digest": HASH, "valid_until": TIME, "scope_ref": ID}, ["evidence_id", "kind", "digest", "scope_ref"])
    s["PublicSpec"] = obj({"model_id": ID, "workflow_id": ID, "workflow_revision": VERSION,
        "width": {"type": "integer", "minimum": 1, "maximum": 16384}, "height": {"type": "integer", "minimum": 1, "maximum": 16384},
        "duration_ms": UINT, "frame_rate_num": VERSION, "frame_rate_den": VERSION,
        "input_token_limit": UINT, "output_token_limit": UINT, "count": {"type": "integer", "minimum": 1, "maximum": 100},
        "media_format": enum("png", "jpeg", "webp", "mp4", "wav", "mp3", "flac", "glb", "json"),
        "sample_rate_hz": UINT, "audio_channels": {"type": "integer", "minimum": 1, "maximum": 8},
        "max_vertices": UINT, "max_texture_bytes": UINT, "coordinate_profile_id": ID}, [],
        description="仅公开规格，私密prompt、工具内容和媒体URL禁止出现在控制面。SKU另施更严格界限。")
    s["LegacyPrice"] = obj({"currency": {"const": "CNY"}, "unit": enum("REQUEST", "IMAGE", "VIDEO", "TOKEN_1K", "AVAILABLE_MINUTE"),
        "unit_price_fen": UINT, "unit_rate": obj({"numerator_fen": UINT, "denominator_units": VERSION}),
        "commission_bps": {"type": "integer", "minimum": 0, "maximum": 10000},
        "channel_cost_owner": enum("BUYER", "SELLER", "PLATFORM"), "fee_policy_version": ID},
        ["currency", "unit", "commission_bps", "channel_cost_owner", "fee_policy_version"],
        oneOf=[{"required": ["unit_price_fen"], "not": {"required": ["unit_rate"]}},
               {"required": ["unit_rate"], "not": {"required": ["unit_price_fen"]}}],
        description="仅历史合同：以旧unit为基准的整数分/有理分；新售使用NBPrice。")
    s["NBRate"] = obj({"numerator_fen": UINT, "denominator_units": {**UINT, "minimum": 1}})
    s["NBPrice"] = obj({"price_contract_version": {"const": "NB_PRICE_1"}, "currency": {"const": "CNY"},
        "unit": enum("AVAILABLE_HOUR", "TOKEN_1M"), "rounding_profile": {"const": "ORDER_CEIL_FEN_1"},
        "hour_rate": ref("NBRate"), "input_rate": ref("NBRate"), "output_rate": ref("NBRate"),
        "commission_bps": {"type": "integer", "minimum": 0, "maximum": 10000},
        "channel_cost_owner": enum("SELLER", "PLATFORM"), "fee_policy_version": ID},
        ["price_contract_version", "currency", "unit", "rounding_profile", "commission_bps", "channel_cost_owner", "fee_policy_version"],
        oneOf=[{"properties": {"unit": {"const": "AVAILABLE_HOUR"}}, "required": ["hour_rate"],
                "not": {"anyOf": [{"required": ["input_rate"]}, {"required": ["output_rate"]}]}},
               {"properties": {"unit": {"const": "TOKEN_1M"}}, "required": ["input_rate", "output_rate"],
                "not": {"required": ["hour_rate"]}}],
        description="新售小时或十进制百万token双价；原始毫秒/token累计，整单向上取分，含卖方佣金；缓存/推理先消重，未知usage不可收费。")
    s["Price"] = {"oneOf": [ref("LegacyPrice"), ref("NBPrice")],
        "description": "不可变价版联合类型；旧读端不能识别NB_PRICE_1时拒绝而非降价版。"}
    s["Policy"] = obj({"service_mode": MODE, "privacy_profile": PRIVACY, "region": TEXT,
        "protocol_profile_id": ID, "acceptance_policy_id": ID, "disclosure_version": ID},
        allOf=[{"if": {"properties": {"service_mode": {"const": "PLAINTEXT"}}, "required": ["service_mode"]},
                "then": {"properties": {"privacy_profile": {"const": "STANDARD"}}}},
               {"if": {"properties": {"service_mode": {"const": "E2E"}}, "required": ["service_mode"]},
                "then": {"properties": {"privacy_profile": enum("PLATFORM_BLIND", "ATTESTED_CONFIDENTIAL")}}},
               {"if": {"properties": {"service_mode": {"const": "GPU_RENTAL"}}, "required": ["service_mode"]},
                "then": {"properties": {"privacy_profile": enum("STANDARD", "ATTESTED_CONFIDENTIAL")}}}],
        description="GPU_RENTAL的加密组合还需单独证据；schema合法不表示已可售。")
    s["ScheduleWindow"] = obj({"starts_at": TIME, "ends_at": TIME})
    s["SellerApplicationWrite"] = obj({"identity_evidence_ref": ID, "device_rights_evidence_refs": arr(ID, 50, 1),
        "payee_eligibility_ref": ID, "regions": arr(TEXT, 20, 1), "agreement_version": ID,
        "agreement_accepted_at": TIME})
    s["SellerProfile"] = obj({"seller_id": ID, "display_name": TEXT, "qualification_state": enum("DRAFT", "SUBMITTED", "NEEDS_EVIDENCE", "APPROVED", "SUSPENDED", "EXITING", "CLOSED"),
        "public_identity_notice": TEXT, "approved_regions": arr(TEXT), "payee_eligibility_state": enum("UNVERIFIED", "APPROVED", "EXPIRED", "REVOKED"),
        "version": VERSION, "evidence_refs": arr(ref("EvidenceRef"))})
    s["ListingWrite"] = obj({"title": TEXT, "sku_id": ID, "device_ids": arr(ID, 50, 1), "policy": ref("Policy"),
        "public_spec": ref("PublicSpec"), "price": ref("Price"), "windows": arr(ref("ScheduleWindow"), 100, 1),
        "capacity": {"type": "integer", "minimum": 1, "maximum": 100}, "evidence_refs": arr(ID, 50, 1)})
    s["ListingPatch"] = obj(deepcopy(s["ListingWrite"]["properties"]), [], minProperties=1,
        description="只创建未来修订；不能覆盖已售快照。更新同时要求If-Match。")
    s["Listing"] = obj({"listing_id": ID, "seller_id": ID, "revision": VERSION, "state": enum("DRAFT", "SUBMITTED", "REVIEWING", "APPROVED", "PUBLISHED", "PAUSED", "EXPIRED", "REJECTED", "REVOKED", "ARCHIVED"),
        "definition": ref("ListingWrite"), "updated_at": TIME})
    s["DevicePairingWrite"] = obj({"platform": enum("WINDOWS", "LINUX", "ANDROID"), "device_label": TEXT, "owner_confirmed": {"const": True}})
    s["DevicePairing"] = obj({"pairing_id": ID, "one_time_token": {"type": "string", "minLength": 32, "maxLength": 4096, "readOnly": True},
        "expires_at": TIME, "seller_id": ID}, ["pairing_id","expires_at","seller_id"], description="R007：凭据仅首次显示；同键恢复只返回配对元数据，不再披露秘密。5分钟有效，最多3个待配对，5次错误关闭。")
    s["EnrollmentWrite"] = obj({"pairing_id": ID, "device_public_key": {"type": "string", "pattern":"^[A-Za-z0-9_-]{43}$", "description":"ED25519_DEVICE_1的规范无填充base64url 32字节公钥；不可替代内容加密钥"},
        "key_profile_id": {"const":"ED25519_DEVICE_1"}, "os_build": TEXT, "client_release_id": ID, "device_fingerprint_hash": HASH, "resource_kind":enum("GPU_DEVICE","ANDROID_DEVICE","AUTHORIZED_RELAY"), "model_name":{"type":"string","minLength":1,"maxLength":128}}, description="来源为提供方自报，登记不等于硬件/型号/许可/可售已验证；手机必须具体型号。")
    s["Device"] = obj({"device_id": ID, "seller_id": ID, "platform": enum("WINDOWS", "LINUX", "ANDROID"),
        "resource_kind": enum("GPU_DEVICE", "ANDROID_DEVICE", "AUTHORIZED_RELAY"),
        "state": enum("UNPAIRED", "PAIRED", "PROBING", "NEEDS_COMPONENTS", "BENCHMARKING", "QUALIFIED", "ONLINE", "DRAINING", "OFFLINE", "CLEANING", "QUARANTINED", "REVOKED", "RETIRED"),
        "version": VERSION, "last_seen_at": {"anyOf":[TIME,{"type":"null"}]}, "heartbeat_sequence":{"anyOf":[COUNTER,{"type":"null"}]}, "reported_resource_state":enum("IDLE","BUSY","DRAINING","HOT","LOW_BATTERY","LOW_DISK","UNKNOWN"), "active_slots": UINT, "capability_ids": arr(ID), "client_release_id": ID,
        "device_label": TEXT, "model_name":{"anyOf":[{"type":"string","minLength":1,"maxLength":128},{"type":"null"}]}, "identity_profile_id":enum("LEGACY_UNVERIFIED","ED25519_DEVICE_1"),"key_fingerprint":HASH,"resource_policy":{"type":"object","description":"本人或本设备只读策略投影；active_slots是实际锁owner计数，能力准入独立"}})
    s["KernelBinding"] = obj({"device_id": ID, "capability_id": ID,
        "kernel_environment_ref": ID, "kernel_environment_digest": HASH},
        description="不可变已批准环境；同卖家重派只能在已确认设备/能力/环境集合中选择。实际文件/许可/签名/当前资格由应用验证。")
    s["CapabilityWrite"] = obj({"engine": enum("COMFYUI", "LM_STUDIO", "LLAMA_CPP_NATIVE", "LMA", "AUTHORIZED_UPSTREAM"),
        "kernel_environment_ref": ID, "kernel_environment_digest": HASH,
        "engine_version": TEXT, "adapter_version": TEXT, "model_digest": HASH, "execution_location": enum("LOCAL_GPU", "LOCAL_CPU", "LOCAL_ANDROID", "AUTHORIZED_UPSTREAM"),
        "protocol_profile_ids": arr(ID, 20, 1), "benchmark_evidence_ref": ID, "license_evidence_refs": arr(ID, 50, 1),
        "supported_policies": arr(ref("Policy"), 20, 1)})
    s["HeartbeatWrite"] = obj({"sequence": COUNTER, "device_version": VERSION, "recorded_at": TIME,
        "active_attempt_id": ID, "fencing_token": COUNTER, "resource_state": enum("IDLE", "BUSY", "DRAINING", "HOT", "LOW_BATTERY", "LOW_DISK", "UNKNOWN")},
        ["sequence", "device_version", "recorded_at", "resource_state"])
    s["ReasonWrite"] = obj({"reason_code": ID, "expected_version": VERSION, "evidence_refs": arr(ID, 50)}, ["reason_code", "expected_version"])
    s["DeviceDisplay"] = obj({"device_id": ID, "model_name": {"type": "string", "minLength": 1, "maxLength": 128}})
    s["Service"] = obj({"listing_id": ID, "listing_revision": VERSION, "seller_id": ID, "seller_display_name": TEXT,
        "sku_id": ID, "policy": ref("Policy"), "public_spec": ref("PublicSpec"), "price": ref("Price"),
        "kernel_bindings": arr(ref("KernelBinding"), 50, 1),
        "availability": enum("AVAILABLE", "SCHEDULED", "BUSY", "UNAVAILABLE"), "evidence_refs": arr(ref("EvidenceRef")),
        "device_displays": arr(ref("DeviceDisplay"),50,1), "deadline_sec": VERSION},
        ["listing_id", "listing_revision", "seller_id", "seller_display_name", "sku_id", "policy", "public_spec", "price", "kernel_bindings", "availability", "evidence_refs"],
        description="NB新售投影另提供device_displays具体型号与deadline_sec；数据面内容不出现在公开字段。")
    s["QuoteWrite"] = obj({"listing_id": ID, "listing_revision": VERSION, "sku_id": ID, "policy": ref("Policy"),
        "public_spec": ref("PublicSpec"), "budget_fen": UINT, "desired_region": TEXT, "quantity": {"type": "integer", "minimum": 1, "maximum": 100}})
    s["ContractSnapshot"] = obj({"seller_id": ID, "listing_id": ID, "listing_revision": VERSION, "device_ids": arr(ID, 50, 1),
        "sku_id": ID, "policy": ref("Policy"), "public_spec": ref("PublicSpec"), "price": ref("Price"),
        "kernel_bindings": arr(ref("KernelBinding"), 50, 1),
        "total_budget_fen": UINT, "seller_price_fen": UINT, "buyer_fee_fen": UINT,
        "deadline_sec": VERSION, "same_seller_retries": {"const": 1}, "evidence_refs": arr(ref("EvidenceRef")),
        "contract_hash": HASH, "contract_schema_version": {"const": "3.0-draft"}})
    s["Quote"] = obj({"quote_id": ID, "state": enum("VALID", "EXPIRED", "CONSUMED", "CANCELED"), "version": VERSION,
        "contract": ref("ContractSnapshot"), "created_at": TIME, "expires_at": TIME})
    s["OrderWrite"] = obj({"quote_id": ID, "contract_hash": HASH, "disclosure_version": ID, "disclosure_accepted_at": TIME,
        "caller_key": ID, "replaces_order_id": ID}, ["quote_id", "contract_hash", "disclosure_version", "disclosure_accepted_at", "caller_key"])
    s["Order"] = obj({"order_id": ID, "buyer_id": ID, "seller_id": ID, "version": VERSION,
        "state": enum("CREATED", "WAITING_PAYMENT", "READY", "FULFILLING", "DELIVERY_PENDING", "COMPLETED", "CLOSED", "REVIEW"),
        "contract": ref("ContractSnapshot"), "payment_id": ID, "job_id": ID, "rental_session_id": ID,
        "created_at": TIME, "updated_at": TIME}, ["order_id", "buyer_id", "seller_id", "version", "state", "contract", "created_at", "updated_at"])
    s["PaymentIntentWrite"] = obj({"expected_order_version": VERSION, "channel_capability_ref": ID, "return_target_id": ID},
        ["expected_order_version", "channel_capability_ref"], description="return_target为已登记安全引用，不接受任意跳转URL。实际渠道交互另按正式协议。")
    s["Payment"] = obj({"payment_id": ID, "order_id": ID, "seller_id": ID, "version": VERSION,
        "state": enum("CREATED", "PENDING", "AUTHORIZED", "SETTLED", "FAILED", "UNKNOWN", "CLOSED"),
        "environment": enum("SANDBOX", "PRODUCTION"), "amount_fen": UINT, "settled_fen": UINT,
        "channel_reference": ID, "channel_action_ref": ID, "updated_at": TIME},
        ["payment_id", "order_id", "seller_id", "version", "state", "environment", "amount_fen", "settled_fen", "updated_at"])
    s["AssetWrite"] = obj({"order_id": ID, "kind": enum("PLAINTEXT_INPUT", "CIPHER_INPUT", "PLAINTEXT_RESULT", "CIPHER_RESULT", "SELECTIVE_EVIDENCE"),
        "media_type": TEXT, "size_bytes": {**UINT, "maximum": 67108864}, "sha256": HASH, "attempt_id": ID,
        "fencing_token": COUNTER}, ["order_id", "kind", "media_type", "size_bytes", "sha256"])
    s["Asset"] = obj({"asset_id": ID, "order_id": ID, "version": VERSION,
        "state": enum("REGISTERED", "UPLOADING", "VERIFIED", "REJECTED", "EXPIRED", "DELETED"),
        "size_bytes": UINT, "sha256": HASH, "expires_at": TIME, "transfer_grant_ref": ID},
        ["asset_id", "order_id", "version", "state", "size_bytes", "sha256", "expires_at"])
    s["AssetCompleteWrite"] = obj({"expected_version": VERSION, "sha256": HASH, "size_bytes": UINT,
        "upload_manifest_hash": HASH})
    s["JobWrite"] = obj({"order_id": ID, "expected_order_version": VERSION, "input_asset_refs": arr(ID, 50, 1),
        "encryption_intent_id": ID, "caller_key": ID, "deadline_at": TIME},
        ["order_id", "expected_order_version", "caller_key", "deadline_at"],
        oneOf=[{"required": ["input_asset_refs"], "not": {"required": ["encryption_intent_id"]}},
               {"required": ["encryption_intent_id"], "not": {"required": ["input_asset_refs"]}}],
        description="明文引用已就绪输入；E2E先建立INPUT_PENDING及PREPARED上下文，无执行权、不占GPU。拿到attempt/token/recipient后才认证加密上传并提交envelopes；完成准入才ISSUED。")
    s["Job"] = obj({"job_id": ID, "order_id": ID, "seller_id": ID, "version": VERSION,
        "state": enum("INPUT_PENDING", "QUEUED", "RUNNING", "VALIDATING", "WAITING_BUYER", "ACCEPTED", "FAILED", "CANCELED", "UNVERIFIED"),
        "current_attempt_id": ID, "current_fencing_token": COUNTER, "recipient_id": ID,
        "accepted_attempt_id": ID, "deadline_at": TIME,
        "failure_code": ID}, ["job_id", "order_id", "seller_id", "version", "state", "deadline_at"])
    s["Result"] = obj({"delivery_id": ID, "job_id": ID, "attempt_id": ID, "delivery_revision": VERSION,
        "asset_refs": arr(ID, 50, 1), "manifest_hash": HASH, "acceptance_state": enum("PENDING", "PLATFORM_ACCEPTED", "BUYER_ACCEPTED", "REJECTED", "UNVERIFIED"),
        "expires_at": TIME})
    s["EncryptionIntentWrite"] = obj({"quote_id": ID, "contract_hash": HASH, "buyer_recipient_key_id": ID,
        "seller_recipient_id": ID, "expected_recipient_key_hash": HASH, "trust_policy_ref": ID})
    s["EncryptionIntent"] = obj({"intent_id": ID, "quote_id": ID, "seller_id": ID, "recipient_id": ID,
        "recipient_key_hash": HASH, "buyer_recipient_key_id": ID, "contract_hash": HASH, "expires_at": TIME, "version": VERSION})
    s["AttestationChallengeWrite"] = obj({"encryption_intent_id": ID, "challenge_nonce_b64u": {"type": "string", "pattern": "^[A-Za-z0-9_-]{43,256}$"},
        "recipient_key_hash": HASH, "verifier_policy_id": ID})
    s["AttestationChallenge"] = obj({"challenge_id": ID, "recipient_id": ID, "recipient_key_hash": HASH,
        "challenge_hash": HASH, "policy_id": ID, "expires_at": TIME})
    s["AttestationEvidence"] = obj({"challenge_id": ID, "recipient_id": ID, "evidence_asset_ref": ID,
        "evidence_digest": HASH, "issued_at": TIME, "expires_at": TIME, "revocation_source_ref": ID,
        "is_mock": BOOL}, description="原始证明单独有界对象，买方/可信验证者独立验证；平台返回不代表通过。")
    s["EnvelopeWrite"] = obj({"intent_id": ID, "attempt_id": ID, "fencing_token": COUNTER, "recipient_id": ID,
        "recipient_key_hash": HASH, "envelope_asset_ref": ID, "input_asset_refs": arr(ID, 50),
        "manifest_hash": HASH, "suite_profile_id": ID, "sender_signature": SIG},
        description="完成INPUT_PENDING的当前加密上下文；空asset集合只用于获准流式输入，附件引用须完整。仍需资金/许可/容量准入，不能仅凭封装开始执行。")
    s["ReceiptWrite"] = obj({"receipt_id": ID, "attempt_id": ID, "fencing_token": COUNTER,
        "delivery_id": ID, "delivery_revision": VERSION, "cipher_manifest_hash": HASH,
        "decision": enum("ACCEPT", "REJECT"), "signer_key_id": ID, "signature_profile_id": ID,
        "signed_at": TIME, "signature": SIG}, description="签名绑定合同/路径job等规范上下文；非空字符串不等于签名有效。")
    s["CipherChannelWrite"] = obj({"order_id": ID, "attempt_id": ID, "fencing_token": COUNTER,
        "recipient_id": ID, "transport": enum("WSS", "HTTPS_FRAMES"), "direction": enum("BUYER_TO_SELLER", "SELLER_TO_BUYER")})
    s["CipherChannel"] = obj({"channel_id": ID, "transport": enum("WSS", "HTTPS_FRAMES"),
        "connection_grant_ref": ID, "expires_at": TIME, "max_frame_bytes": {"const": 131072}, "frame_schema_version": {"const": "3.0-draft"}},
        description="引用单次有限授权而非把长效token放URL；帧协议与握手另行冻结。")
    s["RentalWrite"] = obj({"order_id": ID, "expected_order_version": VERSION, "device_id": ID,
        "environment_revision": ID, "max_duration_ms": UINT, "confirmed_budget_fen": UINT,
        "disconnect_policy": enum("STOP_AFTER_GRACE", "CONTINUE_WITHIN_CONFIRMED_LIMIT")})
    s["RentalSession"] = obj({"session_id": ID, "order_id": ID, "seller_id": ID, "device_id": ID,
        "version": VERSION, "state": enum("REQUESTED", "RESERVED", "PREPARING", "READY_WAIT_BUYER", "ACTIVE", "STOPPING", "FAULTED", "RECONCILING", "CLEANING", "CLOSED", "QUARANTINED"),
        "environment_revision": ID, "hard_deadline_at": TIME, "started_at": TIME,
        "confirmed_budget_fen": UINT, "verified_available_ms": UINT, "charge_fen": UINT,
        "cleanup_state": enum("NOT_STARTED", "PENDING", "ACKED", "FAILED")},
        ["session_id", "order_id", "seller_id", "device_id", "version", "state", "environment_revision", "hard_deadline_at", "confirmed_budget_fen", "verified_available_ms", "charge_fen", "cleanup_state"])
    s["RentalReadyWrite"] = obj({"expected_version": VERSION, "environment_revision": ID,
        "connection_evidence_ref": ID, "readiness_evidence_ref": ID, "confirmed_budget_fen": UINT},
        description="设备就绪由设备证据提交，买方ready仍独立确认；服务器原子判断双端条件。")
    s["RentalRenewalWrite"] = obj({"expected_version": VERSION, "quote_id": ID, "new_hard_deadline_at": TIME,
        "new_total_budget_fen": UINT, "payment_capability_ref": ID, "confirmed_at": TIME})
    s["UsageIntervalWrite"] = obj({"expected_version": VERSION, "sequence": COUNTER, "starts_at": TIME,
        "ends_at": TIME, "monotonic_duration_ms": UINT, "source": enum("DEVICE", "PLATFORM", "CORROBORATED"),
        "evidence_ref": ID}, description="服务器验证不未来/不重叠/可信性；不自动相信设备单方面自报。")
    s["ExportWrite"] = obj({"expected_version": VERSION, "asset_refs": arr(ID, 50, 1)},
        description="只申请合法现有对象导出；不接受主机文件路径。")
    s["RouteContractWrite"] = obj({"confirmed_order_id": ID, "seller_id": ID, "listing_id": ID, "listing_revision": VERSION,
        "policy": ref("Policy"), "budget_per_call_fen": UINT, "total_budget_fen": UINT,
        "max_call_count": VERSION, "expires_at": TIME, "purchase_authorization_ref": ID,
        "recipient_id": ID, "recipient_key_hash": HASH},
        ["confirmed_order_id", "seller_id", "listing_id", "listing_revision", "policy", "budget_per_call_fen", "total_budget_fen", "max_call_count", "expires_at", "purchase_authorization_ref"],
        description="confirmed_order_id绑定原订单；purchase_authorization_ref引用同单原始payments.id，不是客户端自报支付。R015仅实现PLAINTEXT/STANDARD和三种native文本profile；E2E/租赁拒绝，不自动降级。配置不授予执行权。",
        allOf=[{"if": {"properties": {"policy": {"properties": {"service_mode": {"const": "E2E"}}, "required": ["service_mode"]}}, "required": ["policy"]},
                "then": {"required": ["recipient_id", "recipient_key_hash"]}}])
    s["RouteContract"] = obj({"route_id": ID, "buyer_id": ID, "version": VERSION,
        "definition": ref("RouteContractWrite"), "state": enum("ACTIVE", "SUSPENDED", "EXPIRED", "REVOKED"),
        "remaining_budget_fen": UINT, "used_calls": UINT, "contract_hash": HASH,
        "public_model": ID, "protocol_profile_revision": TEXT, "order_contract": {"type": "object"}},
        ["route_id", "buyer_id", "version", "definition", "state", "remaining_budget_fen", "used_calls", "contract_hash"],
        description="R015已实现响应额外给出冻结订单快照、模型及profile修订；历史设计示例不构成实际支付/执行资格。used_calls当前为0，调用签发和调用次数约束尚未接通。")
    s["ApiKeyWrite"] = obj({"label": TEXT, "scopes": arr(enum("ROUTE_INVOKE", "ORDER_READ", "ASSET_READ", "MARKET_READ"), 4, 1),
        "route_ids": arr(ID, 50), "expires_at": TIME}, ["label", "scopes", "expires_at"])
    s["ApiKey"] = obj({"key_id": ID, "fingerprint": HASH, "label": TEXT, "scopes": arr(TEXT, 20),
        "route_ids": arr(ID, 50), "expires_at": TIME, "state": enum("ACTIVE", "REVOKED", "EXPIRED")})
    s["ApiKeyCreated"] = obj({"key": ref("ApiKey"), "secret_once": {"type": "string", "minLength": 32, "maxLength": 4096, "readOnly": True}})
    s["RefundWrite"] = obj({"expected_order_version": VERSION, "reason_code": ID, "requested_fen": UINT,
        "evidence_refs": arr(ID, 50)}, ["expected_order_version", "reason_code", "requested_fen"])
    s["Refund"] = obj({"refund_id": ID, "order_id": ID, "payment_id": ID, "version": VERSION,
        "state": enum("REQUESTED", "PROCESSING", "SUCCEEDED", "FAILED", "UNKNOWN"), "requested_fen": UINT,
        "confirmed_fen": UINT, "updated_at": TIME})
    s["Settlement"] = obj({"settlement_id": ID, "seller_id": ID, "version": VERSION,
        "state": enum("PENDING_EVIDENCE", "DISPUTED", "CHANNEL_ELIGIBLE", "SUBMITTED", "SETTLED", "UNKNOWN", "REVERSED", "FAILED"),
        "ledger_refs": arr(ID, 100, 1), "gross_trade_fen": UINT, "commission_fen": UINT, "channel_cost_fen": UINT,
        "seller_net_fen": UINT, "channel_reference": ID, "updated_at": TIME},
        ["settlement_id", "seller_id", "version", "state", "ledger_refs", "gross_trade_fen", "commission_fen", "channel_cost_fen", "seller_net_fen", "updated_at"])
    s["ReviewWrite"] = obj({"rating": {"type": "integer", "minimum": 1, "maximum": 5},
        "comment": {"type": "string", "maxLength": 2000}, "visibility_consent_version": ID})
    s["DisputeWrite"] = obj({"order_id": ID, "reason_code": ID, "statement": {"type": "string", "minLength": 1, "maxLength": 4000},
        "selective_evidence_refs": arr(ID, 20), "requested_resolution": enum("REFUND", "CORRECTION", "EXPLANATION", "OTHER")})
    s["Dispute"] = obj({"dispute_id": ID, "order_id": ID, "version": VERSION,
        "state": enum("OPEN", "WAITING_PARTIES", "REVIEWING", "DECIDED", "APPEALED", "CLOSED"),
        "reason_code": ID, "decision_code": ID, "evidence_refs": arr(ID, 50), "updated_at": TIME},
        ["dispute_id", "order_id", "version", "state", "reason_code", "evidence_refs", "updated_at"])
    s["AppealWrite"] = obj({"expected_version": VERSION, "reason_code": ID,
        "statement": {"type": "string", "minLength": 1, "maxLength": 4000}, "new_evidence_refs": arr(ID, 20)})
    s["LeaseClaimWrite"] = obj({"device_id": ID, "capability_id": ID, "claim_key": {**ID, "minLength":8, "description":"先持久保存的原领取命令ID；必须与Idempotency-Key完全一致；丢回执复用原值，不作lease ID或新轮询键。"}, "expected_device_version": {**VERSION,"maximum":2147483646}})
    s["Lease"] = obj({"lease_id": ID, "job_id": ID, "attempt_id": ID, "order_id": ID, "seller_id": ID, "device_id": ID,
        "resource_id": ID, "resource_lock_generation": COUNTER,
        "capability_id": ID, "execution_manifest_ref": ID, "execution_manifest_hash": HASH,
        "kernel_environment_digest": HASH,
        "lease_revision": VERSION, "issued_at": TIME, "not_before": TIME, "hard_deadline": TIME,
        "fencing_token": COUNTER, "expires_at": TIME, "contract_hash": HASH, "input_asset_refs": arr(ID, 50),
        "envelope_ref": ID, "signature_profile_id": ID, "signature": SIG},
        ["lease_id", "job_id", "attempt_id", "order_id", "seller_id", "device_id", "resource_id", "resource_lock_generation",
         "capability_id", "execution_manifest_ref", "execution_manifest_hash", "kernel_environment_digest", "lease_revision", "issued_at", "not_before", "hard_deadline",
         "fencing_token", "expires_at", "contract_hash", "signature_profile_id", "signature"])
    s["ProgressAck"] = obj({"ack": ref("Ack"), "action": enum("CONTINUE", "STOP", "NO_CHANGE"), "lease": ref("Lease")},
        ["ack", "action"], allOf=[
            {"if": {"properties": {"action": {"const": "CONTINUE"}}, "required": ["action"]},
             "then": {"required": ["lease"]}, "else": {"not": {"required": ["lease"]}}}],
        description="CONTINUE必须携同lease_id/attempt/token/resource代次的较新签名lease_revision；客户端核签/硬截止后才续本地有效权。普通Ack或网络成功不延长执行。STOP撤权；NO_CHANGE不延长。")
    s["ProgressWrite"] = obj({"lease_id": ID, "fencing_token": COUNTER, "sequence": COUNTER,
        "phase": enum("ACKED", "RUNNING", "UPLOADING", "FAILED", "CANCELED"),
        "fraction": {"type": "number", "minimum": 0, "maximum": 1}, "safe_reason_code": ID},
        ["lease_id", "fencing_token", "sequence", "phase"])
    s["DeliveryWrite"] = obj({"lease_id": ID, "fencing_token": COUNTER, "delivery_revision": VERSION,
        "asset_refs": arr(ID, 50, 1), "manifest_hash": HASH, "engine_result_ref": ID})
    s["TokenUsageWrite"] = obj({"lease_id": ID, "fencing_token": COUNTER, "sequence": COUNTER,
        "model_digest": HASH, "tokenizer_profile_id": ID, "input_tokens": UINT, "output_tokens": UINT,
        "cached_tokens": UINT, "reasoning_tokens": UINT, "source_evidence_ref": ID, "is_final": BOOL},
        description="这是待核验用量证据，不因设备提交即变成可收费账。重复计数/估计不得收费。")
    s["CleanupWrite"] = obj({"lease_id": ID, "fencing_token": COUNTER, "cleanup_manifest_hash": HASH,
        "state": enum("ACKED", "FAILED"), "work_stopped": BOOL, "tokens_revoked": BOOL,
        "workspace_cleared": BOOL, "recorded_at": TIME, "signature_profile_id": ID, "signature": SIG})
    s["EvidenceWrite"] = obj({"kind": s["EvidenceRef"]["properties"]["kind"], "scope_ref": ID, "asset_ref": ID,
        "digest": HASH, "valid_until": TIME, "review_decision": enum("APPROVED", "REJECTED", "NEEDS_REVIEW"),
        "review_reason_code": ID, "is_mock": BOOL})
    s["ReleaseWrite"] = obj({"seller_id": ID, "device_ids": arr(ID, 50, 1), "listing_id": ID,
        "policy": ref("Policy"), "evidence_refs": arr(ID, 50, 1), "release_manifest_hash": HASH,
        "expected_listing_revision": VERSION})
    s["FeatureGateWrite"] = obj({"gate_id": ID, "scope_ref": ID, "enabled": BOOL, "expected_version": VERSION,
        "reason_code": ID, "approved_release_ref": ID},
        ["gate_id", "scope_ref", "enabled", "expected_version", "reason_code"],
        allOf=[{"if": {"properties": {"enabled": {"const": True}}, "required": ["enabled"]}, "then": {"required": ["approved_release_ref"]}}])
    s["SettlementBatchWrite"] = obj({"settlement_ids": arr(ID, 100, 1), "eligibility_evidence_ref": ID,
        "reconciliation_run_ref": ID}, description="金额/收款对象从合格权威账读取，客户端不能提交任意账户金额。")
    s["BatchActionWrite"] = obj({"expected_version": VERSION, "review_evidence_ref": ID})
    s["SettlementBatch"] = obj({"batch_id": ID, "version": VERSION, "state": enum("DRAFT", "REVIEWED", "SUBMITTED", "UNKNOWN", "RECONCILED", "FAILED"),
        "settlement_ids": arr(ID, 100, 1), "created_by": ID, "reviewed_by": ID, "channel_reference": ID},
        ["batch_id", "version", "state", "settlement_ids", "created_by"])
    s["AuditEvent"] = obj({"audit_id": ID, "actor_id": ID, "actor_role": TEXT, "action": ID,
        "resource_ref": ID, "decision": enum("ALLOW", "DENY", "CHANGE", "REVIEW"), "reason_code": ID,
        "evidence_refs": arr(ID, 50), "recorded_at": TIME, "request_id": ID})
    s["SelfProfile"] = obj({"user_id": ID, "roles": arr(enum("BUYER", "SELLER", "SUPPORT", "OPERATOR", "FINANCE", "AUDITOR"), 6, 1),
        "permission_scope_ids": arr(ID, 100), "identity_state": enum("UNVERIFIED", "VERIFIED", "SUSPENDED"), "version": VERSION})
    s["ConsentWrite"] = obj({"document_version": ID, "purpose": enum("TRADE", "PRIVACY", "LAN", "SELECTIVE_DISCLOSURE"),
        "scope_ref": ID, "accepted": BOOL, "confirmed_at": TIME})
    s["ResourcePolicyWrite"] = obj({"expected_version": VERSION, "windows": arr(ref("ScheduleWindow"), 100),
        "sharing_enabled": BOOL, "active_slots": {"const": 1}, "minimum_free_disk_bytes": UINT,
        "minimum_battery_percent": {"type": "integer", "minimum": 0, "maximum": 100},
        "maximum_temperature_celsius": {"type": "number", "minimum": 1, "maximum": 120},
        "charging_required": BOOL, "stop_policy": enum("DRAIN", "FORCE_WITH_RESPONSIBILITY")},
        ["expected_version", "windows", "sharing_enabled", "active_slots", "minimum_free_disk_bytes", "stop_policy"])
    s["CatalogEntry"] = obj({"entry_id": ID, "kind": enum("MODEL", "WORKFLOW", "PROTOCOL_PROFILE", "KERNEL_ENVIRONMENT", "NODE_ALLOWLIST"),
        "revision": VERSION, "name": TEXT, "digest": HASH, "allowed_policy_refs": arr(ID, 20),
        "evidence_refs": arr(ref("EvidenceRef")), "state": enum("DRAFT", "QUALIFIED", "PUBLISHED", "REVOKED")})
    s["Notification"] = obj({"notification_id": ID, "category": enum("ORDER", "RESULT", "FINANCE", "DISPUTE", "DEVICE", "SECURITY"),
        "resource_ref": ID, "safe_message_code": ID, "recorded_at": TIME, "read": BOOL})
    s["Bill"] = obj({"order_id": ID, "version": VERSION, "paid_fen": UINT, "retained_gmv_fen": UINT,
        "commission_fen": UINT, "buyer_fee_fen": UINT, "channel_fee_fen": UINT, "seller_net_fen": UINT,
        "refunded_fen": UINT, "compensation_refs": arr(ID, 50), "ledger_refs": arr(ID, 100),
        "calculation_evidence_refs": arr(ID, 50), "currency": {"const": "CNY"}})
    s["DeletionWrite"] = obj({"scope_ref": ID, "data_categories": arr(enum("INPUT", "RESULT", "CONTENT_DERIVATIVE", "LOCAL_KEY_REFERENCE"), 4, 1),
        "reason_code": ID, "confirmed_at": TIME}, description="可保留合法必要账证；本地私钥不由平台删除/恢复。")
    s["RentalDeviceReadyWrite"] = obj({"expected_version": VERSION, "environment_revision": ID,
        "resource_lock_generation": COUNTER, "connection_evidence_ref": ID, "readiness_evidence_ref": ID})
    s["RentalCleanupWrite"] = obj({"expected_version": VERSION, "resource_lock_generation": COUNTER,
        "work_stopped": {"const": True}, "tokens_revoked": {"const": True}, "workspace_cleared": {"const": True},
        "cleanup_manifest_hash": HASH, "signature_profile_id": ID, "signature": SIG},
        description="这是待核验清理证据；服务端还需当前锁代次和残留验证。失败用安全progress故障报告，不谎报ACK。")
    s["DisputeDecisionWrite"] = obj({"expected_version": VERSION, "decision_code": ID,
        "responsibility": enum("BUYER", "SELLER", "PLATFORM", "CHANNEL", "UNDETERMINED"),
        "evidence_refs": arr(ID, 50, 1), "refund_proposal_ref": ID, "compensation_policy_ref": ID},
        ["expected_version", "decision_code", "responsibility", "evidence_refs"])
    s["AssetGrantWrite"] = obj({"expected_version": VERSION, "purpose": enum("UPLOAD", "DOWNLOAD", "RESUME"),
        "requested_range_start": UINT, "requested_range_end": UINT}, ["expected_version", "purpose"])
    s["AssetGrant"] = obj({"grant_id": ID, "asset_id": ID, "purpose": enum("UPLOAD", "DOWNLOAD", "RESUME"),
        "access_uri": {"type": "string", "format": "uri", "pattern": "^https://", "maxLength": 4096},
        "expires_at": TIME, "cipher_only": BOOL, "allowed_range_start": UINT, "allowed_range_end": UINT},
        ["grant_id", "asset_id", "purpose", "access_uri", "expires_at", "cipher_only"],
        description="本人单对象短时授权，URL不得进入日志/通知；E2E平台只发密文对象，解密端在买方可信域。")
    s["RentalAccessWrite"] = obj({"expected_version": VERSION, "scopes": arr(enum("INVOKE", "QUEUE_SELF", "CANCEL_SELF", "EXPORT_SELF"), 4, 1)})
    s["RentalAccess"] = obj({"grant_id": ID, "session_id": ID, "scopes": arr(TEXT, 4, 1),
        "expires_at": TIME, "connection_ref": ID, "kernel_environment_digest": HASH,
        "credential_once": {"type": "string", "minLength": 32, "maxLength": 4096, "readOnly": True}},
        description="有效期不超已确认租期；停工即撤权；仅本会话受控应用，无主机/shell权限。技术v1.0将credential_once规划为成熟JWS库签发的受限会话授权，签名不加密；claims绑定实际seller/device/resource代次、terms_revision/hash、kernel_environment_digest、scope/硬截止和aud。具体profile/库待原生PoC冻结，Bearer仅首次展示，服务保存摘要。")
    s["MeterStatement"] = obj({"session_id": ID, "revision": VERSION, "verified_billable_ms": UINT,
        "confirmed_budget_fen": UINT, "charge_fen": UINT, "paid_fen": UINT, "refund_fen": UINT,
        "intervals": arr(obj({"sequence": COUNTER, "starts_at": TIME, "ends_at": TIME, "verified_ms": UINT, "evidence_ref": ID}), 100),
        "interval_cursor": {"type": ["string", "null"], "maxLength": 512},
        "price_segment_refs": arr(ID, 100, 1), "calculation_evidence_ref": ID},
        description="区间分页不改变整会话累计计费；同价续租只累计取整一次，修订追加冲正。")
    s["RecipientWrite"] = obj({"role": enum("BUYER_RETURN", "SELLER_EXECUTION"), "key_profile_id": ID,
        "public_key_b64u": {"type": "string", "minLength": 32, "maxLength": 8192, "pattern": "^[A-Za-z0-9_-]+$"},
        "public_key_hash": HASH, "gateway_identity_ref": ID, "expires_at": TIME, "identity_evidence_ref": ID})
    s["Recipient"] = obj({"recipient_id": ID, "owner_ref": ID, "role": enum("BUYER_RETURN", "SELLER_EXECUTION"),
        "version": VERSION, "key_id": ID, "key_profile_id": ID, "public_key_b64u": {"type": "string", "maxLength": 8192},
        "public_key_hash": HASH, "state": enum("ACTIVE", "EXPIRED", "REVOKED"), "expires_at": TIME, "trust_evidence_refs": arr(ID, 50)},
        description="仅公钥目录；平台声明不代替独立端点信任/增强证明，平台无内容私钥。")
    s["RecipientRotationWrite"] = obj({"expected_version": VERSION, "new_key": ref("RecipientWrite"),
        "rotation_evidence_ref": ID, "old_key_signature": SIG, "reidentity_evidence_ref": ID},
        ["expected_version", "new_key", "rotation_evidence_ref"],
        oneOf=[{"required": ["old_key_signature"], "not": {"required": ["reidentity_evidence_ref"]}},
               {"required": ["reidentity_evidence_ref"], "not": {"required": ["old_key_signature"]}}],
        description="旧钥签新钥或明确重新身份核验；买方仍需独立认可轮换，不静默改在途收件端。")
    # R004: authentication is a distinct, single-use control flow, not an API key.
    s["SelfProfile"]["properties"]["roles"]["minItems"] = 0
    s["AuthChallengeWrite"] = {"oneOf": [
        obj({"client_type": enum("WEB", "MINIAPP"), "code_challenge": {"type": "string", "pattern": "^[A-Za-z0-9_-]{43}$"}, "proof_profile": {"const": ""}}, ["client_type", "code_challenge"]),
        obj({"client_type": {"const": "MINIAPP"}, "proof_profile": {"const": "ALIPAY_NATIVE_NONCE_1"}, "code_challenge": {"const": ""}}, ["client_type", "proof_profile"])],
        "description": "WEB及原有MINIAPP使用客户端PKCE；ALIPAY_NATIVE_NONCE_1仅原生MINIAPP显式选择服务端一次性证明，不冒称客户端PKCE。"}
    s["AuthChallenge"] = obj({"challenge_id": ID, "client_type": enum("WEB", "MINIAPP"),
        "expires_at": TIME, "authorization_url": {"type": "string", "format": "uri", "maxLength": 2048},
        "proof_profile": {"const": "ALIPAY_NATIVE_NONCE_1"},
        "native_code_verifier": {"type": "string", "pattern": "^[A-Za-z0-9_-]{43}$", "readOnly": True}},
        ["challenge_id", "client_type", "expires_at"], allOf=[
            {"if": {"required": ["proof_profile"]}, "then": {"properties": {"client_type": {"const": "MINIAPP"}}, "required": ["native_code_verifier"], "not": {"required": ["authorization_url"]}}},
            {"if": {"required": ["native_code_verifier"]}, "then": {"required": ["proof_profile"]}}])
    s["SessionWrite"] = obj({"challenge_id": ID,
        "auth_code": {"type": "string", "minLength": 1, "maxLength": 512, "writeOnly": True},
        "code_verifier": {"type": "string", "pattern": "^[A-Za-z0-9._~-]{43,128}$", "writeOnly": True},
        "web_state": ID}, ["challenge_id", "auth_code", "code_verifier"])
    s["SessionView"] = obj({"session_id": ID, "client_type": enum("WEB", "MINIAPP"),
        "expires_at": TIME, "profile": ref("SelfProfile"),
        "csrf_token": {"type": "string", "pattern": "^[a-f0-9]{64}$", "readOnly": True},
        "access_token": {"type": "string", "minLength": 32, "maxLength": 256, "readOnly": True}},
        ["session_id", "client_type", "expires_at", "profile"],
        allOf=[{"if": {"properties": {"client_type": {"const": "WEB"}}},
                "then": {"required": ["csrf_token"], "not": {"required": ["access_token"]}}}],
        description="WEB只通过HttpOnly Cookie收会话；MINIAPP仅首次签发返回access_token。GET不重显原值，OAuth令牌从不返回。")
    return s


def make_openapi() -> dict:
    schemas = make_schemas()
    error_codes = {400: "输入/帧结构错误", 401: "未认证或凭据已撤销", 403: "权限/隐私/资格/证据不满足",
        404: "资源不存在或不可见", 409: "状态/幂等/资金或执行未知冲突", 410: "报价或对象已过期",
        413: "输入过大", 415: "媒体类型不支持", 422: "能力或字段不支持", 429: "速率/容量受限",
        502: "后端/加密流不完整", 503: "能力不可用", 504: "已超合同期限"}
    responses = {str(code): {"description": text, "content": {"application/json": {"schema": ref("SafeError")}}}
                 for code, text in error_codes.items()}
    paths: dict = {}

    def add(method: str, path: str, operation: str, tag: str, summary: str, response: str,
            request: str | None = None, scopes: tuple = (), *, list_of: bool = False,
            security: str = "BearerAuth", versioned: bool = False, statuses: tuple = (), description: str = ""):
        import re
        parameters = [{"name": name, "in": "path", "required": True, "schema": ID}
                      for name in re.findall(r"\{([^}]+)\}", path)]
        auth_single_use = operation in ("createAuthChallenge", "createUserSession", "revokeUserSession")
        if method in ("post", "patch", "delete") and not auth_single_use:
            parameters.append({"name": "Idempotency-Key", "in": "header", "required": True,
                "schema": {"type": "string", "minLength": 8, "maxLength": 128},
                "description": "同主体/操作/资源范围同键同输入复用；不同输入409。未知结果先查询。"})
        if versioned:
            parameters.append({"name": "If-Match", "in": "header", "required": True,
                "schema": {"type": "string", "pattern": '^"[1-9][0-9]*"$'}, "description": "当前资源版本ETag；只改未来修订。"})
        if list_of:
            parameters += [{"name": "cursor", "in": "query", "schema": {"type": "string", "maxLength": 512}},
                {"name": "limit", "in": "query", "schema": {"type": "integer", "minimum": 1, "maximum": 100, "default": 20}}]
            response_name = response + "Page"
            schemas[response_name] = obj({"items": arr(ref(response)), "next_cursor": {"type": ["string", "null"], "maxLength": 512}}, ["items", "next_cursor"])
        else:
            response_name = response
        status = "201" if method == "post" and operation.startswith(("create", "register", "enroll")) else "200"
        ok = {"description": "设计响应；不是运行证据。跨对象归属、状态与资金门槛仍在服务端验证。",
            "headers": {"X-Request-ID": {"schema": ID}, "Date": {"schema": {"type": "string"}}},
            "content": {"application/json": {"schema": ref(response_name)}}}
        codes = set((400, 401, 403, 404, 429, 503, *statuses))
        if method != "get":
            codes.update((409, 422))
        op = {"operationId": operation, "tags": [tag], "summary": summary,
            "description": description or "主体scope与资源所有权同时验证；不把元数据合法等同为已经执行/收费成功。",
            "security": ([] if operation in ("createAuthChallenge", "createUserSession") else
                         [{security: []}] + ([{"BrowserCookie": []}] if security == "BearerAuth" else [])),
            "x-required-scopes": list(scopes),
            "parameters": parameters, "responses": {status: ok, **{str(c): {"$ref": "#/components/responses/" + str(c)} for c in sorted(codes)}}}
        if request:
            op["requestBody"] = {"required": True, "content": {"application/json": {"schema": ref(request)}}}
        if auth_single_use:
            op["x-idempotency-result-policy"] = "SINGLE_USE_AUTH_FLOW"
        if operation in ("createAuthChallenge", "createUserSession"):
            op["x-authentication-flow"] = "CHALLENGE_VERIFIER_AND_VERIFIED_PROVIDER"
        if security == "BearerAuth" and method != "get":
            op["x-browser-csrf"] = "Exact Origin and session-bound X-CSRF-Token for Cookie requests"
        paths.setdefault(path, {})[method] = op

    # 自有scope以x-required-scopes说明，HTTP Bearer security数组不滥用OAuth scope语义。
    add("get", "/me", "getSelfProfile", "Identity", "读取本人角色与安全权限摘要", "SelfProfile", scopes=("identity:read",))
    add("post", "/me/consents", "recordConsent", "Identity", "记录版本化明确同意", "Ack", "ConsentWrite", ("identity:consent",))
    add("post", "/me/deletion-requests", "createDeletionRequest", "Identity", "申请内容删除及跟踪合法保留", "Ack", "DeletionWrite", ("identity:delete",))
    add("post", "/seller/applications", "createSellerApplication", "Seller", "提交个人卖家资格", "SellerProfile", "SellerApplicationWrite", ("seller:apply",))
    add("get", "/seller/profile", "getSellerProfile", "Seller", "读取本人卖家档案", "SellerProfile", scopes=("seller:read",))
    add("post", "/seller/exit", "requestSellerExit", "Seller", "停新售并清理在途和合法尾款", "SellerProfile", "ReasonWrite", ("seller:manage",))
    add("get", "/seller/listings", "listOwnListings", "Market", "读取本人挂牌", "Listing", scopes=("seller:read",), list_of=True)
    add("post", "/seller/listings", "createListing", "Market", "创建本人未来挂牌", "Listing", "ListingWrite", ("listing:write",))
    add("get", "/seller/listings/{listing_id}", "getOwnListing", "Market", "读取本人挂牌详情", "Listing", scopes=("seller:read",))
    add("patch", "/seller/listings/{listing_id}", "updateFutureListing", "Market", "修改未来挂牌修订", "Listing", "ListingPatch", ("listing:write",), versioned=True)
    add("post", "/seller/listings/{listing_id}/submit", "submitListing", "Market", "提交挂牌审查", "Listing", "ReasonWrite", ("listing:write",))
    add("post", "/seller/listings/{listing_id}/pause", "pauseListing", "Market", "停止该挂牌新售", "Listing", "ReasonWrite", ("listing:write",))
    add("get", "/seller/devices", "listOwnDevices", "Device", "读取本人设备", "Device", scopes=("seller:read",), list_of=True)
    add("post", "/devices/pairing-intents", "createPairingIntent", "Device", "建立一次性设备配对", "DevicePairing", "DevicePairingWrite", ("device:pair",))
    add("post", "/device-enrollments", "enrollDevice", "Device", "消耗一次性配对凭据登记设备", "Device", "EnrollmentWrite", ("pairing:consume",), security="PairingToken")
    add("get", "/devices/{device_id}", "getDevice", "Device", "读取本人或本设备状态", "Device", scopes=("device:read",))
    paths["/devices/{device_id}"]["get"]["security"].append({"DeviceProof":[]})
    add("post", "/devices/{device_id}/capabilities", "registerCapability", "Device", "提交本设备待审能力", "Ack", "CapabilityWrite", ("device:capability",), security="DeviceProof")
    add("post", "/devices/{device_id}/heartbeats", "recordDeviceHeartbeat", "Device", "记录当前身份心跳", "Ack", "HeartbeatWrite", ("device:heartbeat",), security="DeviceProof")
    add("post", "/devices/{device_id}/drain", "drainDevice", "Device", "停止设备新任务准入", "Device", "ReasonWrite", ("device:manage",))
    add("post", "/devices/{device_id}/revoke", "revokeDevice", "Device", "撤销设备执行身份", "Device", "ReasonWrite", ("device:manage",))
    add("patch", "/devices/{device_id}/resource-policy", "updateResourcePolicy", "Device", "设置资源/时段及停止策略", "Ack", "ResourcePolicyWrite", ("device:manage",), versioned=True)
    add("get", "/services", "listServices", "Market", "查询已合格实际卖家供给", "Service", scopes=("market:read",), list_of=True)
    add("get", "/catalog", "listCatalogEntries", "Market", "查询已审核模型/工作流/协议档位", "CatalogEntry", scopes=("market:read",), list_of=True)
    add("get", "/notifications", "listNotifications", "Identity", "读取本人安全事件通知", "Notification", scopes=("identity:read",), list_of=True)
    add("post", "/quotes", "createQuote", "Trade", "生成实际卖家/模式不可变报价", "Quote", "QuoteWrite", ("quote:create",), statuses=(410,))
    add("get", "/quotes/{quote_id}", "getQuote", "Trade", "查询本人报价", "Quote", scopes=("quote:read",), statuses=(410,))
    add("get", "/orders", "listOrders", "Trade", "按本人买卖角色查询订单", "Order", scopes=("order:read",), list_of=True)
    add("post", "/orders", "createOrder", "Trade", "确认披露并消耗报价建业务单", "Order", "OrderWrite", ("order:create",), statuses=(410,))
    add("get", "/orders/{order_id}", "getOrder", "Trade", "查询本人不可变合同与独立状态", "Order", scopes=("order:read",))
    add("get", "/orders/{order_id}/bill", "getOrderBill", "Finance", "查询本人可重算分账账单", "Bill", scopes=("order:read",))
    add("post", "/orders/{order_id}/payment-intents", "createPaymentIntent", "Payment", "按获准个人渠道创建支付意向", "Payment", "PaymentIntentWrite", ("payment:create",))
    add("get", "/payments/{payment_id}", "getPayment", "Payment", "查询原支付而非重复创建", "Payment", scopes=("payment:read",))
    add("post", "/assets", "registerAsset", "Asset", "登记有界单对象传输", "Asset", "AssetWrite", ("asset:write",), statuses=(413, 415))
    add("get", "/assets/{asset_id}", "getAsset", "Asset", "读取本人对象元数据和合法授权引用", "Asset", scopes=("asset:read",), statuses=(410,))
    add("post", "/assets/{asset_id}/complete", "completeAsset", "Asset", "完成并核验上传manifest", "Asset", "AssetCompleteWrite", ("asset:write",), statuses=(413, 415))
    add("post", "/assets/{asset_id}/transfer-grants", "createAssetTransferGrant", "Asset", "签发本人单对象有界传输授权", "AssetGrant", "AssetGrantWrite", ("asset:transfer",), statuses=(410,))
    add("post", "/jobs", "createJob", "Execution", "绑定本人订单和输入申请执行", "Job", "JobWrite", ("job:create",))
    add("get", "/jobs/{job_id}", "getJob", "Execution", "查询当前任务状态", "Job", scopes=("job:read",))
    add("post", "/jobs/{job_id}/cancel", "cancelJob", "Execution", "幂等取消并与接受状态竞争", "Job", "ReasonWrite", ("job:cancel",))
    add("get", "/jobs/{job_id}/results", "listResults", "Asset", "读取有领取权益的正式结果", "Result", scopes=("asset:read",), list_of=True, statuses=(410,))
    add("post", "/encryption-intents", "createEncryptionIntent", "Cipher", "绑定买方信任的接收端", "EncryptionIntent", "EncryptionIntentWrite", ("cipher:intent",))
    add("post", "/recipients", "registerRecipient", "Cipher", "登记本人网关内容接收公钥", "Recipient", "RecipientWrite", ("recipient:manage",))
    add("get", "/recipients/{recipient_id}", "getRecipient", "Cipher", "按合法意向范围读取端点公钥与信任材料", "Recipient", scopes=("recipient:read",))
    add("post", "/recipients/{recipient_id}/rotations", "createRecipientRotation", "Cipher", "记录有证据公钥轮换供买方重新验证", "Recipient", "RecipientRotationWrite", ("recipient:manage",))
    add("post", "/recipients/{recipient_id}/revoke", "revokeRecipient", "Cipher", "撤销本人接收端并停止新放钥", "Recipient", "ReasonWrite", ("recipient:manage",))
    add("post", "/recipients/{recipient_id}/attestation-challenges", "createAttestationChallenge", "Cipher", "生成有界证明挑战", "AttestationChallenge", "AttestationChallengeWrite", ("attestation:request",))
    add("get", "/recipients/{recipient_id}/evidence", "getAttestationEvidence", "Cipher", "读取与挑战绑定的原始证明引用", "AttestationEvidence", scopes=("attestation:read",))
    paths["/recipients/{recipient_id}/evidence"]["get"]["parameters"].append({"name": "challenge_id", "in": "query", "required": True, "schema": ID})
    add("post", "/jobs/{job_id}/envelopes", "registerEnvelope", "Cipher", "当前attempt绑定密钥封装引用", "Ack", "EnvelopeWrite", ("cipher:envelope",))
    add("post", "/jobs/{job_id}/receipts", "recordBuyerReceipt", "Cipher", "验证买方接受/拒绝回执", "Ack", "ReceiptWrite", ("cipher:receipt",))
    add("post", "/cipher/channels", "createCipherChannel", "Cipher", "签发限scope密文通道引用", "CipherChannel", "CipherChannelWrite", ("cipher:connect",))
    add("post", "/rental-sessions", "createRentalSession", "Rental", "按指定设备/合同申请租赁", "RentalSession", "RentalWrite", ("rental:create",))
    add("get", "/rental-sessions/{session_id}", "getRentalSession", "Rental", "查询本人受控租赁", "RentalSession", scopes=("rental:read",))
    add("post", "/rental-sessions/{session_id}/ready", "confirmRentalReady", "Rental", "买方确认就绪和预算后起表", "RentalSession", "RentalReadyWrite", ("rental:ready",))
    add("post", "/rental-sessions/{session_id}/device-ready", "recordRentalDeviceReady", "Rental", "设备提交当前锁代次的准备证据", "RentalSession", "RentalDeviceReadyWrite", ("device:rental",))
    add("post", "/rental-sessions/{session_id}/renewals", "createRentalRenewal", "Rental", "明确确认新预算/期限续租", "RentalSession", "RentalRenewalWrite", ("rental:renew",))
    add("post", "/rental-sessions/{session_id}/stop", "stopRentalSession", "Rental", "停止起表并撤会话工作权", "RentalSession", "ReasonWrite", ("rental:stop",))
    add("post", "/rental-sessions/{session_id}/availability", "recordRentalAvailability", "Rental", "记录待核实可用区间", "Ack", "UsageIntervalWrite", ("device:rental",))
    add("post", "/rental-sessions/{session_id}/exports", "createRentalExport", "Rental", "导出本会话合法产物", "Ack", "ExportWrite", ("rental:export",))
    add("post", "/rental-sessions/{session_id}/cleanup", "recordRentalCleanup", "Rental", "设备提交停工撤权清理证据后待核验解锁", "RentalSession", "RentalCleanupWrite", ("device:rental",))
    add("post", "/rental-sessions/{session_id}/access-grants", "createRentalAccessGrant", "Rental", "签发不超已确认期限的会话受控权限", "RentalAccess", "RentalAccessWrite", ("rental:access",))
    add("get", "/rental-sessions/{session_id}/statement", "getRentalMeterStatement", "Rental", "查询整会话计时与金额证据", "MeterStatement", scopes=("rental:read",))
    add("get", "/route-contracts", "listRouteContracts", "Gateway", "读取本人已确认调用路由", "RouteContract", scopes=("route:read",), list_of=True)
    add("post", "/route-contracts", "createRouteContract", "Gateway", "冻结实际卖家/模式与自动调用授权", "RouteContract", "RouteContractWrite", ("route:create",))
    add("get", "/route-contracts/{route_id}", "getRouteContract", "Gateway", "查询路由授权及剩余预算", "RouteContract", scopes=("route:read",))
    add("get", "/api-keys", "listApiKeys", "Identity", "只读key指纹及scope", "ApiKey", scopes=("key:manage",), list_of=True)
    add("post", "/api-keys", "createApiKey", "Identity", "创建有限授权key且只首次显示secret", "ApiKeyCreated", "ApiKeyWrite", ("key:manage",))
    paths["/api-keys"]["post"]["x-idempotency-result-policy"] = "SECRET_ONCE_WITH_REFERENCE"
    paths["/api-keys"]["post"]["description"] = "仅首次成功响应包含secret_once；同主体同键同输入不再新建或重显密钥，返回409 SECRET_ALREADY_ISSUED并以details.resource_id引用原key。响应丢失时先列出/撤销原key，明确新意图后再创建；数据库和幂等safe_response均不保存原文secret。"
    add("delete", "/api-keys/{key_id}", "revokeApiKey", "Identity", "撤销本人key", "Ack", scopes=("key:manage",))
    add("post", "/orders/{order_id}/refunds", "createRefund", "AfterSales", "申请原单合同内退款", "Refund", "RefundWrite", ("refund:create",))
    add("get", "/refunds/{refund_id}", "getRefund", "AfterSales", "查询原退款请求状态", "Refund", scopes=("refund:read",))
    add("get", "/seller/settlements", "listOwnSettlements", "Finance", "查询本人应结/未知/到账", "Settlement", scopes=("seller:settlement_read",), list_of=True)
    add("get", "/settlements/{settlement_id}", "getSettlement", "Finance", "查询本人结算与原渠道状态", "Settlement", scopes=("seller:settlement_read",))
    add("post", "/orders/{order_id}/reviews", "createReview", "AfterSales", "对有资格真实订单评价", "Ack", "ReviewWrite", ("review:create",))
    add("post", "/disputes", "createDispute", "AfterSales", "创建本人订单争议及选择性举证", "Dispute", "DisputeWrite", ("dispute:create",))
    add("get", "/disputes/{dispute_id}", "getDispute", "AfterSales", "限权读取本人争议进度", "Dispute", scopes=("dispute:read",))
    add("post", "/disputes/{dispute_id}/appeals", "createAppeal", "AfterSales", "按资格申请争议复核", "Dispute", "AppealWrite", ("dispute:appeal",))
    add("post", "/runner/leases/claim", "claimLease", "Runner", "本设备领取当前签名执行租约", "Lease", "LeaseClaimWrite", ("device:execute",))
    paths["/runner/leases/claim"]["post"]["description"] = "仅原生ED25519_DEVICE_1签名证明，不接受本人会话或SDK key代领。claim_key与Idempotency-Key相同；同原键固定原租约，撤权/过期/owner变化后拒绝交付。领取只记录投递意图，不等于设备ACK、模型执行或计费。"
    paths["/runner/leases/claim"]["post"]["security"] = [{"BearerAuth": []}]
    paths["/runner/leases/claim"]["post"].pop("x-browser-csrf", None)
    for suffix, operation, request in [("progress", "recordAttemptProgress", "ProgressWrite"),
        ("deliveries", "registerAttemptDelivery", "DeliveryWrite"), ("usage", "recordAttemptUsage", "TokenUsageWrite"),
        ("cleanup", "recordAttemptCleanup", "CleanupWrite")]:
        add("post", "/runner/attempts/{attempt_id}/" + suffix, operation, "Runner", "提交当前attempt的" + suffix + "证据", "ProgressAck" if suffix == "progress" else "Ack", request, ("device:execute",))
    add("post", "/admin/evidence", "registerReviewedEvidence", "Admin", "提交限权审核证据与决定", "EvidenceRef", "EvidenceWrite", ("admin:evidence",))
    add("post", "/admin/releases", "createCapabilityRelease", "Admin", "证据齐全才发布具体组合", "Ack", "ReleaseWrite", ("admin:release",))
    add("post", "/admin/disputes/{dispute_id}/decisions", "recordDisputeDecision", "AfterSales", "分权裁定争议并引用退款/责任规则", "Dispute", "DisputeDecisionWrite", ("admin:dispute",))
    add("patch", "/admin/feature-gates", "updateFeatureGate", "Admin", "按scope停新售或引用批准版本开放", "Ack", "FeatureGateWrite", ("admin:gate",))
    add("post", "/admin/settlement-batches", "createSettlementBatch", "Finance", "仅从合格账创建结算批次", "SettlementBatch", "SettlementBatchWrite", ("finance:prepare",))
    add("get", "/admin/settlement-batches/{batch_id}", "getSettlementBatch", "Finance", "查询原结算批次及未知状态", "SettlementBatch", scopes=("finance:read",))
    add("post", "/admin/settlement-batches/{batch_id}/approve", "approveSettlementBatch", "Finance", "独立人员复核结算批次", "SettlementBatch", "BatchActionWrite", ("finance:review",))
    add("post", "/admin/settlement-batches/{batch_id}/submit", "submitSettlementBatch", "Finance", "提交已复核原批次且未知先查", "SettlementBatch", "BatchActionWrite", ("finance:submit",))
    add("get", "/admin/audit-events", "listAuditEvents", "Admin", "限权查询无正文审计元数据", "AuditEvent", scopes=("admin:audit",), list_of=True)
    add("post", "/auth/challenges", "createAuthChallenge", "Identity", "创建限时一次性登录绑定", "AuthChallenge", "AuthChallengeWrite")
    add("post", "/auth/sessions", "createUserSession", "Identity", "核验授权码并签发有限会话", "SessionView", "SessionWrite")
    add("get", "/auth/session", "getCurrentSession", "Identity", "读取本人会话及浏览器CSRF值", "SessionView", scopes=("identity:read",))
    add("delete", "/auth/session", "revokeUserSession", "Identity", "撤销当前会话并提升用户版本", "Ack", scopes=("session:manage",))
    paths["/services"]["get"]["parameters"] += [
        {"name": "mode", "in": "query", "schema": MODE},
        {"name": "region", "in": "query", "schema": {"type": "string", "minLength": 1, "maxLength": 128}}]
    for parameter in paths["/services"]["get"]["parameters"]:
        if parameter["name"] == "limit":
            parameter["schema"]["maximum"] = 50
    return {"openapi": "3.1.0", "jsonSchemaDialect": SCHEMA,
        "info": {"title": "P11 C2C算力共享平台自有控制面设计", "version": "3.0.0-draft",
            "description": "文档设计，所有产品验收未执行。这里只定义本项目控制资源，不复制厂商完整模型API、渠道支付回调或引擎协议。服务地址为保留无效域名，不存在部署。数字/结构验证不代替授权、状态、资金、密码或SDK实测。"},
        "servers": [{"url": "https://p11.invalid/v1/p11", "description": "仅设计占位，不可调用"}],
        "tags": [{"name": name} for name in sorted({op["tags"][0] for ops in paths.values() for op in ops.values()})],
        "paths": paths, "components": {"securitySchemes": {
            "DeviceProof": {"type":"http","scheme":"bearer","description":"仅原生P11_DEVICE_ED25519_1逐请求证明，拒绝Cookie/Origin；不是账户会话。"},
            "BearerAuth": {"type": "http", "scheme": "bearer", "description": "平台有限scope凭据；MINIAPP会话/SDK API key/设备严格分离。设备采用P11_DEVICE_ED25519_1逐请求证明：签入method、authority、精确URI、幂等键、If-Match、Content-Type及原正文SHA256；±60秒、持久化nonce防重放。不是内容密钥、支付宝签名或标准HTTP Message Signatures。"},
            "BrowserCookie": {"type": "apiKey", "in": "cookie", "name": "__Host-p11_session",
                "description": "HttpOnly/Secure/SameSite=Strict；浏览器写同时核Origin和CSRF。显式loopback开发使用p11_session_dev。"},
            "PairingToken": {"type": "http", "scheme": "bearer", "description": "5分钟一次性登记凭据，绑定本人pairing_id；消耗后只允许7日内原键/原规范字段恢复原设备，不能重新登记或换身份。"}},
            "schemas": schemas, "responses": responses},
        "x-design-status": "DESIGN_UNIMPLEMENTED", "x-updated-at": DATE}


def make_cipher_schema() -> dict:
    context = obj({"contract_hash": HASH, "order_id": ID, "seller_id": ID, "recipient_id": ID,
        "recipient_key_id": ID, "buyer_key_id": ID, "job_id": ID, "attempt_id": ID,
        "fencing_token": COUNTER, "delivery_revision": VERSION, "stream_id": ID, "object_id": ID})
    return {"$schema": SCHEMA, "$id": "https://p11.invalid/design/cipher-frame-v3.0.schema.json",
        "title": "P11 opaque cipher frame v3.0 draft", "description": "外层有界密文帧，仅说明结构。HEADERS/DATA/USAGE/ERROR/END/CANCEL类型与终态承诺均放密文内部。认证关联数据必须覆盖上下文；hash仅密文摘要，不允许公开prompt明文hash。suite/nonce/握手/签名须另冻结及安全评审。",
        **obj({"frame_version": {"const": "3.0-draft"}, "channel_id": ID, "context": context,
            "direction": enum("BUYER_TO_SELLER", "SELLER_TO_BUYER"), "suite_profile_id": ID,
            "epoch": COUNTER, "sequence": COUNTER, "expires_at": TIME,
            "ciphertext_b64u": {"type": "string", "minLength": 22, "maxLength": 120000, "pattern": "^[A-Za-z0-9_-]+$"},
            "ciphertext_sha256": HASH}),
        "x-limits": {"encoded_frame_max_bytes": 131072, "plaintext_data_chunk_max_bytes": 65536,
            "reorder_max_frames": 32, "reorder_max_bytes": 2097152, "send_buffer_max_bytes": 4194304},
        "x-semantic-checks": ["总编码字节、上下文/有效执行权、AEAD/身份验证独立检查", "seq/epoch不复用nonce；重连仅重传原密文或换安全epoch/key", "终块类型、总数/摘要/完整状态在认证密文内，不以socket关闭或外层flag判成功", "未知字段/重复JSON键拒绝；大计数器按规范十进制字符串"]}


EVENT_TYPES = ["SellerApproved", "CapabilityQualified", "ListingPublished", "ListingPaused", "LicenseRevoked",
    "QuoteConsumed", "PaymentAuthorized", "PaymentSettled", "PaymentUnknown", "RefundConfirmed",
    "AttemptIssued", "AttemptExpired", "DeliveryAvailable", "PlatformAccepted", "BuyerAccepted", "BuyerRejected",
    "RecipientRevoked", "AttestationExpired", "RewrapRequired", "SessionReady", "SessionStarted", "SessionStopped",
    "AvailabilityConfirmed", "CleanupAck", "CleanupFailed", "TradeQualified", "CommissionAdjusted",
    "SellerSettlementEligible", "SellerSettlementUnknown", "DisputeOpened", "DisputeResolved"]


def make_event_schema() -> dict:
    payload = obj({"order_id": ID, "seller_id": ID, "device_id": ID, "listing_id": ID, "capability_id": ID,
        "quote_id": ID, "payment_id": ID, "refund_id": ID, "job_id": ID, "attempt_id": ID, "delivery_id": ID,
        "session_id": ID, "settlement_id": ID, "dispute_id": ID, "recipient_id": ID,
        "fencing_token": COUNTER, "safe_reason_code": ID, "manifest_hash": HASH,
        "amount_fen": UINT, "verified_available_ms": UINT}, [])
    return {"$schema": SCHEMA, "$id": "https://p11.invalid/design/event-envelope-v3.0.schema.json",
        "title": "P11 transactional outbox event draft", "description": "只能由有权限服务从已提交状态产生；设备不能伪造资金/验收事件。schema不代替生产者授权与聚合版本校验。不得包含内容/key/KYC/私密URL。",
        **obj({"event_id": ID, "event_type": enum(*EVENT_TYPES), "schema_version": {"const": "3.0-draft"},
            "occurred_at": TIME, "recorded_at": TIME, "producer_service": ID, "actor_ref": ID,
            "aggregate_type": enum("SELLER", "CAPABILITY", "LISTING", "QUOTE", "PAYMENT", "REFUND", "JOB", "ATTEMPT", "DELIVERY", "RECIPIENT", "SESSION", "TRADE", "SETTLEMENT", "DISPUTE"),
            "aggregate_id": ID, "aggregate_version": VERSION, "correlation_id": ID, "causation_id": ID,
            "idempotency_ref": ID, "evidence_refs": arr(ID, 50), "payload": payload}),
        "allOf": [{"if": {"properties": {"event_type": {"enum": ["PaymentAuthorized", "PaymentSettled", "PaymentUnknown"]}}},
                    "then": {"properties": {"payload": {"required": ["order_id", "payment_id"]}}}},
                  {"if": {"properties": {"event_type": {"enum": ["BuyerAccepted", "BuyerRejected", "PlatformAccepted"]}}},
                    "then": {"properties": {"payload": {"required": ["job_id", "attempt_id", "delivery_id", "fencing_token"]}}}}],
        "x-delivery": "at-least-once; consumer deduplicates event_id and compares aggregate_version; authority state wins"}


def make_route_schema() -> dict:
    schemas = make_schemas()
    def localize(value):
        if isinstance(value, dict):
            return {key: item.replace("#/components/schemas/", "#/$defs/") if key == "$ref" else localize(item) for key, item in value.items()}
        if isinstance(value, list):
            return [localize(item) for item in value]
        return value
    definitions = {name: localize(schemas[name]) for name in ("RouteContractWrite", "RouteContract", "Policy")}
    return {"$schema": SCHEMA, "$id": "https://p11.invalid/design/route-contract-v3.0.schema.json",
        "title": "P11 confirmed route contract draft", "$ref": "#/$defs/RouteContract", "$defs": definitions,
        "description": "标准SDK请求外的已确认控制合同。不能凭schema合法自动购付或跨卖家；签名/支付/设备与证据另验证。"}


CONFIG_VALUES = [
    ("quote_ttl_sec", 900, "仅新报价"), ("admission_ttl_sec", 90, "资金/输入就绪"),
    ("lease_sec", 45, "当前token"), ("heartbeat_sec", 10, "重传不加费"),
    ("node_active_slots", 1, "所有引擎/模式共锁"), ("buyer_active_jobs", 2, "按买方主体"),
    ("sku_queue_cap", "min(20,2*qualified_online)", "扣除租赁/清理占用"), ("same_seller_retries", 1, "已披露同卖家设备"),
    ("image_total_deadline_sec", 300, "含验收不延长"), ("e2e_rewrap_window_sec", 120, "取总期剩余更小值"),
    ("e2e_accept_window_sec", 120, "无回执不自动成交"), ("rental_reserve_sec", 90, "准备阶段设置新截止"),
    ("rental_prepare_sec", 120, "未就绪零费"), ("rental_ready_wait_sec", 120, "未确认零费"),
    ("rental_disconnect_grace_sec", 60, "可用且预算内"), ("result_retention_days", 7, "权益及删除同步"),
    ("download_url_ttl_sec", 300, "本人单对象"), ("platform_input_cleanup_hours", 24, "终态后及含内容衍生对象"),
    ("node_success_cleanup_hours", 1, "清完再复用"), ("node_failure_reconnect_cleanup_hours", 24, "离线重连补清"),
    ("app_log_retention_days", 30, "无正文/key"), ("rolling_backup_days", 7, "恢复不复活删除对象"),
    ("recovery_targets_hours", {"rpo": 24, "rto": 4}, "须真实恢复演练"),
    ("json_request_max_bytes", 2097152, "含base64编码后"), ("media_object_max_bytes", 67108864, "SKU可更低"),
    ("decoded_image_max_pixels", 40000000, "解压内存另限"), ("cipher_data_chunk_max_bytes", 65536, "成熟库认证分块"),
    ("cipher_frame_max_bytes", 131072, "含编码元数据"),
    ("reorder_limits", {"frames": 32, "bytes": 2097152}, "较严者生效"),
    ("stream_send_buffer_bytes", 4194304, "有界背压"), ("pagination", {"default": 20, "max": 100}, "稳定cursor"),
]


def make_configs() -> dict:
    gated = [
        ("commission_bps", None, "H", ["E01", "E13"]), ("channel_cost_owner", None, "E", ["E01", "E02"]),
        ("settlement_cycle_minimum_hold", None, "E", ["E01", "E02"]), ("payment_provider_product_scope", None, "E", ["E01", "E02"]),
        ("subscription_supply_enabled", False, "E", ["E05", "E11"]), ("enhanced_e2e_enabled", False, "E", ["E08", "E09"]),
        ("android_sale_enabled", False, "E", ["E01", "E04", "E10", "E11"]), ("video_sale_enabled", False, "E", ["E02", "E04", "E07"]),
        ("sub2api_commit_license_bundle", None, "E", ["E06"]), ("api_sdk_profile_versions", None, "E", ["E07"]),
        ("crypto_suite_library_signature_profile", None, "E", ["E08"]), ("model_context_output_timeouts", None, "E", ["E04", "E07", "E11"]),
        ("lan_cert_pairing_profile", None, "D", ["E08"]),
    ]
    return {"version": "3.0-draft", "date": DATE, "status": "DESIGN_UNIMPLEMENTED",
        "description": "D为设计初值，未证明运行控制已实现；H/E未定项不能从示例金额补默认。其他模式也必须通过资金/许可/物理测试后可售。",
        "defaults": [{"id": f"CFG{index:02d}", "key": key, "value": value, "status": "D", "current_phase_scope": "DEFERRED" if index in (21,22,23) else "CURRENT", "constraint": constraint}
                     for index, (key, value, constraint) in enumerate(CONFIG_VALUES, 1)],
        "gated": [{"key": key, "value": value, "status": status, "required_evidence": evidence}
                  for key, value, status, evidence in gated],
        "candidate_source": {"sub2api_repo": "https://github.com/Wei-Shaw/sub2api", "frozen_commit": None},
        "external_evidence_ids": [f"E{i:02d}" for i in range(1, 16)]}
