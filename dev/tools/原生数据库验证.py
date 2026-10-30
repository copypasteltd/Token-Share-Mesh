"""临时启动本项目原生 PostgreSQL，执行 Go 测试后关闭该实例。"""
from __future__ import annotations
from datetime import datetime,timezone
from contextlib import contextmanager
import ctypes
import argparse
import json
import os
from pathlib import Path
import secrets
import socket
import subprocess
import sys
import uuid
from urllib.parse import quote
from 原生开发 import ROOT,environment

@contextmanager
def native_path_alias(lease=None):
    """EDB's Windows bootstrap requires an ASCII install/data path.

    SUBST is only a temporary pathname alias to the approved project directory;
    it launches no guest, kernel or virtual machine. Remove only our own alias.
    """
    lease = lease if lease is not None else {'safe_to_release': True}
    if os.name!='nt' or str(ROOT).isascii():
        yield ROOT
        return
    binary=Path(os.environ['SystemRoot'])/'System32/subst.exe'
    occupied=ctypes.windll.kernel32.GetLogicalDrives()
    drive=next((chr(n+65)+':' for n in range(25,17,-1) if not occupied & (1<<n)),None)
    if drive is None:raise RuntimeError('No free temporary project path alias')
    subprocess.run([str(binary),drive,str(ROOT)],check=True,capture_output=True,creationflags=subprocess.CREATE_NO_WINDOW)
    alias=Path(drive+'/')
    try:
        if not os.path.samefile(alias,ROOT):raise RuntimeError('Alias does not refer to the project')
        yield alias
    finally:
        if not lease.get('safe_to_release', False):
            # A running daemon still needs its original executable/data path.
            # Keep this mapping available for an explicit, instance-scoped stop.
            raise RuntimeError('Temporary database stop unconfirmed; retaining project path alias '+drive)
        if os.path.samefile(alias,ROOT):
            subprocess.run([str(binary),drive,'/D'],check=True,capture_output=True,creationflags=subprocess.CREATE_NO_WINDOW)
        else:raise RuntimeError('Project alias changed; refusing to remove another mapping')

def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--round', default='R013', choices=['R003','R004','R005','R006','R007','R008','R009','R010','R011','R012','R013'])
    options=parser.parse_args()
    lease = {'safe_to_release': True}
    with native_path_alias(lease) as alias:
        return run_suite(alias, lease, options.round)

def run_suite(alias, lease, round_id):
    lock=json.loads((ROOT/'dev/toolchain.lock.json').read_text(encoding='utf-8'))
    pg=next(t for t in lock['tools'] if t['name']=='postgresql')
    binaries=alias/pg['directory']/'pgsql/bin'
    run_id=datetime.now(timezone.utc).strftime('%Y%m%dT%H%M%SZ')+'-'+uuid.uuid4().hex[:8]
    directory=ROOT/'runtime/db/integration'/run_id
    if not directory.resolve().is_relative_to((ROOT/'runtime/db/integration').resolve()): raise ValueError('Invalid instance path')
    directory.mkdir(parents=True,exist_ok=False)
    password=secrets.token_urlsafe(36)
    password_file=directory/'bootstrap-password.txt';password_file.write_text(password,encoding='utf-8')
    data=alias/directory.relative_to(ROOT)/'data'
    with socket.socket() as reservation:
        reservation.bind(('127.0.0.1',0));port=reservation.getsockname()[1]
    executable,env=environment()
    env['PGPASSWORD']=password;env['PGUSER']='p11_test';env['PGHOST']='127.0.0.1';env['PGPORT']=str(port)
    dsn=f'postgresql://p11_test:{quote(password,safe="")}@127.0.0.1:{port}/postgres?sslmode=disable'
    env['P11_TEST_DATABASE_URL']=dsn;env['P11_REQUIRE_INTEGRATION']='1'
    if round_id in {'R006','R007','R008','R009','R010','R011','R012','R013'}:
        env['P11_FRONTEND_BROWSER']='1';env['P11_PROJECT_ROOT']=str(ROOT);env['P11_EVIDENCE_ROUND']=round_id
    flags=subprocess.CREATE_NO_WINDOW if os.name=='nt' else 0
    def run(args,timeout=60):
        # A native PostgreSQL daemon can inherit stdout handles from pg_ctl.
        # Waiting on a PIPE's EOF would then wait for the daemon, not pg_ctl.
        # A regular project-local file lets wait() observe the actual command.
        command_log=directory/(Path(args[0]).stem+'-'+uuid.uuid4().hex[:8]+'.log')
        with command_log.open('w+b') as output:
            result=subprocess.run([str(a) for a in args],env=env,stdout=output,stderr=subprocess.STDOUT,timeout=timeout,creationflags=flags)
        if result.returncode:
            raise RuntimeError(command_log.read_text(encoding='utf-8',errors='replace').replace(password,'[REDACTED]').replace(dsn,'[DATABASE_URL]'))
        return result
    started=False;test_code=1;stop_ok=False;shutdown_mode='NOT_STARTED'
    failure=None;cleanup_failure=None
    log=ROOT/'runtime/logs'/('native-db-'+run_id+'.log')
    log.parent.mkdir(parents=True,exist_ok=True)
    try:
        native_password=alias/password_file.relative_to(ROOT)
        # Only the disposable initial catalog skips the final filesystem-wide
        # sync. The server still uses normal fsync/WAL for every test commit.
        run([binaries/'initdb.exe','-D',data,'-U','p11_test','--pwfile='+str(native_password),'--encoding=UTF8','--locale=C','--auth=scram-sha-256','--no-sync'],timeout=180)
        print('Native PostgreSQL initialized:',run_id,flush=True)
        lease['safe_to_release']=False
        run([binaries/'pg_ctl.exe','-D',data,'-l',alias/log.relative_to(ROOT),'-o',f'-h 127.0.0.1 -p {port} -c max_connections=30 -c shared_buffers=32MB','-w','start'])
        started=True
        print('Running native Go integration tests',flush=True)
        report=ROOT/f'dev/tests/evidence/{round_id}-native-go-tests.jsonl';report.parent.mkdir(parents=True,exist_ok=True)
        with report.open('w',encoding='utf-8') as output:
            # Keep package-level browser recording and the native DB regression
            # sequential on this Windows host. Each test's real concurrent
            # goroutines/connections and ordinary 3s business locks are retained.
            process=subprocess.Popen([str(executable),'test','-json','-count=1','-p=1','-timeout=240s','./...'],cwd=ROOT/'dev/src',env=env,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,text=True,encoding='utf-8',errors='replace',creationflags=flags)
            for line in process.stdout:
                safe=line.replace(password,'[REDACTED]').replace(dsn,'[DATABASE_URL]');output.write(safe)
                try:
                    event=json.loads(safe)
                    if event.get('Action') in ('pass','fail','skip'):print(event['Action'],event.get('Package',''),event.get('Test',''),flush=True)
                    elif event.get('Action')=='output' and ('Error:' in event.get('Output','') or 'FAIL' in event.get('Output','')): print(event['Output'].strip(),flush=True)
                except ValueError: print(safe.strip(),flush=True)
            test_code=process.wait()
    except Exception as error:
        failure=type(error).__name__
        print('Native verification failed:',failure,flush=True)
    finally:
        try:
            if (data/'PG_VERSION').exists():
                status=subprocess.run([str(binaries/'pg_ctl.exe'),'-D',str(data),'status'],env=env,capture_output=True,timeout=10,creationflags=flags)
                if status.returncode==0:
                    try:
                        shutdown_mode='FAST'
                        run([binaries/'pg_ctl.exe','-D',data,'-m','fast','-w','-t','90','stop'],timeout=100)
                    except (RuntimeError,subprocess.TimeoutExpired):
                        # Only this disposable test instance may use immediate stop.
                        shutdown_mode='IMMEDIATE_DISPOSABLE_ONLY'
                        run([binaries/'pg_ctl.exe','-D',data,'-m','immediate','-w','-t','20','stop'],timeout=30)
                    stopped=subprocess.run([str(binaries/'pg_ctl.exe'),'-D',str(data),'status'],env=env,capture_output=True,timeout=10,creationflags=flags)
                    if stopped.returncode!=3:raise RuntimeError('Temporary database stop not confirmed')
                    stop_ok=True
                    print('Native PostgreSQL stopped',flush=True)
                elif status.returncode==3:
                    # A start failure or crash is not a successful suite run.
                    stop_ok=True
                    if started:failure=failure or 'DatabaseExitedBeforeCleanup'
                else:raise RuntimeError('Temporary database status unavailable')
                lease['safe_to_release']=stop_ok
        except Exception as error:
            cleanup_failure=type(error).__name__
            print('Temporary database cleanup requires recovery:',cleanup_failure,flush=True)
        finally:
            # This generated bootstrap credential is within the verified task path.
            password_file.unlink(missing_ok=True)
    summary={'round':round_id,'run_id':run_id,'postgresql':pg['version'],'test_exit_code':test_code,'temporary_instance_stopped':stop_ok,'virtualization_used':False,
             'package_parallelism':1,'intra_test_concurrency_retained':True,
             'shutdown_mode':shutdown_mode,'runner_error':failure,'cleanup_error':cleanup_failure,
             'evidence':f'dev/tests/evidence/{round_id}-native-go-tests.jsonl','scope':'Native PostgreSQL and Go source tests only; no GPU, Android, real payment or full product acceptance; not a power-loss durability test'}
    (ROOT/f'dev/tests/evidence/{round_id}-native-summary.json').write_text(json.dumps(summary,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
    return test_code if failure is None and cleanup_failure is None else 1

if __name__=='__main__':raise SystemExit(main())
