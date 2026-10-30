"""原生前端工作区：受版本锁约束的源码副本、依赖、缓存和输出均留在项目 runtime。"""
from __future__ import annotations
import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[2]
SOURCE = ROOT / 'dev/src'
STAGE = ROOT / 'runtime/frontend/workspace'
DIRECTORIES = ['frontend/packages/core', 'miniapp', 'web']

def stage():
    STAGE.mkdir(parents=True, exist_ok=True)
    selected = [SOURCE / 'package.json', SOURCE / 'tsconfig.base.json']
    if (SOURCE / 'package-lock.json').is_file(): selected.append(SOURCE / 'package-lock.json')
    for directory in DIRECTORIES:
        selected.extend(p for p in (SOURCE / directory).rglob('*') if p.is_file() and not any(x in p.parts for x in ('node_modules', 'dist', '.taro')))
    wanted = {p.relative_to(SOURCE).as_posix() for p in selected}
    # Remove only stale staged source files, never dependencies or arbitrary paths.
    for directory in DIRECTORIES:
        for p in (STAGE / directory).rglob('*'):
            if p.is_file() and 'node_modules' not in p.parts and p.relative_to(STAGE).as_posix() not in wanted:
                p.unlink()
    hashes = {}
    for p in selected:
        relative = p.relative_to(SOURCE)
        target = STAGE / relative
        target.parent.mkdir(parents=True, exist_ok=True)
        raw = p.read_bytes()
        target.write_bytes(raw)
        hashes['dev/src/' + relative.as_posix()] = hashlib.sha256(raw).hexdigest()
    env = os.environ.copy()
    for option, sub in [('npm_config_cache', 'cache/npm'), ('TEMP', 'tmp/frontend'), ('TMP', 'tmp/frontend')]:
        directory = ROOT / 'runtime' / sub
        directory.mkdir(parents=True, exist_ok=True)
        env[option] = str(directory)
    env['P11_PROJECT_ROOT'] = str(ROOT)
    env['NODE_ENV'] = 'production'
    env['TARO_ENV'] = ''
    return env, hashes

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=['bootstrap', 'lock', 'check', 'test', 'build', 'alipay', 'h5', 'web'])
    args = parser.parse_args()
    npm = shutil.which('npm.cmd' if os.name == 'nt' else 'npm')
    if not npm: parser.error('Node.js 22.14.x and npm 10.9.x required')
    env, hashes = stage()
    logdir = ROOT / 'runtime/frontend/logs'
    logdir.mkdir(parents=True, exist_ok=True)
    run_id = datetime.now(timezone.utc).strftime('%Y%m%dT%H%M%S%fZ')
    commands_run = []
    def run(command):
        result = subprocess.run(command, cwd=STAGE, env=env, capture_output=True, encoding='utf-8', errors='replace', creationflags=getattr(subprocess, 'CREATE_NO_WINDOW', 0))
        log = logdir / (run_id + '-' + str(len(commands_run) + 1) + '.log')
        log.write_text(result.stdout + result.stderr, encoding='utf-8')
        commands_run.append({'command': [Path(command[0]).name, *command[1:]], 'exit_code': result.returncode, 'log': log.relative_to(ROOT).as_posix(), 'log_sha256': hashlib.sha256(log.read_bytes()).hexdigest()})
        print(' '.join(command[1:]), 'exit_code', result.returncode, 'log', log.relative_to(ROOT).as_posix(), flush=True)
        if result.returncode: print((result.stdout + result.stderr)[-12000:], flush=True)
        return result.returncode
    def receipt(code):
        record = {'action': args.action, 'run_id': run_id, 'exit_code': code, 'node': subprocess.check_output(['node', '--version'], text=True).strip(), 'source_sha256': hashes, 'commands': commands_run, 'virtualization_used': False, 'scope': 'Native frontend compilation/type checks/unit tests; not Alipay device, real channel or full product acceptance'}
        targets = {'alipay': ['miniapp-alipay'], 'h5': ['miniapp-h5'], 'web': ['web'], 'build': ['miniapp-alipay', 'miniapp-h5', 'web']}.get(args.action, [])
        files = [p for folder in targets for p in (ROOT / 'runtime/frontend/builds' / folder).rglob('*') if p.is_file()]
        record['build_sha256'] = {p.relative_to(ROOT).as_posix(): hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted(files)}
        destination = ROOT / ('runtime/frontend/' + args.action + '-receipt.json')
        destination.write_text(json.dumps(record, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
    if args.action in ('bootstrap', 'lock'):
        env.pop('NODE_ENV', None)
        action = 'ci' if args.action == 'bootstrap' and (SOURCE / 'package-lock.json').exists() else 'install'
        result = run([npm, action, '--ignore-scripts', '--no-audit', '--no-fund'])
        if result == 0 and action == 'install':
            shutil.copyfile(STAGE / 'package-lock.json', SOURCE / 'package-lock.json')
            hashes['dev/src/package-lock.json'] = hashlib.sha256((SOURCE / 'package-lock.json').read_bytes()).hexdigest()
        receipt(result)
        return result
    commands = {'check': ['check'], 'test': ['test'], 'build': ['build:alipay', 'build:h5', 'build:web'], 'alipay': ['build:alipay'], 'h5': ['build:h5'], 'web': ['build:web']}[args.action]
    for command in commands:
        result = run([npm, 'run', command])
        if result:
            receipt(result)
            return result
    receipt(0)
    return 0

if __name__ == '__main__': raise SystemExit(main())
