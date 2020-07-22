package v1alpha1

type NetworkPolicyCategory string

const (
	K8sNetworkPolicy NetworkPolicyCategory = "K8sNetworkPolicy"
	ClusterNetworkPolicy NetworkPolicyCategory = "ClusterNetworkPolicy"
)

type NetworkPolicyReference struct {

	Category NetworkPolicyCategory `json:"category,omitempty"`

	Namespace string `json:"namespace,omitempty"`

	Name string `json:"name,omitempty"`
}

type RuleStats struct {
	// Packets is the packets count.
	Packets  int64 `json:"packets,omitempty"`
	// Bytes is the bytes count.
	Bytes int64 `json:"bytes,omitempty"`
	// Sessions is the sessions count.
	Sessions int64 `json:"sessions,omitempty"`
}

type NetworkPolicyStats struct {

	NetworkPolicy NetworkPolicyReference `json:"networkPolicy,omitempty"`

	RuleStats RuleStats `json:"ruleStats,omitempty"`
}
