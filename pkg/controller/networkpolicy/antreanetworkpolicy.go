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
	"fmt"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	"antrea.io/antrea/pkg/apis/controlplane"
	crdv1alpha1 "antrea.io/antrea/pkg/apis/crd/v1alpha1"
	antreatypes "antrea.io/antrea/pkg/controller/types"
	"antrea.io/antrea/pkg/util/k8s"
)

func getANPReference(anp *crdv1alpha1.NetworkPolicy) *controlplane.NetworkPolicyReference {
	return &controlplane.NetworkPolicyReference{
		Type:      controlplane.AntreaNetworkPolicy,
		Namespace: anp.Namespace,
		Name:      anp.Name,
		UID:       anp.UID,
	}
}

// addANP receives AntreaNetworkPolicy ADD events and enqueues a reference of
// the AntreaNetworkPolicy to trigger its process.
func (n *NetworkPolicyController) addANP(obj interface{}) {
	defer n.heartbeat("addANP")
	np := obj.(*crdv1alpha1.NetworkPolicy)
	klog.Infof("Processing Antrea NetworkPolicy %s/%s ADD event", np.Namespace, np.Name)
	n.enqueueInternalNetworkPolicy(getANPReference(np))
}

// updateANP receives AntreaNetworkPolicy UPDATE events and enqueues a reference
// of the AntreaNetworkPolicy to trigger its process.
func (n *NetworkPolicyController) updateANP(old, cur interface{}) {
	defer n.heartbeat("updateANP")
	curNP := cur.(*crdv1alpha1.NetworkPolicy)
	klog.Infof("Processing Antrea NetworkPolicy %s/%s UPDATE event", curNP.Namespace, curNP.Name)
	n.enqueueInternalNetworkPolicy(getANPReference(curNP))
}

// deleteANP receives AntreaNetworkPolicy DELETE events and enqueues a reference
// of the AntreaNetworkPolicy to trigger its process.
func (n *NetworkPolicyController) deleteANP(old interface{}) {
	np, ok := old.(*crdv1alpha1.NetworkPolicy)
	if !ok {
		tombstone, ok := old.(cache.DeletedFinalStateUnknown)
		if !ok {
			klog.Errorf("Error decoding object when deleting Antrea NetworkPolicy, invalid type: %v", old)
			return
		}
		np, ok = tombstone.Obj.(*crdv1alpha1.NetworkPolicy)
		if !ok {
			klog.Errorf("Error decoding object tombstone when deleting Antrea NetworkPolicy, invalid type: %v", tombstone.Obj)
			return
		}
	}
	defer n.heartbeat("deleteANP")
	klog.Infof("Processing Antrea NetworkPolicy %s/%s DELETE event", np.Namespace, np.Name)
	n.enqueueInternalNetworkPolicy(getANPReference(np))
}

// processAntreaNetworkPolicy creates an internal NetworkPolicy instance
// corresponding to the crdv1alpha1.NetworkPolicy object. This method
// does not commit the internal NetworkPolicy in store, instead returns an
// instance to the caller wherein.
func (n *NetworkPolicyController) processAntreaNetworkPolicy(np *crdv1alpha1.NetworkPolicy) *antreatypes.NetworkPolicy {
	appliedToPerRule := len(np.Spec.AppliedTo) == 0
	// appliedToGroupNames tracks all distinct appliedToGroups referred to by the Antrea NetworkPolicy,
	// either in the spec section or in ingress/egress rules.
	// The span calculation and stale appliedToGroup cleanup logic would work seamlessly for both cases.
	appliedToGroupNamesSet := sets.String{}
	rules := make([]controlplane.NetworkPolicyRule, 0, len(np.Spec.Ingress)+len(np.Spec.Egress))
	newUnrealizableInternalNetworkPolicy := func(err error) *antreatypes.NetworkPolicy {
		return &antreatypes.NetworkPolicy{
			SourceRef: &controlplane.NetworkPolicyReference{
				Type:      controlplane.AntreaNetworkPolicy,
				Namespace: np.Namespace,
				Name:      np.Name,
				UID:       np.UID,
			},
			Name:             internalNetworkPolicyKeyFunc(np),
			UID:              np.UID,
			Generation:       np.Generation,
			RealizationError: err,
		}
	}
	// Create AppliedToGroup for each AppliedTo present in AntreaNetworkPolicy spec.
	_, err := n.processAppliedTo(np.UID, np.Namespace, np.Spec.AppliedTo, appliedToGroupNamesSet)
	if err != nil {
		return newUnrealizableInternalNetworkPolicy(err)
	}
	// Compute NetworkPolicyRule for Ingress Rule.
	for idx, ingressRule := range np.Spec.Ingress {
		// Set default action to ALLOW to allow traffic.
		services, namedPortExists := toAntreaServicesForCRD(ingressRule.Ports, ingressRule.Protocols)
		// Create AppliedToGroup for each AppliedTo present in the ingress rule.
		atGroups, err := n.processAppliedTo(np.UID, np.Namespace, ingressRule.AppliedTo, appliedToGroupNamesSet)
		if err != nil {
			return newUnrealizableInternalNetworkPolicy(err)
		}
		rules = append(rules, controlplane.NetworkPolicyRule{
			Direction:       controlplane.DirectionIn,
			From:            *n.toAntreaPeerForCRD(ingressRule.From, np, controlplane.DirectionIn, namedPortExists),
			Services:        services,
			Name:            ingressRule.Name,
			Action:          ingressRule.Action,
			Priority:        int32(idx),
			EnableLogging:   ingressRule.EnableLogging,
			AppliedToGroups: atGroups,
		})
	}
	// Compute NetworkPolicyRule for Egress Rule.
	for idx, egressRule := range np.Spec.Egress {
		// Set default action to ALLOW to allow traffic.
		services, namedPortExists := toAntreaServicesForCRD(egressRule.Ports, egressRule.Protocols)
		// Create AppliedToGroup for each AppliedTo present in the egress rule.
		atGroups, err := n.processAppliedTo(np.UID, np.Namespace, egressRule.AppliedTo, appliedToGroupNamesSet)
		if err != nil {
			return newUnrealizableInternalNetworkPolicy(err)
		}
		var peers *controlplane.NetworkPolicyPeer
		if egressRule.ToServices != nil {
			peers = n.svcRefToPeerForCRD(egressRule.ToServices, np.Namespace)
		} else {
			peers = n.toAntreaPeerForCRD(egressRule.To, np, controlplane.DirectionOut, namedPortExists)
		}
		rules = append(rules, controlplane.NetworkPolicyRule{
			Direction:       controlplane.DirectionOut,
			To:              *peers,
			Services:        services,
			Name:            egressRule.Name,
			Action:          egressRule.Action,
			Priority:        int32(idx),
			EnableLogging:   egressRule.EnableLogging,
			AppliedToGroups: atGroups,
		})
	}
	tierPriority := n.getTierPriority(np.Spec.Tier)
	internalNetworkPolicy := &antreatypes.NetworkPolicy{
		SourceRef: &controlplane.NetworkPolicyReference{
			Type:      controlplane.AntreaNetworkPolicy,
			Namespace: np.Namespace,
			Name:      np.Name,
			UID:       np.UID,
		},
		Name:             internalNetworkPolicyKeyFunc(np),
		UID:              np.UID,
		Generation:       np.Generation,
		AppliedToGroups:  appliedToGroupNamesSet.List(),
		Rules:            rules,
		Priority:         &np.Spec.Priority,
		TierPriority:     &tierPriority,
		AppliedToPerRule: appliedToPerRule,
	}
	return internalNetworkPolicy
}

// processAppliedTo creates AppliedToGroups for an ANP according to the provided NetworkPolicyPeers and adds the created
// AppliedToGroups to the provided set.
func (n *NetworkPolicyController) processAppliedTo(networkPolicyUID types.UID, namespace string, appliedTo []crdv1alpha1.NetworkPolicyPeer, appliedToGroupNamesSet sets.String) ([]string, error) {
	var appliedToGroupNames []string
	for _, at := range appliedTo {
		var atg string
		if at.Group != "" {
			var err error
			atg, err = n.createAppliedToGroupForNamespacedGroup(networkPolicyUID, namespace, at.Group)
			if err != nil {
				return nil, err
			}
		} else {
			atg = n.createAppliedToGroup(networkPolicyUID, namespace, at.PodSelector, at.NamespaceSelector, at.ExternalEntitySelector)
		}
		if atg != "" {
			appliedToGroupNames = append(appliedToGroupNames, atg)
			appliedToGroupNamesSet.Insert(atg)
		}
	}
	return appliedToGroupNames, nil
}

// ErrNetworkPolicyAppliedToUnsupportedGroup is an error response when
// a Group with IPBlocks or NamespaceSelector is used as AppliedTo.
type ErrNetworkPolicyAppliedToUnsupportedGroup struct {
	namespace string
	groupName string
}

func (e ErrNetworkPolicyAppliedToUnsupportedGroup) Error() string {
	return fmt.Sprintf("group %s/%s with IPBlocks or NamespaceSelector can not be used as AppliedTo", e.namespace, e.groupName)
}

func (n *NetworkPolicyController) createAppliedToGroupForNamespacedGroup(networkPolicyUID types.UID, namespace, groupName string) (string, error) {
	// Namespaced group uses NAMESPACE/NAME as the key of the corresponding internal group.
	key := k8s.NamespacedName(namespace, groupName)
	// Find the internal Group corresponding to this Group.
	// There is no need to check if the namespaced group exists in groupLister because its existence will eventually be
	// reflected in internalGroupStore.
	ig, found, _ := n.internalGroupStore.Get(key)
	if !found {
		// Internal Group is not found, which means the corresponding namespaced group is either not created yet or not
		// processed yet. Once the internal Group is created and processed, the sync worker for internal group will
		// re-enqueue the ClusterNetworkPolicy processing which will trigger the creation of AppliedToGroup.
		return "", nil
	}
	intGrp := ig.(*antreatypes.Group)
	if len(intGrp.IPBlocks) > 0 || (intGrp.Selector != nil && intGrp.Selector.NamespaceSelector != nil) {
		klog.V(2).InfoS("Group with IPBlocks or NamespaceSelector can not be used as AppliedTo", "Group", key)
		return "", ErrNetworkPolicyAppliedToUnsupportedGroup{namespace: namespace, groupName: groupName}
	}
	return n.createAppliedToGroupForInternalGroup(networkPolicyUID, intGrp), nil
}
