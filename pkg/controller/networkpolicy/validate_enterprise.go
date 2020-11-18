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

	authenticationv1 "k8s.io/api/authentication/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/klog"

	crdv1alpha1 "github.com/vmware-tanzu/antrea/pkg/apis/crd/v1alpha1"
	secv1alpha1 "github.com/vmware-tanzu/antrea/pkg/apis/security/v1alpha1"
)

// enterpriseAntreaPolicyValidator implements the validator interface for
// Antrea-native Policy resources for enterprise features.
type enterpriseAntreaPolicyValidator antreaPolicyValidator

// authorizeTierUserForEdit verifies whether a given User is authorized to
// add/remove reference to the Tier in a Antrea-native policy.
func (a *enterpriseAntreaPolicyValidator) authorizeTierUserForEdit(tier string, userInfo authenticationv1.UserInfo) (string, bool) {
	clusterAdmin := false
	for _, g := range userInfo.Groups {
		if g == "system:masters" {
			clusterAdmin = true
			break
		}
	}
	// Cluster admins are permitted by default.
	if clusterAdmin {
		klog.V(4).Info("Cluster admins authorized by default")
		return "", true
	}
	// Empty tier name corresponds to the default Tier.
	if tier == "" {
		tier = defaultTierName
	}
	// Retrieve all TierEntitlements associated with this Tier.
	tes, _ := a.networkPolicyController.tierEntitlementInformer.Informer().GetIndexer().ByIndex(TierIndex, tier)
	// If no TierEntitlement exist for the Tier, there are no restrictions applied
	// to this Tier. Allow all users to access and modify.
	if len(tes) == 0 {
		klog.V(4).Infof("Tier %s has no TierEntitlements. User %s is authorized.", tier, userInfo.Username)
		return "", true
	}
	// Filter out TierEntitlements which do not have edit permissions.
	var editTEs []*crdv1alpha1.TierEntitlement
	for _, te := range tes {
		teObj := te.(*crdv1alpha1.TierEntitlement)
		if teObj.Spec.Permission == crdv1alpha1.PermissionEdit {
			editTEs = append(editTEs, teObj)
		}
	}
	// If no edit TierEntitlement exist for the Tier, there is no reason to move
	// forward as no user other than the admin is granted edit access to this
	// Tier. Deny request to access this Tier.
	if len(editTEs) == 0 {
		klog.V(2).Infof("Tier %s has no edit TierEntitlements. User %s is unauthorized.", tier, userInfo.Username)
		return fmt.Sprintf("user not authorized to access Tier %s", tier), false
	}
	// For every TierEntitlement, find all referencing TierEntitlementBindings and
	// verify if the user is part of the bindings.
	for _, te := range editTEs {
		// Retrieve TierEntitlementBindings
		tebs, _ := a.networkPolicyController.tierEntitlementBindingInformer.Informer().GetIndexer().ByIndex(TierEntitlementIndex, te.Name)
		for _, teb := range tebs {
			tebObj := teb.(*crdv1alpha1.TierEntitlementBinding)
			allowed := userInSubjects(userInfo, tebObj.Spec.Subjects)
			if allowed {
				// User exists in binding Subjects. Allow request.
				klog.V(4).Infof("User %s is authorized.", userInfo.Username)
				return "", allowed
			}
		}
	}
	// None of the bindings refer to this user in it's Subjects. Deny request.
	klog.V(2).Infof("User %s is not authorized.", userInfo.Username)
	return fmt.Sprintf("user not authorized to access Tier %s", tier), false
}

// createValidate validates the CREATE events of Antrea-native policies with
// enterprise features.
func (a *enterpriseAntreaPolicyValidator) createValidate(curObj interface{}, userInfo authenticationv1.UserInfo) (string, bool) {
	var tier string
	switch curObj.(type) {
	case *secv1alpha1.ClusterNetworkPolicy:
		curCNP := curObj.(*secv1alpha1.ClusterNetworkPolicy)
		tier = curCNP.Spec.Tier
	case *secv1alpha1.NetworkPolicy:
		curANP := curObj.(*secv1alpha1.NetworkPolicy)
		tier = curANP.Spec.Tier
	}
	return a.authorizeTierUserForEdit(tier, userInfo)
}

// updateValidate validates the UPDATE events of Antrea-native policies with
// enterprise features.
func (a *enterpriseAntreaPolicyValidator) updateValidate(curObj, oldObj interface{}, userInfo authenticationv1.UserInfo) (string, bool) {
	var oldTier, curTier, reason string
	allowed := true
	switch curObj.(type) {
	case *secv1alpha1.ClusterNetworkPolicy:
		curCNP := curObj.(*secv1alpha1.ClusterNetworkPolicy)
		oldCNP := oldObj.(*secv1alpha1.ClusterNetworkPolicy)
		curTier = curCNP.Spec.Tier
		oldTier = oldCNP.Spec.Tier
	case *secv1alpha1.NetworkPolicy:
		curANP := curObj.(*secv1alpha1.NetworkPolicy)
		oldANP := oldObj.(*secv1alpha1.NetworkPolicy)
		curTier = curANP.Spec.Tier
		oldTier = oldANP.Spec.Tier
	}
	// Tier is not being updated.
	if curTier == oldTier {
		return "", true
	}
	// Authorize if User can update reference to old Tier.
	reason, allowed = a.authorizeTierUserForEdit(oldTier, userInfo)
	if !allowed {
		return reason, allowed
	}
	// Authorize if User can update reference to new Tier.
	reason, allowed = a.authorizeTierUserForEdit(curTier, userInfo)
	if !allowed {
		return reason, allowed
	}
	return "", true
}

// deleteValidate validates the DELETE events of Antrea-native policies with
// enterprise features.
func (a *enterpriseAntreaPolicyValidator) deleteValidate(oldObj interface{}, userInfo authenticationv1.UserInfo) (string, bool) {
	var tier string
	switch oldObj.(type) {
	case *secv1alpha1.ClusterNetworkPolicy:
		oldCNP := oldObj.(*secv1alpha1.ClusterNetworkPolicy)
		tier = oldCNP.Spec.Tier
	case *secv1alpha1.NetworkPolicy:
		oldANP := oldObj.(*secv1alpha1.NetworkPolicy)
		tier = oldANP.Spec.Tier
	}
	return a.authorizeTierUserForEdit(tier, userInfo)
}

// userInSubjects checks whether a given user belongs to the list of subjects.
func userInSubjects(user authenticationv1.UserInfo, subjects []rbacv1.Subject) bool {
	for _, s := range subjects {
		if s.APIGroup == "rbac.authorization.k8s.io" {
			switch s.Kind {
			case rbacv1.UserKind:
				if s.Name == user.Username {
					// User name matches the Subject.
					return true
				}
			case rbacv1.GroupKind:
				for _, g := range user.Groups {
					if s.Name == g {
						// User Group matches the
						// Subject.
						return true
					}
				}
			}
		}
	}
	// No match for Group or User name.
	return false
}
