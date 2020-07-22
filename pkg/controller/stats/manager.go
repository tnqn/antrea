package stats

import (
	"sync"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"

	"github.com/vmware-tanzu/antrea/pkg/apis/metrics/v1alpha1"
)

type MetricCollector interface {
	Collect(metrics []v1alpha1.ClusterNetworkPolicyMetric)
}

type MetricProvider interface {
	List() []v1alpha1.ClusterNetworkPolicyMetric
}

type Manager struct {
	networkPolicyStats map[types.NamespacedName]*v1alpha1.NetworkPolicyStats
	metricsLock sync.RWMutex
	dataCh chan []v1alpha1.ClusterNetworkPolicyMetric
}

func NewManager() *Manager {
	manager := &Manager{
		networkPolicyStats: make(map[types.NamespacedName]*v1alpha1.NetworkPolicyStats),
		dataCh: make(chan []v1alpha1.ClusterNetworkPolicyMetric, 1000),
	}
	return manager
}

func (m *Manager) List() []v1alpha1.ClusterNetworkPolicyMetric {
	m.metricsLock.RLock()
	defer m.metricsLock.RUnlock()

	metrics := make([]v1alpha1.ClusterNetworkPolicyMetric, 0, len(m.networkPolicyStats))
	for nn, stats := range m.networkPolicyStats {
		metric := v1alpha1.ClusterNetworkPolicyMetric{}
		metric.Namespace = nn.Namespace
		metric.Name = nn.Name
		metric.Stats = *stats
		metrics = append(metrics, metric)
	}
	return metrics
}

func (m *Manager) Collect(metrics []v1alpha1.ClusterNetworkPolicyMetric) {
	m.dataCh <- metrics
}

func (m *Manager) Run(stopCh <-chan struct{}) {
	klog.Info("Start collecting metrics")
	for {
		select {
		case metrics := <-m.dataCh:
			m.doCollect(metrics)
		case <-stopCh:
			return
		}
	}
}

func (m *Manager) doCollect(metrics []v1alpha1.ClusterNetworkPolicyMetric) {
	m.metricsLock.Lock()
	defer m.metricsLock.Unlock()

	for _, incMetric := range metrics {
		nn := types.NamespacedName{
			Namespace: incMetric.Namespace,
			Name:      incMetric.Name,
		}
		metric, exists := m.networkPolicyStats[nn]
		if !exists {
			metric = &v1alpha1.NetworkPolicyStats{}
			m.networkPolicyStats[nn] = metric
		}
		metric.Packets += incMetric.Stats.Packets
		metric.Sessions += incMetric.Stats.Sessions
		metric.Bytes += incMetric.Stats.Bytes

		klog.Infof("Current metrics: %v, %v", nn, metric)
	}
}
