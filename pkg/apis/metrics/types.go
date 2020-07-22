package metrics

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// NetworkPolicyMetric is the query options to a Pod's proxy call
type ClusterNetworkPolicyMetric struct {
	metav1.TypeMeta
	metav1.ObjectMeta

	StartTime metav1.Time

	Stats  NetworkPolicyStats
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ClusterNetworkPolicyMetricList is the query options to a Pod's proxy call
type ClusterNetworkPolicyMetricList struct {
	metav1.TypeMeta
	metav1.ListMeta

	// List of ClusterNetworkPolicy metrics.
	Items []ClusterNetworkPolicyMetric
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// NetworkPolicyMetric is the query options to a Pod's proxy call
type K8sNetworkPolicyMetric struct {
	metav1.TypeMeta
	metav1.ObjectMeta

	StartTime metav1.Time

	Stats  NetworkPolicyStats
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ClusterNetworkPolicyMetricList is the query options to a Pod's proxy call
type K8sNetworkPolicyMetricList struct {
	metav1.TypeMeta
	metav1.ListMeta

	// List of K8sNetworkPolicy metrics.
	Items []K8sNetworkPolicyMetric
}

type NetworkPolicyStats struct {
	// Packets is the packets count hit by the NetworkPolicy.
	Packets  int64
	// Bytes is the bytes count hit by the NetworkPolicy.
	Bytes int64
	// Sessions is the sessions count hit by the NetworkPolicy.
	Sessions int64
}