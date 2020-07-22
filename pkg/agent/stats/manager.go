package stats

import (
	"bytes"
	"context"
	"time"
	"encoding/json"

	"k8s.io/klog"
	"k8s.io/apimachinery/pkg/types"

	"github.com/vmware-tanzu/antrea/pkg/agent"
	"github.com/vmware-tanzu/antrea/pkg/agent/openflow"
	metricsv1alpha1 "github.com/vmware-tanzu/antrea/pkg/apis/metrics/v1alpha1"
)

type Manager struct {
	antreaClientProvider agent.AntreaClientProvider
	// ofClient is the Openflow interface.
	ofClient openflow.Client

	lastStats map[types.NamespacedName]*metricsv1alpha1.NetworkPolicyStats
}

func NewManager(antreaClientProvider agent.AntreaClientProvider, ofClient openflow.Client) *Manager {
	manager := &Manager{
		antreaClientProvider: antreaClientProvider,
		ofClient:ofClient,
		lastStats: map[types.NamespacedName]*metricsv1alpha1.NetworkPolicyStats{},
	}
	return manager
}

func (m *Manager) Run(stopCh <-chan struct{}) {
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

func (m *Manager) ruleStatsToPolicyStats(ruleStats map[uint32]*metricsv1alpha1.NetworkPolicyStats) map[types.NamespacedName]*metricsv1alpha1.NetworkPolicyStats {
	policyStats := map[types.NamespacedName]*metricsv1alpha1.NetworkPolicyStats{}
	for ofID, rStats := range ruleStats {
		policyName, policyNamespace := m.ofClient.GetPolicyFromConjunction(ofID)
		// Only collect ClusterNetworkPolicy for now.
		//if policyNamespace != "" {
		//	continue
		//}
		nn := types.NamespacedName{
			Namespace: policyNamespace,
			Name:      policyName,
		}
		pStats, exists := policyStats[nn]
		if !exists {
			pStats = new(metricsv1alpha1.NetworkPolicyStats)
			policyStats[nn] = pStats
		}
		pStats.Bytes += rStats.Bytes
		pStats.Sessions += rStats.Sessions
		pStats.Packets += rStats.Packets
	}
	return policyStats
}

func (m *Manager) collect(report bool) {
	curRuleStats, err := m.ofClient.GetNetworkPolicyRuleStats()
	if err != nil {
		klog.Errorf("Failed to get NetworkPolicy stats: %v", err)
		return
	}
	curPolicyStats := m.ruleStatsToPolicyStats(curRuleStats)
	if report {
		metrics := make([]metricsv1alpha1.ClusterNetworkPolicyMetric, 0, len(curPolicyStats))
		for nn, curStats := range curPolicyStats {
			var incStats *metricsv1alpha1.NetworkPolicyStats
			oldStats, exists := m.lastStats[nn]
			if !exists {
				incStats = curStats
			} else {
				incStats = subtract(curStats, oldStats)
				// This could happen if OVS is restarted, do not report negative stats.
				if incStats.Bytes < 0 {
					klog.Warningf("Collected negative stats for NetworkPolicy %s/%s: %v", nn.Namespace, nn.Name, incStats)
					continue
				}
			}
			metric := metricsv1alpha1.ClusterNetworkPolicyMetric{}
			metric.Namespace = nn.Namespace
			metric.Name = nn.Name
			metric.Stats = *incStats
			metrics = append(metrics, metric)
		}
		if err := m.report(metrics); err != nil {
			klog.Errorf("Failed to report NetworkPolicy Stats: %v", err)
			// Do not update m.lastStats so that the incremental stats can be
			// accumulated to next report.
			return
		}
	}
	m.lastStats = curPolicyStats
}

func (m *Manager) report(metrics []metricsv1alpha1.ClusterNetworkPolicyMetric) error {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(metrics); err != nil {
		return err
	}

	antreaClient, err := m.antreaClientProvider.GetAntreaClient()
	if err != nil {
		return err
	}
	_, err = antreaClient.Discovery().RESTClient().Post().Body(buf.Bytes()).RequestURI("/stats/networkpolicy").Timeout(10 * time.Second).DoRaw(context.TODO())
	if err != nil {
		return err
	}
	return nil
}

func subtract(s1, s2 *metricsv1alpha1.NetworkPolicyStats) *metricsv1alpha1.NetworkPolicyStats {
	res := &metricsv1alpha1.NetworkPolicyStats{}
	res.Packets = s1.Packets - s2.Packets
	res.Sessions = s1.Sessions - s2.Sessions
	res.Bytes = s1.Bytes - s2.Bytes
	return res
}
