"""Download one pinned upstream SOURCE and extend the review inventory only.

Never imports, builds, installs or runs ComfyUI/Python dependencies or tests.
"""
from pathlib import Path
import base64
import hashlib
import json
import urllib.request

ROOT = Path(__file__).resolve().parents[2]
COMMIT = '2c75dc1b379a59e0880619d029607af5169a62ef'
SOURCE = 'comfy_execution/progress.py'


def write_json(path, value):
    old = path.read_bytes()
    ending = '\r\n' if b'\r\n' in old else '\n'
    path.write_bytes((json.dumps(value, ensure_ascii=False, indent=2) + '\n').replace('\n', ending).encode('utf-8'))


def main():
    url = 'https://raw.githubusercontent.com/Comfy-Org/ComfyUI/' + COMMIT + '/' + SOURCE
    def read(url):
        request = urllib.request.Request(url, headers={'User-Agent': 'Token-Share-Mesh-source-review'})
        with urllib.request.urlopen(request, timeout=40) as response:
            result = response.read(1 << 20)
            if response.read(1):
                raise RuntimeError('pinned source exceeds review bound')
            return result

    try:
        source = read(url)
    except OSError:
        # Same pinned source, different official transport. Never change the
        # upstream revision in response to a download failure.
        tree = json.loads((ROOT / 'runtime/upstream/ComfyUI/tree.json').read_text(encoding='utf-8'))
        entry = next(v for v in tree['tree'] if v['path'] == SOURCE and v['type'] == 'blob')
        record = json.loads(read('https://api.github.com/repos/Comfy-Org/ComfyUI/git/blobs/' + entry['sha']))
        if record['encoding'] != 'base64':
            raise RuntimeError('unsupported pinned Git blob encoding')
        source = base64.b64decode(record['content'])
        git_sha = hashlib.sha1(b'blob ' + str(len(source)).encode('ascii') + b'\0' + source).hexdigest()
        if git_sha != entry['sha'] or len(source) != record['size']:
            raise RuntimeError('pinned Git blob differs')
    target = ROOT / 'runtime' / 'upstream' / 'ComfyUI' / COMMIT / SOURCE
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_bytes(source)
    record = {'path': SOURCE, 'sha256': hashlib.sha256(source).hexdigest()}
    path = ROOT / 'dev/src/client/comfyenv/upstream.lock.json'
    lock = json.loads(path.read_text(encoding='utf-8'))
    lock['reviewed_source_files'] = [v for v in lock['reviewed_source_files'] if v['path'] != SOURCE] + [record]
    write_json(path, lock)
    print('Pinned progress source downloaded for reading only; no engine or tests executed.')


if __name__ == '__main__':
    main()
