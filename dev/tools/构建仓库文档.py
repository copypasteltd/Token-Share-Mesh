"""从双语人工模板及真实实施登记同时生成根README，避免状态漂移。"""
from __future__ import annotations

import argparse
from collections import Counter
import json
from pathlib import Path
from 整理原生验证证据 import R003_CASES, R004_CASES, R005_CASES, R006_CASES, R007_CASES, R008_CASES, R009_CASES

ROOT = Path(__file__).resolve().parents[2]


def outputs():
    plan = json.loads((ROOT / 'dev/contracts/development-plan-v1.0.json').read_text(encoding='utf-8'))
    progress = json.loads((ROOT / 'dev/progress/implementation-status.json').read_text(encoding='utf-8'))
    native = json.loads((ROOT / f'dev/tests/evidence/{progress["round"]}-验证报告.json').read_text(encoding='utf-8'))
    counts = Counter(task['implementation_status'] for task in progress['tasks'])
    latest = progress['rounds'][-1]
    if len(progress['tasks']) != 56 or len({t['id'] for t in progress['tasks']}) != 56:
        raise ValueError('完整56任务登记缺失')
    expected={'R003':R003_CASES,'R004':R004_CASES,'R005':R005_CASES,'R006':R006_CASES,'R007':R007_CASES,'R008':R008_CASES,'R009':R009_CASES}[progress['round']]
    if native['status'] != 'PASSED_FOR_CONTROL_SERVICES_ONLY' or native['passed_cases'] != len(expected) or native['round']!=progress['round']:
        raise ValueError('当前实际用例登记与README声明不一致')
    common = [
        f"{len(plan['modules'])} modules / {len(plan['tasks'])} tasks / {sum(len(t['steps']) for t in plan['tasks'])} steps / {len(plan['constraints'])} constraints",
        f"{counts['IN_PROGRESS']} in progress · {counts['NOT_STARTED']} not started · {counts['CONDITIONAL']} conditional · {counts['DEFERRED']} deferred",
        latest['id'] + ' · ' + latest['verification_status'],
        str(native['passed_cases']) + ' passed · control services scope only',
        '80 NOT_EXECUTED',
        progress['goal_status'],
    ]
    chinese = [
        f"{len(plan['modules'])}模块 / {len(plan['tasks'])}任务 / {sum(len(t['steps']) for t in plan['tasks'])}子项 / {len(plan['constraints'])}约束",
        f"{counts['IN_PROGRESS']}项进行中 · {counts['NOT_STARTED']}项待开发 · {counts['CONDITIONAL']}项条件能力 · {counts['DEFERRED']}项延后",
        common[2], str(native['passed_cases'])+'项通过，仅控制应用服务范围', '80项全部未执行', progress['goal_status'],
    ]
    files = {}
    for language, filename, values, labels in [
        ('en', 'README.md', common, ['Development baseline', 'Implementation tasks', 'Latest source round', 'Native test evidence', 'Full product acceptance', 'Source goal']),
        ('zh-CN', 'README.zh-CN.md', chinese, ['开发基线', '实施任务', '最新源码轮次', '原生测试证据', '完整产品验收', '源码目标']),
    ]:
        template = (ROOT / f'dev/repository/README.{language}.md.in').read_text(encoding='utf-8')
        if template.count('{{status_table}}') != 1:
            raise ValueError('状态表模板必须唯一')
        table = '| ' + ('Item | Evidence/status' if language == 'en' else '项目 | 证据/状态') + ' |\n| --- | --- |\n'
        table += ''.join(f'| {label} | {value} |\n' for label, value in zip(labels, values))
        body = template.replace('{{status_table}}', table.rstrip())
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
