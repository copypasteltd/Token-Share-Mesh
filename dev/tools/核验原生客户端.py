"""实际运行已构建的Windows控制CLI；不登记卖家/设备，不启动内核或资金操作。"""
from __future__ import annotations
import hashlib
import json
import os
from pathlib import Path
import subprocess
from datetime import datetime, timezone
import uuid

ROOT = Path(__file__).resolve().parents[2]

def main():
    if os.name != 'nt':
        raise RuntimeError('This evidence runner exercises Windows DPAPI only')
    binary = ROOT / 'runtime/bin/p11-node.exe'
    checks = json.loads((ROOT / 'dev/tests/evidence/R008-native-source-checks.json').read_text(encoding='utf-8'))
    artifact = next(a for a in checks['artifacts'] if a['path'] == 'runtime/bin/p11-node.exe')
    digest = hashlib.sha256(binary.read_bytes()).hexdigest()
    if checks['status'] != 'PASSED_WITH_LIMITED_SCOPE' or artifact['sha256'] != digest:
        raise RuntimeError('Build current native source before running the CLI')
    for name, expected in checks['source_sha256'].items():
        if hashlib.sha256((ROOT / name).read_bytes()).hexdigest() != expected:
            raise RuntimeError('Current source differs from build evidence')
    parent = ROOT / 'runtime/node/validation'
    parent.mkdir(parents=True, exist_ok=True)
    state = parent / uuid.uuid4().hex
    config = {'control_base':'http://127.0.0.1:1','model_name':'SYNTHETIC_CONTROL_TEST_RTX4090','resource_kind':'GPU_DEVICE','os_build':'SYNTHETIC_CONTROL_TEST','client_release_id':'node_cli_test_1'}
    records = []
    for action, data, expected in [('init', json.dumps(config).encode(), 0), ('status', None, 0), ('heartbeat', None, 1)]:
        result = subprocess.run([str(binary), action, '--state', str(state)], input=data, cwd=ROOT, capture_output=True, timeout=20, creationflags=subprocess.CREATE_NO_WINDOW)
        if result.returncode != expected:
            raise RuntimeError('Native CLI did not enforce its expected control state')
        status = json.loads(result.stdout.decode('utf-8'))
        if status['device_id'] or status['execution_enabled'] or status['pending_command'] or any(word in status for word in ['control_seed','pairing_credential','body']):
            raise RuntimeError('Native CLI exposed or implicitly enrolled an identity')
        records.append({'action':action,'exit_code':result.returncode,'expected_exit_code':expected,'safe_status':status})
    if len({r['safe_status']['key_fingerprint'] for r in records}) != 1:
        raise RuntimeError('Native CLI identity changed across processes')
    report = {'round':'R008','passed':True,'kind':'ACTUAL_WINDOWS_NODE_CONTROL_CLI','binary':artifact['path'],'binary_sha256':digest,
              'checked_at':datetime.now(timezone.utc).isoformat(),'commands':records,'all_processes_exited':True,'virtualization_used':False,
              'state_directory':state.relative_to(ROOT).as_posix(),'scope':'Actual Windows CLI/DPAPI initialization and process reopen, with unenrolled heartbeat refused; no registration, GPU, Linux runtime, model, customer content, payment or product acceptance'}
    (ROOT / 'dev/tests/evidence/R008-native-node-cli.json').write_bytes((json.dumps(report,ensure_ascii=False,indent=2)+'\n').encode('utf-8'))
    print(json.dumps({'passed':True,'commands':len(records),'all_processes_exited':True,'scope':report['kind']}))

if __name__ == '__main__':
    main()
