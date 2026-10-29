"""项目内安装/运行固定文档工具，不修改系统 Python 或全局配置。"""
from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[2]
PACKAGES = ROOT / 'runtime/tooling/docs-python'
SCRIPTS = {
    'build-whitepaper': 'scripts/构建项目白皮书.py',
    'build-technical': 'scripts/构建技术开发文档.py',
    'build-development': 'dev/tools/构建开发细则.py',
    'progress': 'dev/tools/更新开发进度.py',
    'check-requirements': 'scripts/核验需求文档.py',
    'check-whitepaper': 'scripts/核验项目白皮书.py',
    'check-technical': 'scripts/核验技术开发文档.py',
    'check-development': 'dev/tools/核验开发细则.py',
}


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=['bootstrap', 'build', 'check', *SCRIPTS])
    args = parser.parse_args()
    # Git does not preserve empty future module directories. Recreate only the
    # registered project-local plan paths, without adding fake source files.
    plan = json.loads((ROOT / 'dev/contracts/development-plan-v1.0.json').read_text(encoding='utf-8'))
    directories = {entry['path'] for entry in plan['paths']}
    directories.update(name for module in plan['modules'] for name in module['planned_packages'])
    for name in directories:
        target = (ROOT / name).resolve()
        if not target.is_relative_to(ROOT) or name.replace('\\', '/').split('/')[0] not in {'dev', 'docs', 'scripts', 'runtime', 'assets'}:
            raise ValueError('Registered directory outside the approved project')
        target.mkdir(parents=True, exist_ok=True)
    env = os.environ.copy()
    env['PYTHONUTF8'] = '1'
    env['PYTHONDONTWRITEBYTECODE'] = '1'
    env['PYTHONNOUSERSITE'] = '1'
    env['PYTHONPATH'] = str(PACKAGES)
    for key, directory in [('PIP_CACHE_DIR', 'runtime/cache/pip'), ('TMP', 'runtime/tmp/docs'), ('TEMP', 'runtime/tmp/docs')]:
        path = ROOT / directory
        path.mkdir(parents=True, exist_ok=True)
        env[key] = str(path)
    if args.action == 'bootstrap':
        PACKAGES.mkdir(parents=True, exist_ok=True)
        return subprocess.call([sys.executable, '-X', 'utf8', '-m', 'pip', 'install', '--disable-pip-version-check', '--only-binary=:all:', '--target', str(PACKAGES), '-r', str(ROOT / 'dev/requirements-docs.txt')], cwd=ROOT, env=env)
    if not PACKAGES.is_dir():
        parser.error('Run bootstrap first; packages belong in this project runtime directory.')
    actions = {
        'build': ['build-whitepaper', 'build-technical', 'build-development', 'progress'],
        'check': ['check-requirements', 'check-whitepaper', 'check-technical', 'check-development'],
    }.get(args.action, [args.action])
    for action in actions:
        result = subprocess.call([sys.executable, '-X', 'utf8', str(ROOT / SCRIPTS[action])], cwd=ROOT, env=env)
        if result:
            return result
    return 0


if __name__ == '__main__':
    raise SystemExit(main())

