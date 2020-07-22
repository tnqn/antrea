package stats

import (
	"sync"

	"k8s.io/klog"

	"github.com/vmware-tanzu/antrea/pkg/apis/controlplane/v1alpha1"
)

type Aggregator struct {
	networkPolicyStats map[v1alpha1.NetworkPolicyReference]*v1alpha1.NetworkPolicyStats
	metricsLock sync.RWMutex
	dataCh chan []v1alpha1.NetworkPolicyStats
}

func NewAggregator() *Aggregator {
	manager := &Aggregator{
		networkPolicyStats: make(map[v1alpha1.NetworkPolicyReference]*v1alpha1.NetworkPolicyStats),
		dataCh: make(chan []v1alpha1.NetworkPolicyStats, 1000),
	}
	return manager
}

func (m *Aggregator) List() []*v1alpha1.NetworkPolicyStats {
	m.metricsLock.RLock()
	defer m.metricsLock.RUnlock()

	items := make([]*v1alpha1.NetworkPolicyStats, 0, len(m.networkPolicyStats))
	for _, stats := range m.networkPolicyStats {
		items = append(items, stats)
	}
	return items
}

func (m *Aggregator) Collect(metrics []v1alpha1.NetworkPolicyStats) {
	m.dataCh <- metrics
}

func (m *Aggregator) Run(stopCh <-chan struct{}) {
	klog.Info("Start collecting stats")
	for {
		select {
		case metrics := <-m.dataCh:
			m.doCollect(metrics)
		case <-stopCh:
			return
		}
	}
}

func (m *Aggregator) doCollect(statsList []v1alpha1.NetworkPolicyStats) {
	m.metricsLock.Lock()
	defer m.metricsLock.Unlock()

	for _, stats := range statsList {
		curStats, exists := m.networkPolicyStats[stats.NetworkPolicy]
		if !exists {
			curStats = &v1alpha1.NetworkPolicyStats{NetworkPolicy: stats.NetworkPolicy}
			m.networkPolicyStats[stats.NetworkPolicy] = curStats
		}
		curStats.RuleStats.Packets += stats.RuleStats.Packets
		curStats.RuleStats.Sessions += stats.RuleStats.Sessions
		curStats.RuleStats.Bytes += stats.RuleStats.Bytes

		klog.Infof("Current stats: %v", curStats)
	}
}
