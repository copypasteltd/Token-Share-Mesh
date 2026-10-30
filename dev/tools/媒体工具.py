"""安装固定录制依赖或录制本项目真实文档页面；依赖/缓存留在项目内。"""
from __future__ import annotations

import argparse
import os
from pathlib import Path
import shutil
import subprocess

ROOT = Path(__file__).resolve().parents[2]
MEDIA = ROOT / 'dev/tools/media'
WORKSPACE = ROOT / 'runtime/media/workspace'


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=['bootstrap', 'capture'])
    args = parser.parse_args()
    env = os.environ.copy()
    cache = ROOT / 'runtime/cache/npm'
    temporary = ROOT / 'runtime/tmp/readme-browser'
    cache.mkdir(parents=True, exist_ok=True)
    temporary.mkdir(parents=True, exist_ok=True)
    env['npm_config_cache'] = str(cache)
    env['TEMP'] = env['TMP'] = str(temporary)
    if args.action == 'bootstrap':
        npm = shutil.which('npm.cmd' if os.name == 'nt' else 'npm')
        if not npm:
            parser.error('Node.js/npm required on PATH')
        WORKSPACE.mkdir(parents=True, exist_ok=True)
        for name in ['package.json', 'package-lock.json']:
            shutil.copyfile(MEDIA / name, WORKSPACE / name)
        # Use the explicit project runtime cwd; dependencies never belong in dev.
        return subprocess.call([npm, 'ci', '--ignore-scripts', '--no-audit', '--no-fund'], cwd=WORKSPACE, env=env)
    node = shutil.which('node')
    if not node:
        parser.error('Node.js required on PATH')
    if not (WORKSPACE / 'node_modules/playwright-core/package.json').is_file():
        parser.error('Run bootstrap first; reader dependencies belong in runtime/media/workspace.')
    env['P11_PYTHON'] = os.sys.executable
    return subprocess.call([node, str(MEDIA / 'capture.mjs')], cwd=ROOT, env=env)


if __name__ == '__main__':
    raise SystemExit(main())

