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
	"time"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	networkinginformers "k8s.io/client-go/informers/networking/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"

	"github.com/vmware-tanzu/antrea/pkg/apis/controlplane"
	//cpv1alpha1 "github.com/vmware-tanzu/antrea/pkg/apis/controlplane/v1alpha1"
	metricsv1alpha1 "github.com/vmware-tanzu/antrea/pkg/apis/metrics/v1alpha1"
	secv1alpha1 "github.com/vmware-tanzu/antrea/pkg/apis/security/v1alpha1"
	secvinformers "github.com/vmware-tanzu/antrea/pkg/client/informers/externalversions/security/v1alpha1"
	"github.com/vmware-tanzu/antrea/pkg/features"
	"github.com/vmware-tanzu/antrea/pkg/k8s"
)

// Aggregator collects the metrics from the antrea-agents, aggregates them, caches the result, and provides interfaces
// for Metrics API handlers to query them. It implements the following interfaces:
// - pkg/apiserver/registry/controlplane/nodestatssummary.statsCollector
// - pkg/apiserver/registry/metrics/networkpolicymetrics.metricsProvider
// - pkg/apiserver/registry/metrics/clusternetworkpolicymetrics.metricsProvider
type Aggregator struct {
	// networkPolicyMetrics caches the metrics of K8s NetworkPolicies collected from the antrea-agents.
	networkPolicyMetrics cache.Indexer
	// clusterNetworkPolicyMetrics caches the metrics of Antrea ClusterNetworkPolicies collected from the antrea-agents.
	clusterNetworkPolicyMetrics cache.Indexer
	// dataCh is the channel that buffers the NodeSummaries sent by antrea-agents.
	dataCh chan *controlplane.NodeStatsSummary
	// npListerSynced is a function which returns true if the K8s NetworkPolicy shared informer has been synced at least once.
	npListerSynced cache.InformerSynced
	// cnpListerSynced is a function which returns true if the Antrea ClusterNetworkPolicy shared informer has been synced at least once.
	cnpListerSynced cache.InformerSynced
}

func NewAggregator(networkPolicyInformer networkinginformers.NetworkPolicyInformer, cnpInformer secvinformers.ClusterNetworkPolicyInformer) *Aggregator {
	networkPolicyMetrics := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	clusterNetworkPolicyMetrics := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	aggregator := &Aggregator{
		networkPolicyMetrics:        networkPolicyMetrics,
		clusterNetworkPolicyMetrics: clusterNetworkPolicyMetrics,
		dataCh:                      make(chan *controlplane.NodeStatsSummary, 1000),
		npListerSynced:              networkPolicyInformer.Informer().HasSynced,
	}
	// Add handlers for NetworkPolicy events.
	// They are the source of truth of the NetworkPolicyMetrics, i.e., a NetworkPolicyMetrics is present only if the
	// corresponding NetworkPolicy is present.
	networkPolicyInformer.Informer().AddEventHandlerWithResyncPeriod(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    aggregator.addNetworkPolicy,
			DeleteFunc: aggregator.deleteNetworkPolicy,
		},
		// Set resyncPeriod to 0 to disable resyncing.
		0,
	)
	// Register Informer and add handlers for AntreaPolicy events only if the feature is enabled.
	// They are the source of truth of the ClusterNetworkPolicyMetrics, i.e., a ClusterNetworkPolicyMetrics is present
	// only if the corresponding ClusterNetworkPolicy is present.
	if features.DefaultFeatureGate.Enabled(features.AntreaPolicy) {
		aggregator.cnpListerSynced = cnpInformer.Informer().HasSynced
		cnpInformer.Informer().AddEventHandlerWithResyncPeriod(
			cache.ResourceEventHandlerFuncs{
				AddFunc:    aggregator.addCNP,
				DeleteFunc: aggregator.deleteCNP,
			},
			// Set resyncPeriod to 0 to disable resyncing.
			0,
		)
	}
	return aggregator
}

// addNetworkPolicy handles NetworkPolicy ADD events and creates corresponding NetworkPolicyMetrics objects.
func (a *Aggregator) addNetworkPolicy(obj interface{}) {
	np := obj.(*networkingv1.NetworkPolicy)
	metrics := &metricsv1alpha1.NetworkPolicyMetrics{
		ObjectMeta: metav1.ObjectMeta{
			Name:      np.Name,
			Namespace: np.Namespace,
			// To indicate the duration that the metrics cover, the CreationTimestamp is set to the time that the stats
			// start, instead of the CreationTimestamp of the NetworkPolicy.
			CreationTimestamp: metav1.Time{Time: time.Now()},
		},
	}
	a.networkPolicyMetrics.Add(metrics)
}

// deleteNetworkPolicy handles NetworkPolicy DELETE events and deletes corresponding NetworkPolicyMetrics objects.
func (a *Aggregator) deleteNetworkPolicy(obj interface{}) {
	np, ok := obj.(*networkingv1.NetworkPolicy)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			klog.Errorf("Error decoding object when deleting NetworkPolicy, invalid type: %v", obj)
			return
		}
		np, ok = tombstone.Obj.(*networkingv1.NetworkPolicy)
		if !ok {
			klog.Errorf("Error decoding object tombstone when deleting NetworkPolicy, invalid type: %v", tombstone.Obj)
			return
		}
	}
	metrics := &metricsv1alpha1.NetworkPolicyMetrics{
		ObjectMeta: metav1.ObjectMeta{
			Name:      np.Name,
			Namespace: np.Namespace,
		},
	}
	a.networkPolicyMetrics.Delete(metrics)
}

// addCNP handles ClusterNetworkPolicy ADD events and creates corresponding ClusterNetworkPolicyMetrics objects.
func (a *Aggregator) addCNP(obj interface{}) {
	cnp := obj.(*secv1alpha1.ClusterNetworkPolicy)
	metrics := &metricsv1alpha1.ClusterNetworkPolicyMetrics{
		ObjectMeta: metav1.ObjectMeta{
			Name: cnp.Name,
			// To indicate the duration that the metrics covers, the CreationTimestamp is set to the time that the stats
			// start, instead of the CreationTimestamp of the ClusterNetworkPolicy.
			CreationTimestamp: metav1.Time{Time: time.Now()},
		},
	}
	a.clusterNetworkPolicyMetrics.Add(metrics)
}

// deleteCNP handles ClusterNetworkPolicy DELETE events and deletes corresponding ClusterNetworkPolicyMetrics objects.
func (a *Aggregator) deleteCNP(obj interface{}) {
	cnp, ok := obj.(*secv1alpha1.ClusterNetworkPolicy)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			klog.Errorf("Error decoding object when deleting ClusterNetworkPolicy, invalid type: %v", obj)
			return
		}
		cnp, ok = tombstone.Obj.(*secv1alpha1.ClusterNetworkPolicy)
		if !ok {
			klog.Errorf("Error decoding object tombstone when deleting ClusterNetworkPolicy, invalid type: %v", tombstone.Obj)
			return
		}
	}
	metrics := &metricsv1alpha1.ClusterNetworkPolicyMetrics{
		ObjectMeta: metav1.ObjectMeta{
			Name: cnp.Name,
		},
	}
	a.clusterNetworkPolicyMetrics.Delete(metrics)
}

func (a *Aggregator) ListClusterNetworkPolicyMetrics() []metricsv1alpha1.ClusterNetworkPolicyMetrics {
	objs := a.clusterNetworkPolicyMetrics.List()
	metrics := make([]metricsv1alpha1.ClusterNetworkPolicyMetrics, len(objs))
	for i, obj := range objs {
		metrics[i] = *(obj.(*metricsv1alpha1.ClusterNetworkPolicyMetrics))
	}
	return metrics
}

func (a *Aggregator) GetClusterNetworkPolicyMetric(name string) (*metricsv1alpha1.ClusterNetworkPolicyMetrics, bool) {
	obj, exists, _ := a.clusterNetworkPolicyMetrics.GetByKey(name)
	if !exists {
		return nil, false
	}
	return obj.(*metricsv1alpha1.ClusterNetworkPolicyMetrics), true
}

func (a *Aggregator) ListNetworkPolicyMetrics(namespace string) []metricsv1alpha1.NetworkPolicyMetrics {
	var objs []interface{}
	if namespace == "" {
		objs = a.networkPolicyMetrics.List()
	} else {
		objs, _ = a.networkPolicyMetrics.ByIndex(cache.NamespaceIndex, namespace)
	}

	metrics := make([]metricsv1alpha1.NetworkPolicyMetrics, len(objs))
	for i, obj := range objs {
		metrics[i] = *(obj.(*metricsv1alpha1.NetworkPolicyMetrics))
	}
	return metrics
}

func (a *Aggregator) GetNetworkPolicyMetric(namespace, name string) (*metricsv1alpha1.NetworkPolicyMetrics, bool) {
	obj, exists, _ := a.networkPolicyMetrics.GetByKey(k8s.NamespacedName(namespace, name))
	if !exists {
		return nil, false
	}
	return obj.(*metricsv1alpha1.NetworkPolicyMetrics), true
}

// Collect collects the node summary asynchronously to avoid the competition for the metricsLock and to save clients
// from pending on it.
func (a *Aggregator) Collect(summary *controlplane.NodeStatsSummary) {
	a.dataCh <- summary
}

// Run runs a loop that keeps taking metrics summary from the data channel and actually collecting them until the
// provided stop channel is closed.
func (a *Aggregator) Run(stopCh <-chan struct{}) {
	klog.Info("Starting stats aggregator")
	defer klog.Info("Shutting down stats aggregator")

	klog.Info("Waiting for caches to sync for stats aggregator")
	if !cache.WaitForCacheSync(stopCh, a.npListerSynced) {
		klog.Error("Unable to sync caches for stats aggregator")
		return
	}
	if features.DefaultFeatureGate.Enabled(features.AntreaPolicy) {
		if !cache.WaitForCacheSync(stopCh, a.cnpListerSynced) {
			klog.Error("Unable to sync CNP caches for stats aggregator")
			return
		}
	}
	klog.Info("Caches are synced for stats aggregator")

	for {
		select {
		case nodeSummary := <-a.dataCh:
			a.doCollect(nodeSummary)
		case <-stopCh:
			return
		}
	}
}

func (a *Aggregator) doCollect(summary *controlplane.NodeStatsSummary) {
	for _, stats := range summary.NetworkPolicies {
		switch stats.NetworkPolicy.Category {
		case controlplane.K8sNetworkPolicy:
			obj, exists, _ := a.networkPolicyMetrics.GetByKey(k8s.NamespacedName(stats.NetworkPolicy.Namespace, stats.NetworkPolicy.Name))
			// The policy has been removed, skip processing its metrics.
			if !exists {
				continue
			}
			curMetric := obj.(*metricsv1alpha1.NetworkPolicyMetrics)
			// The object returned by cache is supposed to be read only, create a new object and update it.
			newMetric := curMetric.DeepCopy()
			addUp(&newMetric.TrafficStats, &stats.TrafficStats)
			a.networkPolicyMetrics.Update(newMetric)
		case controlplane.ClusterNetworkPolicy:
			obj, exists, _ := a.clusterNetworkPolicyMetrics.GetByKey(stats.NetworkPolicy.Name)
			if !exists {
				continue
			}
			curMetric := obj.(*metricsv1alpha1.ClusterNetworkPolicyMetrics)
			newMetric := curMetric.DeepCopy()
			addUp(&newMetric.TrafficStats, &stats.TrafficStats)
			a.clusterNetworkPolicyMetrics.Update(newMetric)
		}
	}
}

func addUp(target *metricsv1alpha1.TrafficStats, src *metricsv1alpha1.TrafficStats) {
	target.Sessions += src.Sessions
	target.Packets += src.Packets
	target.Bytes += src.Bytes
}
