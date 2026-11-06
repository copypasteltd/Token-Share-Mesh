"""Materialize pinned public dependencies; native compilation requires CF-03."""
from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import platform
import shutil
import subprocess
import tarfile
import urllib.request
import uuid

ROOT = Path(__file__).resolve().parents[2]
LOCK = ROOT/'dev/src/client/crypto-gateway/dependencies.lock.json'
MAX_DOWNLOAD = 8 << 20
MAX_EXPANDED = 64 << 20


def within_runtime(path: Path) -> Path:
    workspace = ROOT.resolve()
    runtime_root = (ROOT/'runtime').resolve()
    resolved = path.resolve()
    if runtime_root == workspace or not runtime_root.is_relative_to(workspace) or not resolved.is_relative_to(runtime_root):
        raise RuntimeError('Explicit project runtime dependency path required')
    for ancestor in (path, *path.parents):
        if ancestor == ROOT:
            break
        if ancestor.is_symlink() or getattr(ancestor, 'is_junction', lambda: False)():
            raise RuntimeError('Do not adopt linked dependency paths')
    return resolved


def download(url: str, target: Path, digest: str) -> None:
    if target.exists():
        if target.is_symlink() or target.stat().st_size > MAX_DOWNLOAD or hashlib.sha256(target.read_bytes()).hexdigest() != digest:
            raise RuntimeError('Retain conflicting dependency archive for explicit recovery')
        return
    request = urllib.request.Request(url, headers={'User-Agent': 'Token-Share-Mesh-pinned-source/1'})
    stage = target.with_name(target.name+'.part-'+uuid.uuid4().hex)
    total = 0
    calculated = hashlib.sha256()
    with urllib.request.urlopen(request, timeout=20) as response, stage.open('xb') as output:
        if response.status != 200:
            raise RuntimeError('Pinned archive unavailable')
        while block := response.read(64 << 10):
            total += len(block)
            if total > MAX_DOWNLOAD:
                raise RuntimeError('Dependency archive download budget exceeded')
            calculated.update(block)
            output.write(block)
        output.flush()
        os.fsync(output.fileno())
    if calculated.hexdigest() != digest:
        raise RuntimeError('Pinned archive digest mismatch; retain original partial file')
    stage.rename(target)


def extract(archive: Path, target: Path, digest: str) -> None:
    marker = target/'p11-source-material.json'
    if target.exists():
        if target.is_symlink() or not marker.is_file() or json.loads(marker.read_text(encoding='utf-8')).get('archive_sha256') != digest:
            raise RuntimeError('Retain existing unverified dependency directory')
        # A marker is not evidence that the cached tree still matches source.
        # Compare against the original pinned archive, including extra files.
        expected = set()
        with tarfile.open(archive, 'r:*') as bundle:
            for member in bundle.getmembers():
                relative = PurePosixPath(member.name)
                if relative.is_absolute() or '..' in relative.parts or any(c in member.name for c in '\\:\0') or not (member.isfile() or member.isdir()):
                    raise RuntimeError('Unsupported cached source entry')
                if member.isdir():
                    continue
                expected.add(relative.as_posix())
                path = target.joinpath(*relative.parts)
                source = bundle.extractfile(member)
                if source is None or path.is_symlink() or not path.is_file() or path.stat().st_size != member.size:
                    raise RuntimeError('Cached dependency differs from pinned archive')
                with source, path.open('rb') as local:
                    while block := source.read(64 << 10):
                        if local.read(len(block)) != block:
                            raise RuntimeError('Cached dependency bytes changed')
        actual = {p.relative_to(target).as_posix() for p in target.rglob('*') if p.is_file() and p != marker}
        if actual != expected or any(p.is_symlink() for p in target.rglob('*')):
            raise RuntimeError('Unregistered cached dependency entry')
        return
    stage = within_runtime(target.with_name(target.name+'-stage-'+uuid.uuid4().hex))
    stage.mkdir()
    with tarfile.open(archive, 'r:*') as bundle:
        members = bundle.getmembers()
        if len(members) > 8192:
            raise RuntimeError('Dependency archive entry budget exceeded')
        names = set()
        total = 0
        for member in members:
            relative = PurePosixPath(member.name)
            name = relative.as_posix()
            if not name or relative.is_absolute() or any(p in {'', '.', '..'} for p in relative.parts) or any(c in name for c in '\\:\0') or name.casefold() in names or not (member.isfile() or member.isdir()):
                raise RuntimeError('Unsupported or ambiguous dependency archive entry')
            names.add(name.casefold())
            total += member.size
            if member.size < 0 or total > MAX_EXPANDED:
                raise RuntimeError('Expanded dependency budget exceeded')
        for member in members:
            destination = stage.joinpath(*PurePosixPath(member.name).parts)
            if member.isdir():
                destination.mkdir(parents=True, exist_ok=True)
                continue
            destination.parent.mkdir(parents=True, exist_ok=True)
            source = bundle.extractfile(member)
            if source is None:
                raise RuntimeError('Pinned dependency entry unavailable')
            with source, destination.open('xb') as output:
                left = member.size
                while left:
                    block = source.read(min(left, 64 << 10))
                    if not block:
                        raise RuntimeError('Truncated dependency entry')
                    output.write(block)
                    left -= len(block)
            # Never execute during materialization. Preserve only the Unix
            # owner's original executable bit for the later native build.
            if os.name != 'nt':
                destination.chmod(0o700 if member.mode & 0o100 else 0o600)
    (stage/'p11-source-material.json').write_text(json.dumps({
        'archive_sha256': digest, 'compiled': False, 'qualified': False,
    }, indent=2)+'\n', encoding='utf-8')
    stage.rename(target)


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=['fetch', 'build'])
    args = parser.parse_args()
    if args.action == 'build':
        from 开发阶段 import require_test_phase
        require_test_phase()
    pin = json.loads(LOCK.read_text(encoding='utf-8'))['native_cipher_suite']
    system = platform.system()
    if system not in {'Windows', 'Linux'}:
        raise RuntimeError('Only explicitly supported native Windows/Linux hosts; no virtualized fallback')
    if system == 'Windows' and platform.machine().upper() not in {'AMD64', 'X86_64'}:
        raise RuntimeError('This archive is pinned only for native Windows x64')
    base = within_runtime(ROOT/'runtime/dependencies/crypto/libsodium-1.0.22')
    base.mkdir(parents=True, exist_ok=True)
    windows = system == 'Windows'
    url = pin['mingw_url' if windows else 'source_url']
    digest = pin['mingw_sha256' if windows else 'source_sha256']
    archive = base/url.rsplit('/', 1)[1]
    download(url, archive, digest)
    target = base/('mingw-static' if windows else 'source')
    extract(archive, target, digest)
    if windows:
        candidates = list(target.rglob('libsodium.a'))
        selected = [p for p in candidates if 'win64' in p.as_posix().lower()]
        if len(selected) != 1:
            raise RuntimeError('Pinned x64 static library layout requires explicit review')
        prefix = selected[0].parent.parent
    else:
        prefix = base/'native-static'
        if args.action == 'build':
            candidates = list(target.glob('*/configure'))
            if len(candidates) != 1:
                raise RuntimeError('Pinned native source configure entry required')
            source_root = candidates[0].parent
            build_root = within_runtime(base/('native-build-'+uuid.uuid4().hex))
            shutil.copytree(source_root, build_root)
            # Native Linux only. Never invoke WSL/Docker/VM or shell-built args.
            subprocess.run([str(build_root/'configure'), '--disable-shared', '--enable-static',
                            '--with-pic', '--prefix='+str(prefix)], cwd=build_root, check=True)
            subprocess.run(['make', '-j2'], cwd=build_root, check=True)
            subprocess.run(['make', 'install'], cwd=build_root, check=True)
    print(json.dumps({
        'action': args.action, 'native_host': system, 'dependency_pin': pin['commit'],
        'CGO_CFLAGS': '-I'+str(prefix/'include'),
        'CGO_LDFLAGS': '-L'+str(prefix/'lib'),
        'required_build_tag': 'p11_sodium', 'qualified_or_interop_passed': False,
        'product_go_or_android_built': False,
        'full_round_complete': False,
    }, ensure_ascii=False))


if __name__ == '__main__':
    main()
