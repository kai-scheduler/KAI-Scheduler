// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package topology

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/common_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/node_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/pod_status"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/podgroup_info/subgroup_info"
	"github.com/kai-scheduler/KAI-scheduler/pkg/scheduler/api/resource_info"
)

const (
	requiredResourceNotInDomainRatio = 1000.0
	maxAllocatableTasksRatio         = 1.0
)

type jobAllocationMetaData struct {
	maxPodResourcesVector resource_info.ResourceVector
	tasksToAllocate       []*pod_info.PodInfo
}

func (t *topologyPlugin) subSetNodesFn(
	job *podgroup_info.PodGroupInfo, subGroup *subgroup_info.SubGroupInfo, podSets map[string]*subgroup_info.PodSet,
	tasks []*pod_info.PodInfo, nodeSet node_info.NodeSet,
) ([]node_info.NodeSet, error) {
	topologyTree, found := t.getJobTopology(subGroup)
	if !found {
		job.AddSimpleJobFitError(
			podgroup_info.PodSchedulingErrors,
			fmt.Sprintf("Requested topology %s does not exist", subGroup.GetTopologyConstraint().Topology))
		return []node_info.NodeSet{}, nil
	}
	if topologyTree == nil || len(tasks) == 0 {
		return []node_info.NodeSet{nodeSet}, nil
	}

	id, level, validNodes := lowestCommonDomainID(nodeSet, topologyTree.TopologyResource.Spec.Levels, subGroup.GetTopologyConstraint())
	domain, ok := topologyTree.DomainsByLevel[level][id]
	if !ok {
		return nil, fmt.Errorf("domain not found for node set in topology %s", topologyTree.Name)
	}

	t.treeAllocatableCleanup(topologyTree)
	calcSubTreeFreeResources(domain)
	if useRepresentorPodsAccounting(tasks) {
		if err := t.calcTreeAllocatable(tasks, domain); err != nil {
			return nil, err
		}
	}

	tasksResources, tasksCount := getTasksAllocationMetadata(tasks)

	if err := checkJobDomainFit(job, subGroup, tasksResources, tasksCount, domain, topologyTree.VectorMap); err != nil {
		if domain.ID == rootDomainId {
			job.AddSimpleJobFitError(
				podgroup_info.PodSchedulingErrors,
				getNoTopologyMatchError(job, subGroup, topologyTree, "not enough resources in the cluster to allocate the job").Error())

		} else {
			job.AddSimpleJobFitError(
				podgroup_info.PodSchedulingErrors,
				getNoTopologyMatchError(job, subGroup, topologyTree,
					fmt.Sprintf("not enough resources in %s to allocate the job", string(domain.ID))).Error())
		}
		return []node_info.NodeSet{}, nil
	}

	// Sorting the tree for both packing and closest preferred level domain scoring
	preferredLevel := DomainLevel(subGroup.GetTopologyConstraint().PreferredLevel)
	requiredLevel := DomainLevel(subGroup.GetTopologyConstraint().RequiredLevel)
	maxDepthLevel := preferredLevel
	if maxDepthLevel == "" {
		maxDepthLevel = requiredLevel
	}
	sortTreeFromRoot(tasks, domain, maxDepthLevel, topologyTree.VectorMap)
	if preferredLevel != "" {
		t.subGroupNodeScores[subGroup.GetName()] = calculateNodeScores(domain, preferredLevel)
	}

	jobAllocatableDomains, err := t.getJobAllocatableDomains(job, subGroup, podSets, tasksResources, tasksCount, topologyTree)
	if err != nil {
		return nil, err
	}

	jobAllocatableDomains = sortDomainInfos(topologyTree, jobAllocatableDomains)

	var domainNodeSets []node_info.NodeSet
	for _, jobAllocatableDomain := range jobAllocatableDomains {
		var domainNodeSet node_info.NodeSet
		for _, node := range jobAllocatableDomain.Nodes {
			if _, ok := validNodes[node.Name]; !ok {
				continue
			}
			domainNodeSet = append(domainNodeSet, node)
		}
		domainNodeSets = append(domainNodeSets, domainNodeSet)
	}

	return domainNodeSets, nil
}

func getTasksAllocationMetadata(tasks []*pod_info.PodInfo) (resource_info.ResourceVector, int) {
	var tasksResources resource_info.ResourceVector
	for _, task := range tasks {
		if tasksResources == nil {
			tasksResources = task.ResReqVector.Clone()
		} else {
			tasksResources.Add(task.ResReqVector)
		}
	}
	tasksCount := len(tasks)
	return tasksResources, tasksCount
}

func (t *topologyPlugin) getJobTopology(subGroup *subgroup_info.SubGroupInfo) (*Info, bool) {
	if subGroup.GetTopologyConstraint() == nil {
		return nil, true
	}
	jobTopologyName := subGroup.GetTopologyConstraint().Topology
	if jobTopologyName == "" {
		return nil, true
	}
	topologyTree := t.TopologyTrees[jobTopologyName]
	if topologyTree == nil {
		return nil, false
	}
	return topologyTree, true
}

func (t *topologyPlugin) calcTreeAllocatable(tasks []*pod_info.PodInfo, domain *DomainInfo) error {
	jobAllocationData, err := initTasksRepresentorMetadataStruct(tasks)
	if err != nil {
		return err
	}

	_, err = t.calcSubTreeAllocatable(jobAllocationData, domain)
	return err
}

func initTasksRepresentorMetadataStruct(tasksToAllocate []*pod_info.PodInfo) (*jobAllocationMetaData, error) {
	var maxPodVector resource_info.ResourceVector
	for _, podInfo := range tasksToAllocate {
		if maxPodVector == nil {
			maxPodVector = podInfo.ResReqVector.Clone()
		} else {
			maxPodVector.SetMax(podInfo.ResReqVector)
		}
	}
	return &jobAllocationMetaData{
		maxPodResourcesVector: maxPodVector,
		tasksToAllocate:       tasksToAllocate,
	}, nil
}

func (t *topologyPlugin) calcSubTreeAllocatable(
	jobAllocationData *jobAllocationMetaData, domain *DomainInfo,
) (int, error) {
	if domain == nil {
		return 0, nil
	}
	domain.AllocatablePods = 0 // reset the allocatable pods count for the domain

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

func calcSubTreeFreeResources(domain *DomainInfo) resource_info.ResourceVector {
	if domain == nil {
		return nil
	}

	if len(domain.Children) == 0 {
		for _, node := range domain.Nodes {
			domain.IdleOrReleasingVector.Add(node.IdleVector)
			domain.IdleOrReleasingVector.Add(node.ReleasingVector)
			// Ignore fractions of GPUs for now
		}
		return domain.IdleOrReleasingVector
	}

	for _, child := range domain.Children {
		subdomainFreeResources := calcSubTreeFreeResources(child)
		domain.IdleOrReleasingVector.Add(subdomainFreeResources)
	}
	return domain.IdleOrReleasingVector
}

func calcNodeAccommodation(jobAllocationMetaData *jobAllocationMetaData, node *node_info.NodeInfo) int {
	maxPodVector := jobAllocationMetaData.maxPodResourcesVector

	onePodOnlyVector := resource_info.NewResourceVector(node.VectorMap)
	onePodOnlyVector.Set(resource_info.PodsIndex, 1)
	if maxPodVector.LessEqual(onePodOnlyVector) {
		return len(jobAllocationMetaData.tasksToAllocate)
	}

	nonAllocated := node.IdleVector.Clone()
	nonAllocated.Add(node.ReleasingVector)

	// Approximate available GPUs for topology pre-filtering. A job may request multiple
	// fractions or whole GPUs, and accounting for fragmentation precisely is not worth it
	// here — the goal is to quickly identify which topology domains could possibly fit
	// the job. Sum all available fractions from partially-used devices together with idle
	// whole GPUs and compare against the total requested GPUs. This may overcount
	// multi-device requests, but overestimation is acceptable since binding checks are exact.
	gpuRequest := maxPodVector.Get(resource_info.GPUIndex)
	if gpuRequest > 0 && gpuRequest < 1 {
		nonAllocated.Set(resource_info.GPUIndex, nonAllocated.Get(resource_info.GPUIndex)+node.AvailableSharedGPUFractions())
	}

	if !maxPodVector.LessEqual(nonAllocated) {
		return 0
	}

	minPods := math.MaxInt
	for i := 0; i < len(maxPodVector); i++ {
		if maxPodVector[i] <= 0 {
			continue
		}
		pods := int(nonAllocated[i] / maxPodVector[i])
		if pods < minPods {
			minPods = pods
		}
	}

	if minPods == math.MaxInt {
		return len(jobAllocationMetaData.tasksToAllocate)
	}
	return minPods
}

func (t *topologyPlugin) getJobAllocatableDomains(
	job *podgroup_info.PodGroupInfo, subGroup *subgroup_info.SubGroupInfo, podSets map[string]*subgroup_info.PodSet,
	tasksResources resource_info.ResourceVector, tasksCount int, topologyTree *Info,
) ([]*DomainInfo, error) {
	relevantLevels, err := t.calculateRelevantDomainLevels(subGroup, topologyTree)
	if err != nil {
		return nil, err
	}

	// Validate that the domains do not clash with the chosen domain for active pods of the job
	var relevantDomainsByLevel domainsByLevel
	if hasActiveAllocatedTasks(podSets) && hasTopologyRequiredConstraint(subGroup) {
		relevantDomainsByLevel = getRelevantDomainsWithAllocatedPods(podSets, topologyTree,
			DomainLevel(subGroup.GetTopologyConstraint().RequiredLevel))
	} else {
		relevantDomainsByLevel = topologyTree.DomainsByLevel
	}

	var domains []*DomainInfo
	var topLevelFitErrors []common_info.JobFitError
	for levelIndex, level := range relevantLevels {
		for _, domain := range relevantDomainsByLevel[level] {
			err := checkJobDomainFit(job, subGroup, tasksResources, tasksCount, domain, topologyTree.VectorMap)
			if err != nil { // Filter domains that cannot allocate the job
				if levelIndex == len(relevantLevels)-1 {
					topLevelFitErrors = append(topLevelFitErrors, err)
				}
				continue
			}

			domains = append(domains, domain)
		}
	}

	if len(domains) == 0 {
		for _, fitError := range topLevelFitErrors {
			job.AddJobFitError(fitError)
		}
		job.AddSimpleJobFitError(
			podgroup_info.PodSchedulingErrors,
			getNoTopologyMatchError(job, subGroup, topologyTree).Error())
		return []*DomainInfo{}, nil
	}

	return domains, nil
}

func hasActiveAllocatedTasks(podSets map[string]*subgroup_info.PodSet) bool {
	for _, podSet := range podSets {
		if podSet.GetNumActiveAllocatedTasks() > 0 {
			return true
		}
	}
	return false
}

func getRelevantDomainsWithAllocatedPods(
	podSets map[string]*subgroup_info.PodSet, topologyTree *Info, requiredLevel DomainLevel,
) domainsByLevel {
	relevantDomainsByLevel := domainsByLevel{}
	for _, domainAtRequiredLevel := range topologyTree.DomainsByLevel[requiredLevel] {
		if !hasActiveJobPodInDomain(podSets, domainAtRequiredLevel) {
			continue // if the domain at the top level does not have any active pods, then any domains under the subtree cannot satisfy the required constraint for both active and pending pods
		}
		addSubTreeToDomainMap(domainAtRequiredLevel, relevantDomainsByLevel)
	}
	return relevantDomainsByLevel
}

func hasActiveJobPodInDomain(podSets map[string]*subgroup_info.PodSet, domain *DomainInfo) bool {
	for _, podSet := range podSets {
		for _, pod := range podSet.GetPodInfos() {
			if pod_status.IsActiveAllocatedStatus(pod.Status) {
				podInDomain := domain.Nodes[pod.NodeName] != nil
				if podInDomain {
					return true
				}
			}
		}
	}
	return false
}

func addSubTreeToDomainMap(domain *DomainInfo, domainsMap domainsByLevel) {
	if domainsMap[domain.Level] == nil {
		domainsMap[domain.Level] = map[DomainID]*DomainInfo{}
	}
	for _, childDomain := range domain.Children {
		addSubTreeToDomainMap(childDomain, domainsMap)
	}
	domainsMap[domain.Level][domain.ID] = domain
}

func hasTopologyRequiredConstraint(subGroup *subgroup_info.SubGroupInfo) bool {
	return subGroup.GetTopologyConstraint().RequiredLevel != ""
}

func checkJobDomainFit(job *podgroup_info.PodGroupInfo, subGroup *subgroup_info.SubGroupInfo,
	tasksResources resource_info.ResourceVector, tasksCount int, domain *DomainInfo,
	vectorMap *resource_info.ResourceVectorMap) *common_info.TopologyFitError {
	if domain.AllocatablePods != allocatablePodsNotSet {
		if domain.AllocatablePods < tasksCount {
			return common_info.NewTopologyFitError(
				job.Name, subGroup.GetName(), job.Namespace, string(domain.ID), common_info.UnschedulableWorkloadReason,
				[]string{fmt.Sprintf("node-group %s can allocate only %d of %d required pods", domain.ID, domain.AllocatablePods, tasksCount)})
		}
		return nil
	}

	if getJobRatioToFreeResources(tasksResources, domain, vectorMap) > maxAllocatableTasksRatio {
		err := common_info.NewTopologyInsufficientResourcesError(
			job.Name, subGroup.GetName(), job.Namespace, string(domain.ID), tasksResources, domain.IdleOrReleasingVector, vectorMap)
		return err
	}
	return nil
}

func (*topologyPlugin) calculateRelevantDomainLevels(
	subGroup *subgroup_info.SubGroupInfo, topologyTree *Info,
) ([]DomainLevel, error) {
	topologyConstraint := subGroup.GetTopologyConstraint()
	requiredPlacement := DomainLevel(topologyConstraint.RequiredLevel)
	preferredPlacement := DomainLevel(topologyConstraint.PreferredLevel)
	if requiredPlacement == "" && preferredPlacement == "" {
		return nil, fmt.Errorf("no topology constraints were found for subgroup %s, with topology name %s",
			subGroup.GetName(), topologyTree.Name)
	}

	foundRequiredLevel := false
	foundPreferredLevel := false

	levels := make([]DomainLevel, len(topologyTree.TopologyResource.Spec.Levels)+1)
	levels[len(levels)-1] = rootLevel
	for i, level := range topologyTree.TopologyResource.Spec.Levels {
		levels[len(levels)-2-i] = DomainLevel(level.NodeLabel)
	}

	var relevantLevels []DomainLevel
	for _, level := range levels {
		if level == preferredPlacement {
			foundPreferredLevel = true
		}
		if level == requiredPlacement {
			foundRequiredLevel = true
		}
		if foundPreferredLevel || foundRequiredLevel {
			relevantLevels = append(relevantLevels, level)
		}
		if foundRequiredLevel {
			break
		}
	}

	if requiredPlacement != "" && !foundRequiredLevel {
		return nil, newTopologyConstraintConfigError(subGroup, topologyTree, "required", requiredPlacement)
	}
	if preferredPlacement != "" && !foundPreferredLevel {
		return nil, newTopologyConstraintConfigError(subGroup, topologyTree, "preferred", preferredPlacement)
	}
	return relevantLevels, nil
}

func newTopologyConstraintConfigError(subGroup *subgroup_info.SubGroupInfo, topologyTree *Info, placementType string, placementName DomainLevel) error {
	var topologyConstraintPodGroupSet string
	if subGroup.GetName() == podgroup_info.DefaultSubGroup {
		topologyConstraintPodGroupSet = "workload"
	} else {
		topologyConstraintPodGroupSet = fmt.Sprintf("sub-group %s", subGroup.GetName())
	}
	return fmt.Errorf("topology constraint error: %s specified '%s' as the %s topology constraint level, "+
		"but the topology tree '%s' does not contain a level with this name",
		topologyConstraintPodGroupSet, placementName, placementType, topologyTree.Name)
}

func (*topologyPlugin) treeAllocatableCleanup(topologyTree *Info) {
	for _, levelDomains := range topologyTree.DomainsByLevel {
		for _, domain := range levelDomains {
			domain.AllocatablePods = allocatablePodsNotSet
			domain.IdleOrReleasingVector = resource_info.NewResourceVector(topologyTree.VectorMap)
		}
	}
}

func sortTreeFromRoot(tasks []*pod_info.PodInfo, root *DomainInfo, maxDepthLevel DomainLevel,
	vectorMap *resource_info.ResourceVectorMap) {
	var tasksResources resource_info.ResourceVector
	for _, task := range tasks {
		if tasksResources == nil {
			tasksResources = task.ResReqVector.Clone()
		} else {
			tasksResources.Add(task.ResReqVector)
		}
	}

	sortTree(tasksResources, root, maxDepthLevel, vectorMap)
}

// sortTree recursively sorts the topology tree for bin-packing behavior.
// Domains are sorted by AllocatablePods (ascending) to prioritize filling domains
// with fewer available resources first, implementing a bin-packing strategy.
// Within domains with equal AllocatablePods, sorts by ID for deterministic ordering.
func sortTree(tasksResources resource_info.ResourceVector, root *DomainInfo, maxDepthLevel DomainLevel,
	vectorMap *resource_info.ResourceVectorMap) {
	if root == nil || maxDepthLevel == "" {
		return
	}

	domainRatiosCache := make(map[DomainID]float64, len(root.Children))
	for _, child := range root.Children {
		domainRatiosCache[child.ID] = getJobRatioToFreeResources(tasksResources, child, vectorMap)
	}

	slices.SortFunc(root.Children, func(i, j *DomainInfo) int {
		iRatio := domainRatiosCache[i.ID]
		jRatio := domainRatiosCache[j.ID]
		if c := cmp.Compare(jRatio, iRatio); c != 0 {
			return c
		}
		return cmp.Compare(i.ID, j.ID) // For deterministic ordering
	})

	if root.Level == maxDepthLevel {
		return
	}

	for _, child := range root.Children {
		sortTree(tasksResources, child, maxDepthLevel, vectorMap)
	}
}

// Returns a max ratio for all tasks resources to the free resources in the domain.
// The higher the ratio, the more "packed" the domain will be after the job is allocated.
// If the ratio is higher then 1, the domain will not be able to allocate the job.
func getJobRatioToFreeResources(tasksResources resource_info.ResourceVector, domain *DomainInfo,
	vectorMap *resource_info.ResourceVectorMap) float64 {
	dominantResourceRatio := 0.0

	emptyVec := make(resource_info.ResourceVector, len(tasksResources))
	if tasksResources.LessEqual(emptyVec) {
		return dominantResourceRatio
	}

	for i := 0; i < len(tasksResources); i++ {
		taskVal := tasksResources.Get(i)
		if taskVal <= 0 {
			continue
		}
		if i == resource_info.PodsIndex {
			continue
		}
		var resourceRatio float64
		freeVal := domain.IdleOrReleasingVector.Get(i)
		if freeVal <= 0 {
			resourceRatio = requiredResourceNotInDomainRatio
		} else {
			resourceRatio = taskVal / freeVal
		}
		dominantResourceRatio = math.Max(dominantResourceRatio, resourceRatio)
	}

	return dominantResourceRatio
}

// sortDomainInfos orders domains according to the sorted topology tree for consistent allocation.
// Assumes the topology tree is already sorted
func sortDomainInfos(topologyTree *Info, domainInfos []*DomainInfo) []*DomainInfo {
	root := topologyTree.DomainsByLevel[rootLevel][rootDomainId]
	reverseLevelOrderedDomains := reverseLevelOrder(root)

	sortedDomainInfos := make([]*DomainInfo, 0, len(domainInfos))
	for _, domain := range reverseLevelOrderedDomains {
		for _, domainInfo := range domainInfos {
			if domain.ID == domainInfo.ID && domain.Level == domainInfo.Level {
				sortedDomainInfos = append(sortedDomainInfos, domainInfo)
			}
		}
	}

	return sortedDomainInfos
}

// useRepresentorPodsAccounting checks if the tasks are using representor pods accounting.
// If all the tasks are homogeneous, i.e. all the tasks have the same type of resource requirements, then use representor pods accounting (AllocatablePods).
// If the tasks are heterogeneous, i.e. some of the tasks require resources that other tasks do not require,
// then use the job resources sum to see if a domain can allocate the job.
func useRepresentorPodsAccounting(tasks []*pod_info.PodInfo) bool {
	if len(tasks) == 0 {
		return true
	}
	vectorMap := tasks[0].VectorMap
	extendedResources := map[int]int{}
	podsUsingGpu := 0
	for _, task := range tasks {
		for i := 0; i < vectorMap.Len(); i++ {
			if i == resource_info.CPUIndex || i == resource_info.MemoryIndex || i == resource_info.GPUIndex {
				continue
			}
			if task.ResReqVector.Get(i) > 0 {
				extendedResources[i] += 1
			}
		}
		if task.GpuRequirement.GPUs() > 0 {
			podsUsingGpu += 1
		}
	}
	if podsUsingGpu != len(tasks) && podsUsingGpu != 0 {
		return false
	}
	for _, count := range extendedResources {
		if count != len(tasks) {
			return false
		}
	}
	return true
}

func getNoTopologyMatchError(job *podgroup_info.PodGroupInfo, subGroup *subgroup_info.SubGroupInfo, topologyTree *Info, suffixes ...string) error {
	constrainedObjectDescription := fmt.Sprintf("job <%s/%s>", job.Namespace, job.Name)
	if len(subGroup.GetName()) > 0 && subGroup.GetName() != podgroup_info.DefaultSubGroup {
		constrainedObjectDescription += fmt.Sprintf(", subgroup %s", subGroup.GetName())
	}
	suffix := ""
	if len(suffixes) > 0 {
		suffix = fmt.Sprintf(": %s", strings.Join(suffixes, ", "))
	}
	return fmt.Errorf("topology %s, requirement %s couldn't be satisfied for %s%s",
		topologyTree.Name, subGroup.GetTopologyConstraint().RequiredLevel, constrainedObjectDescription, suffix)
}
