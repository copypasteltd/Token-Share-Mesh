"""用户指定的全量代码优先阶段；验收状态不冒充代码完成。"""
from __future__ import annotations
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
PLAN = ROOT / 'dev/progress/code-first-plan.json'

def require_test_phase():
    if not PLAN.is_file():
        raise RuntimeError('缺少全量代码阶段登记；先恢复用户确认的开发范围。')
    plan = json.loads(PLAN.read_text(encoding='utf-8'))
    pending = [s['id'] for t in plan['tasks'] if t['id'] not in plan['deferred_task_ids']
               for s in t['steps'] if s['code_status'] != 'COMPLETE' or not s['implementation_refs']]
    if pending or plan['phase'] not in {'BATCH_VALIDATION', 'BATCH_DEBUGGING', 'ACCEPTANCE'}:
        raise RuntimeError('按2026-11-06用户要求，先补齐全部待办生产代码，再统一测试调试。'
                           f'当前阶段 {plan["phase"]}；仍有 {len(pending)} 个代码事项未完成。')

if __name__ == '__main__':
    plan = json.loads(PLAN.read_text(encoding='utf-8'))
    counts = {}
    for task in plan['tasks']:
        for step in task['steps']:
            counts[step['code_status']] = counts.get(step['code_status'], 0) + 1
    print(json.dumps({'phase': plan['phase'], 'code_steps': counts,
                      'validation': 'NOT_STARTED', 'policy': 'FULL_CODE_BEFORE_BATCH_TESTS'}, ensure_ascii=False))
