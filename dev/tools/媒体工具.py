"""安装固定录制依赖或录制本项目真实文档页面；依赖/缓存留在项目内。"""
from __future__ import annotations

import argparse
import os
from pathlib import Path
import shutil
import subprocess

ROOT = Path(__file__).resolve().parents[2]
MEDIA = ROOT / 'dev/tools/media'


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
        # Use the package directory as npm cwd: npm 10 on Windows can resolve
        # relative --prefix user requests against the caller rather than prefix.
        command = 'ci' if (MEDIA / 'package-lock.json').is_file() else 'install'
        return subprocess.call([npm, command, '--ignore-scripts', '--no-audit', '--no-fund'], cwd=MEDIA, env=env)
    node = shutil.which('node')
    if not node:
        parser.error('Node.js required on PATH')
    env['P11_PYTHON'] = os.sys.executable
    return subprocess.call([node, str(MEDIA / 'capture.mjs')], cwd=ROOT, env=env)


if __name__ == '__main__':
    raise SystemExit(main())

