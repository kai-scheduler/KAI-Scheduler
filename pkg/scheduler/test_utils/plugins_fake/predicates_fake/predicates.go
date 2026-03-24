// Copyright 2025 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package predicates_fake

import (
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	ksf "k8s.io/kube-scheduler/framework"
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"

	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/k8s_internal"
	"github.com/NVIDIA/KAI-scheduler/pkg/scheduler/log"
)

func predicateRequired(_ *v1.Pod) bool {
	return true
}

func emptyPredicatePreFilter(predicateName string) k8s_internal.FitPredicatePreFilter {
	return func(pod *v1.Pod) (sets.Set[string], *ksf.Status) {
		log.InfraLogger.V(6).Infof(
			"Checking pod %s/%s empty predicate: %s", pod.Namespace, pod.Name, predicateName)
		return nil, nil
	}
}

func failingPredicatePreFilter(predicateName string) k8s_internal.FitPredicatePreFilter {
	return func(pod *v1.Pod) (sets.Set[string], *ksf.Status) {
		log.InfraLogger.V(6).Infof(
			"Checking pod %s/%s failed pre predicate: %s", pod.Namespace, pod.Name, predicateName)
		status := ksf.NewStatus(ksf.Unschedulable, "reason1", "reason2")
		status.WithError(fmt.Errorf("failed pre-predicate %s", predicateName))
		return nil, status
	}
}

func failingPredicatePreFilterReasonSameAsError(predicateName string) k8s_internal.FitPredicatePreFilter {
	return func(pod *v1.Pod) (sets.Set[string], *ksf.Status) {
		log.InfraLogger.V(6).Infof("Checking pod %s/%s failed pre predicate: %s", pod.Namespace, pod.Name, predicateName)
		err := fmt.Errorf("failed pre-predicate %s", predicateName)
		status := ksf.NewStatus(ksf.Unschedulable, err.Error())
		status.WithError(err)
		return nil, status
	}
}

func failingPredicatePreFilterNoReason(predicateName string) k8s_internal.FitPredicatePreFilter {
	return func(pod *v1.Pod) (sets.Set[string], *ksf.Status) {
		log.InfraLogger.V(6).Infof("Checking pod %s/%s failed pre predicate: %s",
			pod.Namespace, pod.Name, predicateName)
		status := ksf.NewStatus(ksf.Unschedulable).
			WithError(fmt.Errorf("failed pre-predicate %v", predicateName))
		return nil, status
	}
}

func emptyPredicateFilter(predicateName string) k8s_internal.FitPredicateFilter {
	return func(pod *v1.Pod, nodeInfo *k8sframework.NodeInfo) (bool, []string, error) {
		log.InfraLogger.V(6).Infof(
			"Checking pod %s/%s on node %s with empty predicate: %s",
			pod.Namespace, pod.Name, nodeInfo.Node().Name, predicateName)
		return true, nil, nil
	}
}

func failingPredicateFilter(predicateName string) k8s_internal.FitPredicateFilter {
	return func(pod *v1.Pod, nodeInfo *k8sframework.NodeInfo) (bool, []string, error) {
		log.InfraLogger.V(6).Infof(
			"Checking pod %s/%s on node %s with failing predicate: %s",
			pod.Namespace, pod.Name, nodeInfo.Node().Name, predicateName)
		return false, []string{"reason1", "reason2", "reason3"}, nil
	}
}

func failingPredicateFilterWithError(predicateName string) k8s_internal.FitPredicateFilter {
	return func(pod *v1.Pod, nodeInfo *k8sframework.NodeInfo) (bool, []string, error) {
		log.InfraLogger.V(6).Infof(
			"Checking pod %s/%s on node %s with error failing predicate: %s",
			pod.Namespace, pod.Name, nodeInfo.Node().Name, predicateName)
		return false, []string{"reason1", "reason2"}, fmt.Errorf("failed with error predicate %v", predicateName)
	}
}

func EmptyPredicate(predicateName string) k8s_internal.SessionPredicate {
	predicate := k8s_internal.SessionPredicate{
		IsPreFilterRequired: predicateRequired,
		PreFilter:           emptyPredicatePreFilter(predicateName),
		IsFilterRequired:    predicateRequired,
		Filter:              emptyPredicateFilter(predicateName),
	}
	return predicate
}

func FailingPrePredicate(predicateName string) k8s_internal.SessionPredicate {
	predicate := k8s_internal.SessionPredicate{
		IsPreFilterRequired: predicateRequired,
		PreFilter:           failingPredicatePreFilter(predicateName),
		IsFilterRequired:    predicateRequired,
		Filter:              emptyPredicateFilter(predicateName),
	}
	return predicate
}

func FailingPrePredicateReasonSameAsError(predicateName string) k8s_internal.SessionPredicate {
	predicate := k8s_internal.SessionPredicate{
		IsPreFilterRequired: predicateRequired,
		PreFilter:           failingPredicatePreFilterReasonSameAsError(predicateName),
		IsFilterRequired:    predicateRequired,
		Filter:              emptyPredicateFilter(predicateName),
	}
	return predicate
}

func FailingPrePredicateNoReason(predicateName string) k8s_internal.SessionPredicate {
	predicate := k8s_internal.SessionPredicate{
		IsPreFilterRequired: predicateRequired,
		PreFilter:           failingPredicatePreFilterNoReason(predicateName),
		IsFilterRequired:    predicateRequired,
		Filter:              emptyPredicateFilter(predicateName),
	}
	return predicate
}

func FailingPredicate(predicateName string) k8s_internal.SessionPredicate {
	predicate := k8s_internal.SessionPredicate{
		IsPreFilterRequired: predicateRequired,
		PreFilter:           emptyPredicatePreFilter(predicateName),
		IsFilterRequired:    predicateRequired,
		Filter:              failingPredicateFilter(predicateName),
	}
	return predicate
}

func FailingPredicateWithError(predicateName string) k8s_internal.SessionPredicate {
	predicate := k8s_internal.SessionPredicate{
		IsPreFilterRequired: predicateRequired,
		PreFilter:           emptyPredicatePreFilter(predicateName),
		IsFilterRequired:    predicateRequired,
		Filter:              failingPredicateFilterWithError(predicateName),
	}
	return predicate
}
