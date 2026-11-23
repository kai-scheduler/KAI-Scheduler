# Copyright 2026 NVIDIA CORPORATION
# SPDX-License-Identifier: Apache-2.0
"""Build the job overlap graph and a complete tree presentation with shared references."""
from collections import defaultdict, deque


def build_tree(replay, order):
    ranks={j['namespace']+'/'+j['name']:j['rank'] for j in order['jobs']}
    root=replay['namespace']+'/'+replay['workload']
    jobs={root:dict(id=root,namespace=replay['namespace'],name=replay['workload'],queue=replay['queue'],rank=None,victims=[],placements=[dict(name=t['name'],node=t['node'],gpu=t['gpu']) for t in replay['targets']])}
    for victim in replay['victims']:
        key=victim['namespace']+'/'+victim['podGroup']
        job=jobs.setdefault(key,dict(id=key,namespace=victim['namespace'],name=victim['podGroup'],queue=victim['queue'],rank=ranks.get(key),victims=[],placements=[]))
        job['victims'].append(dict(name=victim['name'],node=victim['source'],gpu=victim['gpu'],destination=victim['destination'],status=victim['finalStatus']))
        if victim['destination']:job['placements'].append(dict(name=victim['name'],node=victim['destination'],gpu=victim['gpu']))
    edges=[];children=defaultdict(list)
    for parent,job in jobs.items():
        by_node=defaultdict(list)
        for p in job['placements']:by_node[p['node']].append(p)
        for child,victim in jobs.items():
            hits=[p for p in victim['victims'] if p['node'] in by_node]
            if not hits:continue
            edge=dict(id=len(edges),parent=parent,child=child,pods=hits,nodes=[dict(name=n,parentPods=by_node[n],victimPods=[p for p in hits if p['node']==n]) for n in sorted({p['node'] for p in hits})])
            edges.append(edge);children[parent].append(edge['id'])
    for parent in children:children[parent].sort(key=lambda ei:(jobs[edges[ei]['child']]['rank'] or 0,edges[ei]['child']))
    depth={root:0};canonical={};pending=deque([root])
    while pending:
        parent=pending.popleft()
        for ei in children[parent]:
            child=edges[ei]['child']
            if child not in depth:depth[child]=depth[parent]+1;canonical[child]=ei;pending.append(child)
    reachable=set(depth); reachable_edges=[e for e in edges if e['parent'] in reachable]
    # Every job expands once. All other edges are references, including back edges/cycles.
    return dict(root=root,jobs=list(jobs.values()),edges=edges,children=dict(children),canonical=canonical,depth=depth,unreachable=sorted(set(jobs)-reachable),
        stats=dict(directChildren=len(children[root]),reachableVictimJobs=len(reachable)-1,totalVictimJobs=len(jobs)-1,edges=len(reachable_edges),referenceEdges=len(reachable_edges)-len(reachable)+1,maxDepth=max(depth.values()),reachableVictimPods=sum(len(jobs[k]['victims']) for k in reachable)),
        rule='Parent A → child B when A has a pipeline reservation on a node from which a victim pod of B was evicted. Apply recursively within this reclaim statement.',
        note='These are node-overlap dependencies, not separate scheduler reclaim actions for every parent. Shared jobs and cycles use reference entries. A disconnected job is an observation to investigate, not proof that its eviction was unnecessary.',ref=replay['ref'])
