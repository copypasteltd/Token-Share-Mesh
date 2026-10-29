"""只核验白皮书设计覆盖、契约结构与离线产物；绝不运行产品或宣称真实验收。"""
from __future__ import annotations

import copy
import json
import re
from collections import Counter
from html.parser import HTMLParser
from pathlib import Path
from urllib.parse import unquote, urlparse

from jsonschema import Draft202012Validator, FormatChecker

from 构建项目白皮书 import baseline, source_documents

ROOT = Path(__file__).resolve().parents[1]
WHITE = ROOT / "docs" / "白皮书"
CONTRACTS = WHITE / "contracts"
REPORT = ROOT / "docs" / "评审" / "白皮书契约核验-v3.0.md"
RESULT = REPORT.with_suffix(".json")


def read_json(path):
    return json.loads(path.read_text(encoding="utf-8"))


def walk(value):
    if isinstance(value, dict):
        yield value
        for child in value.values():
            yield from walk(child)
    elif isinstance(value, list):
        for child in value:
            yield from walk(child)


def resolve_pointer(document, reference):
    if not reference.startswith("#/"):
        raise ValueError("本设计契约不应依赖未冻结远程$ref：" + reference)
    value = document
    for part in unquote(reference[2:]).split("/"):
        value = value[part.replace("~1", "/").replace("~0", "~")]
    return value


class ReaderParser(HTMLParser):
    def __init__(self):
        super().__init__()
        self.ids, self.links, self.external_assets = [], [], []
        self.in_index = False
        self.index_parts = []

    def handle_starttag(self, tag, attributes):
        attrs = dict(attributes)
        if "id" in attrs:
            self.ids.append(attrs["id"])
        if tag == "a" and "href" in attrs:
            self.links.append(attrs["href"])
        for key in ("src", "srcset", "data"):
            if key in attrs and re.match(r"^(https?:)?//", attrs[key]):
                self.external_assets.append(attrs[key])
        if tag == "link" and re.match(r"^(https?:)?//", attrs.get("href", "")):
            self.external_assets.append(attrs["href"])
        if tag == "script" and attrs.get("id") == "search-index":
            self.in_index = True

    def handle_data(self, data):
        if self.in_index:
            self.index_parts.append(data)

    def handle_endtag(self, tag):
        if tag == "script":
            self.in_index = False


def main() -> int:
    errors = []
    sources = source_documents()
    originals = baseline()
    refined = read_json(ROOT / "docs" / "评审" / "需求细化登记表-v3.0.json")["requirements"]
    expected = {row["id"] for row in originals}
    original_map = {row["id"]: row for row in originals}
    refined_ids = [row["id"] for row in refined]
    if set(refined_ids) != expected or len(refined_ids) != 80:
        errors.append("逐项细化必须完整且唯一覆盖80项")
    cards = (WHITE / "15-需求逐项实施规格.md").read_text(encoding="utf-8")
    if len(re.findall(r"^## P11-F\d{2} ", cards, re.M)) != 80:
        errors.append("Markdown需求卡数量错误")
    field_names = ("actors", "preconditions", "inputs", "rules", "exceptions", "evidence", "dependencies", "boundary_case")
    for row in refined:
        if any(not row.get(key) for key in field_names):
            errors.append(row["id"] + "缺实施字段")
        for key in ("title", "priority", "acceptance", "test_case", "test_condition"):
            if row[key] != original_map[row["id"]][key]:
                errors.append(row["id"] + "与主基线不一致：" + key)
        if row["implementation_status"] != "设计" or row["test_execution_status"] != "未执行":
            errors.append(row["id"] + "误改产品状态")
        if any(dep not in expected for dep in row["dependency_ids"]):
            errors.append(row["id"] + "引用不存在依赖")
        for path in row["chapters"]:
            if not (ROOT / path).is_file():
                errors.append(row["id"] + "细化章节断链")
    priorities = dict(sorted(Counter(row["priority"] for row in refined).items()))
    if priorities != {"P0": 72, "P1": 3, "P1-AUTH": 3, "P2": 1, "P2-AUTH": 1}:
        errors.append("需求优先级漂移")

    contract_files = sorted(CONTRACTS.glob("*.json"))
    documents = {path.name: read_json(path) for path in contract_files}
    required = {"openapi-control-v3.0.json", "cipher-frame-v3.0.schema.json", "event-envelope-v3.0.schema.json",
        "route-contract-v3.0.schema.json", "config-catalog-v3.0.json", "examples-v3.0.json"}
    if not required.issubset(documents):
        errors.append("缺机器契约")
    reference_count = 0
    for name, document in documents.items():
        for node in walk(document):
            if "$ref" in node:
                reference_count += 1
                try:
                    resolve_pointer(document, node["$ref"])
                except (ValueError, KeyError, TypeError) as exc:
                    errors.append(name + "引用错误：" + str(exc))
        if name.endswith(".schema.json"):
            try:
                Draft202012Validator.check_schema(document)
            except Exception as exc:
                errors.append(name + "Schema无效：" + str(exc)[:200])
    api = documents["openapi-control-v3.0.json"]
    if api["openapi"] != "3.1.0":
        errors.append("控制面OpenAPI版本错误")
    for name, schema in api["components"]["schemas"].items():
        try:
            Draft202012Validator.check_schema(schema)
        except Exception as exc:
            errors.append("OpenAPI schema " + name + "：" + str(exc)[:200])
    # Compare independent prose state contracts with their machine-readable counterparts.
    state_sources = [
        ("Listing", "01-业务模型与市场规则.md", "挂牌状态为"),
        ("Device", "03-供给端与设备生命周期.md", "设备状态："),
        ("Settlement", "08-计费支付结算与争议.md", "SellerSettlement状态为"),
        ("Quote", "07-订单调度与租赁状态机.md", "| Quote |"),
        ("Order", "07-订单调度与租赁状态机.md", "| Order |"),
        ("Payment", "07-订单调度与租赁状态机.md", "| Payment |"),
        ("Job", "07-订单调度与租赁状态机.md", "| Job |"),
        ("Refund", "07-订单调度与租赁状态机.md", "| Refund |"),
    ]
    for name, filename, marker in state_sources:
        line = next(line for line in (WHITE / filename).read_text(encoding="utf-8").splitlines() if line.startswith(marker))
        if marker.startswith("|"):
            line = line.split("|")[2]
        prose_states = set(re.findall(r"(?<![A-Za-z_])[A-Z][A-Z_]{1,}(?![A-Za-z_])", line))
        machine_states = set(api["components"]["schemas"][name]["properties"]["state"]["enum"])
        if prose_states != machine_states:
            errors.append(name + "正文与契约状态不一致：" + str(sorted(prose_states ^ machine_states)))
    operations = []
    for path, ops in api["paths"].items():
        for method, op in ops.items():
            operations.append(op["operationId"])
            anonymous_auth = op['operationId'] in {'createAuthChallenge','createUserSession'}
            single_use_auth = op['operationId'] in {'createAuthChallenge','createUserSession','revokeUserSession'}
            if anonymous_auth:
                if op.get('security') != [] or op.get('x-required-scopes') != [] or op.get('x-authentication-flow') != 'CHALLENGE_VERIFIER_AND_VERIFIED_PROVIDER':
                    errors.append(path + '匿名授权必须显式一次性绑定与渠道验签')
            elif not op.get("security") or not op.get("x-required-scopes"):
                errors.append(path + "缺认证/scope")
            for security in op["security"]:
                if any(name not in api["components"]["securitySchemes"] or scopes != [] for name, scopes in security.items()):
                    errors.append(path + "Bearer认证定义错误")
            params = op.get("parameters", [])
            if len({(p["in"], p["name"]) for p in params}) != len(params):
                errors.append(path + "重复参数")
            names = set(re.findall(r"\{([^}]+)\}", path))
            actual = {p["name"] for p in params if p["in"] == "path" and p.get("required")}
            if names != actual:
                errors.append(path + "path参数不完整")
            if method in ("post", "patch", "delete"):
                if single_use_auth:
                    if op.get('x-idempotency-result-policy') != 'SINGLE_USE_AUTH_FLOW' or any(p['name']=='Idempotency-Key' for p in params):
                        errors.append(path + '身份一次性操作的重试合同错误')
                elif not any(p["name"] == "Idempotency-Key" and p.get("required") for p in params):
                    errors.append(path + "缺幂等头")
            if method in ("post", "patch") and not op.get("requestBody", {}).get("required"):
                errors.append(path + "缺必需请求模型")
            if not {"401", "403", "404", "429", "503"}.issubset(op["responses"]):
                errors.append(path + "缺关键错误合同")
    if len(operations) != len(set(operations)):
        errors.append("operationId重复")

    examples = documents["examples-v3.0.json"]
    def api_validator(name):
        return Draft202012Validator({"$ref": "#/components/schemas/" + name, "components": api["components"]}, format_checker=FormatChecker())
    validators = {
        "quote_request": api_validator("QuoteWrite"), "buyer_receipt": api_validator("ReceiptWrite"),
        "nb_hour_price":api_validator("Price"),"nb_token_price":api_validator("Price"),"legacy_price":api_validator("Price"),
        "auth_challenge_request":api_validator("AuthChallengeWrite"),
        "miniapp_session_request":api_validator("SessionWrite"),"web_session":api_validator("SessionView"),
        "route_contract": Draft202012Validator(documents["route-contract-v3.0.schema.json"], format_checker=FormatChecker()),
        "cipher_frame": Draft202012Validator(documents["cipher-frame-v3.0.schema.json"], format_checker=FormatChecker()),
        "event_envelope": Draft202012Validator(documents["event-envelope-v3.0.schema.json"], format_checker=FormatChecker())}
    for key, validator in validators.items():
        failures = list(validator.iter_errors(examples[key]))
        if failures:
            errors.append(key + "虚构正向结构夹具失败：" + failures[0].message)
    negative_count = 0
    def expect_invalid(label, validator, value):
        nonlocal negative_count
        negative_count += 1
        if validator.is_valid(value):
            errors.append(label + "错误结构未拒绝")
    bad = copy.deepcopy(examples["quote_request"])
    bad["policy"]["privacy_profile"] = "PLATFORM_BLIND"
    expect_invalid("普通租赁错隐私", validators["quote_request"], bad)
    bad = copy.deepcopy(examples["route_contract"])
    del bad["definition"]["recipient_id"]
    expect_invalid("E2E缺端点", validators["route_contract"], bad)
    bad = copy.deepcopy(examples["cipher_frame"])
    bad["prompt"] = "forbidden-plaintext-field"
    expect_invalid("外层私密字段", validators["cipher_frame"], bad)
    bad = copy.deepcopy(examples["cipher_frame"])
    bad["sequence"] = 1
    expect_invalid("序号非规范字符串", validators["cipher_frame"], bad)
    bad = copy.deepcopy(examples["cipher_frame"])
    bad["ciphertext_b64u"] = "A" * 120001
    expect_invalid("外层密文过长", validators["cipher_frame"], bad)
    bad = copy.deepcopy(examples["event_envelope"])
    del bad["payload"]["fencing_token"]
    expect_invalid("接受事件缺token", validators["event_envelope"], bad)
    bad = copy.deepcopy(examples["buyer_receipt"])
    bad["signature"] = ""
    expect_invalid("空签名形状", validators["buyer_receipt"], bad)
    expect_invalid("开售缺审批引用", api_validator("FeatureGateWrite"), {"gate_id": "gate_demo", "scope_ref": "scope_demo",
        "enabled": True, "expected_version": 1, "reason_code": "review_demo"})
    expect_invalid("E2E与明文输入混用", api_validator("JobWrite"), {"order_id": "order_demo", "expected_order_version": 1,
        "caller_key": "caller_demo", "deadline_at": "2026-10-29T12:00:00Z", "encryption_intent_id": "intent_demo", "input_asset_refs": ["asset_demo"]})
    expect_invalid("分数单价分母为零", api_validator("Price"), {"currency": "CNY", "unit": "TOKEN_1K",
        "unit_rate": {"numerator_fen": 1, "denominator_units": 0}, "commission_bps": 0,
        "channel_cost_owner": "PLATFORM", "fee_policy_version": "fee_demo"})
    bad=copy.deepcopy(examples['nb_token_price']);del bad['output_rate']
    expect_invalid('缺输出费率',api_validator('Price'),bad)
    bad=copy.deepcopy(examples['nb_hour_price']);bad['input_rate']={'numerator_fen':80,'denominator_units':1}
    expect_invalid('小时混入token附加费',api_validator('Price'),bad)
    bad=copy.deepcopy(examples['nb_hour_price']);bad['hour_rate']['denominator_units']=0
    expect_invalid('小时费率分母零',api_validator('Price'),bad)
    bad=copy.deepcopy(examples['nb_hour_price']);del bad['hour_rate']['numerator_fen']
    expect_invalid('费率缺失不得免费',api_validator('Price'),bad)
    bad=copy.deepcopy(examples['nb_hour_price']);bad['hour_rate']['numerator_fen']=9007199254740992
    expect_invalid('费率溢出JSON安全整数',api_validator('Price'),bad)
    bad=copy.deepcopy(examples['legacy_price']);bad['price_contract_version']='NB_PRICE_1'
    expect_invalid('历史价格不能冒充新价版',api_validator('Price'),bad)
    bad=copy.deepcopy(examples['miniapp_session_request']);bad['user_id']='usr_attacker'
    expect_invalid('登录不得自报主体',api_validator('SessionWrite'),bad)
    bad=copy.deepcopy(examples['miniapp_session_request']);del bad['code_verifier']
    expect_invalid('授权兑换缺挑战证明',api_validator('SessionWrite'),bad)
    bad=copy.deepcopy(examples['web_session']);bad['access_token']='x'*64
    expect_invalid('Web响应不得包含Bearer原值',api_validator('SessionView'),bad)
    bad=copy.deepcopy(examples['web_session']);del bad['csrf_token']
    expect_invalid('Web会话缺CSRF',api_validator('SessionView'),bad)

    configs = documents["config-catalog-v3.0.json"]
    if [row["id"] for row in configs["defaults"]] != [f"CFG{i:02d}" for i in range(1, 32)]:
        errors.append("配置不完整/不唯一")
    if len({row["key"] for row in configs["defaults"]}) != 31:
        errors.append("配置键重复")
    evidence_ids = set(configs["external_evidence_ids"])
    for row in configs["gated"]:
        if not set(row["required_evidence"]).issubset(evidence_ids):
            errors.append("门槛引用不存在证据")
        if row["key"].endswith("_enabled") and row["value"] is not False:
            errors.append("未证能力默认开启")
    decision_text = (WHITE / "14-决策记录配置与待验证事项.md").read_text(encoding="utf-8")
    risk_text = (WHITE / "13-商业模型运营与风险登记.md").read_text(encoding="utf-8")
    plan_text = (WHITE / "12-验收矩阵与交付计划.md").read_text(encoding="utf-8")
    for prefix, count, content in [("CFG", 31, decision_text), ("ADR", 14, decision_text),
            ("E", 15, decision_text), ("R", 22, risk_text), ("W", 16, plan_text)]:
        actual = set(re.findall(r"^\| (" + prefix + r"\d{2})\b", content, re.M))
        if actual != {f"{prefix}{i:02d}" for i in range(1, count + 1)}:
            errors.append(prefix + "目录不完整")

    reader = WHITE / "算力共享平台完整阅读版-v3.0.html"
    parser = ReaderParser()
    content = reader.read_text(encoding="utf-8")
    parser.feed(content)
    all_ids = set(parser.ids)
    if len(all_ids) != len(parser.ids):
        errors.append("HTML存在重复id")
    local_html_links = 0
    for target in parser.links:
        if re.match(r"^(https?://|mailto:)", target):
            continue
        local_html_links += 1
        if target.startswith("#"):
            if unquote(target[1:]) not in all_ids:
                errors.append("HTML内部锚点不存在：" + target)
        else:
            parsed = urlparse(target)
            if not (reader.parent / unquote(parsed.path)).exists():
                errors.append("HTML本地链接不存在：" + target)
    if parser.external_assets or re.search(r"\b(fetch\(|XMLHttpRequest|https?://[^\s]+\.(?:js|css))", content):
        errors.append("阅读器包含网络依赖")
    search = json.loads("".join(parser.index_parts))
    if any(row["id"] not in all_ids for row in search):
        errors.append("搜索索引目标不存在")
    for req in expected:
        if not any(req in row["title"] for row in search):
            errors.append("搜索索引漏需求：" + req)
    for source in sources:
        if "P11｜v3.0" not in source.read_text(encoding="utf-8"):
            errors.append(source.name + "版本不同步")

    summary = {"version": "v3.0", "date": "2026-10-29", "passed": not errors,
        "scope": "文档覆盖、设计契约结构/引用/夹具、离线HTML链接与索引；不验证密码/签名/SDK/资金/设备/产品", 
        "source_documents": len(sources), "refined_requirements": len(refined), "main_acceptance_cases": len(originals),
        "priorities": priorities, "api_paths": len(api["paths"]), "api_operations": len(operations),
        "api_schemas": len(api["components"]["schemas"]), "machine_contract_files": len(contract_files),
        "prose_contract_state_axes": len(state_sources),
        "local_json_references": reference_count, "positive_schema_fixtures": len(validators), "negative_schema_fixtures": negative_count,
        "design_config_groups": len(configs["defaults"]), "external_evidence_tasks": len(evidence_ids),
        "risk_items": 22, "work_packages": 16, "html_search_sections": len(search), "html_local_links": local_html_links,
        "external_reader_assets": len(parser.external_assets), "product_source_files": sum(p.is_file() for p in (ROOT / "dev/src").rglob("*")),
        "product_test_status": "全部未执行", "errors": errors}
    RESULT.write_text(json.dumps(summary, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    lines = ["# v3.0 白皮书与设计契约专项核验", "", "P11｜v3.0｜2026-10-29｜本报告只描述实际执行的文档/Schema/阅读器结构检查。", "",
        f"结果：{'通过' if not errors else '未通过'}。", "",
        f"总册及章节源文件{len(sources)}份，80项逐项实施规格与同号主验收追踪；控制面{len(api['paths'])}条路径/{len(operations)}个操作/{len(api['components']['schemas'])}个模型；31组D配置、15项外部门槛、22项风险、16个工作包。", "",
        f"实际执行：本地JSON引用{reference_count}处；{len(validators)}个虚构正向结构夹具、{negative_count}个反向结构夹具；HTML {len(search)}段索引与{local_html_links}个本地链接、重复ID、外部资源依赖检查。", "",
        "重要边界：示例签名/密文不是有效密码报文。拒绝错误结构不能证明真实签名、nonce、E2E、证明、内容隔离、SDK兼容、支付或真机正确；OpenAPI核验是本地结构/引用/必要字段检查，未宣称通过完整厂商兼容认证。本脚本不运行产品或连接节点；源码实施/原生验证进展另见dev/progress，80项完整产品主验收不能据此记为通过。", "",
        "原生执行：python -X utf8 scripts/核验项目白皮书.py；不调用Docker/WSL/虚拟机/模拟器或浏览器。", "",
        "[机器报告](白皮书契约核验-v3.0.json)；[阅读导航](../白皮书/00-阅读导航与需求基线.md)；[细化登记](需求细化登记表-v3.0.json)；[契约目录](../白皮书/contracts/README.md)。", ""]
    if errors:
        lines += ["问题：", "", *["- " + error for error in errors], ""]
    REPORT.write_text("\n".join(lines), encoding="utf-8")
    print(json.dumps(summary, ensure_ascii=False, indent=2))
    return int(bool(errors))


if __name__ == "__main__":
    raise SystemExit(main())
