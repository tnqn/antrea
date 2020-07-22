package stats

import (
	"bytes"
	"context"
	"time"
	"encoding/json"

	"k8s.io/klog"

	"github.com/vmware-tanzu/antrea/pkg/agent"
	"github.com/vmware-tanzu/antrea/pkg/agent/openflow"
	"github.com/vmware-tanzu/antrea/pkg/agent/types"
	"github.com/vmware-tanzu/antrea/pkg/apis/controlplane/v1alpha1"
)

type Collector struct {
	antreaClientProvider agent.AntreaClientProvider
	// ofClient is the Openflow interface.
	ofClient openflow.Client

	lastStats map[v1alpha1.NetworkPolicyReference]*v1alpha1.RuleStats
}

func NewCollector(antreaClientProvider agent.AntreaClientProvider, ofClient openflow.Client) *Collector {
	manager := &Collector{
		antreaClientProvider: antreaClientProvider,
		ofClient:ofClient,
		lastStats: map[v1alpha1.NetworkPolicyReference]*v1alpha1.RuleStats{},
	}
	return manager
}

func (m *Collector) Run(stopCh <-chan struct{}) {
	klog.Info("Start collecting metrics")
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	m.collect(false)
	for {
		select {
		case <-ticker.C:
			m.collect(true)
		case <-stopCh:
			return
		}
	}
}

func (m *Collector) ruleStatsToPolicyStats(ruleStats map[uint32]*types.RuleMetric) map[v1alpha1.NetworkPolicyReference]*v1alpha1.RuleStats {
	policyStats := map[v1alpha1.NetworkPolicyReference]*v1alpha1.RuleStats{}
	for ofID, rStats := range ruleStats {
		policyName, policyNamespace := m.ofClient.GetPolicyFromConjunction(ofID)
		klog.Infof("Converting ofID %v to policy %s/%s", ofID, policyNamespace, policyName)
		if policyNamespace == "" || policyName == "" {
			klog.Warningf("Cannot find NetworkPolicy that has ofID %v", ofID)
			continue
		}
		policy := v1alpha1.NetworkPolicyReference{
			Namespace: policyNamespace,
			Name:      policyName,
		}
		// TODO: This is not ideal but works for now. We should find a reliable way to identify the category of the
		//  NetworkPolicy, especially after Antrea NetworkPolicy is supported.
		if policyNamespace == "" {
			policy.Category = v1alpha1.ClusterNetworkPolicy
		} else {
			policy.Category = v1alpha1.K8sNetworkPolicy
		}
		pStats, exists := policyStats[policy]
		if !exists {
			pStats = new(v1alpha1.RuleStats)
			policyStats[policy] = pStats
		}
		pStats.Bytes += int64(rStats.Bytes)
		pStats.Sessions += int64(rStats.Connections)
		pStats.Packets += int64(rStats.Packets)
	}
	return policyStats
}

func (m *Collector) collect(report bool) {
	curRuleStats := m.ofClient.NetworkPolicyMetrics()
	curPolicyStats := m.ruleStatsToPolicyStats(curRuleStats)
	klog.Infof("Collected NetworkPolicy stats: %v", curPolicyStats)
	if report {
		metrics := make([]v1alpha1.NetworkPolicyStats, 0, len(curPolicyStats))
		for policy, curStats := range curPolicyStats {
			var incStats *v1alpha1.RuleStats
			oldStats, exists := m.lastStats[policy]
			if !exists {
				incStats = curStats
			} else {
				incStats = subtract(curStats, oldStats)
				// This could happen if OVS is restarted, do not report negative stats.
				if incStats.Bytes < 0 {
					klog.Infof("Collected negative stats for NetworkPolicy %s/%s: %v", policy.Namespace, policy.Name, incStats)
					continue
				}
			}
			metric := v1alpha1.NetworkPolicyStats{}
			metric.NetworkPolicy = policy
			metric.RuleStats = *incStats
			metrics = append(metrics, metric)
		}
		if err := m.report(metrics); err != nil {
			klog.Errorf("Failed to report NetworkPolicy stats: %v", err)
			// Do not update m.lastStats so that the incremental stats can be
			// accumulated to next report.
			return
		}
	}
	m.lastStats = curPolicyStats
}

func (m *Collector) report(metrics []v1alpha1.NetworkPolicyStats) error {
	var buf bytes.Buffer
	klog.Infof("Reporting %v", metrics)
	if err := json.NewEncoder(&buf).Encode(metrics); err != nil {
		return err
	}

	antreaClient, err := m.antreaClientProvider.GetAntreaClient()
	if err != nil {
		return err
	}
	_, err = antreaClient.Discovery().RESTClient().Post().Body(buf.Bytes()).RequestURI("/networkpolicy/stats").Timeout(10 * time.Second).DoRaw(context.TODO())
	if err != nil {
		return err
	}
	return nil
}

func subtract(s1, s2 *v1alpha1.RuleStats) *v1alpha1.RuleStats {
	res := &v1alpha1.RuleStats{}
	res.Packets = s1.Packets - s2.Packets
	res.Sessions = s1.Sessions - s2.Sessions
	res.Bytes = s1.Bytes - s2.Bytes
	return res
}
