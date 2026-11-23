# Copyright 2026 NVIDIA CORPORATION
# SPDX-License-Identifier: Apache-2.0
"""Normalize captured Kubernetes objects without exporting pod environments or secrets."""
import collections
import json
import re
import zipfile
from pathlib import Path


def read_snapshot(path):
    with zipfile.ZipFile(path) as archive:
        return json.loads(archive.read('snapshot.json'))


def quantity(value):
    match = re.fullmatch(r'([+-]?(?:\d+(?:\.\d*)?|\.\d+)(?:[eE][+-]?\d+)?)([a-zA-Z]*)', str(value or 0))
    scales = {'': 1, 'n': 1e-9, 'u': 1e-6, 'm': .001, 'k': 1e3, 'K': 1e3,
              'M': 1e6, 'G': 1e9, 'T': 1e12, 'P': 1e15, 'E': 1e18,
              **{suffix+'i': 1024**power for power, suffix in enumerate('KMGTPE', 1)}}
    if not match or match[2] not in scales:
        raise ValueError(f'Unsupported Kubernetes quantity: {value}')
    return float(match[1]) * scales[match[2]]


def request(pod, key):
    def amount(container):
        return quantity(((container.get('resources') or {}).get('requests') or {}).get(key))
    spec = pod.get('spec') or {}
    regular = sum(amount(c) for c in spec.get('containers') or [])
    sidecars = peak = 0
    for container in spec.get('initContainers') or []:
        if container.get('restartPolicy') == 'Always':
            sidecars += amount(container)
            peak = max(peak, sidecars)
        else:
            peak = max(peak, sidecars + amount(container))
    result = max(regular + sidecars, peak) + quantity((spec.get('overhead') or {}).get(key))
    # Pod-level requests (where supported) set the pod budget; retain the larger effective request.
    pod_budget = ((spec.get('resources') or {}).get('requests') or {}).get(key)
    return max(result, quantity(pod_budget)) if pod_budget is not None else result


def resolve_workload(snapshot, value):
    namespace, _, name = value.rpartition('/')
    name = name or value
    groups = (snapshot.get('rawObjects') or {}).get('podGroups') or []
    eligible = [g for g in groups if not namespace or g['metadata'].get('namespace', 'default') == namespace]
    exact = [g for g in eligible if g['metadata']['name'] == name]
    matches = exact or [g for g in eligible if any(o.get('name') == name for o in g['metadata'].get('ownerReferences') or [])
                        or re.search(r'^name:\s*'+re.escape(name)+r'\s*$',
                            (g['metadata'].get('annotations') or {}).get('kai.scheduler/top-owner-metadata', ''), re.M)]
    if len(matches) != 1:
        candidates = [g['metadata'].get('namespace', 'default')+'/'+g['metadata']['name'] for g in matches]
        raise ValueError(f'Workload must resolve to one PodGroup; found {len(matches)}: {candidates}')
    return matches[0]


def cluster_data(snapshot, filename):
    raw = snapshot.get('rawObjects') or {}
    objects = lambda key: raw.get(key) or []
    pg_queue = {(g['metadata'].get('namespace', 'default'), g['metadata']['name']): (g.get('spec') or {}).get('queue')
                for g in objects('podGroups')}
    queue_label = (snapshot.get('schedulerParams') or {}).get('queueLabelKey') or 'kai.scheduler/queue'
    def queue(pod):
        meta = pod['metadata']
        return ((meta.get('labels') or {}).get(queue_label)
                or pg_queue.get((meta.get('namespace', 'default'), (meta.get('annotations') or {}).get('pod-group-name')))
                or 'Unlabelled')
    namespaces = collections.defaultdict(lambda: dict(phases=collections.Counter(), gpu=0, queues=set()))
    assigned = collections.defaultdict(list)
    pending = collections.Counter()
    phases = collections.Counter()
    for pod in objects('pods'):
        phase = (pod.get('status') or {}).get('phase', 'Unknown')
        name = pod['metadata'].get('namespace', 'default')
        q = queue(pod)
        phases[phase] += 1
        namespaces[name]['phases'][phase] += 1
        namespaces[name]['queues'].add(q)
        if phase not in ('Succeeded', 'Failed'):
            namespaces[name]['gpu'] += request(pod, 'nvidia.com/gpu')
            if pod['spec'].get('nodeName'):
                assigned[pod['spec']['nodeName']].append(pod)
        if phase == 'Pending' and not pod['spec'].get('nodeName'):
            pending[q] += 1
    topology_keys = [level.get('nodeLabel') for topology in objects('topologies')
                     for level in (topology.get('spec') or {}).get('levels') or []]
    node_pool = ((snapshot.get('schedulerParams') or {}).get('partitionParams') or {}).get('NodePoolLabelKey', 'kai.scheduler/node-pool')
    keys = topology_keys + ['nvidia.com/gpu.clique', node_pool]
    group_key = next((key for key in keys if key and key != 'kubernetes.io/hostname'
                      and any(key in (n['metadata'].get('labels') or {}) for n in objects('nodes'))), None)
    nodes = []
    node_names = sorted(n['metadata']['name'] for n in objects('nodes'))
    for index, node in enumerate(sorted(objects('nodes'), key=lambda n: n['metadata']['name'])):
        meta = node['metadata']; labels = meta.get('labels') or {}; status = node.get('status') or {}
        name = meta['name']; pods = assigned[name]; buckets = collections.defaultdict(lambda: [0, 0])
        for pod in pods:
            b = buckets[pod['metadata'].get('namespace', 'default')+' / '+queue(pod)]
            b[0] += 1; b[1] += request(pod, 'nvidia.com/gpu')
        rack = re.search(r'r\d+-\d+', name)
        identifier = rack[0] if rack else name.split('.')[0]
        if len(identifier) > 8:
            suffix = identifier.rsplit('-', 1)[-1]
            identifier = suffix if len(suffix) <= 8 and sum(x.split('.')[0].rsplit('-', 1)[-1] == suffix for x in node_names) == 1 else f'N{index+1:03}'
        nodes.append(dict(name=name, id=identifier, clique=labels.get(group_key, 'No topology label') if group_key else 'All nodes',
            product=labels.get('nvidia.com/gpu.product', 'CPU / GPU type unlabelled'),
            ready=next((c['status'] for c in status.get('conditions') or [] if c['type'] == 'Ready'), 'Unknown'),
            cordoned=bool((node.get('spec') or {}).get('unschedulable')), taints=(node.get('spec') or {}).get('taints') or [],
            gpu=quantity((status.get('capacity') or {}).get('nvidia.com/gpu')),
            alloc=quantity((status.get('allocatable') or {}).get('nvidia.com/gpu')),
            cpu=quantity((status.get('allocatable') or {}).get('cpu')),
            memory=round(quantity((status.get('allocatable') or {}).get('memory')) / 1024**3),
            requested=sum(request(p, 'nvidia.com/gpu') for p in pods), pods=len(pods),
            pending=sum((p.get('status') or {}).get('phase') == 'Pending' for p in pods),
            workloads=[dict(name=k, pods=v[0], gpu=v[1]) for k,v in sorted(buckets.items(), key=lambda item: -item[1][1])],
            role='control plane' if 'node-role.kubernetes.io/control-plane' in labels else 'worker'))
    groups = collections.defaultdict(list)
    for i,n in enumerate(nodes): groups[n['clique']].append(i)
    normalized_groups = []
    for key, members in sorted(groups.items(), key=lambda item: (item[0] == 'No topology label', min(nodes[i]['id'] for i in item[1]))):
        label = ('CPU nodes' if all(nodes[i]['gpu'] == 0 for i in members) and key == 'No topology label' else key)
        if group_key == 'nvidia.com/gpu.clique' and key != 'No topology label': label = f'Clique {len(normalized_groups)+1}'
        normalized_groups.append(dict(id=key, label=label, nodes=members))
    pg_counts = collections.Counter((g.get('spec') or {}).get('queue', 'Unlabelled') for g in objects('podGroups'))
    queues = []
    for q in sorted(objects('queues'), key=lambda q: q['metadata']['name']):
        spec=q.get('spec') or {}; status=q.get('status') or {}; gpu=(spec.get('resources') or {}).get('gpu') or {}; name=q['metadata']['name']
        queues.append(dict(name=name, parent=spec.get('parentQueue'), quota=gpu.get('quota', 0), limit=gpu.get('limit', -1),
            allocated=quantity((status.get('allocated') or {}).get('nvidia.com/gpu')), requested=quantity((status.get('requested') or {}).get('nvidia.com/gpu')),
            priority=spec.get('priority', 0), pending=pending[name], podGroups=pg_counts[name]))
    for values in namespaces.values(): values['queues'] = sorted(values['queues'])
    capture = re.search(r'(\d{8})T(\d{6})Z', Path(filename).name)
    if capture:
        from datetime import datetime
        capture = datetime.strptime(''.join(capture.groups()), '%Y%m%d%H%M%S').strftime('%d %b %Y · %H:%M UTC')+' · capture time from filename'
    else: capture = Path(filename).name+' · captured scheduler state'
    return dict(nodes=nodes, groups=normalized_groups, queues=queues, namespaces=dict(namespaces), phases=dict(phases),
        unbound=sum(pending.values()), counts={k:len(v or []) for k,v in raw.items() if v is None or isinstance(v, list)},
        storage=[dict(name=s['metadata']['name'], provisioner=s.get('provisioner', '')) for s in objects('storageClasses')],
        drivers=dict(collections.Counter(s['spec']['driver'] for s in objects('resourceSlices'))),
        capture=capture, topologyLabel=group_key,
        gpuAccounting='Free GPUs = allocatable nvidia.com/gpu minus active assigned pod requests. This excludes queue, taint, topology, DRA-device and health eligibility; requests are not measured utilization.')
