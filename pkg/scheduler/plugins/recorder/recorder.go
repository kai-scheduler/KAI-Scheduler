// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package recorder

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"

	kubeaischedulerinfo "github.com/kai-scheduler/api/client/informers/externalversions"
	enginev2alpha2listers "github.com/kai-scheduler/api/client/listers/scheduling/v2alpha2"
	commonconstants "github.com/kai-scheduler/api/constants"
	schedulingv1alpha2 "github.com/kai-scheduler/api/scheduling/v1alpha2"
	enginev2alpha2 "github.com/kai-scheduler/api/scheduling/v2alpha2"

	"github.com/kai-scheduler/KAI-scheduler/pkg/recorder"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/conf"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/framework"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/snapshot"
	schedrecorder "github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/recorder"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/version"
)

const (
	Name = "recorder"

	defaultCycleBuffer     = 4
	defaultLifecycleBuffer = 1024
)

type enqueueFunc func(stream, kind string, payload any) bool

type recorderPlugin struct {
	enqueue     enqueueFunc
	clusterName string
	shardName   string

	sessionID string
	openedAt  time.Time
	raw       *snapshot.RawKubernetesObjects
	config    *conf.SchedulerConfiguration
	params    *conf.SchedulerParams
	discovery chan *snapshot.DiscoverySnapshot
	partial   bool

	mu        sync.Mutex
	stats     map[string]*recorder.ActionStat
	committed bool
}

var (
	informersOnce           sync.Once
	kaiInformersUnavailable bool
)

type kaiInformerCache interface {
	KubeAiSchedulerInformerFactory() kubeaischedulerinfo.SharedInformerFactory
}

func New(arguments framework.PluginArguments) framework.Plugin {
	cycleBuffer, err := arguments.GetInt("cycleBuffer", defaultCycleBuffer)
	if err != nil {
		log.InfraLogger.Errorf("Recorder cycleBuffer: %v", err)
		cycleBuffer = defaultCycleBuffer
	}
	lifecycleBuffer, err := arguments.GetInt("lifecycleBuffer", defaultLifecycleBuffer)
	if err != nil {
		log.InfraLogger.Errorf("Recorder lifecycleBuffer: %v", err)
		lifecycleBuffer = defaultLifecycleBuffer
	}
	sender := schedrecorder.EnsureStarted(schedrecorder.Config{
		Address:         arguments.GetString("address", ""),
		CycleBuffer:     cycleBuffer,
		LifecycleBuffer: lifecycleBuffer,
	})
	return newPlugin(arguments, sender.TryEnqueue)
}

func newPlugin(arguments framework.PluginArguments, enqueue enqueueFunc) *recorderPlugin {
	return &recorderPlugin{
		enqueue:     enqueue,
		clusterName: arguments.GetString("clusterName", ""),
		shardName:   arguments.GetString("shardName", ""),
		stats:       map[string]*recorder.ActionStat{},
	}
}

func (p *recorderPlugin) Name() string {
	return Name
}

func (p *recorderPlugin) OnSessionOpen(ssn *framework.Session) {
	p.sessionID = ssn.ID
	p.openedAt = time.Now().UTC()
	p.stats = map[string]*recorder.ActionStat{}
	p.committed = false
	p.partial = false
	p.config = cloneConfig(ssn.Config)
	p.params = cloneParams(&ssn.SchedulerParams)

	if ssn.Cache != nil {
		p.raw = snapshot.CollectRawObjects(ssn.Cache.GetDataLister())
		if p.raw == nil {
			p.partial = true
		}
		p.discovery = make(chan *snapshot.DiscoverySnapshot, 1)
		client := ssn.Cache.KubeClient()
		go func() {
			if client == nil {
				p.discovery <- &snapshot.DiscoverySnapshot{}
				return
			}
			p.discovery <- snapshot.CollectDiscovery(context.Background(), client.Discovery())
		}()
		informersOnce.Do(func() { p.registerInformers(ssn) })
		if kaiInformersUnavailable {
			p.partial = true
		}
	} else {
		p.partial = true
	}

	ssn.AddEventHandler(&framework.EventHandler{OnCommit: p.onCommit})
}

func (p *recorderPlugin) OnSessionClose(ssn *framework.Session) {
	pending := hasPending(ssn)
	if !pending && !p.committed {
		return
	}
	discovery, partialDiscovery := p.takeDiscovery()
	p.mu.Lock()
	stats := p.snapshotStats(ssn)
	committed := p.committed
	p.mu.Unlock()

	p.enqueue(recorder.StreamCycle, recorder.KindState, recorder.StateRecord{
		SessionID:       p.sessionID,
		Partial:         p.partial || partialDiscovery,
		CommitSHA:       version.GitCommit(),
		ClusterName:     p.clusterName,
		ShardName:       p.shardName,
		OpenedAt:        p.openedAt,
		Config:          p.config,
		SchedulerParams: p.params,
		RawObjects:      p.raw,
		Discovery:       discovery,
	})
	p.enqueue(recorder.StreamCycle, recorder.KindSessionEnd, recorder.SessionEndRecord{
		SessionID:   p.sessionID,
		Pending:     pending,
		Committed:   committed,
		Partial:     p.partial || partialDiscovery,
		ActionStats: stats,
		FailReasons: failReasons(ssn),
	})
}

func (p *recorderPlugin) onCommit(event *framework.CommitEvent) {
	if event == nil || p.enqueue == nil {
		return
	}
	p.mu.Lock()
	stat := p.stat(event.Action)
	if event.DiscardedOpCount > 0 && len(event.Ops) == 0 {
		stat.DiscardedAttempts++
		stat.DiscardedOps += event.DiscardedOpCount
		p.mu.Unlock()
		return
	}
	for _, op := range event.Ops {
		if !op.NotAttempted {
			p.committed = true
			stat.Commits++
			break
		}
	}
	p.mu.Unlock()
	p.enqueue(recorder.StreamCycle, recorder.KindDecision, decisionRecord(event))
}

func decisionRecord(event *framework.CommitEvent) recorder.DecisionRecord {
	ops := make([]recorder.DecisionOp, len(event.Ops))
	for i, op := range event.Ops {
		eviction := op.Eviction
		if op.Eviction.Preemptor != nil {
			preemptor := *op.Eviction.Preemptor
			eviction.Preemptor = &preemptor
		}
		ops[i] = recorder.DecisionOp{
			Kind:         op.Kind,
			UID:          op.UID,
			Namespace:    op.Namespace,
			Name:         op.Name,
			Node:         op.Node,
			Message:      op.Message,
			Error:        op.Error,
			NotAttempted: op.NotAttempted,
			Eviction:     eviction,
		}
	}
	return recorder.DecisionRecord{
		SessionID: event.SessionID,
		Action:    event.Action,
		Ops:       ops,
	}
}

func (p *recorderPlugin) stat(action string) *recorder.ActionStat {
	stat, found := p.stats[action]
	if !found {
		stat = &recorder.ActionStat{}
		p.stats[action] = stat
	}
	return stat
}

func (p *recorderPlugin) snapshotStats(ssn *framework.Session) map[string]recorder.ActionStat {
	out := map[string]recorder.ActionStat{}
	for action, stat := range p.stats {
		out[action] = *stat
	}
	for action, d := range ssn.ActionDurations() {
		stat := out[action]
		stat.WallTimeMs = d.Milliseconds()
		out[action] = stat
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (p *recorderPlugin) takeDiscovery() (*snapshot.DiscoverySnapshot, bool) {
	if p.discovery == nil {
		return nil, true
	}
	select {
	case discovery := <-p.discovery:
		if discovery == nil || discovery.ServerVersion == nil {
			return discovery, true
		}
		return discovery, false
	default:
		return nil, true
	}
}

func hasPending(ssn *framework.Session) bool {
	if ssn == nil || ssn.ClusterInfo == nil {
		return false
	}
	for _, job := range ssn.ClusterInfo.PodGroupInfos {
		if job.GetNumPendingTasks() > 0 || job.GetNumGatedTasks() > 0 {
			return true
		}
	}
	return false
}

func failReasons(ssn *framework.Session) []recorder.FailReason {
	if ssn == nil || ssn.ClusterInfo == nil {
		return nil
	}
	var reasons []recorder.FailReason
	for _, job := range ssn.ClusterInfo.PodGroupInfos {
		for uid, fit := range job.TasksFitErrors {
			if fit == nil {
				continue
			}
			reason := recorder.FailReason{PodUID: string(uid), Reason: fit.Error()}
			if task, found := job.GetAllPodsMap()[uid]; found && task != nil {
				reason.Namespace = task.Namespace
				reason.Name = task.Name
			}
			reasons = append(reasons, reason)
		}
	}
	return reasons
}

func (p *recorderPlugin) registerInformers(ssn *framework.Session) {
	if p.enqueue == nil || ssn.Cache == nil {
		return
	}
	selector := labels.Everything()
	if ssn.SchedulerParams.PartitionParams != nil {
		parsed, err := ssn.SchedulerParams.PartitionParams.GetLabelSelector()
		if err != nil {
			log.InfraLogger.Errorf("Recorder partition selector: %v", err)
		} else {
			selector = parsed
		}
	}
	var nodeLister corelisters.NodeLister
	var groupLister enginev2alpha2listers.PodGroupLister
	kube := ssn.Cache.KubeInformerFactory()
	if kube != nil {
		nodeLister = kube.Core().V1().Nodes().Lister()
		p.watchNodes(kube.Core().V1().Nodes().Informer(), selector)
	}
	kaiCache, ok := ssn.Cache.(kaiInformerCache)
	if !ok {
		kaiInformersUnavailable = true
		log.InfraLogger.Errorf("Recorder cache does not expose KubeAiSchedulerInformerFactory; pod group and bind request lifecycle will be missing and cycles are marked partial")
	} else if kai := kaiCache.KubeAiSchedulerInformerFactory(); kai == nil {
		kaiInformersUnavailable = true
		log.InfraLogger.Errorf("Recorder KAI informer factory is nil; pod group and bind request lifecycle will be missing and cycles are marked partial")
	} else {
		groupLister = kai.Scheduling().V2alpha2().PodGroups().Lister()
		p.watchPodGroups(kai.Scheduling().V2alpha2().PodGroups().Informer(), selector)
		p.watchBindRequests(kai.Scheduling().V1alpha2().BindRequests().Informer(), selector)
	}
	if kube != nil {
		p.watchPods(kube.Core().V1().Pods().Informer(), selector, nodeLister, groupLister)
	}
}

func (p *recorderPlugin) watchPods(
	informer cache.SharedIndexInformer, selector labels.Selector,
	nodes corelisters.NodeLister, groups enginev2alpha2listers.PodGroupLister,
) {
	keep := func(pod *v1.Pod) bool {
		return podInShard(pod, selector, nodes, groups)
	}
	addDetailedHandler(informer, func(obj any, initial bool) {
		if initial {
			return
		}
		pod, ok := unwrap[*v1.Pod](obj)
		if !ok || !keep(pod) {
			return
		}
		p.enqueue(recorder.StreamLifecycle, recorder.KindLifecycle, podLifecycle("add", pod))
	}, func(oldObj, newObj any) {
		oldPod, oldOK := unwrap[*v1.Pod](oldObj)
		newPod, newOK := unwrap[*v1.Pod](newObj)
		if !newOK || !keep(newPod) {
			return
		}
		if oldOK && samePodLife(oldPod, newPod) {
			return
		}
		p.enqueue(recorder.StreamLifecycle, recorder.KindLifecycle, podLifecycle("update", newPod))
	}, func(obj any) {
		pod, ok := unwrap[*v1.Pod](obj)
		if !ok || !keep(pod) {
			return
		}
		p.enqueue(recorder.StreamLifecycle, recorder.KindLifecycle, podLifecycle("delete", pod))
	})
}

func (p *recorderPlugin) watchNodes(informer cache.SharedIndexInformer, selector labels.Selector) {
	addDetailedHandler(informer, func(obj any, initial bool) {
		if initial {
			return
		}
		node, ok := unwrap[*v1.Node](obj)
		if !ok || !selector.Matches(labels.Set(node.Labels)) {
			return
		}
		p.enqueue(recorder.StreamLifecycle, recorder.KindLifecycle, nodeLifecycle("add", node))
	}, func(oldObj, newObj any) {
		oldNode, oldOK := unwrap[*v1.Node](oldObj)
		newNode, newOK := unwrap[*v1.Node](newObj)
		if !newOK || !selector.Matches(labels.Set(newNode.Labels)) {
			return
		}
		if oldOK && nodeReady(oldNode) == nodeReady(newNode) && oldNode.Spec.Unschedulable == newNode.Spec.Unschedulable {
			return
		}
		p.enqueue(recorder.StreamLifecycle, recorder.KindLifecycle, nodeLifecycle("update", newNode))
	}, func(obj any) {
		node, ok := unwrap[*v1.Node](obj)
		if !ok || !selector.Matches(labels.Set(node.Labels)) {
			return
		}
		p.enqueue(recorder.StreamLifecycle, recorder.KindLifecycle, nodeLifecycle("delete", node))
	})
}

func (p *recorderPlugin) watchPodGroups(informer cache.SharedIndexInformer, selector labels.Selector) {
	addDetailedHandler(informer, func(obj any, initial bool) {
		if initial {
			return
		}
		group, ok := unwrap[*enginev2alpha2.PodGroup](obj)
		if !ok || !selector.Matches(labels.Set(group.Labels)) {
			return
		}
		p.enqueue(recorder.StreamLifecycle, recorder.KindLifecycle, podGroupLifecycle("add", group))
	}, func(_, newObj any) {
		group, ok := unwrap[*enginev2alpha2.PodGroup](newObj)
		if !ok || !selector.Matches(labels.Set(group.Labels)) {
			return
		}
		p.enqueue(recorder.StreamLifecycle, recorder.KindLifecycle, podGroupLifecycle("update", group))
	}, func(obj any) {
		group, ok := unwrap[*enginev2alpha2.PodGroup](obj)
		if !ok || !selector.Matches(labels.Set(group.Labels)) {
			return
		}
		p.enqueue(recorder.StreamLifecycle, recorder.KindLifecycle, podGroupLifecycle("delete", group))
	})
}

func (p *recorderPlugin) watchBindRequests(informer cache.SharedIndexInformer, selector labels.Selector) {
	addDetailedHandler(informer, func(obj any, initial bool) {
		if initial {
			return
		}
		request, ok := unwrap[*schedulingv1alpha2.BindRequest](obj)
		if !ok || !selector.Matches(labels.Set(request.Labels)) {
			return
		}
		p.enqueue(recorder.StreamLifecycle, recorder.KindLifecycle, bindLifecycle("add", request))
	}, func(oldObj, newObj any) {
		oldRequest, oldOK := unwrap[*schedulingv1alpha2.BindRequest](oldObj)
		newRequest, newOK := unwrap[*schedulingv1alpha2.BindRequest](newObj)
		if !newOK || !selector.Matches(labels.Set(newRequest.Labels)) {
			return
		}
		if oldOK && oldRequest.Status.Phase == newRequest.Status.Phase && oldRequest.Spec.SelectedNode == newRequest.Spec.SelectedNode {
			return
		}
		p.enqueue(recorder.StreamLifecycle, recorder.KindLifecycle, bindLifecycle("update", newRequest))
	}, func(obj any) {
		request, ok := unwrap[*schedulingv1alpha2.BindRequest](obj)
		if !ok || !selector.Matches(labels.Set(request.Labels)) {
			return
		}
		p.enqueue(recorder.StreamLifecycle, recorder.KindLifecycle, bindLifecycle("delete", request))
	})
}

func addDetailedHandler(informer cache.SharedIndexInformer, add func(any, bool), update func(any, any), del func(any)) {
	if informer == nil {
		return
	}
	_, err := informer.AddEventHandler(cache.ResourceEventHandlerDetailedFuncs{
		AddFunc:    add,
		UpdateFunc: update,
		DeleteFunc: del,
	})
	if err != nil {
		log.InfraLogger.Errorf("Recorder informer handler: %v", err)
	}
}

func unwrap[T any](obj any) (T, bool) {
	var zero T
	if obj == nil {
		return zero, false
	}
	if typed, ok := obj.(T); ok {
		return typed, true
	}
	tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
	if !ok {
		return zero, false
	}
	typed, ok := tombstone.Obj.(T)
	return typed, ok
}

func podInShard(pod *v1.Pod, selector labels.Selector, nodes corelisters.NodeLister, groups enginev2alpha2listers.PodGroupLister) bool {
	if pod == nil {
		return false
	}
	if selector == nil || selector.Empty() {
		return true
	}
	if pod.Spec.NodeName != "" && nodes != nil {
		node, err := nodes.Get(pod.Spec.NodeName)
		if err == nil && selector.Matches(labels.Set(node.Labels)) {
			return true
		}
	}
	if groups == nil {
		return false
	}
	name := pod.Annotations[commonconstants.PodGroupAnnotationForPod]
	if name == "" {
		return false
	}
	group, err := groups.PodGroups(pod.Namespace).Get(name)
	if err != nil {
		return false
	}
	return selector.Matches(labels.Set(group.Labels))
}

func podLifecycle(op string, pod *v1.Pod) recorder.LifecycleRecord {
	record := recorder.LifecycleRecord{
		ObjectKind: "pod",
		Op:         op,
		Namespace:  pod.Namespace,
		Name:       pod.Name,
		UID:        string(pod.UID),
		Phase:      string(pod.Status.Phase),
		NodeName:   pod.Spec.NodeName,
		SeenAt:     time.Now().UTC(),
	}
	if pod.Status.StartTime != nil {
		started := pod.Status.StartTime.Time
		record.StartTime = &started
	}
	return record
}

func samePodLife(left, right *v1.Pod) bool {
	if left.Status.Phase != right.Status.Phase || left.Spec.NodeName != right.Spec.NodeName {
		return false
	}
	return (left.Status.StartTime == nil) == (right.Status.StartTime == nil)
}

func nodeLifecycle(op string, node *v1.Node) recorder.LifecycleRecord {
	unschedulable := node.Spec.Unschedulable
	return recorder.LifecycleRecord{
		ObjectKind:    "node",
		Op:            op,
		Name:          node.Name,
		UID:           string(node.UID),
		Ready:         nodeReady(node),
		Unschedulable: &unschedulable,
		SeenAt:        time.Now().UTC(),
	}
}

func nodeReady(node *v1.Node) string {
	for _, condition := range node.Status.Conditions {
		if condition.Type == v1.NodeReady {
			return string(condition.Status)
		}
	}
	return ""
}

func podGroupLifecycle(op string, group *enginev2alpha2.PodGroup) recorder.LifecycleRecord {
	phase := ""
	if len(group.Status.Conditions) > 0 {
		phase = string(group.Status.Conditions[len(group.Status.Conditions)-1].Type)
	}
	return recorder.LifecycleRecord{
		ObjectKind: "podgroup",
		Op:         op,
		Namespace:  group.Namespace,
		Name:       group.Name,
		UID:        string(group.UID),
		Phase:      phase,
		SeenAt:     time.Now().UTC(),
	}
}

func bindLifecycle(op string, request *schedulingv1alpha2.BindRequest) recorder.LifecycleRecord {
	return recorder.LifecycleRecord{
		ObjectKind:   "bindrequest",
		Op:           op,
		Namespace:    request.Namespace,
		Name:         request.Name,
		UID:          string(request.UID),
		Phase:        request.Status.Phase,
		NodeName:     request.Spec.SelectedNode,
		Reason:       request.Status.Reason,
		GPUGroups:    request.Spec.SelectedFractionalGpuGroupIDs(),
		ResourceType: request.Spec.ReceivedResourceType,
		BackoffLimit: request.Spec.BackoffLimit,
		SeenAt:       time.Now().UTC(),
	}
}

func cloneConfig(in *conf.SchedulerConfiguration) *conf.SchedulerConfiguration {
	return cloneJSON(in)
}

func cloneParams(in *conf.SchedulerParams) *conf.SchedulerParams {
	return cloneJSON(in)
}

func cloneJSON[T any](in *T) *T {
	if in == nil {
		return nil
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return nil
	}
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return &out
}
