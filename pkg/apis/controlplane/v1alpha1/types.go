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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	metricsv1alpha1 "github.com/vmware-tanzu/antrea/pkg/apis/metrics/v1alpha1"
)

// The category of NetworkPolicy, could be NetworkPolicy, ClusterNetworkPolicy, and AntreaNetworkPolicy.
type NetworkPolicyCategory string

const (
	// It represents the K8s NetworkPolicy.
	K8sNetworkPolicy NetworkPolicyCategory = "K8sNetworkPolicy"
	// It represents the Antrea ClusterNetworkPolicy.
	ClusterNetworkPolicy NetworkPolicyCategory = "ClusterNetworkPolicy"
	// It represents the Antrea NetworkPolicy.
	AntreaNetworkPolicy NetworkPolicyCategory = "AntreaNetworkPolicy"
)

// +genclient
// +genclient:nonNamespaced
// +genclient:onlyVerbs=create
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// NodeStatsSummary contains stats produced on a Node. It's used by the antrea-agents to report stats to the antrea-controller.
type NodeStatsSummary struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	// The TrafficStats collected from the Node.
	NetworkPolicies []NetworkPolicyStats `json:"networkPolicies,omitempty" protobuf:"bytes,2,rep,name=networkPolicies"`
}

// NetworkPolicyReference identifies a NetworkPolicy across all kinds of NetworkPolicies.
type NetworkPolicyReference struct {
	// The category of the NetworkPolicy.
	Category NetworkPolicyCategory `json:"category,omitempty" protobuf:"bytes,1,opt,name=category"`
	// The Namespace of the NetworkPolicy. It's empty for ClusterNetworkPolicy.
	Namespace string `json:"namespace,omitempty" protobuf:"bytes,2,opt,name=namespace"`
	// The name of the NetworkPolicy.
	Name string `json:"name,omitempty" protobuf:"bytes,3,opt,name=name"`
}

// NetworkPolicyStats contains the information and traffic stats of a NetworkPolicy.
type NetworkPolicyStats struct {
	// The reference of the NetworkPolicy.
	NetworkPolicy NetworkPolicyReference `json:"networkPolicy,omitempty" protobuf:"bytes,1,opt,name=networkPolicy"`
	// The stats of the NetworkPolicy.
	TrafficStats metricsv1alpha1.TrafficStats `json:"trafficStats,omitempty" protobuf:"bytes,2,opt,name=trafficStats"`
}
