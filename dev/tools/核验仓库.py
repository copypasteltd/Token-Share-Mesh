"""核验公开仓库的双语文档、真实媒体、文件边界及本机预览隔离。"""
from __future__ import annotations

from datetime import datetime, timezone
import hashlib
from http.server import ThreadingHTTPServer
import json
from pathlib import Path
import re
import subprocess
import threading
from urllib.error import HTTPError
from urllib.parse import quote, unquote, urlsplit
from urllib.request import urlopen

from 构建仓库文档 import outputs
from 预览文档 import ReaderHandler, WHITEPAPER

ROOT = Path(__file__).resolve().parents[2]
EXPECTED_REMOTE = 'git@github.com:copypasteltd/Token-Share-Mesh.git'


def git(*args):
    return subprocess.check_output(['git', *args], cwd=ROOT)


def main():
    checks, errors = [], []
    progress = json.loads((ROOT / 'dev/progress/implementation-status.json').read_text(encoding='utf-8'))
    round_id=progress['round']
    def check(condition, title, detail=None):
        item = {'id': f'{round_id}-CHECK-{len(checks)+1:03d}', 'title': title, 'passed': bool(condition)}
        if detail is not None:
            item['detail'] = detail
        checks.append(item)
        if not condition:
            errors.append(title)

    if round_id == 'R021':
        capi = json.loads((ROOT/'dev/tests/evidence/R021-native-capi-fixture-summary.json').read_text(encoding='utf-8'))
        build = json.loads((ROOT/'dev/tests/evidence/R021-native-worker-build.json').read_text(encoding='utf-8'))
        digest = lambda name: hashlib.sha256((ROOT/name).read_bytes()).hexdigest()
        sources_current = all((ROOT/name).is_file() and digest(name)==value for name,value in capi['sources'].items())
        built_current = all(t['returncode']==0 and digest(t['source'])==t['source_sha256'] and digest(t['output'])==t['sha256'] for t in build['targets'])
        check(capi['passed'] and capi['returncode']==0 and len(capi['cases'])==5 and set(capi['cases'].values())=={'pass'} and capi['owned_worker_exit_waited_by_adapter'] and
              digest(capi['raw'])==capi['raw_sha256'] and sources_current and build['passed'] and len(build['targets'])==2 and built_current and
              digest('dev/src/client/kernels/llama-worker/dependencies.lock.json')==build['dependency_lock_sha256'] and not capi['virtualization_used'],
              'Current native C API worker build and five actual Windows-process synthetic ABI tests; no real inference/GPU/privacy qualification')

    expected = outputs()
    check(all(p.is_file() and p.read_bytes() == body for p, body in expected.items()), 'Both READMEs match the same maintained sources and actual progress')
    readme_media = [re.findall(r'\]\((dev/media/R\d{3}/[^)]+)\)', body.decode('utf-8')) for body in expected.values()]
    check(all(paths and all(name.startswith(f'dev/media/{round_id}/') for name in paths) for paths in readme_media), 'Both READMEs reference current-round product screenshots and recordings')
    progress = json.loads((ROOT / 'dev/progress/implementation-status.json').read_text(encoding='utf-8'))
    check(len(progress['tasks']) == 56 and {t['id'] for t in progress['tasks']} == {f'DEV-{i:02d}' for i in range(1, 57)}, 'All 56 outstanding task entries are retained')
    logs = [(ROOT / p).read_text(encoding='utf-8') for p in ['CHANGELOG.md', 'CHANGELOG.zh-CN.md']]
    ids = [re.findall(r'^## (R\d{3})\b', body, re.M) for body in logs]
    check(ids[0] == ids[1] and ids[0][0] == progress['rounds'][-1]['id'], 'English/Chinese changelogs share the current round IDs')

    documents = [ROOT / 'README.md', ROOT / 'README.zh-CN.md', ROOT / 'CONTRIBUTING.md', ROOT / 'CHANGELOG.md', ROOT / 'CHANGELOG.zh-CN.md', *sorted((ROOT / 'dev/repository').glob('*.md'))]
    broken, links = [], 0
    for file in documents:
        body = file.read_text(encoding='utf-8')
        if not body.startswith('# ') or '\ufffd' in body or sum(line.startswith('```') for line in body.splitlines()) % 2:
            broken.append(file.relative_to(ROOT).as_posix() + ': invalid structure')
        for value in re.findall(r'!?\[[^\]]*\]\(([^)]+)\)', body):
            value = value.strip('<>')
            url = urlsplit(value)
            if url.scheme or value.startswith('#'):
                continue
            links += 1
            target = (file.parent / unquote(url.path)).resolve()
            if not target.is_relative_to(ROOT) or not target.is_file():
                broken.append(file.relative_to(ROOT).as_posix() + ': ' + value)
    check(not broken, 'Public Markdown files are valid with resolvable local links', {'links': links, 'broken': broken})

    media = json.loads((ROOT / 'dev/media/media-manifest.json').read_text(encoding='utf-8'))
    bad_assets = []
    for entry in [*media['sources'], *media['assets']]:
        file = ROOT / entry['path']
        if not file.is_file() or hashlib.sha256(file.read_bytes()).hexdigest() != entry['sha256']:
            bad_assets.append(entry['path'])
    check(not bad_assets, 'Media and actual source HTML match their capture hashes', bad_assets)
    check(media['browser']['closed'] and media['browser']['remaining_owned_tabs'] == 0 and media['preview_process_stopped'] and not media['virtualization_used'] and not media['page_errors'], 'Capture closed its owned browser/pages and preview without page errors')
    stream = next(s for s in media['recording']['video']['streams'] if s['codec_type'] == 'video')
    check(media['recording']['actual_frames'] >= 15 and stream['codec_name'] == 'h264' and float(media['recording']['video']['format']['duration']) >= 8, 'Recording contains actual browser frames and a probed H.264 video')
    check(sum(a['kind'] == 'ACTUAL_BROWSER_SCREENSHOT' for a in media['assets']) == 4 and sum(a['kind'] == 'ACTUAL_BROWSER_RECORDING' for a in media['assets']) == 2, 'Four real screenshots, MP4 and GIF are present')

    native = json.loads((ROOT / f'dev/tests/evidence/{round_id}-native-source-checks.json').read_text(encoding='utf-8'))
    changed = [name for name, digest in native['source_sha256'].items() if hashlib.sha256((ROOT / name).read_bytes()).hexdigest() != digest]
    check(native['status'] == 'PASSED_WITH_LIMITED_SCOPE' and native['round']==round_id and not changed, 'Current native evidence matches the Go/migration source', changed)
    if round_id == 'R008':
        cli = json.loads((ROOT / 'dev/tests/evidence/R008-native-node-cli.json').read_text(encoding='utf-8'))
        artifact = next(a for a in native['artifacts'] if a['path'] == cli['binary'])
        records = cli['commands']
        check(cli['passed'] and cli['all_processes_exited'] and not cli['virtualization_used'] and cli['binary_sha256'] == artifact['sha256'] and
              [(r['action'],r['exit_code']) for r in records] == [('init',0),('status',0),('heartbeat',1)] and
              len({r['safe_status']['key_fingerprint'] for r in records}) == 1 and
              all(not r['safe_status']['device_id'] and not r['safe_status']['execution_enabled'] for r in records),
              'Actual current Windows node binary retains its identity across processes and refuses unenrolled heartbeats')
    if round_id in {'R006','R007','R008','R009','R010','R011','R012','R013','R014','R015','R016','R017','R018','R019','R020','R021'}:
        front = json.loads((ROOT / f'dev/tests/evidence/{round_id}-frontend-checks.json').read_text(encoding='utf-8'))
        changed_front = [name for name,digest in front['source_sha256'].items() if hashlib.sha256((ROOT/name).read_bytes()).hexdigest() != digest]
        units = 21 if round_id in {'R009','R010','R011','R012','R013','R014','R015','R016','R017','R018','R019','R020','R021'} else 13
        check(front['status']=='PASSED_FOR_FRONTEND_CONTROL_SUBSET_ONLY' and front['unit_cases']==units and len(front['browser_checks'])==2 and not changed_front, f'Current frontend evidence includes {units} unit cases, all builds and both native browser journeys', changed_front)
        missing_logs = []
        for record in front['records']:
            for command in record['commands']:
                name = command.get('evidence_log', '')
                path = (ROOT / name).resolve()
                if (not name or not path.is_relative_to(ROOT / 'dev/tests/evidence') or not path.is_file() or
                    hashlib.sha256(path.read_bytes()).hexdigest() != command['log_sha256'] or
                    subprocess.run(['git','check-ignore','-q',name],cwd=ROOT).returncode == 0):
                    missing_logs.append(name)
        check(not missing_logs, 'Current frontend build and unit logs are public evidence with matching recorded hashes', missing_logs)
        product_media = json.loads((ROOT / 'dev/media/frontend-manifest.json').read_text(encoding='utf-8'))
        stale = [item['path'] for item in product_media['assets'] if not (ROOT/item['path']).is_file() or hashlib.sha256((ROOT/item['path']).read_bytes()).hexdigest()!=item['sha256']]
        screenshots = 14 if round_id in {'R009','R010','R011','R012','R013','R014','R015','R016','R017','R018','R019','R020','R021'} else 10
        check(not stale and len(product_media['assets'])==screenshots+4, f'{screenshots} actual product screenshots and four recording files match their independent provenance', stale)
        closed = []
        for target in ['web','miniapp']:
            item = json.loads((ROOT / f'dev/tests/evidence/{round_id}-{target}-browser.json').read_text(encoding='utf-8'))
            closed.append(item['passed'] and item['browser_closed'] and item['remaining_owned_tabs']==0 and bool(item['recording']))
        check(all(closed) and not product_media['virtualization_used'], 'Product captures closed their owned browsers and do not claim native Alipay or device/payment acceptance')
        if round_id in {'R009','R010','R011','R012','R013','R014','R015','R016','R017','R018','R019','R020','R021'}:
            binary = next(a for a in native['artifacts'] if a['path'] == 'runtime/bin/p11-node.exe')
            providers = [json.loads((ROOT / f'dev/tests/evidence/{round_id}-{target}-browser.json').read_text(encoding='utf-8')) for target in ['web','miniapp']]
            check(all(r.get('provider_native_binary_sha256') == binary['sha256'] and r.get('provider_native_processes_exited') and
                      [(c['action'],c['exit_code']) for c in r.get('provider_native_commands',[])] == ([('init',0),('enroll',0)] + ([('execution-init',0),('execution-status',0)] if round_id in {'R010','R011','R012','R013','R014','R015','R016','R017','R018','R019','R020','R021'} else []) + [('heartbeat',0),('heartbeat',1)]) and
                      all(not c['safe_status']['execution_enabled'] for c in r['provider_native_commands']) and
                      r['provider_recovery']['original_pair_preserved'] and r['provider_recovery']['original_policy_preserved'] and
                      not r['provider_recovery']['credentials_persisted'] and not r['provider_recovery']['native_engine_started'] for r in providers),
                  'Both current UIs pair a real Windows control binary, recover original writes and reject a revoked heartbeat without execution')

    names = set(git('ls-files', '--cached', '--others', '--exclude-standard', '-z').decode('utf-8').split('\0')) - {''}
    forbidden, oversize, secret_hits = [], [], []
    signatures = [re.compile(rb'-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----'), re.compile(rb'AKIA[A-Z0-9]{16}'), re.compile(rb'gh[pousr]_[A-Za-z0-9]{30,}'), re.compile(rb'github_pat_[A-Za-z0-9_]{30,}'), re.compile(rb'xox[baprs]-[A-Za-z0-9-]{20,}')]
    for name in names:
        file = ROOT / name
        if not file.is_file():
            continue
        parts = Path(name).parts
        if parts[0] == 'runtime' or 'node_modules' in parts or '__pycache__' in parts or any(p.startswith('.env') for p in parts) or file.suffix.lower() in {'.exe', '.dll', '.pem', '.key', '.p12', '.pfx'}:
            forbidden.append(name)
        if file.stat().st_size > 16 * 1024 * 1024:
            oversize.append(name)
        if file.suffix.lower() not in {'.png', '.jpg', '.gif', '.mp4', '.webm'}:
            data = file.read_bytes()
            if any(pattern.search(data) for pattern in signatures):
                secret_hits.append(name)
    check(not forbidden and not oversize, 'Public file set excludes runtime/dependencies/credentials/binaries and oversized assets', {'files': len(names), 'forbidden': forbidden, 'oversize': oversize})
    check(not secret_hits, 'No high-confidence private keys or service tokens in the public file set', secret_hits)
    ignored = subprocess.run(['git', 'check-ignore', '-q', 'runtime/private-probe.txt'], cwd=ROOT).returncode == 0
    check(ignored, 'Runtime data is excluded by Git rules')
    check(git('remote', 'get-url', 'origin').decode().strip() == EXPECTED_REMOTE and git('branch', '--show-current').decode().strip() == 'main', 'Expected SSH remote and main branch are configured')

    # Exercise actual HTTP behavior: private paths and listings must never leak.
    server = ThreadingHTTPServer(('127.0.0.1', 0), ReaderHandler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    statuses = {}
    try:
        for request_path, wanted in [('/' + quote(WHITEPAPER, safe='/'), 200), ('/.git/config', 403), ('/runtime/private-probe.txt', 403), ('/docs/', 403), ('/docs/%2e%2e/runtime/private-probe.txt', 403), ('/%2e%2e/AGENTS.md', 403)]:
            try:
                with urlopen(f'http://127.0.0.1:{server.server_port}' + request_path, timeout=5) as response:
                    status = response.status
                    response.read(32)
            except HTTPError as error:
                status = error.code
            statuses[request_path] = {'actual': status, 'expected': wanted}
        check(all(v['actual'] == v['expected'] for v in statuses.values()), 'Actual loopback preview serves readers and rejects private/traversal/listing requests', statuses)
    finally:
        server.shutdown()
        server.server_close()
        thread.join(timeout=3)
    check(not thread.is_alive(), 'Repository verification stopped its own HTTP server thread')

    report = {'round': round_id, 'checked_at': datetime.now(timezone.utc).isoformat(), 'scope': 'Repository documentation, packaging, media provenance and local preview only; no model/GPU/payment/product acceptance', 'passed': not errors, 'checks': checks, 'errors': errors}
    evidence = ROOT / f'dev/tests/evidence/{round_id}-repository-check.json'
    evidence.parent.mkdir(parents=True, exist_ok=True)
    evidence.write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
    print(json.dumps({'round': round_id, 'passed': not errors, 'checks': len(checks), 'files': len(names), 'links': links, 'errors': errors}, ensure_ascii=False))
    return 0 if not errors else 1


if __name__ == '__main__':
    raise SystemExit(main())
