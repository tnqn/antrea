// Copyright 2021 Antrea Authors
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
	"context"
	"reflect"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"

	"github.com/vmware-tanzu/antrea/pkg/apis/controlplane"
	corev1a2 "github.com/vmware-tanzu/antrea/pkg/apis/core/v1alpha2"
	secv1alpha1 "github.com/vmware-tanzu/antrea/pkg/apis/security/v1alpha1"
	antreatypes "github.com/vmware-tanzu/antrea/pkg/controller/types"
)

type clusterGroup struct {
	groupSelector *antreatypes.GroupSelector
	ipBlock *controlplane.IPBlock
}

// addClusterGroup is responsible for processing the ADD event of a ClusterGroup resource.
func (n *NetworkPolicyController) addClusterGroup(curObj interface{}) {
	cg := curObj.(*corev1a2.ClusterGroup)
	key := internalGroupKeyFunc(cg)
	klog.V(2).Infof("Processing ADD event for ClusterGroup %s", cg.Name)
	n.enqueueInternalGroup(key)
}

// updateClusterGroup is responsible for processing the UPDATE event of a ClusterGroup resource.
func (n *NetworkPolicyController) updateClusterGroup(oldObj, curObj interface{}) {
	cg := curObj.(*corev1a2.ClusterGroup)
	key := internalGroupKeyFunc(cg)
	klog.V(2).Infof("Processing UPDATE event for ClusterGroup %s", cg.Name)
	n.enqueueInternalGroup(key)
}

// deleteClusterGroup is responsible for processing the DELETE event of a ClusterGroup resource.
func (n *NetworkPolicyController) deleteClusterGroup(oldObj interface{}) {
	og, ok := oldObj.(*corev1a2.ClusterGroup)
	klog.V(2).Infof("Processing DELETE event for ClusterGroup %s", og.Name)
	if !ok {
		tombstone, ok := oldObj.(cache.DeletedFinalStateUnknown)
		if !ok {
			klog.Errorf("Error decoding object when deleting ClusterGroup, invalid type: %v", oldObj)
			return
		}
		og, ok = tombstone.Obj.(*corev1a2.ClusterGroup)
		if !ok {
			klog.Errorf("Error decoding object tombstone when deleting ClusterGroup, invalid type: %v", tombstone.Obj)
			return
		}
	}
	key := internalGroupKeyFunc(og)
	n.enqueueInternalGroup(key)
}

// filterInternalGroupsForService computes a list of internal Group keys which references the Service.
func (n *NetworkPolicyController) filterInternalGroupsForService(obj metav1.Object) sets.String {
	matchingKeySet := sets.String{}
	indexKey, _ := cache.MetaNamespaceKeyFunc(obj)
	matchedSvcGroups, _ := n.cgInformer.Informer().GetIndexer().ByIndex(ServiceIndex, indexKey)
	for _, group := range matchedSvcGroups {
		matchingKeySet.Insert(internalGroupKeyFunc(group.(*corev1a2.ClusterGroup)))
	}
	return matchingKeySet
}

func (n *NetworkPolicyController) enqueueInternalGroup(key string) {
	klog.V(4).Infof("Adding new key %s to internal Group queue", key)
	n.internalGroupQueue.Add(key)
}

func (c *NetworkPolicyController) internalGroupWorker() {
	for c.processNextInternalGroupWorkItem() {
	}
}

// Processes an item in the "internalGroup" work queue, by calling
// syncInternalGroup after casting the item to a string (Group key).
// If syncInternalGroup returns an error, this function handles it by re-queueing
// the item so that it can be processed again later. If syncInternalGroup is
// successful, the ClusterGroup is removed from the queue until we get notify
// of a new change. This function return false if and only if the work queue
// was shutdown (no more items will be processed).
func (c *NetworkPolicyController) processNextInternalGroupWorkItem() bool {
	key, quit := c.internalGroupQueue.Get()
	if quit {
		return false
	}
	defer c.internalGroupQueue.Done(key)

	err := c.syncInternalGroup(key.(string))
	if err != nil {
		// Put the item back in the workqueue to handle any transient errors.
		c.internalGroupQueue.AddRateLimited(key)
		klog.Errorf("Failed to sync internal Group %s: %v", key, err)
		return true
	}
	// If no error occurs we Forget this item so it does not get queued again until
	// another change happens.
	c.internalGroupQueue.Forget(key)
	return true
}

func (n *NetworkPolicyController) syncInternalGroup(key string) error {
	// Retrieve the ClusterGroup corresponding to this key.
	cg, err := n.cgLister.Get(key)
	if err != nil {
		klog.Infof("Didn't find the ClusterGroup %s, skip processing of internal group", key)
		delete(n.clusterGroupStore, key)
		n.podLabelIndex.DeleteGroup(&GroupReference{Type:ClusterGroupType, Name: key})
		return nil
	}

	group := &clusterGroup{}
	if cg.Spec.IPBlock != nil {
		group.ipBlock, _ = toAntreaIPBlockForCRD(cg.Spec.IPBlock)
	} else if cg.Spec.ServiceReference != nil {
		svc, err := n.serviceLister.Services(cg.Spec.ServiceReference.Namespace).Get(cg.Spec.ServiceReference.Name)
		if err != nil {
			klog.V(2).Infof("Error getting Service object %s/%s: %v, skip processing ClusterGroup %s", cg.Spec.ServiceReference.Namespace, cg.Spec.ServiceReference.Name, err, cg.Name)
			return nil
		}
		group.groupSelector = n.serviceToGroupSelector(svc)
	} else {
		group.groupSelector = toGroupSelector("", cg.Spec.PodSelector, cg.Spec.NamespaceSelector, cg.Spec.ExternalEntitySelector)
	}

	oldGroup, exists := n.clusterGroupStore[key]
	if exists && reflect.DeepEqual(oldGroup, group) {
		return nil
	}

	if group.groupSelector != nil {
		n.podLabelIndex.AddGroup(&GroupReference{Type:ClusterGroupType, Name: key}, group.groupSelector)
	} else if exists && oldGroup.groupSelector != nil {
		n.podLabelIndex.DeleteGroup(&GroupReference{Type:ClusterGroupType, Name: key})
	}

	n.clusterGroupStore[key] = group
	// Update the ClusterGroup status to Realized as Antrea has recognized the Group and
	// processed its group members.
	err = n.updateGroupStatus(cg, v1.ConditionTrue)
	if err != nil {
		klog.Errorf("Failed to update ClusterGroup %s GroupMembersComputed condition to %s: %v", cg.Name, v1.ConditionTrue, err)
		return err
	}
	return n.triggerCNPUpdates(cg)
}

// triggerCNPUpdates triggers processing of ClusterNetworkPolicies associated with the input ClusterGroup.
func (n *NetworkPolicyController) triggerCNPUpdates(cg *corev1a2.ClusterGroup) error {
	// If a ClusterGroup is added/updated, it might have a reference in ClusterNetworkPolicy.
	cnps, err := n.cnpInformer.Informer().GetIndexer().ByIndex(ClusterGroupIndex, cg.Name)
	if err != nil {
		klog.Errorf("Error retrieving ClusterNetworkPolicies corresponding to ClusterGroup %s", cg.Name)
		return err
	}
	for _, obj := range cnps {
		cnp := obj.(*secv1alpha1.ClusterNetworkPolicy)
		// Re-process ClusterNetworkPolicies which may be affected due to updates to CG.
		curInternalNP := n.processClusterNetworkPolicy(cnp)
		klog.V(2).Infof("Updating existing internal NetworkPolicy %s for %s", curInternalNP.Name, curInternalNP.SourceRef.ToString())
		key := internalNetworkPolicyKeyFunc(cnp)
		// Lock access to internal NetworkPolicy store such that concurrent access
		// to an internal NetworkPolicy is not allowed. This will avoid the
		// case in which an Update to an internal NetworkPolicy object may
		// cause the SpanMeta member to be overridden with stale SpanMeta members
		// from an older internal NetworkPolicy.
		n.internalNetworkPolicyMutex.Lock()
		oldInternalNPObj, _, _ := n.internalNetworkPolicyStore.Get(key)
		oldInternalNP := oldInternalNPObj.(*antreatypes.NetworkPolicy)
		// Must preserve old internal NetworkPolicy Span.
		curInternalNP.SpanMeta = oldInternalNP.SpanMeta
		n.internalNetworkPolicyStore.Update(curInternalNP)
		// Unlock the internal NetworkPolicy store.
		n.internalNetworkPolicyMutex.Unlock()
		// Enqueue addressGroup keys to update their group members.
		// TODO: optimize this to avoid enqueueing address groups when not updated.
		for _, atg := range curInternalNP.AppliedToGroups {
			n.enqueueAppliedToGroup(atg)
		}
		for _, rule := range curInternalNP.Rules {
			for _, addrGroupName := range rule.From.AddressGroups {
				n.enqueueAddressGroup(addrGroupName)
			}
			for _, addrGroupName := range rule.To.AddressGroups {
				n.enqueueAddressGroup(addrGroupName)
			}
		}
		n.enqueueInternalNetworkPolicy(key)
		n.deleteDereferencedAddressGroups(oldInternalNP)
		for _, atg := range oldInternalNP.AppliedToGroups {
			n.deleteDereferencedAppliedToGroup(atg)
		}
	}
	return nil
}

// updateGroupStatus updates the Status subresource for a ClusterGroup.
func (n *NetworkPolicyController) updateGroupStatus(cg *corev1a2.ClusterGroup, cStatus v1.ConditionStatus) error {
	condStatus := corev1a2.GroupCondition{
		Status: cStatus,
		Type:   corev1a2.GroupMembersComputed,
	}
	if groupMembersComputedConditionEqual(cg.Status.Conditions, condStatus) {
		// There is no change in conditions.
		return nil
	}
	condStatus.LastTransitionTime = metav1.Now()
	status := corev1a2.GroupStatus{
		Conditions: []corev1a2.GroupCondition{condStatus},
	}
	klog.V(4).Infof("Updating ClusterGroup %s status to %#v", cg.Name, condStatus)
	toUpdate := cg.DeepCopy()
	toUpdate.Status = status
	_, err := n.crdClient.CoreV1alpha2().ClusterGroups().UpdateStatus(context.TODO(), toUpdate, metav1.UpdateOptions{})
	return err
}

// groupMembersComputedConditionEqual checks whether the condition status for GroupMembersComputed condition
// is same. Returns true if equal, otherwise returns false. It disregards the lastTransitionTime field.
func groupMembersComputedConditionEqual(conds []corev1a2.GroupCondition, condition corev1a2.GroupCondition) bool {
	for _, c := range conds {
		if c.Type == corev1a2.GroupMembersComputed {
			if c.Status == condition.Status {
				return true
			}
		}
	}
	return false
}

// serviceToGroupSelector knows how to generate GroupSelector for a Service.
func (n *NetworkPolicyController) serviceToGroupSelector(service *v1.Service) *antreatypes.GroupSelector {
	if len(service.Spec.Selector) == 0 {
		klog.Infof("Service %s/%s is without selectors and not supported by serviceReference in ClusterGroup", service.Namespace, service.Name)
		return nil
	}
	svcPodSelector := metav1.LabelSelector{
		MatchLabels: service.Spec.Selector,
	}
	// Convert Service.spec.selector to GroupSelector by setting the Namespace to the Service's Namespace
	// and podSelector to Service's selector.
	groupSelector := toGroupSelector(service.Namespace, &svcPodSelector, nil, nil)
	return groupSelector
}

// GetAssociatedGroups retrieves the internal Groups associated with the entity being
// queried (Pod or ExternalEntity identified by name and namespace).
func (n *NetworkPolicyController) GetAssociatedGroups(name, namespace string) ([]antreatypes.Group, error) {
	//memberKey := k8s.NamespacedName(namespace, name)
	groups := n.podLabelIndex.GetGroups(&v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace:namespace, Name: name},
	})
	//groups, err := n.internalGroupStore.GetByIndex(store.GroupMemberIndex, memberKey)
	//if err != nil {
	//	return nil, fmt.Errorf("failed to retrieve Group associated with key %s", memberKey)
	//}
	groupObjs := make([]antreatypes.Group, len(groups))
	for i, g := range groups {
		groupObjs[i].Name = g.Name
	}
	return groupObjs, nil
}

// GetGroupMembers returns the current members of a ClusterGroup.
func (n *NetworkPolicyController) GetGroupMembers(cgName string) (controlplane.GroupMemberSet, error) {
	//entities, err := n.podLabelIndex.GetEntities(&GroupReference{Type:ClusterGroupType, Name:cgName})
	return nil, nil
	//g, found, err := n.internalGroupStore.Get(cgName)
	//if err != nil || !found {
	//	return nil, fmt.Errorf("failed to get internal group associated with ClusterGroup %s", cgName)
	//}
	//internalGroup := g.(*antreatypes.Group)
	//return internalGroup.GroupMembers, nil
}
