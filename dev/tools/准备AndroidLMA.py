"""Prepare the pinned LMA-derived Android source, never models or inference.

All network/cache/output stays in this project runtime. Existing upstream and
derived trees are retained; no checkout reset, system install or virtualization.
"""
from __future__ import annotations
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import urllib.request

ROOT = Path(__file__).resolve().parents[2]
SOURCE = ROOT / 'dev/src/client/android'

def git(repo, *args):
    return subprocess.check_output(['git', '-C', str(repo), *args], cwd=ROOT, text=True).strip()

def prepare(fetch=False):
    lock = json.loads((SOURCE / 'upstream.lock.json').read_text(encoding='utf-8'))
    repo = ROOT / 'runtime/upstream/LMA'
    if not repo.exists():
        if not fetch:
            raise RuntimeError('Pinned LMA source missing; use --fetch for source download only.')
        repo.parent.mkdir(parents=True, exist_ok=True)
        subprocess.run(['git', 'clone', '--no-checkout', '--filter=blob:none', lock['repository'], str(repo)], cwd=ROOT, check=True)
        subprocess.run(['git', '-C', str(repo), 'fetch', '--depth', '1', 'origin', lock['commit']], cwd=ROOT, check=True)
        subprocess.run(['git', '-C', str(repo), 'checkout', '--detach', lock['commit']], cwd=ROOT, check=True)
    if git(repo, 'remote', 'get-url', 'origin') != lock['repository'] or git(repo, 'rev-parse', 'HEAD') != lock['commit']:
        raise RuntimeError('Existing LMA source differs; preserve it and explicitly review a new upstream lock.')
    if git(repo, 'status', '--porcelain'):
        raise RuntimeError('Upstream tree has local changes; never reset another developer’s work.')
    overlay = SOURCE / 'overlay'
    stamp = hashlib.sha256()
    stamp.update(Path(__file__).read_bytes())
    stamp.update((SOURCE / 'upstream.lock.json').read_bytes())
    stamp.update((SOURCE / 'dependencies.lock.json').read_bytes())
    stamp.update((SOURCE / 'integration-patches.json').read_bytes())
    for file in sorted(overlay.rglob('*')):
        if file.is_file():
            stamp.update(file.relative_to(overlay).as_posix().encode()); stamp.update(file.read_bytes())
    target = ROOT / 'runtime/android/source' / (lock['commit'][:12] + '-' + stamp.hexdigest()[:16])
    if target.exists():
        raise RuntimeError('Prepared source already exists; retain it and use its recorded manifest.')
    target.mkdir(parents=True)
    records = []
    for entry in lock['files']:
        rel = Path(entry['path'])
        if rel.is_absolute() or '..' in rel.parts:
            raise RuntimeError('Pinned source path escapes upstream')
        source = repo / rel
        if source.is_symlink() or not source.is_file():
            raise RuntimeError('Pinned source is missing or is a link: ' + str(rel))
        data = source.read_bytes()
        if len(data) != entry['bytes'] or hashlib.sha256(data).hexdigest() != entry['sha256']:
            raise RuntimeError('Pinned source changed: ' + str(rel))
        destination = target / rel; destination.parent.mkdir(parents=True, exist_ok=True)
        destination.write_bytes(data); records.append(entry)
    integrations = json.loads((SOURCE / 'integration-patches.json').read_text(encoding='utf-8'))
    if integrations['schema'] != 'p11.lma-integration-patches/1' or integrations['upstream_commit'] != lock['commit']:
        raise RuntimeError('Integration patch does not belong to the pinned upstream')
    patched = []
    pinned_paths = {entry['path'] for entry in lock['files']}
    for patch in integrations['patches']:
        relative = Path(patch['path'])
        if relative.is_absolute() or '..' in relative.parts or relative.as_posix() not in pinned_paths:
            raise RuntimeError('Integration patch must target a pinned upstream file')
        destination = target / relative
        original = destination.read_bytes()
        content = original.decode('utf-8')
        newline = '\r\n' if b'\r\n' in original else '\n'
        for change in patch['replacements']:
            before, after = change['old'], change['new']
            if not isinstance(before, str) or not before or not isinstance(after, str) or content.count(before) != 1:
                raise RuntimeError('Pinned integration location changed: ' + relative.as_posix())
            content = content.replace(before, after.replace('\n', newline), 1)
        transformed = content.encode('utf-8')
        destination.write_bytes(transformed)
        patched.append({'path': relative.as_posix(), 'source_sha256': hashlib.sha256(original).hexdigest(),
                        'derived_sha256': hashlib.sha256(transformed).hexdigest()})
    for file in sorted(overlay.rglob('*')):
        if file.is_file():
            relative = file.relative_to(overlay)
            destination = target / relative; destination.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(file, destination)
    # A distinct app identity protects the original LMA install and user assets.
    gradle = target / 'app/build.gradle'
    text = gradle.read_text(encoding='utf-8')
    old = "applicationId 'com.lma.app'"
    if text.count(old) != 1:
        raise RuntimeError('Upstream app identity location changed; review required')
    text = text.replace(old, "applicationId 'com.tokenshare.mesh.provider'")
    text = text.replace("versionName '1.0.0'", "versionName '1.0.0-mesh-r026'")
    text += "\n// SHA-pinned lightweight Ed25519; Android OS keystore wraps private seeds.\ndependencies { implementation files('../../deps/bcprov-jdk18on-1.86.jar') }\n"
    gradle.write_text(text, encoding='utf-8')
    receipt = {'schema': 'p11.lma-derived-source/1', 'upstream': lock['commit'],
               'overlay_sha256': stamp.hexdigest(), 'application_id': 'com.tokenshare.mesh.provider',
               'source_only': True, 'apk_built': False, 'tests_executed': False,
               'models_downloaded': False, 'upstream_files': records, 'integration_patches': patched}
    (target / 'mesh-source.json').write_text(json.dumps(receipt, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
    return target

def fetch_dependencies():
    directory=ROOT/'runtime/android/source/deps';directory.mkdir(parents=True,exist_ok=True)
    lock=json.loads((SOURCE/'dependencies.lock.json').read_text(encoding='utf-8'))
    for entry in lock['files']:
        name=entry['name']
        if Path(name).name!=name or not name.endswith('.jar'):
            raise RuntimeError('Dependency name must be a local JAR basename')
        target=directory/name
        if target.exists():
            if hashlib.sha256(target.read_bytes()).hexdigest()!=entry['sha256']:
                raise RuntimeError('Existing dependency differs; preserve it for review')
            continue
        with urllib.request.urlopen(entry['url'],timeout=45) as response:
            data=response.read(20*1024*1024+1)
        if len(data)>20*1024*1024 or hashlib.sha256(data).hexdigest()!=entry['sha256']:
            raise RuntimeError('Pinned dependency digest or size mismatch')
        with target.open('xb') as output: output.write(data)

if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--fetch', action='store_true', help='Explicit pinned source fetch only; never model assets')
    parser.add_argument('--fetch-dependencies',action='store_true',help='Explicit SHA-pinned Java dependency fetch; never model assets or execution')
    args = parser.parse_args()
    if args.fetch_dependencies: fetch_dependencies()
    print(prepare(args.fetch))
