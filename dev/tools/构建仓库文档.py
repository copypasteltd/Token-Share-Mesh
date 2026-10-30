"""从双语人工模板及真实实施登记同时生成根README，避免状态漂移。"""
from __future__ import annotations

import argparse
from collections import Counter
import json
from pathlib import Path
from 整理原生验证证据 import R003_CASES, R004_CASES, R005_CASES, R006_CASES, R007_CASES, R008_CASES, R009_CASES, R010_CASES, R011_CASES, R012_CASES, R013_CASES, R014_CASES, R015_CASES, R016_CASES, R017_CASES, R018_CASES, R019_CASES

ROOT = Path(__file__).resolve().parents[2]


def outputs():
    plan = json.loads((ROOT / 'dev/contracts/development-plan-v1.0.json').read_text(encoding='utf-8'))
    progress = json.loads((ROOT / 'dev/progress/implementation-status.json').read_text(encoding='utf-8'))
    native = json.loads((ROOT / f'dev/tests/evidence/{progress["round"]}-验证报告.json').read_text(encoding='utf-8'))
    counts = Counter(task['implementation_status'] for task in progress['tasks'])
    latest = progress['rounds'][-1]
    if len(progress['tasks']) != 56 or len({t['id'] for t in progress['tasks']}) != 56:
        raise ValueError('完整56任务登记缺失')
    expected={'R003':R003_CASES,'R004':R004_CASES,'R005':R005_CASES,'R006':R006_CASES,'R007':R007_CASES,'R008':R008_CASES,'R009':R009_CASES,'R010':R010_CASES,'R011':R011_CASES,'R012':R012_CASES,'R013':R013_CASES,'R014':R014_CASES,'R015':R015_CASES,'R016':R016_CASES,'R017':R017_CASES,'R018':R018_CASES,'R019':R019_CASES}[progress['round']]
    limited = progress['round'] == 'R012' and native['status'] == 'VERIFIED_CHANGED_RELAY_WITH_UNCHANGED_BASELINE_FULL_RERUN_FAILED'
    if limited:
        expected_ids = {item[0] for item in expected}
        tests = native.get('tests', [])
        if (native['round'] != progress['round'] or latest['verification_status'] != native['status'] or
                native['passed_cases'] != 20 or native.get('reused_identical_source_cases') != 114 or
                native.get('evidence_covered_cases') != len(expected) or native.get('failed_cases') != 7 or
                native.get('current_full_rerun_status') != 'FAILED' or
                native.get('current_full_rerun_passed_cases') != 127 or native.get('current_full_rerun_total_cases') != 134 or
                len(native.get('current_full_rerun_failures', [])) != 7 or
                len(native.get('current_diagnostic_failures', [])) != 6 or
                native.get('baseline_commit') != '694f9be4bd370ac349b90295b3153fd2e18fdf88' or
                len(tests) != len(expected_ids) or {item['id'] for item in tests} != expected_ids or
                Counter(item['result'] for item in tests) != Counter(PASSED_CURRENT_RELAY=20, REUSED_IDENTICAL_SOURCE_BASELINE=114)):
            raise ValueError('受限补充证据必须保留当前全量失败及精确复用边界')
        native_en = '20 targeted relay cases passed; latest full rerun: 127 passed / 7 FAILED; 114 baseline cases are historical only'
        native_zh = '当前转发20项通过；末次全量127通过/7失败；114项对照仅为历史依据'
    else:
        if native['status'] != ('PASSED_FOR_CONTROL_AND_RELAY_SUBSET_ONLY' if progress['round'] in {'R012','R013','R014','R015','R016','R017','R018','R019'} else 'PASSED_FOR_CONTROL_SERVICES_ONLY') or native['passed_cases'] != len(expected) or native['round']!=progress['round']:
            raise ValueError('当前实际用例登记与README声明不一致')
        native_en = str(native['passed_cases']) + (' passed · control/native/relay source subsets only' if progress['round'] in {'R012','R013','R014','R015','R016','R017','R018','R019'} else ' passed · control services scope only')
        native_zh = str(native['passed_cases'])+('项通过，仅控制/原生/转发源码子集' if progress['round'] in {'R012','R013','R014','R015','R016','R017','R018','R019'} else '项通过，仅控制应用服务范围')
    common = [
        f"{len(plan['modules'])} modules / {len(plan['tasks'])} tasks / {sum(len(t['steps']) for t in plan['tasks'])} steps / {len(plan['constraints'])} constraints",
        f"{counts['IN_PROGRESS']} in progress · {counts['NOT_STARTED']} not started · {counts['CONDITIONAL']} conditional · {counts['DEFERRED']} deferred",
        latest['id'] + ' · ' + latest['verification_status'],
        native_en,
        '80 NOT_EXECUTED',
        progress['goal_status'],
    ]
    chinese = [
        f"{len(plan['modules'])}模块 / {len(plan['tasks'])}任务 / {sum(len(t['steps']) for t in plan['tasks'])}子项 / {len(plan['constraints'])}约束",
        f"{counts['IN_PROGRESS']}项进行中 · {counts['NOT_STARTED']}项待开发 · {counts['CONDITIONAL']}项条件能力 · {counts['DEFERRED']}项延后",
        common[2], native_zh, '80项全部未执行', progress['goal_status'],
    ]
    files = {}
    for language, filename, values, labels in [
        ('en', 'README.md', common, ['Development baseline', 'Implementation tasks', 'Latest source round', 'Native test evidence', 'Full product acceptance', 'Source goal']),
        ('zh-CN', 'README.zh-CN.md', chinese, ['开发基线', '实施任务', '最新源码轮次', '原生测试证据', '完整产品验收', '源码目标']),
    ]:
        template = (ROOT / f'dev/repository/README.{language}.md.in').read_text(encoding='utf-8')
        if template.count('{{status_table}}') != 1:
            raise ValueError('状态表模板必须唯一')
        if '{{current_media_round}}' not in template:
            raise ValueError('产品媒体必须引用当前轮次模板')
        table = '| ' + ('Item | Evidence/status' if language == 'en' else '项目 | 证据/状态') + ' |\n| --- | --- |\n'
        table += ''.join(f'| {label} | {value} |\n' for label, value in zip(labels, values))
        body = template.replace('{{status_table}}', table.rstrip()).replace('{{current_media_round}}', progress['round'])
        first, rest = body.split('\n', 1)
        body = first + '\n\n<!-- Generated from dev/repository/README.' + language + '.md.in; run dev/tools/构建仓库文档.py. -->\n' + rest
        files[ROOT / filename] = body.encode('utf-8')
    return files


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--check', action='store_true')
    args = parser.parse_args()
    for path, expected in outputs().items():
        if args.check:
            if not path.is_file() or path.read_bytes() != expected:
                raise ValueError('双语README需重建：' + path.name)
        else:
            path.write_bytes(expected)
    print('Bilingual READMEs ' + ('verified' if args.check else 'generated') + ' from the same implementation registry')


if __name__ == '__main__':
    main()
