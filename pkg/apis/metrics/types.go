package metrics

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +resourceName=clusternetworkpolicies
// +genclient:readonly
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// NetworkPolicyMetric is the query options to a Pod's proxy call
type ClusterNetworkPolicyMetric struct {
	metav1.TypeMeta
	metav1.ObjectMeta

	Stats  NetworkPolicyStats
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ClusterNetworkPolicyMetricList is the query options to a Pod's proxy call
type ClusterNetworkPolicyMetricList struct {
	metav1.TypeMeta
	metav1.ObjectMeta

	// List of ClusterNetworkPolicy metrics.
	Items []ClusterNetworkPolicyMetric
}

type NetworkPolicyStats struct {
	// Packets is the packets count hit by the NetworkPolicy.
	Packets  int64
	// Bytes is the bytes count hit by the NetworkPolicy.
	Bytes int64
	// Sessions is the sessions count hit by the NetworkPolicy.
	Sessions int64
}