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

package v1alpha1

import (
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// PermissionRead allows only read permission for Tiers
	PermissionRead = "read"
	// Permissionedit allows create, update, delete and read permission for Tiers
	PermissionEdit = "edit"
)

// +genclient
// +genclient:nonNamespaced
// +genclient:noStatus
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type TierEntitlement struct {
	metav1.TypeMeta `json:",inline"`
	// Standard metadata of the object.
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Specification of the desired behavior of TierEntitlement.
	Spec TierEntitlementSpec `json:"spec"`
}

// TierEntitlementSpec defines the desired state for TierEntitlement.
type TierEntitlementSpec struct {
	// Tier is a list of Tier names to which this entitlement belongs to.
	// TiersAll represents all Tiers.
	Tiers []string `json:"tiers"`
	// Permission defines the allowed actions to be performed on the Tiers
	// specified in Tiers. Allowed permissions are "edit" and "read". "edit"
	// permission allows any authorized user to add/remove Tier references in
	// an Antrea-native policy. "read" permission only allows authorized users
	// to read Antrea-native policies referring the Tier names set in Tiers field.
	Permission string `json:"permission"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type TierEntitlementList struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []TierEntitlement `json:"items"`
}

// +genclient
// +genclient:nonNamespaced
// +genclient:noStatus
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type TierEntitlementBinding struct {
	metav1.TypeMeta `json:",inline"`
	// Standard metadata of the object.
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Specification of the desired behavior of TierEntitlementBinding.
	Spec TierEntitlementBindingSpec `json:"spec"`
}

// TierEntitlementBindingSpec defines the desired state for TierEntitlementBinding.
type TierEntitlementBindingSpec struct {
	// Subjects holds references to the objects the entitlement applies to.
	// +optional
	Subjects []rbacv1.Subject `json:"subjects,omitempty"`
	// TierEntitlement references a TierEntitlement in the global namespace.
	// If the TierEntitlement cannot be resolved, the Authorizer must return an
	// error.
	TierEntitlement string `json:"tierEntitlement"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type TierEntitlementBindingList struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []TierEntitlementBinding `json:"items"`
}
