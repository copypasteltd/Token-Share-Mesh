"""构建固定 C API 的原生 worker；不运行模型/GPU，不启动虚拟化。"""
from __future__ import annotations
import argparse
import base64
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import subprocess
import urllib.request
import zipfile

ROOT = Path(__file__).resolve().parents[2]
SOURCE = ROOT / 'dev/src/client/kernels/llama-worker'
LOCK = SOURCE / 'dependencies.lock.json'
CACHE = ROOT / 'runtime/native-worker-research'

def sha(path: Path) -> str:
    with path.open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()

def sources(fetch: bool):
    lock = json.loads(LOCK.read_text(encoding='utf-8'))
    CACHE.mkdir(parents=True, exist_ok=True)
    for relative, digest in lock['llama']['headers'].items():
        path = CACHE / Path(relative).name
        if not path.exists() and fetch:
            url = f"https://api.github.com/repos/{lock['llama']['repository']}/contents/{relative}?ref={lock['llama']['commit']}"
            req = urllib.request.Request(url, headers={'User-Agent': 'Token-Share-Mesh-native-build'})
            with urllib.request.urlopen(req, timeout=40) as response:
                path.write_bytes(base64.b64decode(json.load(response)['content']))
        if not path.exists() or sha(path) != digest:
            raise RuntimeError(f'pinned upstream header missing or changed: {relative}')
    compiler = lock['compiler']
    archive = ROOT / 'runtime/toolchains' / compiler['archive']
    target = archive.with_suffix('')
    binary = target / 'bin/clang++.exe'
    if not archive.exists() and fetch:
        archive.parent.mkdir(parents=True, exist_ok=True)
        partial = archive.with_suffix('.part')
        url = f"https://github.com/{compiler['repository']}/releases/download/{compiler['release']}/{compiler['archive']}"
        req = urllib.request.Request(url, headers={'User-Agent': 'Token-Share-Mesh-native-build'})
        with urllib.request.urlopen(req, timeout=40) as response, partial.open('wb') as output:
            while block := response.read(1 << 20):
                output.write(block)
        if partial.stat().st_size != compiler['size'] or sha(partial) != compiler['sha256']:
            raise RuntimeError('native compiler archive rejected')
        partial.replace(archive)
    if not archive.exists() or sha(archive) != compiler['sha256']:
        raise RuntimeError('pinned compiler archive missing or changed')
    if not binary.exists() and fetch:
        with zipfile.ZipFile(archive) as source:
            for name in source.namelist():
                if not (archive.parent / name).resolve().is_relative_to(archive.parent.resolve()):
                    raise RuntimeError('compiler archive traversal rejected')
            source.extractall(archive.parent)
    if not binary.exists():
        raise RuntimeError('native compiler not prepared; use --fetch')
    # Recheck the executable against its pinned archive, including cached builds.
    with zipfile.ZipFile(archive) as source:
        expected = hashlib.sha256(source.read(target.name + '/bin/clang++.exe')).hexdigest()
    if sha(binary) != expected:
        raise RuntimeError('native compiler executable changed')
    return binary, lock

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--fetch', action='store_true', help='explicitly prepare pinned native compiler and headers')
    parser.add_argument('--fixture', action='store_true', help='also compile a synthetic C API DLL; never real inference')
    args = parser.parse_args()
    compiler, lock = sources(args.fetch)
    out = ROOT / 'runtime/native-worker/build';out.mkdir(parents=True, exist_ok=True)
    env = os.environ.copy()
    temporary = ROOT / 'runtime/tmp/native-worker';temporary.mkdir(parents=True, exist_ok=True)
    env.update(TMP=str(temporary), TEMP=str(temporary))
    common = [str(compiler), '-std=c++20', '-O2', '-static', '-Wall', '-Wextra', '-Werror', '-Wpedantic', '-I', str(CACHE)]
    targets = [(SOURCE / 'worker.cpp', out / 'p11-llama-worker.exe', [])]
    if args.fixture:
        targets.append((ROOT / 'dev/tests/native-worker/fixture.cpp', out / 'fixture/llama-fixture.dll', ['-shared']))
    receipt = {'round': 'R025', 'recorded_at': datetime.now(timezone.utc).isoformat(), 'virtualization_used': False,
               'scope': 'Windows native C API worker compilation; optional synthetic ABI fixture, not model/GPU/privacy acceptance',
               'worker_header_sha256': sha(SOURCE/'gpu_identity.h'), 'dependency_lock_sha256': sha(LOCK), 'compiler_sha256': sha(compiler), 'targets': []}
    for src, output, options in targets:
        output.parent.mkdir(parents=True, exist_ok=True)
        result = subprocess.run(common + options + [str(src), '-o', str(output)], cwd=ROOT, env=env, capture_output=True, text=True, encoding='utf-8', errors='replace', timeout=180)
        receipt['targets'].append({'source': src.relative_to(ROOT).as_posix(), 'source_sha256': sha(src), 'output': output.relative_to(ROOT).as_posix(), 'returncode': result.returncode, 'diagnostic': result.stdout + result.stderr})
        if result.returncode == 0:
            receipt['targets'][-1]['sha256'] = sha(output)
        else:
            break
    receipt['passed'] = all(t['returncode'] == 0 for t in receipt['targets'])
    target = ROOT / 'dev/tests/evidence/R025-native-worker-build.json'
    target.write_text(json.dumps(receipt, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
    print(json.dumps({'passed': receipt['passed'], 'targets': len(receipt['targets']), 'receipt': str(target.relative_to(ROOT))}))
    return 0 if receipt['passed'] else 1

if __name__ == '__main__':
    raise SystemExit(main())
