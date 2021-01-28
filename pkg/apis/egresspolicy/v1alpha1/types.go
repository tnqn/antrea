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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +genclient:noStatus
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type EgressPolicy struct {
	metav1.TypeMeta `json:",inline"`
	// Standard metadata of the object.
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Specification of the desired behavior of EgressPolicy.
	Spec EgressPolicySpec `json:"spec"`
}

// EgressPolicySpec defines the desired state for EgressPolicy.
type EgressPolicySpec struct {
	// AppliedTo selects Pods to which the policy will be applied.
	AppliedTo AppliedTo
	// EgressIP specifies the SNAT IP address for the selected Pods.
	EgressIP string
}

// AppliedTo defines the workloads to which a policy is applied.
type AppliedTo struct {
	// Select Pods matched by this selector. If set with NamespaceSelector,
	// Pods are matched from Namespaces matched by the NamespaceSelector;
	// otherwise, Pods are matched from all Namespaces.
	// +optional
	PodSelector *metav1.LabelSelector `json:"podSelector,omitempty"`
	// Select all Pods from Namespaces matched by this selector, as
	// workloads in To/From fields. If set with PodSelector,
	// Pods are matched from Namespaces matched by the NamespaceSelector.
	// +optional
	NamespaceSelector *metav1.LabelSelector `json:"namespaceSelector,omitempty"`
}


// SNATPolicyStatus defines the actual state for SNATPolicy.
type SNATPolicyStatus struct {
	EgressNode string
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type SNATPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []SNATPolicy `json:"items"`
}

// +genclient
// +genclient:noStatus
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type NodeIPAddressBinding struct {
	metav1.TypeMeta `json:",inline"`
	// Standard metadata of the object.
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// IP addresses of the Node.
	IPAddresses []string `json:"ipAddresses"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type NodeIPAddressBindingList struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []NodeIPAddressBinding `json:"items"`
}