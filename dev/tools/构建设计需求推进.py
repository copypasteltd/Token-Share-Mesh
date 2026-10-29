"""Build and check the NB requirement increment; never execute product, devices or funds."""
from __future__ import annotations

import argparse
from fractions import Fraction
import hashlib
import json
from pathlib import Path
import re
from urllib.parse import unquote

ROOT = Path(__file__).resolve().parents[2]
DOC = ROOT / 'docs/需求推进/NB-1.0'
SOURCE = DOC / '需求增量登记-NB-R1.json'
MATRIX = DOC / '06-需求追踪与验收矩阵.md'
REPORT = DOC / '核验报告-NB-R1.json'


def sha(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def table(headers, rows):
    escape = lambda x: str(x).replace('|', '\\|').replace('\n', '<br>')
    return '\n'.join('| ' + ' | '.join(map(escape, row)) + ' |'
                     for row in [headers, ['---'] * len(headers), *rows]) + '\n'


def validate(data):
    plan = json.loads((ROOT / 'dev/contracts/development-plan-v1.0.json').read_text(encoding='utf-8'))
    known_tasks = {row['id'] for row in plan['tasks']}
    known_req = {row['requirement_id'] for row in plan['requirements']}
    valid_pages = {f'UI-P0{i}' for i in range(1, 6)} | {f'NB-S{i:02d}' for i in range(1, 11)}
    rows = data['requirements']
    assert [r['id'] for r in rows] == [f'NB-R{i:03d}' for i in range(1, 49)], 'Requirement IDs must be stable and unique'
    assert data['scope'] in {'REQUIREMENTS_ONLY_SOURCE_PAUSED','REQUIREMENTS_WITH_PARTIAL_SOURCE'}
    assert data['runtime_acceptance'] == 'NOT_EXECUTED'
    seen_pages = set()
    for r in rows:
        assert r['status'] == 'DEFINED' and r['acceptance_status'] == 'NOT_EXECUTED'
        assert r['requirements'] and set(r['requirements']) <= known_req, r['id']
        assert r['tasks'] and set(r['tasks']) <= known_tasks, r['id']
        assert r['pages'] and set(r['pages']) <= valid_pages, r['id']
        assert all(r[k].strip() for k in ['group', 'title', 'rule', 'positive', 'negative'])
        seen_pages.update(r['pages'])
    assert seen_pages == valid_pages, 'All five pages and ten auxiliary views need coverage'
    assert [g['id'] for g in data['gaps']] == [f'GAP-NB{i:02d}' for i in range(1, 7)]
    for g in data['gaps']:
        assert set(g['tasks']) <= known_tasks
        assert g['status'] in {'REQUIREMENTS_DEFINED_NOT_IMPLEMENTED','PARTIALLY_IMPLEMENTED'}
        if g['status']=='PARTIALLY_IMPLEMENTED':
            assert g.get('implementation_round') in {'R003','R004','R005'}
            progress=json.loads((ROOT/'dev/progress/implementation-status.json').read_text(encoding='utf-8'))
            rounds={r['id']:r for r in progress['rounds']}
            assert rounds[g['implementation_round']]['verification_status']=='PASSED_FOR_CONTROL_SERVICES_ONLY'
            path=(ROOT/g['implementation_ref']).resolve()
            assert path.is_relative_to(ROOT) and path.is_file()
    return plan


def render(data):
    lines = ['# 48项需求与96个验收场景', '',
             'NB-RD06｜NB-R1｜2026-10-30。由需求增量登记-NB-R1.json生成，不手改。', '',
             'NB-R是原80项F/T下的设计细化子项，不新增收费功能或主验收；每项两个场景，均NOT_EXECUTED。DEV编号沿既有56项，不能以本表改变源码进度。', '',
             '[阅读入口](README.md) · [登记源](需求增量登记-NB-R1.json) · [核验报告](核验报告-NB-R1.json)', '',
             table(['子需求', '内容/规则', '页面', '原功能/主验收', '承接任务'], [
                 [r['id'], r['title'] + '：' + r['rule'], '、'.join(r['pages']),
                  '、'.join(x + '/' + x.replace('-F', '-T') for x in r['requirements']), '、'.join(r['tasks'])]
                 for r in data['requirements']]), '', '## 逐项验收', '']
    for group in dict.fromkeys(r['group'] for r in data['requirements']):
        lines += ['### ' + group, '', table(['场景编号', '通过条件', '状态'], [
            [r['id'] + '-' + suffix, r[key], 'NOT_EXECUTED']
            for r in data['requirements'] if r['group'] == group
            for suffix, key in [('P', 'positive'), ('N', 'negative')]])]
    lines += ['', '## 六项契约缺口', '', table(['编号', '内容', '承接任务', '状态'], [
        [g['id'], g['title'], '、'.join(g['tasks']), g['status']] for g in data['gaps']]), '',
        '详细字段与关闭要求见[契约缺口](03-字段动作与契约缺口.md)，实施顺序见[工作包](05-实施次序与验收门槛.md)。']
    return '\n'.join(lines) + '\n'


def check(data, expected):
    assert MATRIX.read_text(encoding='utf-8') == expected, 'Generated matrix is stale'
    files = [DOC / 'README.md', *sorted(DOC.glob('[0-9][0-9]-*.md'))]
    assert len(files) == 7
    broken = []
    link_count = 0
    for source in files:
        for target in re.findall(r'\]\(([^)]+)\)', source.read_text(encoding='utf-8')):
            if target.startswith(('https://', 'http://', '#', 'mailto:')):
                continue
            local = unquote(target.split('#', 1)[0].strip('<>'))
            if not local:
                continue
            link_count += 1
            path = (source.parent / local).resolve()
            if not path.is_relative_to(ROOT) or not path.exists():
                broken.append({'source': source.name, 'target': target})
    assert not broken, broken
    # Independently evaluate published arithmetic, with exact integer/rational math.
    ceil = lambda x: -(-x.numerator // x.denominator)
    examples = {
        'gpu_half_hour_fen': ceil(Fraction(360 * 1800000, 3600000)),
        'gpu_ten_minutes_fen': ceil(Fraction(360 * 600000, 3600000)),
        'gpu_10001_ms_fen': ceil(Fraction(360 * 10001, 3600000)),
        'llm_total_fen': ceil(Fraction(1000000 * 80 + 250000 * 160, 1000000)),
        'llm_small_request_fen': ceil(Fraction(1000 * 80 + 500 * 160, 1000000)),
        'gross180_fee_fen': 180 * 500 // 10000,
        'gross180_net_fen': 180 - 180 * 500 // 10000,
        'gross60_net_fen': 60 - 60 * 500 // 10000,
        'retained120_fee_fen': 120 * 500 // 10000,
        'retained120_net_fen': 120 - 120 * 500 // 10000,
        'rate350_legacy_compatible': 350 % 60 == 0,
    }
    assert list(examples.values()) == [180, 60, 2, 120, 1, 9, 171, 57, 6, 114, False]
    design = json.loads((ROOT / 'dev/design/新粗野主义-v1.0/示例数据.json').read_text(encoding='utf-8'))
    assert design['gpu']['hourly_price_fen'] == 360
    assert design['gpu']['sample_total_fen'] == examples['gpu_half_hour_fen']
    assert design['provider']['estimated_net_fen'] == examples['gross60_net_fen']
    return {'status': 'STATIC_REQUIREMENT_CHECKS_PASSED', 'version': data['version'],
            'scope': data['verification_scope'], 'runtime_acceptance': 'NOT_EXECUTED',
            'requirement_count': len(data['requirements']), 'acceptance_scenarios': len(data['requirements']) * 2,
            'primary_pages': 5, 'auxiliary_views': 10, 'contract_gaps': len(data['gaps']),
            'referenced_original_requirements': len({x for r in data['requirements'] for x in r['requirements']}),
            'referenced_development_tasks': len({x for r in data['requirements'] for x in r['tasks']}),
            'local_links_checked': link_count, 'broken_links': broken,
            'examples_fen': examples, 'registry_sha256': sha(SOURCE),
            'documents': [{'path': p.relative_to(ROOT).as_posix(), 'sha256': sha(p)} for p in files]}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--check', action='store_true', help='Check without rebuilding the matrix')
    args = parser.parse_args()
    data = json.loads(SOURCE.read_text(encoding='utf-8'))
    validate(data)
    expected = render(data)
    if not args.check:
        MATRIX.write_text(expected, encoding='utf-8')
    # The report is a link target during the first build as well.
    if not REPORT.exists():
        REPORT.write_text('{}\n', encoding='utf-8')
    report = check(data, expected)
    REPORT.write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
    print(json.dumps({k: v for k, v in report.items() if k not in ['documents', 'examples_fen']}, ensure_ascii=False))
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
