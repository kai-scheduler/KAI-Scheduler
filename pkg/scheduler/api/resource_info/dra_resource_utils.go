// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resource_info

import (
	v1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	PodOwnerKind         string = "Pod"
	ReservedForPodPlural string = "pods"
)

func ResourceClaimSliceToMap(draResourceClaims []*resourceapi.ResourceClaim) map[string]*resourceapi.ResourceClaim {
	draClaimMap := map[string]*resourceapi.ResourceClaim{}
	for _, draClaim := range draResourceClaims {
		draClaimMap[types.NamespacedName{Namespace: draClaim.Namespace, Name: draClaim.Name}.String()] = draClaim
	}
	return draClaimMap
}

// CalcClaimsToPodsBaseMap calculates a pod to claims map. The first key in the map is the pod UID, and the second key in the map is the claim UID.
// The relationships are calculated based on the resourceclaim owner references and allocation reservedFor references. This might not be enough to identify all the claims that are related to a pod.
// Use GetDraPodClaims with the resulted pod-claim map to get all the claims that are related to a pod.
func CalcClaimsToPodsBaseMap(draClaimsMap map[string]*resourceapi.ResourceClaim) map[types.UID]map[types.UID]*resourceapi.ResourceClaim {
	podsToClaimsMap := map[types.UID]map[types.UID]*resourceapi.ResourceClaim{}
	for _, claim := range draClaimsMap {
		claimIsRelatedToPod := false
		for _, ownerReference := range claim.OwnerReferences {
			if ownerReference.Kind == PodOwnerKind {
				addClaimToPodClaimMap(claim, ownerReference.UID, podsToClaimsMap)
				claimIsRelatedToPod = true
				break
			}
		}
		if claimIsRelatedToPod {
			continue
		}
		for _, reservedFor := range claim.Status.ReservedFor {
			if reservedFor.Resource == ReservedForPodPlural {
				addClaimToPodClaimMap(claim, reservedFor.UID, podsToClaimsMap)
				break
			}
		}
	}
	return podsToClaimsMap
}

// GetDraPodClaims gets all the claims that are related to a pod. The relationships are calculated based previously calculated pod-claim map (from CalcClaimsToPodsBaseMap) and pod spec resource claims.
func GetDraPodClaims(pod *v1.Pod, draClaimMap map[string]*resourceapi.ResourceClaim,
	podsToClaimsMap map[types.UID]map[types.UID]*resourceapi.ResourceClaim) []*resourceapi.ResourceClaim {
	for _, claimReference := range pod.Spec.ResourceClaims {
		if claimReference.ResourceClaimName == nil {
			continue
		}
		claimKey := types.NamespacedName{Namespace: pod.Namespace, Name: *claimReference.ResourceClaimName}.String()
		claim, found := draClaimMap[claimKey]
		if !found {
			continue
		}
		if claim.Namespace != pod.Namespace {
			continue
		}
		addClaimToPodClaimMap(claim, pod.UID, podsToClaimsMap)
	}

	draPodClaims := []*resourceapi.ResourceClaim{}
	for _, claim := range podsToClaimsMap[pod.UID] {
		draPodClaims = append(draPodClaims, claim)
	}
	return draPodClaims
}

func addClaimToPodClaimMap(claim *resourceapi.ResourceClaim, podUid types.UID,
	podsToClaimsMap map[types.UID]map[types.UID]*resourceapi.ResourceClaim) {
	if len(podsToClaimsMap[podUid]) == 0 {
		podsToClaimsMap[podUid] = map[types.UID]*resourceapi.ResourceClaim{}
	}
	podsToClaimsMap[podUid][claim.UID] = claim
}
