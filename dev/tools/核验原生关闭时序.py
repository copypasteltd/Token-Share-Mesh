"""Repeat two unchanged native shutdown cases; never replace full-round evidence."""
from __future__ import annotations
from datetime import datetime,timezone
import hashlib,json,subprocess
from 原生开发 import ROOT,environment

def sha(path):return hashlib.sha256(path.read_bytes()).hexdigest()
def main():
 binary,env=environment();env['GOPROXY']='off'
 directory=ROOT/'dev/tests/evidence';raw=directory/'R022-native-timing-replay.jsonl'
 command=[str(binary),'test','-p','1','-count=3','-timeout=90s','-run=^(TestNodeControlDrainStopsSupervisedOriginalOwner|TestSupervisorRequiresWatchdogAndFrozenRegistry)$','-json','./client/node','./client/supervisor']
 started=datetime.now(timezone.utc).isoformat()
 with raw.open('wb') as output:
  result=subprocess.run(command,cwd=ROOT/'dev/src',env=env,stdout=output,stderr=subprocess.STDOUT,timeout=180)
 events=[]
 for line in raw.read_text(encoding='utf-8').splitlines():
  try:events.append(json.loads(line))
  except json.JSONDecodeError:pass
 terminals=[{'package':e['Package'],'test':e['Test'],'action':e['Action']} for e in events if e.get('Test') and '/' not in e['Test'] and e['Action'] in ('pass','fail','skip')]
 paths=['dev/src/client/node/supervisor_test.go','dev/src/client/node/supervisor.go','dev/src/client/node/control.go','dev/src/client/supervisor/supervisor.go','dev/src/client/supervisor/supervisor_test.go','dev/src/client/execution/gate.go','dev/src/client/execution/store.go']
 source=[]
 for name in paths:
  previous=subprocess.check_output(['git','show','490805930aa42b9b124639a52f3939a77295b4cc:'+name],cwd=ROOT)
  source.append({'path':name,'sha256':sha(ROOT/name),'base_sha256':hashlib.sha256(previous).hexdigest(),'unchanged_from_R021':previous==(ROOT/name).read_bytes()})
 passed=result.returncode==0 and len(terminals)==6 and all(e['action']=='pass' for e in terminals)
 receipt={'round':'R022','recorded_at':started,'returncode':result.returncode,'passed':passed,'virtualization_used':False,'scope':'two unchanged original shutdown cases, three repetitions each, not full-round or timing-stability acceptance','command':['go',*command[1:]],'raw':raw.relative_to(ROOT).as_posix(),'raw_sha256':sha(raw),'terminals':terminals,'original_paths':source,'tool_sha256':sha(__import__('pathlib').Path(__file__)),'limitation':'ErrShutdown cause remains unproven. No original timeout, assertion, SQLite durability or cleanup gate was relaxed; a later pass cannot erase the original failure.'}
 (directory/'R022-native-timing-replay.json').write_text(json.dumps(receipt,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
 print(json.dumps({'passed':passed,'terminal_cases':len(terminals),'unchanged_original_paths':all(x['unchanged_from_R021'] for x in source)}))
 return 0 if passed else 1

if __name__=='__main__':raise SystemExit(main())
