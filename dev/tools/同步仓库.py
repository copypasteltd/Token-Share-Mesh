"""核验并普通推送已授权的本项目；不强推、不覆盖远端历史。"""
from __future__ import annotations

import argparse
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import subprocess
import sys

ROOT=Path(__file__).resolve().parents[2]
REMOTE='git@github.com:copypasteltd/Token-Share-Mesh.git'


def git(*args, allow=(0,)):
    env=os.environ.copy()
    env['GIT_TERMINAL_PROMPT']='0'
    result=subprocess.run(['git',*args],cwd=ROOT,env=env,capture_output=True,
                          encoding='utf-8',errors='replace',timeout=240,
                          creationflags=getattr(subprocess,'CREATE_NO_WINDOW',0))
    if result.returncode not in allow:
        # These fixed commands never contain credentials or private payloads.
        print(result.stderr.strip(),file=sys.stderr)
        raise RuntimeError('Git '+args[0]+' failed')
    return result


def remote_head():
    rows=git('ls-remote','--heads','origin').stdout.splitlines()
    heads={row.split()[1]:row.split()[0] for row in rows}
    if heads and 'refs/heads/main' not in heads:
        raise RuntimeError('Remote has existing history without main; review before publishing')
    return heads.get('refs/heads/main')


def validate():
    for command in [['dev/tools/构建仓库文档.py','--check'],['dev/tools/构建设计需求推进.py','--check'],['dev/tools/核验仓库.py']]:
        result=subprocess.run([sys.executable,'-X','utf8',*command],cwd=ROOT,timeout=120,
                              creationflags=getattr(subprocess,'CREATE_NO_WINDOW',0))
        if result.returncode:
            raise RuntimeError('Public repository checks did not pass')
    for name in ['docs/评审/需求登记表-v3.0.json','docs/评审/白皮书契约核验-v3.0.json','docs/评审/技术开发文档核验-v1.0.json','dev/评审/开发细则核验-v1.0.json']:
        path=ROOT/name
        # A missing report must never be treated as a pass.
        if not path.is_file():
            raise RuntimeError('Required document report missing: '+name)
        if json.loads(path.read_text(encoding='utf-8')).get('passed') is not True:
            raise RuntimeError('Required document report failed: '+name)


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action',choices=['check','publish'])
    parser.add_argument('--message',help='Human-readable bilingual commit explanation')
    args=parser.parse_args()
    if git('remote','get-url','origin').stdout.strip()!=REMOTE or git('branch','--show-current').stdout.strip()!='main':
        raise RuntimeError('Expected project remote and main branch required')
    if Path(git('rev-parse','--show-toplevel').stdout.strip()).resolve()!=ROOT.resolve():
        raise RuntimeError('Refusing to stage a parent or unrelated repository')
    before=remote_head()
    local=git('rev-parse','--verify','HEAD',allow=(0,128)).stdout.strip() or None
    if before:
        git('fetch','--no-tags','origin','main')
        if local is None or git('merge-base','--is-ancestor',before,'HEAD',allow=(0,1)).returncode:
            raise RuntimeError('Remote is not an ancestor; integrate its history before publishing')
    validate()
    if args.action=='check':
        print(json.dumps({'status':'READY_FOR_REVIEWED_PUBLICATION','remote_main':before,'local_head':local},ensure_ascii=False))
        return 0
    if not args.message or '\x00' in args.message or len(args.message)>2000:
        parser.error('publish requires an explicit --message')
    names=set(git('ls-files','--cached','--others','--exclude-standard','-z').stdout.split('\0'))-{''}
    for name in names:
        path=(ROOT/name).resolve()
        if not path.is_relative_to(ROOT) or (ROOT/name).is_symlink():
            raise RuntimeError('Public file escapes the project or is a symlink')
    git('add','--all','--','.')
    changed=set(git('diff','--cached','--name-only','-z').stdout.split('\0'))-{''}
    if not changed:
        raise RuntimeError('No staged change; refusing an empty publication')
    if not {'README.md','README.zh-CN.md','CHANGELOG.md','CHANGELOG.zh-CN.md'}<=changed:
        raise RuntimeError('Each publication must update both READMEs and changelogs')
    directory=ROOT/'runtime/git';directory.mkdir(parents=True,exist_ok=True)
    message=directory/'commit-message.txt'
    message.write_text(args.message+'\n',encoding='utf-8')
    git('commit','--file',str(message))
    commit=git('rev-parse','HEAD').stdout.strip()
    print('Local commit created; pushing approved project history',flush=True)
    git('push','--set-upstream','origin','HEAD:refs/heads/main')
    confirmed=remote_head()
    if confirmed!=commit:
        raise RuntimeError('Remote commit differs; publication not confirmed')
    round_id=json.loads((ROOT/'dev/progress/implementation-status.json').read_text(encoding='utf-8'))['round']
    receipt={'round':round_id,'confirmed_at':datetime.now(timezone.utc).isoformat(),
             'remote':REMOTE,'branch':'main','local_commit':commit,'remote_commit':confirmed,
             'pushed':True,'force_push':False,'scope':'Source/docs/media snapshot; no platform deployment or full acceptance'}
    destination=directory/(round_id+'-publication.json')
    destination.write_text(json.dumps(receipt,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
    print(json.dumps(receipt,ensure_ascii=False))
    return 0


if __name__=='__main__':
    raise SystemExit(main())
