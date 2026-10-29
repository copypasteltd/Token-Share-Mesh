"""仅核验P11需求的结构/追踪/本地链接，不运行产品、安全、支付或设备测试。"""

from __future__ import annotations

import json
import re
from collections import Counter
from pathlib import Path
from urllib.parse import unquote


ROOT = Path(__file__).resolve().parents[1]
DOCS = ROOT / "docs"
VERSION = "v3.0"
REQUIREMENT_COUNT = 80
REPORT = DOCS / "评审" / f"文档结构核验-{VERSION}.md"
REGISTRY = DOCS / "评审" / f"需求登记表-{VERSION}.json"


def main() -> int:
    errors: list[str] = []
    rows: list[dict] = []
    source = DOCS / "02-功能需求与用户流程.md"
    cases_source = DOCS / "05-验收测试与实施计划.md"
    cases = re.findall(
        r"^\| (P11-T\d{2}) \| (P11-F\d{2}) \| (.+?) \|$",
        cases_source.read_text(encoding="utf-8"), re.M,
    )
    case_map = {req_id: (case_id, condition) for case_id, req_id, condition in cases}
    requirements = re.findall(
        r"^### (P11-F\d{2}) (.+?)\n(.*?)(?=^### |\Z)",
        source.read_text(encoding="utf-8"), re.M | re.S,
    )
    expected = {f"P11-F{number:02d}" for number in range(1, REQUIREMENT_COUNT + 1)}
    if {req_id for req_id, _, _ in requirements} != expected or len(requirements) != REQUIREMENT_COUNT:
        errors.append(f"功能编号应为唯一且连续的P11-F01—F{REQUIREMENT_COUNT:02d}")
    if set(case_map) != expected or len(cases) != REQUIREMENT_COUNT:
        errors.append(f"主验收应为唯一P11-T01—T{REQUIREMENT_COUNT:02d}，逐项覆盖功能")
    for req_id, title, body in requirements:
        priority = re.search(r"优先级：(P[012](?:-AUTH)?)。", body)
        acceptance = re.search(r"^验收：(.+)$", body, re.M)
        case = case_map.get(req_id)
        if not priority or not acceptance or not case:
            errors.append(f"{req_id}缺优先级、验收条件或用例")
            continue
        if case[0] != req_id.replace("-F", "-T"):
            errors.append(f"{req_id}同号主用例错误")
        rows.append({
            "id": req_id, "title": title, "priority": priority.group(1),
            "acceptance": acceptance.group(1), "test_case": case[0],
            "test_condition": case[1], "source": "docs/" + source.name,
            "test_source": "docs/" + cases_source.name,
            "implementation_status": "设计", "test_execution_status": "未执行",
        })
    mains = sorted(DOCS.glob("[0-9][0-9]-*.md"))
    if len(mains) != 7:
        errors.append("应有7份主文档")
    for path in mains:
        if f"P11｜{VERSION}" not in path.read_text(encoding="utf-8"):
            errors.append(f"{path.name}主文档版本未同步")
    must_exist = [
        "docs/交易/C2C交易与市场规则.md", "docs/评审/v2.1-C2C纠偏与待决事项.md",
        "docs/协议/统一API与本地加密网关需求.md",
        "docs/资料/Sub2API与双协议接入核查-2026-10-29.md",
        "docs/评审/v2.2-双协议与加密网关变更.md",
        "docs/评审/v3.0-完整需求深化与白皮书变更.md",
        "docs/白皮书/00-阅读导航与需求基线.md", "docs/白皮书/15-需求逐项实施规格.md",
        "docs/白皮书/算力共享平台项目白皮书-v3.0.md", "docs/白皮书/算力共享平台完整阅读版-v3.0.html",
        "docs/白皮书/contracts/openapi-control-v3.0.json", "docs/白皮书/contracts/cipher-frame-v3.0.schema.json",
        "docs/评审/需求细化登记表-v3.0.json",
        "docs/模式/00-三模式总览与边界.md", "docs/模式/01-端对端加密模式需求.md",
        "docs/模式/02-显卡租赁模式需求.md", "docs/模式/03-明文模式需求.md",
        "docs/供给端/00-供给端能力矩阵.md", "docs/供给端/01-Windows与Linux供给端需求.md",
        "docs/供给端/02-Android与LMA供给端需求.md", "dev/src/client/windows", "dev/src/client/linux",
        "dev/src/client/android", "dev/src/client/protocol", "dev/src/client/adapters/comfyui",
        "dev/src/client/adapters/lmstudio", "dev/src/client/adapters/lma",
        "dev/src/gateway/platform", "dev/src/gateway/core", "dev/src/gateway/cipher-relay",
        "dev/src/client/crypto-gateway", "dev/src/client/protocol/openai",
        "dev/src/client/protocol/anthropic", "dev/src/client/protocol/envelope", "dev/src/client/android/gateway",
    ]
    for target in must_exist:
        if not (ROOT / target).exists():
            errors.append(f"缺少路径：{target}")
    overview = (DOCS / "模式" / "00-三模式总览与边界.md").read_text(encoding="utf-8")
    for identifier in ("E2E", "GPU_RENTAL", "PLAINTEXT", "PLATFORM_BLIND", "ATTESTED_CONFIDENTIAL", "ANDROID_DEVICE"):
        if identifier not in overview:
            errors.append(f"能力矩阵缺少{identifier}")
    # Ensure the generated report also participates in local-link checks.
    REPORT.write_text(f"# P11 {VERSION}文档结构核验\n", encoding="utf-8")
    markdown = sorted(DOCS.rglob("*.md")) + [ROOT / "README.md", ROOT / "AGENTS.md"]
    link_count = 0
    for path in markdown:
        content = path.read_text(encoding="utf-8")
        if not content.startswith("# ") or "\ufffd" in content:
            errors.append(f"{path.relative_to(ROOT)}标题/编码异常")
        if sum(line.startswith("```") for line in content.splitlines()) % 2:
            errors.append(f"{path.relative_to(ROOT)}代码围栏不配对")
        for target in re.findall(r"\[[^\]\n]+\]\(([^)]+)\)", content):
            if re.match(r"^(https?://|#|mailto:)", target):
                continue
            link_count += 1
            resolved = path.parent / unquote(target.split("#", 1)[0].strip("<>"))
            if not resolved.exists():
                errors.append(f"{path.relative_to(ROOT)}本地链接不存在：{target}")
    priorities = dict(sorted(Counter(row["priority"] for row in rows).items()))
    summary = {
        "scope": "P11 only", "version": VERSION, "business_model": "C2C闲置设备用户交易市场", "date": "2026-10-29",
        "data_protocols": ["OpenAI Chat Completions", "OpenAI Responses", "Anthropic Messages"],
        "gateway_design": "Sub2API类明文内核＋买卖端本地/LAN加解密＋平台密文中转；未实现",
        "passed": not errors, "main_documents": len(mains), "markdown_files": len(markdown),
        "functional_requirements": len(requirements), "acceptance_cases": len(cases),
        "local_links": link_count, "priorities": priorities,
        "product_source_files": sum(path.is_file() for path in (ROOT / "dev/src").rglob("*")),
        "verification_kind": "文档结构；软件/密码安全/支付/硬件及Android真机均未验收",
        "errors": errors,
    }
    REGISTRY.write_text(json.dumps({**summary, "requirements": rows}, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    lines = [
        f"# P11 {VERSION}文档结构核验", "", "2026-10-29｜范围仅为算力共享平台C2C需求；不以其他项目或历史档案状态替代本项目结果。", "",
        f"结果：{'通过' if not errors else '未通过'}。7份主文档版本、{REQUIREMENT_COUNT}项功能/同号主验收、专项和端目录、本地链接、UTF-8和代码围栏核对。", "",
        f"实际：{len(mains)}份主文档，{len(markdown)}份Markdown，{len(requirements)}项功能，{len(cases)}项主验收，{link_count}个本地链接。", "",
        "优先级：" + "、".join(f"{key}={value}" for key, value in priorities.items()) + "。", "",
        "来源核对了官方协议、Sub2API main浏览快照及锁定LMA提交；Sub2API未冻结commit。源码实施与原生测试另见 dev/progress；本脚本仅文档检查，不运行SDK/推理/密码/支付或设备，不把结构通过视为产品验收。", "",
        "执行：在本项目根目录原生运行 python -X utf8 scripts/核验需求文档.py；不调度虚拟化或模拟器。", "",
        f"结构化追踪：[需求登记表-{VERSION}.json](需求登记表-{VERSION}.json)；[主验收](../05-验收测试与实施计划.md)；[C2C交易规则](../交易/C2C交易与市场规则.md)。", "",
        "完整深化：[白皮书与阅读导航](../白皮书/00-阅读导航与需求基线.md)、[逐项实施规格](../白皮书/15-需求逐项实施规格.md)、[设计契约](../白皮书/contracts/README.md)；本报告只验证文档结构，不证明双协议、安全或商业可行性。", "",
    ]
    if errors:
        lines.extend(["问题：", "", *[f"- {error}" for error in errors], ""])
    REPORT.write_text("\n".join(lines), encoding="utf-8")
    print(json.dumps(summary, ensure_ascii=False, indent=2))
    return int(bool(errors))


if __name__ == "__main__":
    raise SystemExit(main())
