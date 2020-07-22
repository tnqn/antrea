package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +resourceName=clusternetworkpolicies
// +genclient:readonly
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ClusterNetworkPolicyMetric is the query options to a Pod's proxy call
type ClusterNetworkPolicyMetric struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	Stats  NetworkPolicyStats `json:"stats,omitempty" protobuf:"varint,2,opt,name=stats"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ClusterNetworkPolicyMetricList is the query options to a Pod's proxy call
type ClusterNetworkPolicyMetricList struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	// List of ClusterNetworkPolicy metrics.
	Items []ClusterNetworkPolicyMetric `json:"items" protobuf:"bytes,2,rep,name=items"`
}

type NetworkPolicyStats struct {
	// Packets is the packets count hit by the NetworkPolicy.
	Packets  int64 `json:"packets,omitempty" protobuf:"varint,1,opt,name=packets"`
	// Bytes is the bytes count hit by the NetworkPolicy.
	Bytes int64 `json:"bytes,omitempty" protobuf:"varint,2,opt,name=bytes"`
	// Sessions is the sessions count hit by the NetworkPolicy.
	Sessions int64 `json:"sessions,omitempty" protobuf:"varint,3,opt,name=sessions"`
}