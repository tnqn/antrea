// Copyright 2022 Antrea Authors
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
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	"antrea.io/antrea/pkg/apis/controlplane"
	crdv1alpha1 "antrea.io/antrea/pkg/apis/crd/v1alpha1"
	antreatypes "antrea.io/antrea/pkg/controller/types"
)

func getL7NPReference(anp *crdv1alpha1.L7NetworkPolicy) *controlplane.NetworkPolicyReference {
	return &controlplane.NetworkPolicyReference{
		Type:      controlplane.AntreaL7NetworkPolicy,
		Namespace: anp.Namespace,
		Name:      anp.Name,
		UID:       anp.UID,
	}
}

// addANP receives AntreaNetworkPolicy ADD events and enqueues a reference of
// the AntreaNetworkPolicy to trigger its process.
func (n *NetworkPolicyController) addL7NP(obj interface{}) {
	defer n.heartbeat("addANP")
	np := obj.(*crdv1alpha1.L7NetworkPolicy)
	klog.Infof("Processing Antrea NetworkPolicy %s/%s ADD event", np.Namespace, np.Name)
	n.enqueueInternalNetworkPolicy(getL7NPReference(np))
}

// updateANP receives AntreaNetworkPolicy UPDATE events and enqueues a reference
// of the AntreaNetworkPolicy to trigger its process.
func (n *NetworkPolicyController) updateL7NP(old, cur interface{}) {
	defer n.heartbeat("updateANP")
	curNP := cur.(*crdv1alpha1.L7NetworkPolicy)
	klog.Infof("Processing Antrea NetworkPolicy %s/%s UPDATE event", curNP.Namespace, curNP.Name)
	n.enqueueInternalNetworkPolicy(getL7NPReference(curNP))
}

// deleteANP receives AntreaNetworkPolicy DELETE events and enqueues a reference
// of the AntreaNetworkPolicy to trigger its process.
func (n *NetworkPolicyController) deleteL7NP(old interface{}) {
	np, ok := old.(*crdv1alpha1.L7NetworkPolicy)
	if !ok {
		tombstone, ok := old.(cache.DeletedFinalStateUnknown)
		if !ok {
			klog.Errorf("Error decoding object when deleting Antrea NetworkPolicy, invalid type: %v", old)
			return
		}
		np, ok = tombstone.Obj.(*crdv1alpha1.L7NetworkPolicy)
		if !ok {
			klog.Errorf("Error decoding object tombstone when deleting Antrea NetworkPolicy, invalid type: %v", tombstone.Obj)
			return
		}
	}
	defer n.heartbeat("deleteANP")
	klog.Infof("Processing Antrea NetworkPolicy %s/%s DELETE event", np.Namespace, np.Name)
	n.enqueueInternalNetworkPolicy(getL7NPReference(np))
}

// processAntreaNetworkPolicy creates an internal NetworkPolicy instance
// corresponding to the crdv1alpha1.NetworkPolicy object. This method
// does not commit the internal NetworkPolicy in store, instead returns an
// instance to the caller.
func (n *NetworkPolicyController) processL7NetworkPolicy(np *crdv1alpha1.L7NetworkPolicy) (*antreatypes.NetworkPolicy, map[string]*antreatypes.AppliedToGroup, map[string]*antreatypes.AddressGroup) {
	appliedToPerRule := len(np.Spec.AppliedTo) == 0
	// appliedToGroups tracks all distinct appliedToGroups referred to by the Antrea NetworkPolicy,
	// either in the spec section or in ingress/egress rules.
	// The span calculation and stale appliedToGroup cleanup logic would work seamlessly for both cases.
	appliedToGroups := map[string]*antreatypes.AppliedToGroup{}
	addressGroups := map[string]*antreatypes.AddressGroup{}
	rules := make([]controlplane.NetworkPolicyRule, 0, len(np.Spec.Ingress)+len(np.Spec.Egress))
	// Create AppliedToGroup for each AppliedTo present in AntreaNetworkPolicy spec.
	atgs := n.processAppliedTo(np.Namespace, np.Spec.AppliedTo)
	appliedToGroups = mergeAppliedToGroups(appliedToGroups, atgs...)
	// Compute NetworkPolicyRule for Ingress Rule.
	for _, ingressRule := range np.Spec.Ingress {
		var services []controlplane.Service
		protocolHTTP := controlplane.ProtocolHTTP
		if len(ingressRule.Protocols) > 0 {
			protocol := ingressRule.Protocols[0]
			services = append(services, controlplane.Service{
				Protocol: &protocolHTTP,
				Host:     protocol.HTTP.Host,
				Method:   protocol.HTTP.Method,
				Path:     protocol.HTTP.Path,
			})
		}
		peer, ags := n.toAntreaPeerForCRD(ingressRule.From, np, controlplane.DirectionIn, false)
		addressGroups = mergeAddressGroups(addressGroups, ags...)
		rules = append(rules, controlplane.NetworkPolicyRule{
			Direction: controlplane.DirectionIn,
			From:      *peer,
			Name:      ingressRule.Name,
			Services:  services,
		})
	}
	// Compute NetworkPolicyRule for Egress Rule.
	for _, egressRule := range np.Spec.Egress {
		peer, ags := n.toAntreaPeerForCRD(egressRule.From, np, controlplane.DirectionIn, false)
		addressGroups = mergeAddressGroups(addressGroups, ags...)
		rules = append(rules, controlplane.NetworkPolicyRule{
			Direction: controlplane.DirectionOut,
			From:      *peer,
			Name:      egressRule.Name,
		})
	}
	internalNetworkPolicy := &antreatypes.NetworkPolicy{
		SourceRef: &controlplane.NetworkPolicyReference{
			Type:      controlplane.AntreaL7NetworkPolicy,
			Namespace: np.Namespace,
			Name:      np.Name,
			UID:       np.UID,
		},
		Name:             internalNetworkPolicyKeyFunc(np),
		UID:              np.UID,
		Generation:       np.Generation,
		AppliedToGroups:  sets.StringKeySet(appliedToGroups).List(),
		Rules:            rules,
		AppliedToPerRule: appliedToPerRule,
	}
	return internalNetworkPolicy, appliedToGroups, addressGroups
}
