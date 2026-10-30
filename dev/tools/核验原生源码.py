"""核验当前 Go 源码和锁定依赖；构建原生 CLI，记录有限范围证据。"""
from __future__ import annotations

from datetime import datetime, timezone
import argparse
import hashlib
import json
from pathlib import Path
import subprocess

from 原生开发 import ROOT, environment


def sha256(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--round', default='R022', choices=['R003','R004','R005','R006','R007','R008','R009','R010','R011','R012','R013','R014','R015','R016','R017','R018','R019','R020','R021','R022'])
    round_id = parser.parse_args().round
    binary, env = environment()
    # Dependencies were fetched by the explicit toolchain/bootstrap step.
    # Verification itself must not silently resolve another dependency version.
    env['GOPROXY'] = 'off'
    env['GOOS'] = 'windows'
    env['GOARCH'] = 'amd64'
    source = ROOT / 'dev/src'
    evidence = ROOT / f'dev/tests/evidence/{round_id}-native-source-checks.json'
    evidence.parent.mkdir(parents=True, exist_ok=True)
    report = {
        'round': round_id,
        'recorded_at': datetime.now(timezone.utc).isoformat(),
        'scope': 'Go source/dependencies and native admin/API/node builds only; not full product, native Linux runtime, GPU, Android, crypto or payment acceptance',
        'virtualization_used': False,
        'checks': [],
        'artifacts': [],
    }

    def run(args: list[str], variables=None) -> str:
        timeout = 600 if args[:2] == ['mod','verify'] else 180
        try:
            result = subprocess.run(
                [str(binary), *args], cwd=source, env=variables or env,
                capture_output=True, text=True, encoding='utf-8', errors='replace',
                timeout=timeout, creationflags=getattr(subprocess, 'CREATE_NO_WINDOW', 0),
            )
        except subprocess.TimeoutExpired as failure:
            report['checks'].append({'command': args, 'target_goos': (variables or env).get('GOOS'),
                                     'exit_code': None, 'timeout_seconds': timeout, 'error_type': 'TimeoutExpired'})
            print('go', args[0], 'timed out', flush=True)
            raise
        report['checks'].append({'command': ['go', *args], 'exit_code': result.returncode})
        print('go', args[0], 'exit_code', result.returncode, flush=True)
        if result.returncode:
            # Go diagnostics contain paths/types; never print process environment.
            print(result.stdout + result.stderr)
            raise RuntimeError('Go source check failed')
        return result.stdout

    try:
        version = run(['version']).strip()
        if 'go1.27.3 windows/amd64' not in version:
            raise RuntimeError('Locked Go version required')
        report['go'] = version
        run(['mod', 'verify'])
        run(['vet', './...'])

        # Check formatting without mutating files or weakening a failing check.
        unformatted = subprocess.run(
            [str(binary.with_name('gofmt.exe')), '-l', '.'], cwd=source,
            env=env, capture_output=True, text=True, encoding='utf-8',
            timeout=60, creationflags=getattr(subprocess, 'CREATE_NO_WINDOW', 0),
        )
        report['checks'].append({'command': ['gofmt', '-l', '.'], 'exit_code': unformatted.returncode, 'unformatted_files': unformatted.stdout.splitlines()})
        if unformatted.returncode or unformatted.stdout.strip():
            raise RuntimeError('Go formatting check failed')

        for command, package in {'p11-admin':'./api/cmd/p11-admin','p11-api':'./api/cmd/p11-api','p11-node':'./client/cmd/p11-node'}.items():
            for target, suffix in [('windows','.exe'),('linux','-linux-amd64')]:
                localenv = env.copy()
                localenv['GOOS'] = target
                output = ROOT / 'runtime/bin' / (command+suffix)
                output.parent.mkdir(parents=True, exist_ok=True)
                run(['build', '-trimpath', '-o', str(output), package], localenv)
                report['artifacts'].append({'target': target + '-amd64', 'path': output.relative_to(ROOT).as_posix(), 'sha256': sha256(output), 'runtime_test': 'NOT_EXECUTED'})

        if round_id in {'R021','R022'}:
            cross = env.copy();cross['GOOS'] = 'linux'
            run(['build','./client/adapters/llamaworker','./client/results','./client/protocol/textoutput'],cross)
            report['native_adapter_linux_compile_only'] = True

        # Inspect packages actually compiled by this module and its tests.
        # Listing the entire transitive module graph would additionally fetch
        # unused upstream test/build dependencies, even in Go's lazy graph.
        module_text = run(['list', '-deps', '-test', '-json', './...'])
        decoder = json.JSONDecoder()
        modules = []
        seen = set()
        sums = {}
        for line in (source / 'go.sum').read_text(encoding='utf-8').splitlines():
            path, version, checksum = line.split()
            sums[(path, version)] = checksum
        while module_text.strip():
            module_text = module_text.lstrip()
            entry, offset = decoder.raw_decode(module_text)
            module_text = module_text[offset:]
            entry = entry.get('Module')
            if not entry or entry['Path'] in seen:
                continue
            seen.add(entry['Path'])
            record = {k: entry[k] for k in ['Path', 'Version', 'Sum', 'GoModSum'] if k in entry}
            if not entry.get('Main'):
                version = entry['Version']
                record['Sum'] = sums[(entry['Path'], version)]
                record['GoModSum'] = sums[(entry['Path'], version + '/go.mod')]
                directory = Path(entry['Dir'])
                license_file = next((directory / name for name in ['LICENSE', 'LICENSE.txt', 'LICENCE'] if (directory / name).is_file()), None)
                if license_file is None:
                    raise RuntimeError('Go dependency license is missing')
                destination = ROOT / 'dev/licenses/go' / (entry['Path'].replace('/', '_') + '@' + entry['Version'] + '.txt')
                destination.parent.mkdir(parents=True, exist_ok=True)
                destination.write_bytes(license_file.read_bytes())
                record['license_file'] = destination.relative_to(ROOT).as_posix()
                record['license_sha256'] = sha256(destination)
                if entry['Path'] == 'modernc.org/sqlite':
                    record['bundled_notices'] = []
                    for name in ['LICENSE-SQLITE', 'LICENSE-SQLITE_VEC', 'LICENSE-3RD-PARTY.md']:
                        notice = directory / name
                        if not notice.is_file(): raise RuntimeError('Bundled SQLite notice is missing')
                        dest = ROOT / 'dev/licenses/go' / ('modernc.org_sqlite@' + version + '-' + name)
                        dest.write_bytes(notice.read_bytes())
                        record['bundled_notices'].append({'path': dest.relative_to(ROOT).as_posix(), 'sha256': sha256(dest)})
            modules.append(record)
        inventory = ROOT / 'dev/licenses/go-dependencies.json'
        inventory.write_text(json.dumps({'scope': 'Go modules actually used by this source and its tests on windows-amd64; not the entire upstream graph or a complete product SBOM', 'modules': sorted(modules, key=lambda m: m['Path'])}, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
        report['dependency_inventory'] = inventory.relative_to(ROOT).as_posix()
        report['source_sha256'] = {
            p.relative_to(ROOT).as_posix(): sha256(p)
            for p in sorted(source.rglob('*'))
            if p.is_file() and (p.suffix in {'.go', '.sql', '.json'} or p.name in {'go.mod', 'go.sum'})
        }
        report['status'] = 'PASSED_WITH_LIMITED_SCOPE'
        return 0
    except Exception as error:
        report['status'] = 'FAILED'
        report['error_type'] = type(error).__name__
        print('Native source verification failed:', type(error).__name__, flush=True)
        return 1
    finally:
        evidence.write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')


if __name__ == '__main__':
    raise SystemExit(main())
