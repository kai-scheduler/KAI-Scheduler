// Copyright 2026 NVIDIA CORPORATION
// SPDX-License-Identifier: Apache-2.0

package resource_info

import (
	v1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("DRA Resource Utils", func() {
	Context("ResourceClaimSliceToMap", func() {
		It("should convert empty slice to empty map", func() {
			claims := []*resourceapi.ResourceClaim{}
			result := ResourceClaimSliceToMap(claims)
			Expect(result).To(BeEmpty())
		})

		It("should convert single claim to map", func() {
			claim := &resourceapi.ResourceClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "claim-1",
					Namespace: "namespace-1",
					UID:       "uid-1",
				},
			}
			claims := []*resourceapi.ResourceClaim{claim}
			result := ResourceClaimSliceToMap(claims)
			Expect(result).To(HaveLen(1))
			Expect(result["namespace-1/claim-1"]).To(Equal(claim))
		})

		It("should convert multiple claims to map", func() {
			claim1 := &resourceapi.ResourceClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "claim-1",
					Namespace: "namespace-1",
					UID:       "uid-1",
				},
			}
			claim2 := &resourceapi.ResourceClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "claim-2",
					Namespace: "namespace-1",
					UID:       "uid-2",
				},
			}
			claim3 := &resourceapi.ResourceClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "claim-1",
					Namespace: "namespace-2",
					UID:       "uid-3",
				},
			}
			claims := []*resourceapi.ResourceClaim{claim1, claim2, claim3}
			result := ResourceClaimSliceToMap(claims)
			Expect(result).To(HaveLen(3))
			Expect(result["namespace-1/claim-1"]).To(Equal(claim1))
			Expect(result["namespace-1/claim-2"]).To(Equal(claim2))
			Expect(result["namespace-2/claim-1"]).To(Equal(claim3))
		})
	})

	Context("CalcClaimsToPodsBaseMap", func() {
		It("should return empty map for empty input", func() {
			draClaimsMap := map[string]*resourceapi.ResourceClaim{}
			result := CalcClaimsToPodsBaseMap(draClaimsMap)
			Expect(result).To(BeEmpty())
		})

		It("should map claim with Pod owner reference", func() {
			podUID := types.UID("pod-uid-1")
			claim := &resourceapi.ResourceClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name: "claim-1",
					UID:  "claim-uid-1",
					OwnerReferences: []metav1.OwnerReference{
						{
							Kind: PodOwnerKind,
							UID:  podUID,
						},
					},
				},
			}
			draClaimsMap := map[string]*resourceapi.ResourceClaim{"claim-1": claim}
			result := CalcClaimsToPodsBaseMap(draClaimsMap)
			Expect(result).To(HaveLen(1))
			Expect(result[podUID]).To(HaveLen(1))
			Expect(result[podUID]["claim-uid-1"]).To(Equal(claim))
		})

		It("should map claim with reservedFor reference", func() {
			podUID := types.UID("pod-uid-1")
			claim := &resourceapi.ResourceClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name: "claim-1",
					UID:  "claim-uid-1",
				},
				Status: resourceapi.ResourceClaimStatus{
					ReservedFor: []resourceapi.ResourceClaimConsumerReference{
						{
							Resource: ReservedForPodPlural,
							UID:      podUID,
						},
					},
				},
			}
			draClaimsMap := map[string]*resourceapi.ResourceClaim{"claim-1": claim}
			result := CalcClaimsToPodsBaseMap(draClaimsMap)
			Expect(result).To(HaveLen(1))
			Expect(result[podUID]).To(HaveLen(1))
			Expect(result[podUID]["claim-uid-1"]).To(Equal(claim))
		})

		It("should prefer owner reference over reservedFor", func() {
			podUID1 := types.UID("pod-uid-1")
			podUID2 := types.UID("pod-uid-2")
			claim := &resourceapi.ResourceClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name: "claim-1",
					UID:  "claim-uid-1",
					OwnerReferences: []metav1.OwnerReference{
						{
							Kind: PodOwnerKind,
							UID:  podUID1,
						},
					},
				},
				Status: resourceapi.ResourceClaimStatus{
					ReservedFor: []resourceapi.ResourceClaimConsumerReference{
						{
							Resource: ReservedForPodPlural,
							UID:      podUID2,
						},
					},
				},
			}
			draClaimsMap := map[string]*resourceapi.ResourceClaim{"claim-1": claim}
			result := CalcClaimsToPodsBaseMap(draClaimsMap)
			Expect(result).To(HaveLen(1))
			Expect(result[podUID1]).To(HaveLen(1))
			Expect(result[podUID2]).To(BeNil())
		})

		It("should ignore claims with non-Pod owner references", func() {
			claim := &resourceapi.ResourceClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name: "claim-1",
					UID:  "claim-uid-1",
					OwnerReferences: []metav1.OwnerReference{
						{
							Kind: "Deployment",
							UID:  "deploy-uid-1",
						},
					},
				},
			}
			draClaimsMap := map[string]*resourceapi.ResourceClaim{"claim-1": claim}
			result := CalcClaimsToPodsBaseMap(draClaimsMap)
			Expect(result).To(BeEmpty())
		})

		It("should ignore claims with non-pods reservedFor resource", func() {
			claim := &resourceapi.ResourceClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name: "claim-1",
					UID:  "claim-uid-1",
				},
				Status: resourceapi.ResourceClaimStatus{
					ReservedFor: []resourceapi.ResourceClaimConsumerReference{
						{
							Resource: "services",
							UID:      "service-uid-1",
						},
					},
				},
			}
			draClaimsMap := map[string]*resourceapi.ResourceClaim{"claim-1": claim}
			result := CalcClaimsToPodsBaseMap(draClaimsMap)
			Expect(result).To(BeEmpty())
		})

		It("should map multiple claims to same pod", func() {
			podUID := types.UID("pod-uid-1")
			claim1 := &resourceapi.ResourceClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name: "claim-1",
					UID:  "claim-uid-1",
					OwnerReferences: []metav1.OwnerReference{
						{
							Kind: PodOwnerKind,
							UID:  podUID,
						},
					},
				},
			}
			claim2 := &resourceapi.ResourceClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name: "claim-2",
					UID:  "claim-uid-2",
					OwnerReferences: []metav1.OwnerReference{
						{
							Kind: PodOwnerKind,
							UID:  podUID,
						},
					},
				},
			}
			draClaimsMap := map[string]*resourceapi.ResourceClaim{
				"claim-1": claim1,
				"claim-2": claim2,
			}
			result := CalcClaimsToPodsBaseMap(draClaimsMap)
			Expect(result).To(HaveLen(1))
			Expect(result[podUID]).To(HaveLen(2))
			Expect(result[podUID]["claim-uid-1"]).To(Equal(claim1))
			Expect(result[podUID]["claim-uid-2"]).To(Equal(claim2))
		})

		It("should map claims to multiple pods", func() {
			podUID1 := types.UID("pod-uid-1")
			podUID2 := types.UID("pod-uid-2")
			claim1 := &resourceapi.ResourceClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name: "claim-1",
					UID:  "claim-uid-1",
					OwnerReferences: []metav1.OwnerReference{
						{
							Kind: PodOwnerKind,
							UID:  podUID1,
						},
					},
				},
			}
			claim2 := &resourceapi.ResourceClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name: "claim-2",
					UID:  "claim-uid-2",
					OwnerReferences: []metav1.OwnerReference{
						{
							Kind: PodOwnerKind,
							UID:  podUID2,
						},
					},
				},
			}
			draClaimsMap := map[string]*resourceapi.ResourceClaim{
				"claim-1": claim1,
				"claim-2": claim2,
			}
			result := CalcClaimsToPodsBaseMap(draClaimsMap)
			Expect(result).To(HaveLen(2))
			Expect(result[podUID1]).To(HaveLen(1))
			Expect(result[podUID2]).To(HaveLen(1))
			Expect(result[podUID1]["claim-uid-1"]).To(Equal(claim1))
			Expect(result[podUID2]["claim-uid-2"]).To(Equal(claim2))
		})
	})

	Context("GetDraPodClaims", func() {
		It("should return empty slice for pod with no resource claims", func() {
			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod-1",
					Namespace: "default",
					UID:       "pod-uid-1",
				},
			}
			draClaimMap := map[string]*resourceapi.ResourceClaim{}
			podsToClaimsMap := map[types.UID]map[types.UID]*resourceapi.ResourceClaim{}
			result := GetDraPodClaims(pod, draClaimMap, podsToClaimsMap)
			Expect(result).To(BeEmpty())
		})

		It("should return claims referenced in pod spec", func() {
			claimName := "claim-1"
			podUID := types.UID("pod-uid-1")
			namespace := "default"
			claim := &resourceapi.ResourceClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      claimName,
					Namespace: namespace,
					UID:       "claim-uid-1",
				},
			}
			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod-1",
					Namespace: namespace,
					UID:       podUID,
				},
				Spec: v1.PodSpec{
					ResourceClaims: []v1.PodResourceClaim{
						{
							ResourceClaimName: &claimName,
						},
					},
				},
			}
			draClaimMap := map[string]*resourceapi.ResourceClaim{namespace + "/" + claimName: claim}
			podsToClaimsMap := map[types.UID]map[types.UID]*resourceapi.ResourceClaim{}
			result := GetDraPodClaims(pod, draClaimMap, podsToClaimsMap)
			Expect(result).To(HaveLen(1))
			Expect(result[0]).To(Equal(claim))
		})

		It("should skip claims with nil ResourceClaimName - even if a template name is provided", func() {
			podUID := types.UID("pod-uid-1")
			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod-1",
					Namespace: "default",
					UID:       podUID,
				},
				Spec: v1.PodSpec{
					ResourceClaims: []v1.PodResourceClaim{
						{
							Name:                      "claim-1",
							ResourceClaimTemplateName: ptr.To("template-1"),
							ResourceClaimName:         nil,
						},
					},
				},
			}
			draClaimMap := map[string]*resourceapi.ResourceClaim{
				"default/template-1": {
					ObjectMeta: metav1.ObjectMeta{
						Name: "template-1",
						UID:  "template-uid-1",
					},
				},
			}
			podsToClaimsMap := map[types.UID]map[types.UID]*resourceapi.ResourceClaim{}
			result := GetDraPodClaims(pod, draClaimMap, podsToClaimsMap)
			Expect(result).To(BeEmpty())
		})

		It("should skip claims not found in draClaimMap", func() {
			claimName := "claim-1"
			podUID := types.UID("pod-uid-1")
			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod-1",
					Namespace: "default",
					UID:       podUID,
				},
				Spec: v1.PodSpec{
					ResourceClaims: []v1.PodResourceClaim{
						{
							ResourceClaimName: &claimName,
						},
					},
				},
			}
			draClaimMap := map[string]*resourceapi.ResourceClaim{}
			podsToClaimsMap := map[types.UID]map[types.UID]*resourceapi.ResourceClaim{}
			result := GetDraPodClaims(pod, draClaimMap, podsToClaimsMap)
			Expect(result).To(BeEmpty())
		})

		It("should skip claims in different namespace", func() {
			claimName := "claim-1"
			podUID := types.UID("pod-uid-1")
			claim := &resourceapi.ResourceClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      claimName,
					Namespace: "other-namespace",
					UID:       "claim-uid-1",
				},
			}
			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod-1",
					Namespace: "default",
					UID:       podUID,
				},
				Spec: v1.PodSpec{
					ResourceClaims: []v1.PodResourceClaim{
						{
							ResourceClaimName: &claimName,
						},
					},
				},
			}
			draClaimMap := map[string]*resourceapi.ResourceClaim{claimName: claim}
			podsToClaimsMap := map[types.UID]map[types.UID]*resourceapi.ResourceClaim{}
			result := GetDraPodClaims(pod, draClaimMap, podsToClaimsMap)
			Expect(result).To(BeEmpty())
		})

		It("should return claims from podsToClaimsMap", func() {
			podUID := types.UID("pod-uid-1")
			namespace := "default"
			claim1 := &resourceapi.ResourceClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "claim-1",
					Namespace: namespace,
					UID:       "claim-uid-1",
				},
			}
			claim2 := &resourceapi.ResourceClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "claim-2",
					Namespace: namespace,
					UID:       "claim-uid-2",
				},
			}
			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod-1",
					Namespace: namespace,
					UID:       podUID,
				},
			}
			draClaimMap := map[string]*resourceapi.ResourceClaim{}
			podsToClaimsMap := map[types.UID]map[types.UID]*resourceapi.ResourceClaim{
				podUID: {
					"claim-uid-1": claim1,
					"claim-uid-2": claim2,
				},
			}
			result := GetDraPodClaims(pod, draClaimMap, podsToClaimsMap)
			Expect(result).To(HaveLen(2))
			Expect(result).To(ContainElement(claim1))
			Expect(result).To(ContainElement(claim2))
		})

		It("should combine claims from pod spec and podsToClaimsMap", func() {
			claimName := "claim-1"
			podUID := types.UID("pod-uid-1")
			namespace := "default"
			claim1 := &resourceapi.ResourceClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      claimName,
					Namespace: namespace,
					UID:       "claim-uid-1",
				},
			}
			claim2 := &resourceapi.ResourceClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "claim-2",
					Namespace: namespace,
					UID:       "claim-uid-2",
				},
			}
			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod-1",
					Namespace: namespace,
					UID:       podUID,
				},
				Spec: v1.PodSpec{
					ResourceClaims: []v1.PodResourceClaim{
						{
							ResourceClaimName: &claimName,
						},
					},
				},
			}
			draClaimMap := map[string]*resourceapi.ResourceClaim{namespace + "/" + claimName: claim1}
			podsToClaimsMap := map[types.UID]map[types.UID]*resourceapi.ResourceClaim{
				podUID: {
					"claim-uid-2": claim2,
				},
			}
			result := GetDraPodClaims(pod, draClaimMap, podsToClaimsMap)
			Expect(result).To(HaveLen(2))
			Expect(result).To(ContainElement(claim1))
			Expect(result).To(ContainElement(claim2))
		})

		It("should not duplicate claims already in podsToClaimsMap", func() {
			claimName := "claim-1"
			podUID := types.UID("pod-uid-1")
			namespace := "default"
			claim := &resourceapi.ResourceClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      claimName,
					Namespace: namespace,
					UID:       "claim-uid-1",
				},
			}
			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod-1",
					Namespace: namespace,
					UID:       podUID,
				},
				Spec: v1.PodSpec{
					ResourceClaims: []v1.PodResourceClaim{
						{
							ResourceClaimName: &claimName,
						},
					},
				},
			}
			draClaimMap := map[string]*resourceapi.ResourceClaim{claimName: claim}
			podsToClaimsMap := map[types.UID]map[types.UID]*resourceapi.ResourceClaim{
				podUID: {
					"claim-uid-1": claim,
				},
			}
			result := GetDraPodClaims(pod, draClaimMap, podsToClaimsMap)
			Expect(result).To(HaveLen(1))
			Expect(result[0]).To(Equal(claim))
		})
	})

	Context("addClaimToPodClaimMap", func() {
		It("should add claim to new pod entry", func() {
			podUID := types.UID("pod-uid-1")
			claim := &resourceapi.ResourceClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name: "claim-1",
					UID:  "claim-uid-1",
				},
			}
			podsToClaimsMap := map[types.UID]map[types.UID]*resourceapi.ResourceClaim{}
			addClaimToPodClaimMap(claim, podUID, podsToClaimsMap)
			Expect(podsToClaimsMap).To(HaveLen(1))
			Expect(podsToClaimsMap[podUID]).To(HaveLen(1))
			Expect(podsToClaimsMap[podUID]["claim-uid-1"]).To(Equal(claim))
		})

		It("should add claim to existing pod entry", func() {
			podUID := types.UID("pod-uid-1")
			claim1 := &resourceapi.ResourceClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name: "claim-1",
					UID:  "claim-uid-1",
				},
			}
			claim2 := &resourceapi.ResourceClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name: "claim-2",
					UID:  "claim-uid-2",
				},
			}
			podsToClaimsMap := map[types.UID]map[types.UID]*resourceapi.ResourceClaim{
				podUID: {
					"claim-uid-1": claim1,
				},
			}
			addClaimToPodClaimMap(claim2, podUID, podsToClaimsMap)
			Expect(podsToClaimsMap).To(HaveLen(1))
			Expect(podsToClaimsMap[podUID]).To(HaveLen(2))
			Expect(podsToClaimsMap[podUID]["claim-uid-1"]).To(Equal(claim1))
			Expect(podsToClaimsMap[podUID]["claim-uid-2"]).To(Equal(claim2))
		})

		It("should overwrite claim with same UID", func() {
			podUID := types.UID("pod-uid-1")
			claim1 := &resourceapi.ResourceClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name: "claim-1",
					UID:  "claim-uid-1",
				},
			}
			claim2 := &resourceapi.ResourceClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name: "claim-1-updated",
					UID:  "claim-uid-1",
				},
			}
			podsToClaimsMap := map[types.UID]map[types.UID]*resourceapi.ResourceClaim{
				podUID: {
					"claim-uid-1": claim1,
				},
			}
			addClaimToPodClaimMap(claim2, podUID, podsToClaimsMap)
			Expect(podsToClaimsMap).To(HaveLen(1))
			Expect(podsToClaimsMap[podUID]).To(HaveLen(1))
			Expect(podsToClaimsMap[podUID]["claim-uid-1"]).To(Equal(claim2))
		})
	})
})
