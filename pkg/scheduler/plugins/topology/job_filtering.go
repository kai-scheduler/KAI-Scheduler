// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package topology

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/types"
	ksf "k8s.io/kube-scheduler/framework"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/log"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/plugins/scores"
)

type domainsByLevel map[string]map[TopologyDomainID]*TopologyDomainInfo

type topologyStateData struct {
	relevantDomains []*TopologyDomainInfo
}

func (t *topologyStateData) Clone() ksf.StateData {
	return &topologyStateData{
		relevantDomains: t.relevantDomains,
	}
}

type jobAllocationMetaData struct {
	maxPodResources    *resource_info.ResourceRequirements
	allocationTestPods []*pod_info.PodInfo
	tasksToAllocate    []*pod_info.PodInfo
}

func (t *topologyPlugin) prePredicateFn(_ *pod_info.PodInfo, job *podgroup_info.PodGroupInfo) error {
	topologyTree, err := t.getJobTopology(job)
	if err != nil {
		return err
	}
	if topologyTree == nil {
		return nil
	}

	// Check in cache if the job has already been allocated to a domain
	jobAllocatableDomains, err := t.loadAllocatableDomainsFromCache(types.UID(job.PodGroupUID))
	if err != nil {
		return err
	}
	if len(jobAllocatableDomains) > 0 {
		// Cache is already populated, no need to calculate anything
		return nil
	}

	defer t.treeAllocatableCleanup(topologyTree)
	maxAllocatablePods, err := t.calcTreeAllocatable(job, topologyTree)
	if err != nil {
		return err
	}

	if maxAllocatablePods < len(podgroup_info.GetTasksToAllocate(job, t.subGroupOrderFunc, t.taskOrderFunc, true)) {
		log.InfraLogger.V(6).Infof("no relevant domains found for job %s, workload topology name: %s",
			job.PodGroup.Name, topologyTree.Name)
		return nil
	}

	jobAllocatableDomain, err := t.getBestJobAllocatableDomains(job, topologyTree)
	if err != nil {
		return err
	}

	//Save results to cycle cache
	cycleJobState := (ksf.CycleState)(t.sessionStateGetter.GetSessionStateForResource(job.PodGroupUID))
	cycleJobState.Write(
		ksf.StateKey(topologyPluginName),
		&topologyStateData{relevantDomains: jobAllocatableDomain},
	)

	return nil
}

func (t *topologyPlugin) getJobTopology(job *podgroup_info.PodGroupInfo) (*TopologyInfo, error) {
	jobTopologyName := job.PodGroup.Spec.TopologyConstraint.Topology
	if jobTopologyName == "" {
		return nil, nil
	}
	topologyTree := t.TopologyTrees[jobTopologyName]
	if topologyTree == nil {
		return nil, fmt.Errorf("matching topology tree haven't been found for job %s, workload topology name: %s",
			job.PodGroup.Name, jobTopologyName)
	}
	return topologyTree, nil
}

func (t *topologyPlugin) calcTreeAllocatable(job *podgroup_info.PodGroupInfo, topologyTree *TopologyInfo) (int, error) {
	jobAllocationData, err := initJobAllocationMetadataStruct(job, t)
	if err != nil {
		return 0, err
	}

	return t.calcSubTreeAllocatable(jobAllocationData, topologyTree.Root)
}

func initJobAllocationMetadataStruct(job *podgroup_info.PodGroupInfo, t *topologyPlugin) (*jobAllocationMetaData, error) {
	tasksToAllocate := podgroup_info.GetTasksToAllocate(job, t.subGroupOrderFunc, t.taskOrderFunc, true)
	maxPodResources := resource_info.NewResourceRequirements(0, 0, 0)
	for _, podInfo := range tasksToAllocate {
		err := maxPodResources.SetMaxResource(podInfo.ResReq)
		if err != nil {
			return nil, err
		}
	}
	initialAllocationTestPods := []*pod_info.PodInfo{
		{Name: "1-pods-resources", ResReq: maxPodResources},
	}
	jobAllocationData := &jobAllocationMetaData{
		maxPodResources:    maxPodResources,
		allocationTestPods: initialAllocationTestPods,
		tasksToAllocate:    tasksToAllocate,
	}
	return jobAllocationData, nil
}

func (t *topologyPlugin) calcSubTreeAllocatable(
	jobAllocationData *jobAllocationMetaData, domain *TopologyDomainInfo,
) (int, error) {
	if domain == nil {
		return 0, nil
	}

	if len(domain.Children) == 0 {
		for _, node := range domain.Nodes {
			domain.AllocatablePods += calcNodeAccommodation(jobAllocationData, node)
		}
		return domain.AllocatablePods, nil
	}

	for _, child := range domain.Children {
		childAllocatable, err := t.calcSubTreeAllocatable(jobAllocationData, child)
		if err != nil {
			return 0, err
		}
		domain.AllocatablePods += childAllocatable
	}
	return domain.AllocatablePods, nil
}

func calcNodeAccommodation(jobAllocationMetaData *jobAllocationMetaData, node *node_info.NodeInfo) int {
	allocatablePodsCount := 0
	for _, resourceRepresentorPod := range jobAllocationMetaData.allocationTestPods {
		if node.IsTaskAllocatable(resourceRepresentorPod) {
			allocatablePodsCount++
		} else {
			break
		}
	}
	// Add more to jobResourcesAllocationsRepresenters until the node cannot accommodate any more pods
	if allocatablePodsCount == len(jobAllocationMetaData.allocationTestPods) {
		for i := allocatablePodsCount; i < len(jobAllocationMetaData.tasksToAllocate); i++ {
			latestTestPod := jobAllocationMetaData.allocationTestPods[len(jobAllocationMetaData.allocationTestPods)-1]

			iAllocationsTestPod := &pod_info.PodInfo{
				Name:   fmt.Sprintf("%d-pods-resources", allocatablePodsCount+1),
				ResReq: calcNextAllocationTestPodResources(latestTestPod.ResReq, jobAllocationMetaData.maxPodResources),
			}
			jobAllocationMetaData.allocationTestPods = append(jobAllocationMetaData.allocationTestPods, iAllocationsTestPod)
			if node.IsTaskAllocatable(iAllocationsTestPod) {
				allocatablePodsCount++
			} else {
				break
			}
		}
	}
	return allocatablePodsCount
}

func calcNextAllocationTestPodResources(previousTestResources, maxPodResources *resource_info.ResourceRequirements) *resource_info.ResourceRequirements {
	nPlus1Resources := previousTestResources.Clone()
	nPlus1Resources.BaseResource.Add(&maxPodResources.BaseResource)
	if len(nPlus1Resources.GpuResourceRequirement.MigResources()) > 0 {
		for migResource, quant := range maxPodResources.GpuResourceRequirement.MigResources() {
			nPlus1Resources.GpuResourceRequirement.MigResources()[migResource] += quant
		}
	} else {
		updatedGpuResource := resource_info.NewGpuResourceRequirementWithMultiFraction(
			nPlus1Resources.GetNumOfGpuDevices()+maxPodResources.GetNumOfGpuDevices(),
			nPlus1Resources.GpuFractionalPortion(),
			nPlus1Resources.GpuMemory())
		nPlus1Resources.GpuResourceRequirement = *updatedGpuResource
	}
	return nPlus1Resources
}

func (t *topologyPlugin) getBestJobAllocatableDomains(job *podgroup_info.PodGroupInfo, topologyTree *TopologyInfo) ([]*TopologyDomainInfo, error) {
	relevantLevels, err := t.calculateRelevantDomainLevels(job, topologyTree.Name, topologyTree)
	if err != nil {
		return nil, err
	}

	// Validate that the domains do not clash with the chosen domain for active pods of the job
	var relevantDomainsByLevel domainsByLevel
	if job.GetActiveAllocatedTasksCount() > 0 && jobHasTopologyRequiredConstraint(job) {
		relevantDomainsByLevel = getRelevantDomainsWithAllocatedPods(job, topologyTree, job.PodGroup.Spec.TopologyConstraint.RequiredTopologyLevel)
	} else {
		relevantDomainsByLevel = topologyTree.DomainsByLevel
	}

	taskToAllocateCount := len(podgroup_info.GetTasksToAllocate(job, t.subGroupOrderFunc, t.taskOrderFunc, true))

	maxDepthDomains := []*TopologyDomainInfo{}
	for _, level := range relevantLevels {
		for _, domain := range relevantDomainsByLevel[level] {
			if domain.AllocatablePods < taskToAllocateCount { // Filter domains that cannot allocate the job
				continue
			}

			maxDepthDomains = append(maxDepthDomains, domain)
		}
		if len(maxDepthDomains) > 0 {
			break
		}
	}

	if len(maxDepthDomains) == 0 {
		return nil, fmt.Errorf("no domains found for the job %s, workload topology name: %s",
			job.PodGroup.Name, topologyTree.Name)
	}

	if job.PodGroup.Spec.TopologyConstraint.PreferredTopologyLevel != "" &&
		maxDepthDomains[0].Level != job.PodGroup.Spec.TopologyConstraint.PreferredTopologyLevel {
		// If Preferred is defined and we couldn't find a domain on the preferred level,
		// return a children subset and not a single domain
		return t.improveChoiceForPreference(maxDepthDomains, job)
	}

	// For stage 1, return a single domain
	return []*TopologyDomainInfo{maxDepthDomains[0]}, nil
}

func getRelevantDomainsWithAllocatedPods(job *podgroup_info.PodGroupInfo, topologyTree *TopologyInfo, requiredLevel string) domainsByLevel {
	relevantDomainsByLevel := domainsByLevel{}
	for _, domainAtRequiredLevel := range topologyTree.DomainsByLevel[requiredLevel] {
		activePodsInDomain := countActiveJobPodsInDomain(job, domainAtRequiredLevel)
		if activePodsInDomain == 0 {
			continue // if the domain at the top level does not have any active pods, then any domains under the subtree cannot satisfy the required constraint for both active and pending pods
		}
		addSubTreeToDomainMap(domainAtRequiredLevel, relevantDomainsByLevel)
	}
	return relevantDomainsByLevel
}

func countActiveJobPodsInDomain(job *podgroup_info.PodGroupInfo, domain *TopologyDomainInfo) int {
	activePodsInDomain := 0
	for _, pod := range job.GetAllPodsMap() {
		if pod_status.IsActiveAllocatedStatus(pod.Status) {
			podInDomain := domain.Nodes[pod.NodeName] != nil
			if podInDomain {
				activePodsInDomain++
			}
		}
	}
	return activePodsInDomain
}

func addSubTreeToDomainMap(domain *TopologyDomainInfo, domainsMap domainsByLevel) {
	if domainsMap[domain.Level] == nil {
		domainsMap[domain.Level] = map[TopologyDomainID]*TopologyDomainInfo{}
	}
	for _, childDomain := range domain.Children {
		addSubTreeToDomainMap(childDomain, domainsMap)
	}
	domainsMap[domain.Level][domain.ID] = domain
}

func jobHasTopologyRequiredConstraint(job *podgroup_info.PodGroupInfo) bool {
	return job.PodGroup.Spec.TopologyConstraint.RequiredTopologyLevel != ""
}

func (*topologyPlugin) calculateRelevantDomainLevels(
	job *podgroup_info.PodGroupInfo, jobTopologyName string,
	topologyTree *TopologyInfo) ([]string, error) {
	requiredPlacement := job.PodGroup.Spec.TopologyConstraint.RequiredTopologyLevel
	preferredPlacement := job.PodGroup.Spec.TopologyConstraint.PreferredTopologyLevel
	if requiredPlacement == "" && preferredPlacement == "" {
		return nil, fmt.Errorf("no topology placement annotations found for job %s, workload topology name: %s", job.PodGroup.Name, jobTopologyName)
	}

	foundRequiredLevel := false
	foundPreferredLevel := false
	relevantLevels := []string{}
	abovePreferredLevel := preferredPlacement == ""
	for i := len(topologyTree.TopologyResource.Spec.Levels) - 1; i >= 0; i-- {
		level := topologyTree.TopologyResource.Spec.Levels[i]
		if preferredPlacement != "" && preferredPlacement == level.NodeLabel {
			foundPreferredLevel = true
			abovePreferredLevel = true
		}

		if !abovePreferredLevel {
			continue
		}
		relevantLevels = append(relevantLevels, level.NodeLabel)

		if requiredPlacement != "" && requiredPlacement == level.NodeLabel {
			foundRequiredLevel = true
			break // Next level won't fulfill the required placement
		}
	}
	if requiredPlacement != "" && !foundRequiredLevel {
		return nil, fmt.Errorf("the topology %s doesn't have a level matching the required(%s) specified for the job %s",
			jobTopologyName, requiredPlacement, job.Name,
		)
	}
	if preferredPlacement != "" && !foundPreferredLevel {
		return nil, fmt.Errorf("the topology %s doesn't have a level matching the preferred(%s) specified for the job %s",
			jobTopologyName, preferredPlacement, job.Name,
		)
	}
	if requiredPlacement == "" {
		relevantLevels = append(relevantLevels, "root")
	}
	return relevantLevels, nil
}

func (t *topologyPlugin) improveChoiceForPreference(maxDepthDomains []*TopologyDomainInfo, job *podgroup_info.PodGroupInfo) ([]*TopologyDomainInfo, error) {
	taskToAllocateCount := len(podgroup_info.GetTasksToAllocate(job, t.subGroupOrderFunc, t.taskOrderFunc, true))
	// Look for a subgroup of children domains that allows the job to be allocated
	// and return the one with the least number of domains required for the allocation
	bestChildrenSubset := []*TopologyDomainInfo{}
	for _, domain := range maxDepthDomains {
		childDomainSubset := getJobAllocatableChildrenSubset(domain, taskToAllocateCount)
		if len(bestChildrenSubset) == 0 || len(childDomainSubset) < len(bestChildrenSubset) {
			bestChildrenSubset = childDomainSubset
		}
	}
	return bestChildrenSubset, nil
}

func getJobAllocatableChildrenSubset(domain *TopologyDomainInfo, taskToAllocateCount int) []*TopologyDomainInfo {
	children := make([]*TopologyDomainInfo, 0, len(domain.Children))
	for _, child := range domain.Children {
		children = append(children, child)
	}
	sort.SliceStable(children, func(i, j int) bool {
		return children[i].AllocatablePods > children[j].AllocatablePods
	})

	allocatablePodsSum := 0
	childDomainSubset := []*TopologyDomainInfo{}
	for _, childDomain := range children {
		allocatablePodsSum += childDomain.AllocatablePods
		childDomainSubset = append(childDomainSubset, childDomain)
		if allocatablePodsSum >= taskToAllocateCount {
			break
		}
	}
	return childDomainSubset
}

func (*topologyPlugin) treeAllocatableCleanup(topologyTree *TopologyInfo) {
	for _, levelDomains := range topologyTree.DomainsByLevel {
		for _, domain := range levelDomains {
			domain.AllocatablePods = 0
		}
	}
}

func (t *topologyPlugin) predicateFn(pod *pod_info.PodInfo, job *podgroup_info.PodGroupInfo, node *node_info.NodeInfo) error {
	jobAllocatableDomains, err := t.loadAllocatableDomainsFromCache(job.PodGroupUID)
	if err != nil {
		return err
	}

	if len(jobAllocatableDomains) > 0 {
		jobDomainsNames := []string{}
		for _, domain := range jobAllocatableDomains {
			if domain.Nodes[node.Node.Name] != nil {
				return nil
			}
			jobDomainsNames = append(jobDomainsNames, domain.Name)
		}
		return fmt.Errorf("the node %s is not part of the chosen topology domain for the job %s. The chosen domains are %s",
			node.Node.Name, job.PodGroup.Name, strings.Join(jobDomainsNames, ", "))
	}

	return nil
}

func (t *topologyPlugin) nodeOrderFn(pod *pod_info.PodInfo, node *node_info.NodeInfo) (float64, error) {
	score := 0.0

	jobAllocatableDomains, err := t.loadAllocatableDomainsFromCache(types.UID(pod.Job))
	if err != nil {
		return score, err
	}

	if len(jobAllocatableDomains) > 0 {
		for _, domain := range jobAllocatableDomains {
			if domain.Nodes[node.Node.Name] != nil {
				score = scores.Topology
				break
			}
		}
	}

	return score, nil
}

func (t *topologyPlugin) loadAllocatableDomainsFromCache(podGroupUID types.UID) ([]*TopologyDomainInfo, error) {
	cycleJobState := (ksf.CycleState)(t.sessionStateGetter.GetSessionStateForResource(podGroupUID))
	if cycleJobState == nil {
		return nil, nil
	}
	jobTopologyStateData, err := cycleJobState.Read(ksf.StateKey(topologyPluginName))
	if err != nil {
		if errors.Is(err, ksf.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	jobAllocatableDomains := jobTopologyStateData.(*topologyStateData).relevantDomains
	return jobAllocatableDomains, nil
}

func (t *topologyPlugin) cleanAllocationAttemptCache(job *podgroup_info.PodGroupInfo) error {
	if job.PodGroup.Spec.TopologyConstraint.Topology == "" {
		return nil
	}

	cycleJobState := (ksf.CycleState)(t.sessionStateGetter.GetSessionStateForResource(job.PodGroupUID))
	if cycleJobState == nil {
		return nil
	}
	cycleJobState.Delete(ksf.StateKey(topologyPluginName))
	return nil
}
