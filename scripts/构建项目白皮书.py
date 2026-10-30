"""原生生成P11 v3.0需求卡、契约和离线阅读器；仅写本项目文档。"""
from __future__ import annotations

import base64
import hashlib
import html
import json
import re
from collections import Counter
from html.parser import HTMLParser
from pathlib import Path
from urllib.parse import unquote

import markdown

from 白皮书契约数据 import make_cipher_schema, make_configs, make_event_schema, make_openapi, make_route_schema
from 白皮书需求细化数据 import chapters_for, load_details

ROOT = Path(__file__).resolve().parents[1]
WHITE = ROOT / "docs" / "白皮书"
CONTRACTS = WHITE / "contracts"
MAIN = WHITE / "算力共享平台项目白皮书-v3.0.md"
MERGED = WHITE / "算力共享平台完整阅读版-v3.0.md"
READER = WHITE / "算力共享平台完整阅读版-v3.0.html"
DATE = "2026-10-29"


def write_json(path: Path, value) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(value, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")


def baseline() -> list[dict]:
    source = ROOT / "docs" / "02-功能需求与用户流程.md"
    tests = ROOT / "docs" / "05-验收测试与实施计划.md"
    cases = {req: (case, text) for case, req, text in re.findall(
        r"^\| (P11-T\d{2}) \| (P11-F\d{2}) \| (.+?) \|$", tests.read_text(encoding="utf-8"), re.M)}
    rows = []
    for req, title, body in re.findall(r"^### (P11-F\d{2}) (.+?)\n(.*?)(?=^### |\Z)", source.read_text(encoding="utf-8"), re.M | re.S):
        rows.append({"id": req, "title": title, "priority": re.search(r"优先级：(P[012](?:-AUTH)?)。", body).group(1),
            "acceptance": re.search(r"^验收：(.+)$", body, re.M).group(1),
            "test_case": cases[req][0], "test_condition": cases[req][1],
            "source": "docs/" + source.name, "test_source": "docs/" + tests.name})
    if len(rows) != 80 or len(cases) != 80:
        raise ValueError("基线必须是80项需求和80项主验收")
    return rows


def write_requirement_cards() -> list[dict]:
    details = {row["id"]: row for row in load_details()}
    rows = []
    lines = ["# 80项需求逐项实施规格", "", "P11｜v3.0｜2026-10-29｜每项均为设计；主验收和附加边界场景均未执行。下述是D细化规则，不表示许可/硬件/支付/密码已获证。", "",
        "本表与七主文档同号关联，不增删80项主需求；先看角色、前置和输入，再按规则/异常实现，保存输出证据，最后按同号主用例及边界场景验收。各模块的公共不变量、错误码、数据权限和外部发布门槛仍适用。", "",
        "[结构化细化登记](../评审/需求细化登记表-v3.0.json)；[控制契约](contracts/README.md)；[配置和证据任务](14-决策记录配置与待验证事项.md)；[验收与工作包](12-验收矩阵与交付计划.md)。", "",
        "## 逐项索引", "", "| 功能 | 名称 | 优先级 | 主验收 |", "| --- | --- | --- | --- |"]
    base = baseline()
    for row in base:
        lines.append(f"| {row['id']} | {row['title']} | {row['priority']} | {row['test_case']} |")
    lines.append("")
    for original in base:
        detail = details[original["id"]]
        row = {**original, **detail, "chapters": ["docs/白皮书/" + name for name in chapters_for(original["id"])],
            "implementation_status": "设计", "test_execution_status": "未执行", "refinement_status": "D设计细化"}
        dependency_ids = re.findall(r"F\d{2}", detail["dependencies"])
        row["dependency_ids"] = ["P11-" + dep for dep in dependency_ids]
        rows.append(row)
        lines += [f"## {row['id']} {row['title']}", "",
            f"优先级：{row['priority']}；主验收：{row['test_case']}；实现：设计；执行：未执行。", "", f"本轮范围：{row['current_phase_scope']}。{row['scope_note']}", ""]
        for label, key in [("参与角色", "actors"), ("前置条件", "preconditions"), ("输入与配置", "inputs"),
            ("业务步骤与规则", "rules"), ("异常与恢复", "exceptions"), ("输出与审计证据", "evidence"),
            ("实现依赖/协同", "dependencies")]:
            lines += [f"**{label}：** {row[key].rstrip('。')}。", ""]
        lines += [f"**主通过条件：** {row['acceptance']}", "", f"**主场景：** {row['test_condition']}", "",
            f"**附加边界场景（未执行）：** {row['boundary_case']}。", "",
            "**详规：** " + "；".join(f"[{name[3:-3]}]({name})" for name in chapters_for(row["id"])) + "。", ""]
    (WHITE / "15-需求逐项实施规格.md").write_text("\n".join(lines), encoding="utf-8")
    write_json(ROOT / "docs" / "评审" / "需求细化登记表-v3.0.json", {
        "version": "v3.0", "date": DATE, "scope": "P11 C2C算力共享平台", "requirement_count": len(rows),
        "priorities": dict(sorted(Counter(row["priority"] for row in rows).items())),
        "verification_scope": "人工细化设计及结构追踪；产品/真机/SDK/密码/资金均未执行", "requirements": rows})
    return rows


def make_examples() -> dict:
    policy = {"service_mode": "PLAINTEXT", "privacy_profile": "STANDARD", "region": "TW-design-example",
        "protocol_profile_id": "profile_chat_draft", "acceptance_policy_id": "accept_image_v1", "disclosure_version": "notice_v3"}
    route_policy = {**policy, "service_mode": "E2E", "privacy_profile": "PLATFORM_BLIND"}
    route = {"route_id": "route_demo_01", "buyer_id": "buyer_demo_01", "version": 1, "state": "ACTIVE",
        "remaining_budget_fen": 500, "used_calls": 0, "contract_hash": "1" * 64,
        "definition": {"confirmed_order_id": "order_demo_01", "seller_id": "seller_demo_01", "listing_id": "listing_demo_01", "listing_revision": 1,
            "policy": route_policy, "budget_per_call_fen": 50, "total_budget_fen": 500, "max_call_count": 10,
            "expires_at": "2026-10-29T12:00:00Z", "purchase_authorization_ref": "auth_demo_01",
            "recipient_id": "recipient_demo_01", "recipient_key_hash": "2" * 64}}
    raw = b"synthetic-non-cryptographic-fixture"
    frame = {"frame_version": "3.0-draft", "channel_id": "channel_demo_01", "direction": "BUYER_TO_SELLER",
        "suite_profile_id": "suite_unfrozen_draft", "epoch": "0", "sequence": "0", "expires_at": "2026-10-29T12:00:00Z",
        "ciphertext_b64u": base64.urlsafe_b64encode(raw).decode().rstrip("="), "ciphertext_sha256": hashlib.sha256(raw).hexdigest(),
        "context": {"contract_hash": "1" * 64, "order_id": "order_demo_01", "seller_id": "seller_demo_01",
            "recipient_id": "recipient_demo_01", "recipient_key_id": "key_seller_demo", "buyer_key_id": "key_buyer_demo",
            "job_id": "job_demo_01", "attempt_id": "attempt_demo_01", "fencing_token": "1", "delivery_revision": 1,
            "stream_id": "stream_demo_01", "object_id": "object_demo_01"}}
    event = {"event_id": "event_demo_01", "event_type": "BuyerAccepted", "schema_version": "3.0-draft",
        "occurred_at": "2026-10-29T10:00:00Z", "recorded_at": "2026-10-29T10:00:01Z",
        "producer_service": "service_acceptance", "actor_ref": "buyer_demo_01", "aggregate_type": "JOB",
        "aggregate_id": "job_demo_01", "aggregate_version": 4, "correlation_id": "corr_demo_01",
        "causation_id": "receipt_demo_01", "idempotency_ref": "idem_demo_01", "evidence_refs": ["receipt_demo_01"],
        "payload": {"order_id": "order_demo_01", "job_id": "job_demo_01", "attempt_id": "attempt_demo_01",
            "delivery_id": "delivery_demo_01", "fencing_token": "1"}}
    receipt = {"receipt_id": "receipt_demo_01", "attempt_id": "attempt_demo_01", "fencing_token": "1",
        "delivery_id": "delivery_demo_01", "delivery_revision": 1, "cipher_manifest_hash": "3" * 64,
        "decision": "ACCEPT", "signer_key_id": "key_buyer_demo", "signature_profile_id": "sig_unfrozen_draft",
        "signed_at": "2026-10-29T10:00:00Z", "signature": "A" * 86}
    quote = {"listing_id": "listing_demo_01", "listing_revision": 1, "sku_id": "sku_rental_demo",
        "policy": {**policy,"service_mode":"GPU_RENTAL","acceptance_policy_id":"accept_rental_v1"},
        "public_spec": {"duration_ms":1800000}, "budget_fen": 500,
        "desired_region": "TW-design-example", "quantity": 1}
    nb_common = {"price_contract_version":"NB_PRICE_1","currency":"CNY","rounding_profile":"ORDER_CEIL_FEN_1",
        "commission_bps":500,"channel_cost_owner":"SELLER","fee_policy_version":"fee_demo"}
    hour_price = {**nb_common,"unit":"AVAILABLE_HOUR","hour_rate":{"numerator_fen":360,"denominator_units":1}}
    token_price = {**nb_common,"unit":"TOKEN_1M","input_rate":{"numerator_fen":80,"denominator_units":1},
        "output_rate":{"numerator_fen":160,"denominator_units":1}}
    legacy_price = {"currency":"CNY","unit":"AVAILABLE_MINUTE","unit_price_fen":6,
        "commission_bps":500,"channel_cost_owner":"SELLER","fee_policy_version":"legacy_fee_demo"}
    return {"version": "3.0-draft", "notice": "虚构设计数据；金额不是定价，地区不是准入。密文/签名只是schema夹具，不是有效密码报文，不是资金/执行事件。",
        "quote_request": quote, "route_contract": route, "cipher_frame": frame, "event_envelope": event, "buyer_receipt": receipt,
        "nb_hour_price":hour_price,"nb_token_price":token_price,"legacy_price":legacy_price,
        "auth_challenge_request":{"client_type":"MINIAPP","code_challenge":"A"*43},
        "native_nonce_request":{"client_type":"MINIAPP","proof_profile":"ALIPAY_NATIVE_NONCE_1"},
        "native_nonce_challenge":{"challenge_id":"ach_native_schema_only","client_type":"MINIAPP","expires_at":"2026-10-30T12:00:00Z","proof_profile":"ALIPAY_NATIVE_NONCE_1","native_code_verifier":"n"*43},
        "miniapp_session_request":{"challenge_id":"ach_schema_only","auth_code":"SYNTHETIC_NOT_A_CHANNEL_CODE","code_verifier":"v"*43},
        "web_session":{"session_id":"ses_schema_only","client_type":"WEB","expires_at":"2026-10-30T12:00:00Z",
            "csrf_token":"a"*64,"profile":{"user_id":"usr_schema_only","roles":["BUYER"],
                "permission_scope_ids":["perm_identity_read"],"identity_state":"UNVERIFIED","version":2}}}


def write_contracts() -> dict:
    CONTRACTS.mkdir(parents=True, exist_ok=True)
    api = make_openapi()
    documents = {"openapi-control-v3.0.json": api, "cipher-frame-v3.0.schema.json": make_cipher_schema(),
        "event-envelope-v3.0.schema.json": make_event_schema(), "route-contract-v3.0.schema.json": make_route_schema(),
        "config-catalog-v3.0.json": make_configs(), "examples-v3.0.json": make_examples()}
    for name, value in documents.items():
        write_json(CONTRACTS / name, value)
    lines = ["# P11 v3.0机器可读设计契约", "", "P11｜v3.0｜2026-10-29｜设计草案；没有已部署服务。仅检查结构不能证明SDK、签名、密码、状态、安全或资金正确。", "",
        "| 文件 | 范围 |", "| --- | --- |",
        "| [控制面OpenAPI](openapi-control-v3.0.json) | 自有控制面：权限、请求响应、幂等、归属与错误。地址p11.invalid为保留占位 |",
        "| [密文帧Schema](cipher-frame-v3.0.schema.json) | 认证上下文与密文外层；payload类型/终态在密文内，suite仍待冻结 |",
        "| [事件信封Schema](event-envelope-v3.0.schema.json) | outbox事件、关联、版本和安全载荷；生产者权限另核 |",
        "| [路由合同Schema](route-contract-v3.0.schema.json) | 标准请求之外绑定卖家/模式/预算的已确认路由 |",
        "| [配置目录](config-catalog-v3.0.json) | CFG01—31设计初值、未定参数与默认关闭的证据门槛 |",
        "| [虚构示例](examples-v3.0.json) | schema形状夹具；签名/密文不是真实有效报文，金额不代表价格 |", "",
        "完整厂商模型/媒体API、OAuth登录、支付宝支付签名/回调和ComfyUI/LM Studio/LMA原生协议未被这份控制面取代；必须冻结实际版本并用官方规范/真实环境验证。未知个人渠道不以私有模拟接口冒充。", "",
        "## 操作目录", "", "| 方法 | 相对/v1/p11路径 | 用途 | 操作ID |", "| --- | --- | --- | --- |"]
    for path, ops in api["paths"].items():
        for method, operation in ops.items():
            lines.append(f"| {method.upper()} | `{path}` | {operation['summary']} | `{operation['operationId']}` |")
    lines += ["", "## 实施前必须验证的语义", "",
        "对象归属、合同/价版不变、当前fencing_token、容量互斥、状态竞争、资金资格/未知先查、证据有效、签名真实性、完整终块和清理后解锁均非JSON Schema能自动证明。禁止把结构合格当作允许付款或放钥。", "",
        "E2E先生成INPUT_PENDING/PREPARED上下文用于绑定attempt/token，再加密上传和提交封装；此时无执行权且不锁GPU。资金、输入/封装、端点身份和资源准入全部满足后才签ISSUED租约。换端先换上下文并由买方核验重封。", "",
        "[详细接口事件规则](../10-接口事件与错误契约.md)；[需求逐项规格](../15-需求逐项实施规格.md)；[配置与外部证据](../14-决策记录配置与待验证事项.md)。", ""]
    (CONTRACTS / "README.md").write_text("\n".join(lines), encoding="utf-8")
    return api


def source_documents() -> list[Path]:
    sources = [MAIN, *sorted(WHITE.glob("[0-9][0-9]-*.md"))]
    if len(sources) != 18:
        raise ValueError("应为1份白皮书总册与00—16共17份章节文件")
    return sources


class PlainText(HTMLParser):
    def __init__(self):
        super().__init__()
        self.parts = []

    def handle_data(self, data):
        self.parts.append(data)


def strip_html(value: str) -> str:
    parser = PlainText()
    parser.feed(value)
    return " ".join(" ".join(parser.parts).split())


def shifted_headings(content: str) -> str:
    output, fenced = [], False
    for line in content.splitlines():
        if line.startswith("```"):
            fenced = not fenced
        if not fenced and re.match(r"^#{1,5} ", line):
            line = "#" + line
        output.append(line)
    return "\n".join(output)


def source_title(source: Path) -> str:
    return source.read_text(encoding="utf-8").splitlines()[0].removeprefix("# ")


def build_reader() -> dict:
    sources = source_documents()
    anchors = {path.resolve(): ("chapter-main" if index == 0 else f"chapter-{index - 1:02d}") for index, path in enumerate(sources)}
    merged = ["# 算力共享平台完整需求与项目白皮书", "", "P11｜v3.0｜2026-10-29｜由项目脚本合订；80项功能与80项主验收均为设计/未执行。软件、资金、真机、SDK与密码未验收。", "",
        "[原始阅读导航](00-阅读导航与需求基线.md)；[离线HTML阅读器](算力共享平台完整阅读版-v3.0.html)。", "", "## 合订目录", ""]
    for source in sources:
        merged.append(f"- [{source_title(source)}](#{anchors[source.resolve()]})")
    merged.append("")
    sections, search_index, nav = [], [], []
    for source in sources:
        anchor = anchors[source.resolve()]
        content = source.read_text(encoding="utf-8")

        def rewrite(match):
            label, target = match.group(1), match.group(2).strip("<>")
            if re.match(r"^(https?://|#|mailto:)", target):
                return match.group(0)
            path_part, _, fragment = target.partition("#")
            target_path = (source.parent / unquote(path_part)).resolve()
            if target_path in anchors:
                return f"[{label}](#{anchors[target_path]})"
            # All current sources reside directly in WHITE; keep valid paths relative to it.
            import os
            relative = Path(os.path.relpath(target_path, WHITE)).as_posix()
            return f"[{label}]({relative}" + ("#" + fragment if fragment else "") + ")"

        content = re.sub(r"\[([^\]\n]+)\]\(([^)]+)\)", rewrite, content)
        content = shifted_headings(content)
        merged += [f'<a id="{anchor}"></a>', "", content, ""]

        def slug(value, separator):
            cleaned = re.sub(r"[^\w\s-]", "", html.unescape(value).lower())
            return anchor + "-" + (re.sub(r"[\s-]+", separator, cleaned).strip(separator) or "section")

        rendered = markdown.markdown(content, extensions=["tables", "fenced_code", "toc"],
            extension_configs={"toc": {"slugify": slug, "permalink": False}})
        rendered = re.sub(r'<a href="(https?://[^\"]+)"', r'<a rel="noopener noreferrer" href="\1"', rendered)
        headings = list(re.finditer(r'<h([1-6]) id="([^\"]+)">(.*?)</h\1>', rendered, re.S))
        for index, match in enumerate(headings):
            end = headings[index + 1].start() if index + 1 < len(headings) else len(rendered)
            search_index.append({"id": match.group(2), "title": strip_html(match.group(3)),
                "chapter": source_title(source), "text": strip_html(rendered[match.end():end])})
        sections.append(f'<section class="chapter" id="{anchor}">{rendered}</section>')
        nav.append(f'<li><a href="#{anchor}">{html.escape(source_title(source))}</a></li>')
    MERGED.write_text("\n".join(merged), encoding="utf-8")
    embedded_index = json.dumps(search_index, ensure_ascii=False).replace("<", "\\u003c").replace(">", "\\u003e").replace("&", "\\u0026")
    template = r'''<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="color-scheme" content="light"><title>算力共享平台 · 完整需求白皮书 v3.0</title>
<style>
:root{--ink:#172a38;--muted:#526776;--line:#dbe3e8;--accent:#146b65;--paper:#fff;--back:#eef3f4}*{box-sizing:border-box}html{scroll-behavior:smooth;scroll-padding-top:24px}body{margin:0;background:var(--back);color:var(--ink);font:16px/1.85 "Segoe UI","Microsoft YaHei",sans-serif}a{color:var(--accent);text-underline-offset:3px}header{padding:32px 42px;background:#153b3a;color:white}header p{margin:8px 0 0;color:#c9dfdd}h1{font-size:30px;margin:0}h2{font-size:26px;line-height:1.5;margin:0 0 24px}h3{font-size:21px;margin-top:30px}h4{font-size:18px;margin-top:24px}.layout{display:grid;grid-template-columns:300px minmax(0,1fr);max-width:1600px;margin:auto;align-items:start}aside{position:sticky;top:0;max-height:100vh;overflow:auto;padding:24px 22px}aside ul{list-style:none;padding:0;margin:16px 0}aside li{margin:7px 0}aside a{text-decoration:none;font-size:14px}aside a:hover{text-decoration:underline}main{min-width:0;padding:30px 32px 80px}.chapter{background:var(--paper);padding:32px 38px;margin-bottom:24px;border:1px solid var(--line);border-radius:12px;overflow-wrap:anywhere}table{border-collapse:collapse;width:100%;font-size:14px;line-height:1.7;margin:20px 0}th,td{border:1px solid var(--line);padding:9px 11px;text-align:left;vertical-align:top}th{background:#edf5f3}tr:nth-child(even){background:#fafcfc}pre{background:#f1f5f6;padding:16px;overflow:auto;font-size:13px;line-height:1.65}code{font-family:Consolas,monospace;background:#f1f5f6;padding:1px 4px}pre code{padding:0}.note{padding:12px 16px;border-left:4px solid var(--accent);background:#e5efed;color:#204c47}.search-box input{width:100%;padding:11px;border:1px solid #adc4c0;border-radius:6px;font:inherit}.search-box label{display:block;font-weight:600;margin-bottom:8px}.small{font-size:13px;color:var(--muted)}button{padding:8px 12px;background:#fff;border:1px solid #adc4c0;border-radius:6px;cursor:pointer}#results{padding:0;list-style:none}#results li{border-bottom:1px solid var(--line);padding:12px 0}#results a{font-weight:600}#results p{margin:3px 0;font-size:13px;color:var(--muted)}#search-panel{display:none;background:#fff;padding:24px;border:1px solid var(--line);margin-bottom:24px;border-radius:12px}.top-link{position:fixed;right:20px;bottom:18px;padding:9px 13px;background:#153b3a;color:#fff;border-radius:24px;text-decoration:none;font-size:13px}.diagram-caption{font-size:13px;color:var(--muted)}svg.overview{width:100%;height:auto;max-height:240px}svg text{font-family:"Segoe UI","Microsoft YaHei",sans-serif;font-size:15px;fill:#172a38}@media(max-width:1000px){.layout{grid-template-columns:1fr}aside{position:relative;max-height:none;padding:20px 24px}aside ul{columns:2}main{padding:0 16px 60px}.chapter{padding:24px 20px}header{padding:25px 24px}table{display:block;overflow:auto}}@media print{@page{size:A4;margin:18mm}body{background:#fff;font-size:10pt;line-height:1.6}header{padding:0;color:#000;background:#fff;border-bottom:2px solid #333}header p{color:#333}h1{font-size:22pt}.layout{display:block}aside,#search-panel,.top-link,.no-print{display:none!important}main{padding:18px 0}.chapter{padding:0;border:0;margin:0 0 24px;break-before:page;border-radius:0}h2,h3,h4{break-after:avoid}pre{white-space:pre-wrap;overflow-wrap:anywhere}table{font-size:8pt;display:table}tr{break-inside:avoid}a{color:#000}svg.overview{max-height:150px}}
</style></head><body id="top">
<header><h1>算力共享平台 · 完整需求白皮书</h1><p>v3.0 · 2026-10-29 · C2C闲置设备市场 · 80项需求 / 80项主验收 · 设计，未执行产品验收</p></header>
<div class="layout"><aside aria-label="阅读导航"><div class="search-box"><label for="search">全文检索</label><input id="search" type="search" placeholder="例如：P11-F74、结算、Android" autocomplete="off"><p class="small">完全离线，无外部资源。搜索定位段落；Ctrl+F可在正文继续查找。</p><button id="print" type="button">打印 / 保存PDF</button></div><ul>__NAV__</ul><p class="small">机器契约见章节链接。Mermaid源图保留为可读代码；下方提供静态链路总览。</p></aside>
<main><div class="note no-print">本交付物是需求和实施依据，不是平台软件或已验证密码系统。个人卖家资金路线、许可、SDK、真机及增强硬件证据仍须逐项取得。</div>
<figure class="no-print" aria-label="平台链路静态总览"><svg class="overview" viewBox="0 0 980 160" role="img"><title>C2C平台控制合同，明文与加密链路</title><defs><marker id="arrow" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="6" markerHeight="6" orient="auto"><path d="M 0 0 L 10 5 L 0 10 z" fill="#146b65"/></marker></defs><g fill="#fff" stroke="#a6c4be"><rect x="5" y="25" width="190" height="65" rx="8"/><rect x="270" y="25" width="190" height="65" rx="8"/><rect x="535" y="25" width="190" height="65" rx="8"/><rect x="800" y="25" width="175" height="65" rx="8"/></g><g text-anchor="middle"><text x="100" y="51">买家 / 标准SDK</text><text x="100" y="75">本机或可信LAN加密端</text><text x="365" y="51">C2C平台</text><text x="365" y="75">合同 / 明文网关或盲转</text><text x="630" y="51">实际用户卖家</text><text x="630" y="75">解密端 / 内置转发内核</text><text x="887" y="51">闲置GPU / Android</text><text x="887" y="75">受控本地执行</text></g><g stroke="#146b65" stroke-width="2" marker-end="url(#arrow)"><path d="M195 58 H260"/><path d="M460 58 H525"/><path d="M725 58 H790"/></g><text x="100" y="130">三模式：明文 / E2E两档 / 受控显卡租赁</text><text x="560" y="130">合同锁定实际卖家 · 计量/成交/结算分开</text></svg><figcaption class="diagram-caption">加密响应沿相反方向回传；基础档执行所有者可见内容，增强档另需全路径真实证明。</figcaption></figure>
<section id="search-panel" aria-label="搜索结果"><h2>检索结果</h2><p id="search-status" role="status" aria-live="polite"></p><ol id="results"></ol></section>
__SECTIONS__
</main></div><a class="top-link" href="#top">返回顶部</a>
<script type="application/json" id="search-index">__INDEX__</script>
<script>
(()=>{const index=JSON.parse(document.getElementById('search-index').textContent);const input=document.getElementById('search'),panel=document.getElementById('search-panel'),list=document.getElementById('results'),status=document.getElementById('search-status');let timer;function search(){const q=input.value.trim().toLocaleLowerCase();list.replaceChildren();panel.style.display=q?'block':'none';if(!q)return;const words=q.split(/\s+/);const matches=index.filter(x=>words.every(w=>(x.title+' '+x.text).toLocaleLowerCase().includes(w)));status.textContent='共 '+matches.length+' 处匹配'+(matches.length>40?'，显示前40处':'')+'。点击跳转至正文。';for(const x of matches.slice(0,40)){const li=document.createElement('li'),a=document.createElement('a'),p=document.createElement('p');a.href='#'+x.id;a.textContent=x.title;const low=x.text.toLocaleLowerCase(),pos=Math.max(0,low.indexOf(words[0])-40);p.textContent=x.chapter+' · '+x.text.slice(pos,pos+200);li.append(a,p);list.append(li);}}input.addEventListener('input',()=>{clearTimeout(timer);timer=setTimeout(search,130)});input.addEventListener('keydown',e=>{if(e.key==='Escape'){input.value='';search()}});document.getElementById('print').addEventListener('click',()=>window.print());})();
</script></body></html>'''
    output = template.replace("__NAV__", "".join(nav)).replace("__SECTIONS__", "\n".join(sections)).replace("__INDEX__", embedded_index)
    READER.write_text(output, encoding="utf-8")
    return {"source_documents": len(sources), "search_sections": len(search_index), "html_bytes": READER.stat().st_size,
        "merged_markdown_bytes": MERGED.stat().st_size}


def main() -> int:
    WHITE.mkdir(parents=True, exist_ok=True)
    rows = write_requirement_cards()
    api = write_contracts()
    reader = build_reader()
    print(json.dumps({"status": "生成设计文档，非产品实现", "requirements": len(rows),
        "control_api_operations": sum(len(ops) for ops in api["paths"].values()), "api_schemas": len(api["components"]["schemas"]),
        **reader}, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
