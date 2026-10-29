"""项目原生 Go 命令入口；缓存/临时文件均写 runtime。"""
from __future__ import annotations
import json
import os
from pathlib import Path
import subprocess
import sys

ROOT=Path(__file__).resolve().parents[2]

def environment():
    lock=json.loads((ROOT/'dev/toolchain.lock.json').read_text(encoding='utf-8'))
    go=next(t for t in lock['tools'] if t['name']=='go')
    binary=ROOT/go['directory']/'go/bin/go.exe'
    env=os.environ.copy()
    for key,sub in [('GOCACHE','cache/go-build'),('GOMODCACHE','cache/go-mod'),('GOPATH','tooling/gopath'),('GOTMPDIR','tmp/go'),('TMP','tmp/go'),('TEMP','tmp/go')]:
        directory=ROOT/'runtime'/sub;directory.mkdir(parents=True,exist_ok=True);env[key]=str(directory)
    env['GOENV']='off';env['GOTOOLCHAIN']='local';env['CGO_ENABLED']='0'
    # Regional mirror transports modules only; Go's signed checksum database
    # remains enabled and go.sum pins every downloaded module.
    env['GOPROXY']=env.get('P11_GOPROXY','https://goproxy.cn,direct')
    env['PATH']=str(binary.parent)+os.pathsep+env.get('PATH','')
    return binary,env

def main():
    binary,env=environment()
    return subprocess.call([str(binary),*sys.argv[1:]],cwd=ROOT/'dev/src',env=env)

if __name__=='__main__':raise SystemExit(main())
