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

package stats

import (
	"bytes"
	"context"
	"encoding/json"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog"

	"github.com/vmware-tanzu/antrea/pkg/agent"
	"github.com/vmware-tanzu/antrea/pkg/agent/openflow"
	"github.com/vmware-tanzu/antrea/pkg/apis"
	cpv1alpha1 "github.com/vmware-tanzu/antrea/pkg/apis/controlplane/v1alpha1"
	metricsv1alpha1 "github.com/vmware-tanzu/antrea/pkg/apis/metrics/v1alpha1"
	"github.com/vmware-tanzu/antrea/pkg/util/env"
)

const (
	// Period for performing stats collection and report.
	collectPeriod = 60 * time.Second
)

// Collector is responsible for collecting stats from the Openflow client, calculating the delta compared with the last
// reported stats, and reporting it to the antrea-controller summary API.
type Collector struct {
	nodeName string
	// antreaClientProvider provides interfaces to get antreaClient, which will be used to report the statistics to the
	// antrea-controller.
	antreaClientProvider agent.AntreaClientProvider
	// ofClient is the Openflow interface that can fetch the statistic of the Openflow entries.
	ofClient openflow.Client
	// lastStatsMap is the last statistics that has been reported to antrea-controller successfully.
	// It is used to calculate the delta of the statistics that will be reported.
	lastStatsMap map[cpv1alpha1.NetworkPolicyReference]*metricsv1alpha1.TrafficStats
}

func NewCollector(antreaClientProvider agent.AntreaClientProvider, ofClient openflow.Client) *Collector {
	nodeName, _ := env.GetNodeName()
	manager := &Collector{
		nodeName:             nodeName,
		antreaClientProvider: antreaClientProvider,
		ofClient:             ofClient,
		lastStatsMap:         map[cpv1alpha1.NetworkPolicyReference]*metricsv1alpha1.TrafficStats{},
	}
	return manager
}

// Run runs a loop that collects statistics and reports them until the provided channel is closed.
func (m *Collector) Run(stopCh <-chan struct{}) {
	klog.Info("Start collecting metrics")
	ticker := time.NewTicker(collectPeriod)
	defer ticker.Stop()

	// Record the initial statistics as the base that will be used to calculate the delta.
	// If the counters increase during antrea-agent's downtime, the delta will not be reported to the antrea-controller,
	// it's however better than reporting the full statistics twice which could introduce greater deviations.
	m.lastStatsMap = m.collect()

	for {
		select {
		case <-ticker.C:
			curStatsMap := m.collect()
			// Do not update m.lastStatsMap if the report fails so that the next report attempt can add up the
			// statistics produced in this duration.
			if err := m.report(curStatsMap); err != nil {
				klog.Errorf("Failed to report stats: %v", err)
			} else {
				m.lastStatsMap = curStatsMap
			}
		case <-stopCh:
			return
		}
	}
}

// collect collects the stats of Openflow rules, maps them to the stats of NetworkPolicies.
// It returns a map from NetworkPolicyReferences to their stats.
func (m *Collector) collect() map[cpv1alpha1.NetworkPolicyReference]*metricsv1alpha1.TrafficStats {
	// TODO: The following process is not atomic, there's a chance that the ofID is released and reused by another
	//  NetworkPolicy rule in-between, leading to incorrect metrics. We should return relevant NetworkPolicy references
	//  along with metrics to avoid it.
	ruleStatsMap := m.ofClient.NetworkPolicyMetrics()
	policyStatsMap := map[cpv1alpha1.NetworkPolicyReference]*metricsv1alpha1.TrafficStats{}
	for ofID, ruleStats := range ruleStatsMap {
		policyName, policyNamespace := m.ofClient.GetPolicyFromConjunction(ofID)
		// Same as above, this may be because the NetworkPolicy is removed right after the metrics are fetched.
		if policyNamespace == "" && policyName == "" {
			klog.Infof("Cannot find NetworkPolicy that has ofID %v", ofID)
			continue
		}
		klog.V(4).Infof("Converting ofID %v to policy %s/%s", ofID, policyNamespace, policyName)
		policy := cpv1alpha1.NetworkPolicyReference{
			Namespace: policyNamespace,
			Name:      policyName,
		}
		// TODO: This is not ideal but works for now. We should find a reliable way to identify the category of the
		//  NetworkPolicy. (#1173)
		if policyNamespace == "" {
			policy.Category = cpv1alpha1.ClusterNetworkPolicy
		} else {
			policy.Category = cpv1alpha1.K8sNetworkPolicy
		}
		policyStats, exists := policyStatsMap[policy]
		if !exists {
			policyStats = new(metricsv1alpha1.TrafficStats)
			policyStatsMap[policy] = policyStats
		}
		policyStats.Bytes += int64(ruleStats.Bytes)
		policyStats.Sessions += int64(ruleStats.Sessions)
		policyStats.Packets += int64(ruleStats.Packets)
	}
	return policyStatsMap
}

// report calculates the delta of the stats and pushes it to the antrea-controller summary API.
func (m *Collector) report(curStatsMap map[cpv1alpha1.NetworkPolicyReference]*metricsv1alpha1.TrafficStats) error {
	metrics := calculateDiff(curStatsMap, m.lastStatsMap)
	if len(metrics) == 0 {
		klog.V(4).Info("No metrics to report, skip reporting")
		return nil
	}

	summary := &cpv1alpha1.NodeStatsSummary{
		ObjectMeta: metav1.ObjectMeta{
			Name: m.nodeName,
		},
		NetworkPolicies: metrics,
	}
	klog.V(6).Infof("Reporting NodeStatsSummary: %v", summary)
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(summary); err != nil {
		return err
	}

	antreaClient, err := m.antreaClientProvider.GetAntreaClient()
	if err != nil {
		return err
	}
	_, err = antreaClient.Discovery().RESTClient().Post().Body(buf.Bytes()).RequestURI(apis.StatsNodeSummary).Timeout(10 * time.Second).DoRaw(context.TODO())
	if err != nil {
		return err
	}
	return nil
}

func calculateDiff(curStatsMap, lastStatsMap map[cpv1alpha1.NetworkPolicyReference]*metricsv1alpha1.TrafficStats) []cpv1alpha1.NetworkPolicyStats {
	if len(curStatsMap) == 0 {
		return nil
	}
	metrics := make([]cpv1alpha1.NetworkPolicyStats, 0, len(curStatsMap))
	for policy, curStats := range curStatsMap {
		var incStats *metricsv1alpha1.TrafficStats
		lastStats, exists := lastStatsMap[policy]
		// curStats.Bytes < lastStats.Bytes could happen if one of the following conditions happens:
		// 1. OVS is restarted and Openflow entries are reinstalled.
		// 2. The NetworkPolicy is removed and recreated in-between two collection.
		// In these cases, curStats is the delta it should report.
		if !exists || curStats.Bytes < lastStats.Bytes {
			incStats = curStats
		} else {
			incStats = &metricsv1alpha1.TrafficStats{
				Packets:  curStats.Packets - lastStats.Packets,
				Sessions: curStats.Sessions - lastStats.Sessions,
				Bytes:    curStats.Bytes - lastStats.Bytes,
			}
		}
		// If the statistics of the NetworkPolicy remain unchanged, no need to report it.
		if incStats.Bytes == 0 {
			continue
		}
		metric := cpv1alpha1.NetworkPolicyStats{
			NetworkPolicy: policy,
			TrafficStats:  *incStats,
		}
		metrics = append(metrics, metric)
	}
	return metrics
}
