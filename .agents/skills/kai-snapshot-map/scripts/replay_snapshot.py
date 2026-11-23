# Copyright 2026 NVIDIA CORPORATION
# SPDX-License-Identifier: Apache-2.0
"""Replay with temporary overlays capturing a single reclaimer's solution before commit."""
import argparse
import collections
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
from snapshot_data import read_snapshot, request, resolve_workload

ASSETS = Path(__file__).resolve().parents[1] / 'assets'


def replace_once(source, old, new):
    if source.count(old) != 1:
        raise ValueError(f'Trace hook is incompatible with this source version: {old[:100]!r}. Adapt the overlay; do not guess.')
    return source.replace(old, new, 1)


def prepare_overlay(repo, output):
    source = repo / 'pkg/scheduler/actions/reclaim/reclaim.go'
    code = source.read_text()
    code = replace_once(code, 'import (', 'import (\n "encoding/json"\n "os"\n "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"')
    code = replace_once(code, 'func getOrderedVictimsQueue(', 'func baseMapOrderedVictimsQueue(')
    anchor = '\tqueue := ssn.ClusterInfo.Queues[reclaimer.Queue]'
    code = replace_once(code, anchor, '\tmapTraceSession(ssn,reclaimer,"before-target")\n'+anchor)
    anchor = '\t\tif err := statement.Commit(); err != nil {'
    code = replace_once(code, anchor, '\t\tmapTraceSession(ssn,job,"target-solution")\n'+anchor+'\n\t\t\tif mapTraceMatches(job) { mapTraceWrite("commit-error",err.Error()) }')
    code += '\n'+(ASSETS / 'reclaim-trace.go.txt').read_text()
    overlay_source = output / 'reclaim-trace.go'
    overlay_source.write_text(code)
    (output / 'overlay.json').write_text(json.dumps({'Replace': {str(source): str(overlay_source)}}))
    main = (repo / 'cmd/snapshot-tool/main.go').read_text()
    anchor = '\n\tif *memprofile != "" {'
    export = '''
 schedulerCache.WaitForWorkers(stopCh)
 time.Sleep(1500 * time.Millisecond)
 replayEvents, eventErr := kubeClient.CoreV1().Events("").List(context.Background(),v1.ListOptions{})
 if eventErr != nil { panic(eventErr) }
 eventBytes, marshalErr := json.MarshalIndent(replayEvents,"","  ")
 if marshalErr != nil { panic(marshalErr) }
 if writeErr := os.WriteFile(os.Getenv("KAI_MAP_OUTPUT")+string(os.PathSeparator)+"events.json",eventBytes,0600); writeErr != nil { panic(writeErr) }
'''
    main = replace_once(main, anchor, '\n'+export+anchor)
    (output / 'trace-main.go').write_text(main)


def normalize_replay(snapshot, target, directory, provenance):
    raw = snapshot['rawObjects']; meta = target['metadata']; name = meta['name']; namespace = meta.get('namespace', 'default')
    load = lambda n, default: json.loads((directory/n).read_text()) if (directory/n).exists() else default
    before = {(p['namespace'],p['name']):p for p in load('before-target.json', [])}
    solution = {(p['namespace'],p['name']):p for p in load('target-solution.json', [])}
    pods = {(p['metadata'].get('namespace','default'),p['metadata']['name']):p for p in raw.get('pods') or []}
    def brief(key):
        p=pods[key]
        return dict(namespace=key[0],name=key[1],gpu=request(p,'nvidia.com/gpu'),queue=(p['metadata'].get('labels') or {}).get('kai.scheduler/queue','Unlabelled'),
                    podGroup=(p['metadata'].get('annotations') or {}).get('pod-group-name'),snapshotPhase=(p.get('status') or {}).get('phase'),snapshotNode=p['spec'].get('nodeName'))
    events = load('events.json', {}).get('items', [])
    pipelines = collections.defaultdict(set)
    for event in events:
        if event['reason'] == 'Pipelined':
            match = re.search(r'Pod ([^/]+)/([^ ]+) was pipelined to node ([^ ]+)', event.get('message',''))
            if match: pipelines[match[1],match[2]].add(match[3])
    victims=[]; seen=set(); evidence=[]
    for event in events:
        annotations=event['metadata'].get('annotations') or {}
        if event['reason']!='Evict' or annotations.get('evictor-pod-group-name')!=name or annotations.get('evictor-pod-group-namespace')!=namespace: continue
        match=re.search(r'Pod ([^/]+)/([^ ]+) was',event['message'])
        if not match: raise ValueError('Cannot resolve victim pod from replay event')
        key=match[1],match[2]
        if key in seen: continue
        seen.add(key)
        if key not in before or key not in solution: raise ValueError(f'Missing target-statement state for {key}')
        state=solution[key]
        if state['status'] not in ('Pipelined','Releasing'):raise ValueError(f'Committed eviction has an unexpected target-solution status: {key}: {state["status"]}')
        destination=state['node'] if state['status']=='Pipelined' else None
        if destination and destination not in pipelines[key]: raise ValueError(f'No committed pipeline event corroborates target solution for {key}')
        victims.append(dict(**brief(key),source=before[key]['node'],destination=destination,finalStatus=state['status'],action=annotations['evictor-action-type']))
        evidence.append(event)
    targets=[]
    for key,state in solution.items():
        if state['job']==name and state['namespace']==namespace and state['status'] in ('Pipelined','Binding','Allocated','Bound'):
            if state['status']=='Pipelined' and state['node'] not in pipelines[key]: raise ValueError(f'Target pipeline was not committed: {key}')
            targets.append(dict(**brief(key),node=state['node'],status=state['status']))
    log=(directory/'replay.log').read_text(); clean=re.sub(r'\x1b\[[0-9;]*m','',log)
    timing={}
    match=re.search(r'\{\s*"total_duration_ms"',clean)
    if match: timing=json.JSONDecoder().raw_decode(clean[match.start():])[0].get('action_durations_ms',{})
    commit_error=load('commit-error.json',None)
    if commit_error: raise ValueError(f'Target commit failed ({commit_error}); do not render its speculative solution as committed. Evidence: {directory}')
    note='Simulation of the supplied capture on the recorded checkout; original live decisions and current utilization are not established.'
    if not solution: note+=' This workload did not produce a successful reclaim solution in this cycle.'
    error_count=clean.count('\tERROR\t')
    if error_count: note+=f' Replay contains {error_count} error log entries; inspect replay.log before interpreting causal findings.'
    replay=dict(workload=name,namespace=namespace,queue=target['spec'].get('queue','Unlabelled'),action='reclaim',ref=provenance['ref'],timings=timing,
        targets=sorted(targets,key=lambda p:p['node']),victims=sorted(victims,key=lambda p:(p['source'],p['name'])),note=note,
        command=provenance['command'],instrumentation='Temporary Go overlays record pre-target state and the successful target solution immediately before commit. Fake-client events corroborate committed operations; later jobs never supply target placement state.',
        placementBasis='Target statement solution at commit, before later reclaim attempts.',provenance=provenance)
    order=load('victim-order.json',[]); by_key={(v['namespace'],v['name']):v for v in victims}
    for job in order:
        job['pods'].sort(key=lambda p:(p['node'],p['name']))
        for pod in job['pods']:
            key=pod['namespace'],pod['name']; v=by_key.get(key)
            pod.update(evicted=bool(v),outcome=v['finalStatus'] if v else 'Not evicted',gpu=request(pods[key],'nvidia.com/gpu'))
    order_data=dict(workload=name,ref=provenance['ref'],jobs=order,basis='Initial full victim-candidate queue before solving this reclaim. Rank 1 is popped first; multi-node jobs retain the same rank on each occupied node.',
        method='Drain an independent victim queue built from the same cached candidates, leaving the solver queue intact.',validation='All recorded eviction events match the target reclaimer and use pre-target source nodes.',command=provenance['command'],note=note)
    for filename,value in [('replay-data.json',replay),('order-data.json',order_data),('target-events.json',evidence)]: (directory/filename).write_text(json.dumps(value,indent=2))
    return replay,order_data


def run_replay(repo, snapshot_path, reclaimer, output, timeout=180):
    repo=Path(repo).resolve(); snapshot_path=Path(snapshot_path).resolve(); output=Path(output).resolve()
    output.mkdir(parents=True,exist_ok=True)
    # Isolate every invocation: stale files must not stand in for a missing solution.
    import tempfile
    run=Path(tempfile.mkdtemp(prefix='run-',dir=output))
    snapshot=read_snapshot(snapshot_path); target=resolve_workload(snapshot,reclaimer)
    prepare_overlay(repo,run)
    env={**os.environ,'GOCACHE':str(output/'go-cache'),'KAI_MAP_TARGET':target['metadata'].get('namespace','default')+'/'+target['metadata']['name'],'KAI_MAP_OUTPUT':str(run)}
    binary=run/'snapshot-tool'
    with (run/'build.log').open('w') as log:
        subprocess.run(['go','build','-mod=readonly','-overlay='+str(run/'overlay.json'),'-o',str(binary),str(run/'trace-main.go')],cwd=repo,env=env,stdout=log,stderr=subprocess.STDOUT,check=True,timeout=timeout)
    command=[str(binary),'--filename',str(snapshot_path),'--verbosity','4']
    provenance=dict(ref=subprocess.check_output(['git','rev-parse','HEAD'],cwd=repo,text=True).strip(),
        dirty=subprocess.check_output(['git','status','--short'],cwd=repo,text=True).splitlines(),command=' '.join(command),snapshot=str(snapshot_path),snapshotSha256=hashlib.sha256(snapshot_path.read_bytes()).hexdigest())
    (run/'provenance.json').write_text(json.dumps(provenance,indent=2))
    with (run/'replay.log').open('w') as log:
        subprocess.run(command,cwd=repo,env=env,stdout=log,stderr=subprocess.STDOUT,check=True,timeout=timeout)
    normalize_replay(snapshot,target,run,provenance)
    return run


def main():
    p=argparse.ArgumentParser(description=__doc__);p.add_argument('--repo',required=True);p.add_argument('--snapshot',required=True);p.add_argument('--reclaimer',required=True);p.add_argument('--output',required=True);p.add_argument('--timeout',type=int,default=180)
    a=p.parse_args();print(run_replay(a.repo,a.snapshot,a.reclaimer,a.output,a.timeout))

if __name__=='__main__': main()
