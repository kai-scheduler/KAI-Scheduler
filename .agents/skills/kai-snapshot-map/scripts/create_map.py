# Copyright 2026 NVIDIA CORPORATION
# SPDX-License-Identifier: Apache-2.0
"""Render three overview tabs, optionally adding a reclaimer's three analysis tabs."""
import argparse
import html
import hashlib
import json
from pathlib import Path
import re
from snapshot_data import read_snapshot, cluster_data, resolve_workload
from victim_tree import build_tree

ASSETS=Path(__file__).resolve().parents[1]/'assets'


def embedded(value):
    return json.dumps(value,separators=(',',':')).replace('<','\\u003c').replace('>','\\u003e').replace('&','\\u0026')


def render(snapshot, output, snapshot_name, replay=None, order=None):
    data=cluster_data(snapshot,snapshot_name)
    for key in ['persistentVolumes','persistentVolumeClaims','storageClasses','resourceClaims','resourceSlices','deviceClasses']:
        data['counts'].setdefault(key,0)
    source=(ASSETS/'cluster-map.html').read_text()
    with_replay=replay is not None
    if with_replay:
        if order is None:raise ValueError('Reclaim rendering requires order-data.json, even when its jobs list is empty.')
        known={n['name'] for n in data['nodes']}
        for victim in replay['victims']:
            if victim['source'] not in known or (victim['destination'] and victim['destination'] not in known):
                raise ValueError('Replay references a node absent from this snapshot')
        tree=build_tree(replay,order)
        source=source.replace('<!--RECLAIM_BEGIN-->','').replace('<!--RECLAIM_END-->','').replace('<!--OPTIONAL_JS_BEGIN-->','').replace('<!--OPTIONAL_JS_END-->','')
        for marker,value in [('__REPLAY_DATA__',replay),('__ORDER_DATA__',order),('__TREE_DATA__',tree)]:source=source.replace(marker,embedded(value))
    else:
        source=re.sub(r'<!--RECLAIM_BEGIN-->.*?<!--RECLAIM_END-->','',source,flags=re.S)
        source=re.sub(r'<!--OPTIONAL_JS_BEGIN-->.*?<!--OPTIONAL_JS_END-->','',source,flags=re.S)
    fmt=lambda n:f'{n:,.2f}'.rstrip('0').rstrip('.') if not float(n).is_integer() else f'{int(n):,}'
    products={}
    for n in data['nodes']:products[n['product']]=products.get(n['product'],0)+1
    metrics=[f"{len(data['nodes']):,} nodes · "+' + '.join(f'{count:,} {name}' for name,count in sorted(products.items())),
             f"{fmt(sum(n['gpu'] for n in data['nodes']))} GPUs · {fmt(sum(n['alloc'] for n in data['nodes']))} allocatable",
             f"{data['counts'].get('pods',0):,} pod records · {len(data['queues'])} queues"]
    source=source.replace('__METRICS__',''.join('<span>'+html.escape(m)+'</span>' for m in metrics))
    source=source.replace('__CAPTURE__',html.escape(data['capture']))
    source=source.replace('__PHASES__',''.join(f'<span>{count:,} {html.escape(name)}</span>' for name,count in sorted(data['phases'].items())))
    source=source.replace('__PENDING__',f"Pending: {data['unbound']:,} unassigned · {data['phases'].get('Pending',0)-data['unbound']:,} already assigned to nodes")
    source=source.replace('__CLUSTER_DATA__',embedded(data))
    if re.search(r'__[A-Z_]+__',source):raise ValueError('Unfilled template marker')
    if len(source.encode())>=1_000_000:raise ValueError('Map exceeds 1 MB: aggregate namespace/pod detail or split the visual before delivery.')
    output=Path(output).resolve();output.parent.mkdir(parents=True,exist_ok=True);output.write_text(source)
    output.with_name(output.stem+'-cluster-data.json').write_text(json.dumps(data,indent=2))
    return output


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--snapshot',required=True);parser.add_argument('--output',required=True)
    parser.add_argument('--reclaimer');parser.add_argument('--repo',help='KAI checkout for native snapshot replay')
    parser.add_argument('--replay-dir',help='Existing run directory containing replay-data.json and order-data.json')
    parser.add_argument('--timeout',type=int,default=180)
    args=parser.parse_args();snapshot=read_snapshot(args.snapshot)
    replay=order=None
    if args.reclaimer:
        target=resolve_workload(snapshot,args.reclaimer)
        if args.replay_dir:directory=Path(args.replay_dir)
        else:
            if not args.repo:parser.error('--repo is required for reclaimer mode unless --replay-dir is provided')
            from replay_snapshot import run_replay
            directory=run_replay(args.repo,args.snapshot,args.reclaimer,Path(args.output).resolve().parent/'replay',args.timeout)
        replay=json.loads((directory/'replay-data.json').read_text());order=json.loads((directory/'order-data.json').read_text())
        expected_hash=(replay.get('provenance') or {}).get('snapshotSha256')
        if expected_hash and hashlib.sha256(Path(args.snapshot).read_bytes()).hexdigest()!=expected_hash:raise ValueError('Replay snapshot hash does not match the supplied capture')
        if (replay['namespace'],replay['workload'])!=(target['metadata'].get('namespace','default'),target['metadata']['name']):raise ValueError('Replay belongs to a different workload')
    elif args.replay_dir:parser.error('--replay-dir requires --reclaimer; omit both for the three overview tabs')
    print(render(snapshot,args.output,args.snapshot,replay,order))

if __name__=='__main__':main()
