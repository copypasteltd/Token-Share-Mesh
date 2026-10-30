"""核实R012叶子转发包补充修复；复用相同源码对照，明确保留全量复跑失败。"""
from __future__ import annotations

import hashlib
import json
from pathlib import Path
import subprocess

from 整理原生验证证据 import R012_CASES

ROOT = Path(__file__).resolve().parents[2]
EVIDENCE = ROOT / 'dev/tests/evidence'
BASE = '694f9be4bd370ac349b90295b3153fd2e18fdf88'
STATUS = 'VERIFIED_CHANGED_RELAY_WITH_UNCHANGED_BASELINE_FULL_RERUN_FAILED'
CHANGED = {'dev/src/gateway/core/json.go', 'dev/src/gateway/core/response.go', 'dev/src/gateway/core/core_test.go'}


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def events(path):
    records = {}
    for line in path.read_text(encoding='utf-8').splitlines():
        entry = json.loads(line)
        if entry.get('Action') in {'pass', 'fail', 'skip'} and entry.get('Test'):
            records[(entry['Package'], entry['Test'])] = entry
    return records


def top(records):
    return {k: v for k, v in records.items() if '/' not in k[1]}


def package_results(path):
    return {entry['Package']: entry['Action']
            for line in path.read_text(encoding='utf-8').splitlines()
            if (entry := json.loads(line)).get('Action') in {'pass', 'fail', 'skip'} and not entry.get('Test')}


def main():
    subprocess.run(['git', 'merge-base', '--is-ancestor', BASE, 'HEAD'], cwd=ROOT, check=True)
    copied = []
    for suffix in ['native-source-checks.json', 'native-go-tests.jsonl', 'native-summary.json', '验证报告.json']:
        source = f'dev/tests/evidence/R012-{suffix}'
        raw = subprocess.check_output(['git', 'show', BASE + ':' + source], cwd=ROOT)
        dest = EVIDENCE / ('R012-unchanged-baseline-' + suffix)
        dest.write_bytes(raw)
        copied.append({'path': dest.relative_to(ROOT).as_posix(), 'sha256': sha(dest), 'git_source': BASE + ':' + source})

    baseline = json.loads((EVIDENCE / 'R012-unchanged-baseline-native-source-checks.json').read_text(encoding='utf-8'))
    current = json.loads((EVIDENCE / 'R012-native-source-checks.json').read_text(encoding='utf-8'))
    if baseline['status'] != 'PASSED_WITH_LIMITED_SCOPE' or current['status'] != 'PASSED_WITH_LIMITED_SCOPE':
        raise ValueError('Both source/build checks must actually pass')
    old, new = baseline['source_sha256'], current['source_sha256']
    if set(old) != set(new) or {n for n in old if old[n] != new[n]} != CHANGED:
        raise ValueError('Reuse is limited to this exact leaf-package source change')
    if any(sha(ROOT / n) != h for n, h in new.items()):
        raise ValueError('Current source differs from current checked source')
    importing = [p.relative_to(ROOT).as_posix() for p in (ROOT / 'dev/src').rglob('*.go')
                 if not p.is_relative_to(ROOT / 'dev/src/gateway/core') and b'gateway/core' in p.read_bytes()]
    if importing:
        raise ValueError('Another package references the changed core; independent evidence cannot be reused')

    original = events(EVIDENCE / 'R012-unchanged-baseline-native-go-tests.jsonl')
    expected = {('p11.local/compute/' + p, n) for _, p, n, _ in R012_CASES}
    baseline_packages = package_results(EVIDENCE / 'R012-unchanged-baseline-native-go-tests.jsonl')
    if (set(top(original)) != expected or any(v['Action'] != 'pass' for v in original.values()) or
            any(baseline_packages.get(package) != 'pass' for package, _ in expected) or
            'fail' in baseline_packages.values()):
        raise ValueError('Immutable baseline must contain every expected passed case')
    baseline_summary = json.loads((EVIDENCE / 'R012-unchanged-baseline-native-summary.json').read_text(encoding='utf-8'))
    if baseline_summary['test_exit_code'] != 0 or not baseline_summary['temporary_instance_stopped']:
        raise ValueError('Original complete suite must pass and stop its database')

    fresh_path = EVIDENCE / 'R012-field-whitelist-after-fix.jsonl'
    fresh = events(fresh_path)
    relay = {k for k in expected if k[0] == 'p11.local/compute/gateway/core'}
    if (len(relay) != 20 or set(top(fresh)) != relay or any(v['Action'] != 'pass' for v in fresh.values()) or
            package_results(fresh_path) != {'p11.local/compute/gateway/core': 'pass'}):
        raise ValueError('All current changed relay cases must pass with no failure/skip')
    summary = json.loads((EVIDENCE / 'R012-native-summary.json').read_text(encoding='utf-8'))
    latest = events(EVIDENCE / 'R012-native-go-tests.jsonl')
    failures = [{'package': k[0], 'test': k[1]} for k, v in top(latest).items() if v['Action'] == 'fail']
    if set(top(latest)) != expected or any(v['Action'] == 'skip' for v in latest.values()):
        raise ValueError('Current failed full rerun must retain every expected test without skips')
    full_passed = sum(v['Action'] == 'pass' for v in top(latest).values())
    if not summary['temporary_instance_stopped'] or summary['test_exit_code'] != 1 or not failures:
        raise ValueError('This limited report must explicitly preserve the actual failed full rerun')
    if any(k not in latest or latest[k]['Action'] != 'pass' for k in relay):
        raise ValueError('The current full rerun must also have passed all relay cases')
    diagnostic = events(EVIDENCE / 'R012-native-contention-diagnostic.jsonl')
    diagnostic_failures = [{'package': k[0], 'test': k[1]} for k, v in top(diagnostic).items() if v['Action'] == 'fail']

    tests = [{'id': code, 'package': 'p11.local/compute/' + p, 'test': n, 'description': description,
              'result': 'PASSED_CURRENT_RELAY' if p == 'gateway/core' else 'REUSED_IDENTICAL_SOURCE_BASELINE',
              'current_full_rerun_result': latest[('p11.local/compute/' + p, n)]['Action'].upper(),
              'evidence': 'dev/tests/evidence/R012-field-whitelist-after-fix.jsonl' if p == 'gateway/core' else 'dev/tests/evidence/R012-unchanged-baseline-native-go-tests.jsonl'}
             for code, p, n, description in R012_CASES]
    report = {
        'round': 'R012', 'status': STATUS, 'passed_cases': len(relay),
        'reused_identical_source_cases': len(expected - relay), 'evidence_covered_cases': len(expected),
        'failed_cases': len(failures), 'skipped_cases': 0,
        'current_full_rerun_passed_cases': full_passed, 'current_full_rerun_total_cases': len(top(latest)),
        'current_full_rerun_status': 'FAILED', 'current_full_rerun_failures': failures,
        'current_diagnostic_failures': diagnostic_failures,
        'database_run': summary['run_id'], 'temporary_instance_stopped': True, 'virtualization_used': False,
        'product_acceptance_status': 'NOT_EXECUTED', 'baseline_commit': BASE, 'baseline_artifacts': copied,
        'reuse_conditions': {'source_manifest_same_files': True, 'only_changed_source_files': sorted(CHANGED),
                            'other_source_module_sql_json_hashes_identical': True, 'outside_core_importers': importing},
        'fresh_relay_evidence': {'path': fresh_path.relative_to(ROOT).as_posix(), 'sha256': sha(fresh_path)},
        'tests': tests,
        'limitations': [
            'This is not a successful current 134-case full run: seven existing short-deadline cases failed in the latest full rerun and six in a serialized GOMAXPROCS=2 diagnostic. Exact failures are retained.',
            'The 114 unchanged-package cases reuse immutable previously passed evidence after exact source/module/input hash and leaf-import checks. Only 20 changed-relay cases are claimed fresh here.',
            'Host load was observed but is not a proven cause of every failure. No lease, storage, cleanup or test deadline was widened; storage/expiry failures retain resources and close authority.',
            'No GPU, paid model, real payment, SDK certification, native Linux keyring, Android, content E2E or performance acceptance is claimed.',
        ],
        'development_attempts': ['dev/tests/evidence/R012-development-attempts.json',
                                 'dev/tests/evidence/R012-native-go-tests-attempt1.jsonl',
                                 'dev/tests/evidence/R012-native-go-tests-attempt2.jsonl',
                                 'dev/tests/evidence/R012-native-contention-diagnostic.jsonl',
                                 'dev/tests/evidence/R012-field-whitelist-before-fix.jsonl'],
    }
    (EVIDENCE / 'R012-验证报告.json').write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
    lines = ['# R012 源码证据与补充修复报告', '',
             f'**末次全量复跑未通过：{full_passed}项通过，{len(failures)}项失败。** 当前转发包20项另行通过；114项其他模块历史用例仅为提交`' + BASE + '`的相同源码对照，不能覆盖当前失败，不能表述为当前134项全量通过。', '',
             '初次完整R012回归134项通过并已发布；其后只修改转发包三份文件。当前Go依赖/格式/vet/六个命令构建通过，所有其他Go/SQL/JSON/模块摘要相同且其他源码没有导入转发包。普通完整证据整理器仍要求全量成功，本补充工具单独登记受限复用依据。', '',
             f'末次完整复跑中{len(failures)}项已有短时限用例失败；串行诊断另有{len(diagnostic_failures)}项失败。CPU/磁盘忙只是一次观察，不能证明每项失败原因。未放宽150毫秒租约、元数据关闭、清理CAS或断言；失败记录均保留，当前原生PG已停止，双端浏览器关闭。', '',
             '| 编号 | 内容 | 补充/历史证据依据 | 末次全量实际结果 |', '| --- | --- | --- | --- |']
    lines += [f"| {t['id']} | {t['description']} | {t['result']} | {t['current_full_rerun_result']} |" for t in tests]
    lines += ['', '本轮实际全量复跑失败清单：', '', '| 包 | 用例 |', '| --- | --- |']
    lines += [f"| {f['package']} | {f['test']} |" for f in failures]
    lines += ['', '原生HTTP后端、准入和凭据均为合成夹具；没有真实GPU、资金、SDK认证、Android、内容E2E或性能验收。', '',
              '- [当前完整事件及失败](R012-native-go-tests.jsonl)',
              '- [当前数据库退出](R012-native-summary.json)',
              '- [当前转发包20项](R012-field-whitelist-after-fix.jsonl)',
              '- [当前静态/构建/源码摘要](R012-native-source-checks.json)',
              '- [不可变相同源码对照](R012-unchanged-baseline-native-go-tests.jsonl)',
              '- [失败开发过程](R012-development-attempts.json)',
              '- [实际双端页面与未变前端构建证据](R012-frontend-checks.json)', '']
    (EVIDENCE / 'R012-验证报告.md').write_text('\n'.join(lines), encoding='utf-8')
    print(json.dumps({'status': STATUS, 'fresh_relay_passed': len(relay), 'reused_unchanged_cases': len(expected - relay), 'current_full_failures': len(failures)}, ensure_ascii=False))


if __name__ == '__main__':
    main()
