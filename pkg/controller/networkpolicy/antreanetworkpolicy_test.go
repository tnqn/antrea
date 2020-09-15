// Copyright 2020 Antrea Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package networkpolicy

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/vmware-tanzu/antrea/pkg/apis/controlplane"
	secv1alpha1 "github.com/vmware-tanzu/antrea/pkg/apis/security/v1alpha1"
	antreatypes "github.com/vmware-tanzu/antrea/pkg/controller/types"
)

var (
	selectorA = metav1.LabelSelector{MatchLabels: map[string]string{"foo1": "bar1"}}
	selectorB = metav1.LabelSelector{MatchLabels: map[string]string{"foo2": "bar2"}}
	selectorC = metav1.LabelSelector{MatchLabels: map[string]string{"foo3": "bar3"}}
	selectorD = metav1.LabelSelector{MatchLabels: map[string]string{"foo4": "bar4"}}
)

func TestProcessAntreaNetworkPolicy(t *testing.T) {
	p10 := float64(10)
	appTier := antreatypes.TierApplication
	allowAction := secv1alpha1.RuleActionAllow
	protocolTCP := controlplane.ProtocolTCP
	intstr80, intstr81 := intstr.FromInt(80), intstr.FromInt(81)
	tests := []struct {
		name                    string
		inputPolicy             *secv1alpha1.NetworkPolicy
		expectedPolicy          *antreatypes.NetworkPolicy
		expectedAppliedToGroups int
		expectedAddressGroups   int
	}{
		{
			name: "rules-with-same-selectors",
			inputPolicy: &secv1alpha1.NetworkPolicy{
				ObjectMeta: metav1.ObjectMeta{Namespace: "ns1", Name: "npA", UID: "uidA"},
				Spec: secv1alpha1.NetworkPolicySpec{
					AppliedTo: []secv1alpha1.NetworkPolicyPeer{
						{PodSelector: &selectorA},
					},
					Priority: p10,
					Ingress: []secv1alpha1.Rule{
						{
							Ports: []secv1alpha1.NetworkPolicyPort{
								{
									Port: &intstr80,
								},
							},
							From: []secv1alpha1.NetworkPolicyPeer{
								{
									PodSelector:       &selectorB,
									NamespaceSelector: &selectorC,
								},
							},
							Action: &allowAction,
						},
					},
					Egress: []secv1alpha1.Rule{
						{
							Ports: []secv1alpha1.NetworkPolicyPort{
								{
									Port: &intstr81,
								},
							},
							To: []secv1alpha1.NetworkPolicyPeer{
								{
									PodSelector:       &selectorB,
									NamespaceSelector: &selectorC,
								},
							},
							Action: &allowAction,
						},
					},
				},
			},
			expectedPolicy: &antreatypes.NetworkPolicy{
				UID:  "uidA",
				Name: "uidA",
				SourceRef: &controlplane.NetworkPolicyReference{
					Type:      controlplane.AntreaNetworkPolicy,
					Namespace: "ns1",
					Name:      "npA",
					UID:       "uidA",
				},
				Priority:     &p10,
				TierPriority: &appTier,
				Rules: []controlplane.NetworkPolicyRule{
					{
						Direction: controlplane.DirectionIn,
						From: controlplane.NetworkPolicyPeer{
							AddressGroups: []string{getNormalizedUID(toGroupSelector("", &selectorB, &selectorC, nil).NormalizedName)},
						},
						Services: []controlplane.Service{
							{
								Protocol: &protocolTCP,
								Port:     &intstr80,
							},
						},
						Priority: 0,
						Action:   &allowAction,
					},
					{
						Direction: controlplane.DirectionOut,
						To: controlplane.NetworkPolicyPeer{
							AddressGroups: []string{getNormalizedUID(toGroupSelector("", &selectorB, &selectorC, nil).NormalizedName)},
						},
						Services: []controlplane.Service{
							{
								Protocol: &protocolTCP,
								Port:     &intstr81,
							},
						},
						Priority: 0,
						Action:   &allowAction,
					},
				},
				AppliedToGroups: []string{getNormalizedUID(toGroupSelector("ns1", &selectorA, nil, nil).NormalizedName)},
			},
			expectedAppliedToGroups: 1,
			expectedAddressGroups:   1,
		},
		{
			name: "rules-with-different-selectors",
			inputPolicy: &secv1alpha1.NetworkPolicy{
				ObjectMeta: metav1.ObjectMeta{Namespace: "ns2", Name: "npB", UID: "uidB"},
				Spec: secv1alpha1.NetworkPolicySpec{
					AppliedTo: []secv1alpha1.NetworkPolicyPeer{
						{PodSelector: &selectorA},
					},
					Priority: p10,
					Ingress: []secv1alpha1.Rule{
						{
							Ports: []secv1alpha1.NetworkPolicyPort{
								{
									Port: &intstr80,
								},
							},
							From: []secv1alpha1.NetworkPolicyPeer{
								{
									PodSelector: &selectorB,
								},
							},
							Action: &allowAction,
						},
						{
							Ports: []secv1alpha1.NetworkPolicyPort{
								{
									Port: &intstr81,
								},
							},
							From: []secv1alpha1.NetworkPolicyPeer{
								{
									NamespaceSelector: &selectorC,
								},
							},
							Action: &allowAction,
						},
					},
				},
			},
			expectedPolicy: &antreatypes.NetworkPolicy{
				UID:  "uidB",
				Name: "uidB",
				SourceRef: &controlplane.NetworkPolicyReference{
					Type:      controlplane.AntreaNetworkPolicy,
					Namespace: "ns2",
					Name:      "npB",
					UID:       "uidB",
				},
				Priority:     &p10,
				TierPriority: &appTier,
				Rules: []controlplane.NetworkPolicyRule{
					{
						Direction: controlplane.DirectionIn,
						From: controlplane.NetworkPolicyPeer{
							AddressGroups: []string{getNormalizedUID(toGroupSelector("ns2", &selectorB, nil, nil).NormalizedName)},
						},
						Services: []controlplane.Service{
							{
								Protocol: &protocolTCP,
								Port:     &intstr80,
							},
						},
						Priority: 0,
						Action:   &allowAction,
					},
					{
						Direction: controlplane.DirectionIn,
						From: controlplane.NetworkPolicyPeer{
							AddressGroups: []string{getNormalizedUID(toGroupSelector("", nil, &selectorC, nil).NormalizedName)},
						},
						Services: []controlplane.Service{
							{
								Protocol: &protocolTCP,
								Port:     &intstr81,
							},
						},
						Priority: 1,
						Action:   &allowAction,
					},
				},
				AppliedToGroups: []string{getNormalizedUID(toGroupSelector("ns2", &selectorA, nil, nil).NormalizedName)},
			},
			expectedAppliedToGroups: 1,
			expectedAddressGroups:   2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, c := newController()

			if actualPolicy := c.processAntreaNetworkPolicy(tt.inputPolicy); !reflect.DeepEqual(actualPolicy, tt.expectedPolicy) {
				t.Errorf("processClusterNetworkPolicy() got %v, want %v", actualPolicy, tt.expectedPolicy)
			}

			if actualAddressGroups := len(c.addressGroupStore.List()); actualAddressGroups != tt.expectedAddressGroups {
				t.Errorf("len(addressGroupStore.List()) got %v, want %v", actualAddressGroups, tt.expectedAddressGroups)
			}

			if actualAppliedToGroups := len(c.appliedToGroupStore.List()); actualAppliedToGroups != tt.expectedAppliedToGroups {
				t.Errorf("len(appliedToGroupStore.List()) got %v, want %v", actualAppliedToGroups, tt.expectedAppliedToGroups)
			}
		})
	}
}

func TestAddANP(t *testing.T) {
	p10 := float64(10)
	appTier := antreatypes.TierApplication
	allowAction := secv1alpha1.RuleActionAllow
	protocolTCP := controlplane.ProtocolTCP
	intstr80 := intstr.FromInt(80)
	int80 := intstr.FromInt(80)
	selectorAll := metav1.LabelSelector{}
	matchAllPeerEgress := matchAllPeer
	matchAllPeerEgress.AddressGroups = []string{getNormalizedUID(toGroupSelector("", nil, &selectorAll, nil).NormalizedName)}
	tests := []struct {
		name               string
		inputPolicy        *secv1alpha1.NetworkPolicy
		expPolicy          *antreatypes.NetworkPolicy
		expAppliedToGroups int
		expAddressGroups   int
	}{
		{
			name: "application-tier-policy",
			inputPolicy: &secv1alpha1.NetworkPolicy{
				ObjectMeta: metav1.ObjectMeta{Namespace: "nsA", Name: "anpA", UID: "uidA"},
				Spec: secv1alpha1.NetworkPolicySpec{
					AppliedTo: []secv1alpha1.NetworkPolicyPeer{
						{PodSelector: &selectorA},
					},
					Priority: p10,
					Tier:     "Application",
					Ingress: []secv1alpha1.Rule{
						{
							Ports: []secv1alpha1.NetworkPolicyPort{
								{
									Port: &intstr80,
								},
							},
							From: []secv1alpha1.NetworkPolicyPeer{
								{
									PodSelector:            &selectorB,
									NamespaceSelector:      &selectorC,
									ExternalEntitySelector: &selectorD,
								},
							},
							Action: &allowAction,
						},
					},
				},
			},
			expPolicy: &antreatypes.NetworkPolicy{
				UID:  "uidA",
				Name: "uidA",
				SourceRef: &controlplane.NetworkPolicyReference{
					Type:      controlplane.AntreaNetworkPolicy,
					Namespace: "nsA",
					Name:      "anpA",
					UID:       "uidA",
				},
				Priority:     &p10,
				TierPriority: &appTier,
				Rules: []controlplane.NetworkPolicyRule{
					{
						Direction: controlplane.DirectionIn,
						From: controlplane.NetworkPolicyPeer{
							AddressGroups: []string{getNormalizedUID(toGroupSelector("", &selectorB, &selectorC, &selectorD).NormalizedName)},
						},
						Services: []controlplane.Service{
							{
								Protocol: &protocolTCP,
								Port:     &int80,
							},
						},
						Priority: 0,
						Action:   &allowAction,
					},
				},
				AppliedToGroups: []string{getNormalizedUID(toGroupSelector("nsA", &selectorA, nil, nil).NormalizedName)},
			},
			expAppliedToGroups: 1,
			expAddressGroups:   1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, npc := newController()
			npc.addANP(tt.inputPolicy)
			actualPolicyObj, exists, _ := npc.internalNetworkPolicyStore.Get(internalNetworkPolicyKeyFunc(tt.inputPolicy))
			require.True(t, exists)
			actualPolicy := actualPolicyObj.(*antreatypes.NetworkPolicy)
			if !reflect.DeepEqual(actualPolicy, tt.expPolicy) {
				t.Errorf("addANP() got %v, want %v", actualPolicy, tt.expPolicy)
			}

			if actualAddressGroups := len(npc.addressGroupStore.List()); actualAddressGroups != tt.expAddressGroups {
				t.Errorf("len(addressGroupStore.List()) got %v, want %v", actualAddressGroups, tt.expAddressGroups)
			}

			if actualAppliedToGroups := len(npc.appliedToGroupStore.List()); actualAppliedToGroups != tt.expAppliedToGroups {
				t.Errorf("len(appliedToGroupStore.List()) got %v, want %v", actualAppliedToGroups, tt.expAppliedToGroups)
			}
		})
	}
}

func TestDeleteANP(t *testing.T) {
	selectorA := metav1.LabelSelector{MatchLabels: map[string]string{"foo1": "bar1"}}
	anpObj := getANP()
	apgID := getNormalizedUID(toGroupSelector("test-ns", &selectorA, nil, nil).NormalizedName)
	_, npc := newController()
	npc.addANP(anpObj)
	npc.deleteANP(anpObj)
	_, found, _ := npc.appliedToGroupStore.Get(apgID)
	assert.False(t, found, "expected AppliedToGroup to be deleted")
	adgs := npc.addressGroupStore.List()
	assert.Len(t, adgs, 0, "expected empty AddressGroup list")
	key, _ := keyFunc(anpObj)
	_, found, _ = npc.internalNetworkPolicyStore.Get(key)
	assert.False(t, found, "expected internal NetworkPolicy to be deleted")
}

// util functions for testing.
func getANP() *secv1alpha1.NetworkPolicy {
	p10 := float64(10)
	allowAction := secv1alpha1.RuleActionAllow
	selectorA := metav1.LabelSelector{MatchLabels: map[string]string{"foo1": "bar1"}}
	selectorB := metav1.LabelSelector{MatchLabels: map[string]string{"foo2": "bar2"}}
	selectorC := metav1.LabelSelector{MatchLabels: map[string]string{"foo3": "bar3"}}
	ingressRules := []secv1alpha1.Rule{
		{
			From: []secv1alpha1.NetworkPolicyPeer{
				{
					NamespaceSelector: &selectorB,
				},
			},
			Action: &allowAction,
		},
	}
	egressRules := []secv1alpha1.Rule{
		{
			To: []secv1alpha1.NetworkPolicyPeer{
				{
					ExternalEntitySelector: &selectorC,
				},
			},
			Action: &allowAction,
		},
	}
	npObj := &secv1alpha1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{Namespace: "test-ns", Name: "test-anp", UID: "test-id"},
		Spec: secv1alpha1.NetworkPolicySpec{
			AppliedTo: []secv1alpha1.NetworkPolicyPeer{
				{PodSelector: &selectorA},
			},
			Priority: p10,
			Ingress:  ingressRules,
			Egress:   egressRules,
		},
	}
	return npObj
}

func getEETestANP(selectorAppliedTo, selectorIn, selectorOut metav1.LabelSelector) *secv1alpha1.NetworkPolicy {
	allowAction := secv1alpha1.RuleActionAllow
	return &secv1alpha1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "anpA",
			Namespace: "nsA",
		},
		Spec: secv1alpha1.NetworkPolicySpec{
			AppliedTo: []secv1alpha1.NetworkPolicyPeer{
				{
					PodSelector: &selectorAppliedTo,
				},
			},
			Ingress: []secv1alpha1.Rule{
				{
					From: []secv1alpha1.NetworkPolicyPeer{
						{
							ExternalEntitySelector: &selectorIn,
						},
					},
					Action: &allowAction,
				},
			},
			Egress: []secv1alpha1.Rule{
				{
					To: []secv1alpha1.NetworkPolicyPeer{
						{
							ExternalEntitySelector: &selectorOut,
						},
					},
					Action: &allowAction,
				},
			},
		},
	}
}
