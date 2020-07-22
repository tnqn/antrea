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
	"fmt"
	"time"

	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/meta"
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

const (
	uidIndex = "uid"
)

// Aggregator collects the metrics from the antrea-agents, aggregates them, caches the result, and provides interfaces
// for Metrics API handlers to query them. It implements the following interfaces:
// - pkg/apiserver/registry/controlplane/nodestatssummary.statsCollector
// - pkg/apiserver/registry/metrics/networkpolicymetrics.metricsProvider
// - pkg/apiserver/registry/metrics/antreaclusternetworkpolicymetrics.metricsProvider
// - pkg/apiserver/registry/metrics/antreanetworkpolicymetrics.metricsProvider
type Aggregator struct {
	// networkPolicyMetrics caches the metrics of K8s NetworkPolicies collected from the antrea-agents.
	networkPolicyMetrics cache.Indexer
	// antreaClusterNetworkPolicyMetrics caches the metrics of Antrea ClusterNetworkPolicies collected from the antrea-agents.
	antreaClusterNetworkPolicyMetrics cache.Indexer
	// antreaNetworkPolicyMetrics caches the metrics of Antrea NetworkPolicies collected from the antrea-agents.
	antreaNetworkPolicyMetrics cache.Indexer
	// dataCh is the channel that buffers the NodeSummaries sent by antrea-agents.
	dataCh chan *controlplane.NodeStatsSummary
	// npListerSynced is a function which returns true if the K8s NetworkPolicy shared informer has been synced at least once.
	npListerSynced cache.InformerSynced
	// cnpListerSynced is a function which returns true if the Antrea ClusterNetworkPolicy shared informer has been synced at least once.
	cnpListerSynced cache.InformerSynced
	// anpListerSynced is a function which returns true if the Antrea NetworkPolicy shared informer has been synced at least once.
	anpListerSynced cache.InformerSynced
}

// uidIndexFunc is an index function that indexes based on an object's UID.
func uidIndexFunc(obj interface{}) ([]string, error) {
	meta, err := meta.Accessor(obj)
	if err != nil {
		return []string{""}, fmt.Errorf("object has no meta: %v", err)
	}
	return []string{string(meta.GetUID())}, nil
}

func NewAggregator(networkPolicyInformer networkinginformers.NetworkPolicyInformer, cnpInformer secvinformers.ClusterNetworkPolicyInformer, anpInformer secvinformers.NetworkPolicyInformer) *Aggregator {
	aggregator := &Aggregator{
		networkPolicyMetrics: cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc, uidIndex: uidIndexFunc}),
		dataCh:               make(chan *controlplane.NodeStatsSummary, 1000),
		npListerSynced:       networkPolicyInformer.Informer().HasSynced,
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
		aggregator.antreaClusterNetworkPolicyMetrics = cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{uidIndex: uidIndexFunc})
		aggregator.cnpListerSynced = cnpInformer.Informer().HasSynced
		cnpInformer.Informer().AddEventHandlerWithResyncPeriod(
			cache.ResourceEventHandlerFuncs{
				AddFunc:    aggregator.addCNP,
				DeleteFunc: aggregator.deleteCNP,
			},
			// Set resyncPeriod to 0 to disable resyncing.
			0,
		)

		aggregator.antreaNetworkPolicyMetrics = cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc, uidIndex: uidIndexFunc})
		aggregator.anpListerSynced = anpInformer.Informer().HasSynced
		anpInformer.Informer().AddEventHandlerWithResyncPeriod(
			cache.ResourceEventHandlerFuncs{
				AddFunc:    aggregator.addANP,
				DeleteFunc: aggregator.deleteANP,
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
			UID:       np.UID,
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
			UID:       np.UID,
		},
	}
	a.networkPolicyMetrics.Delete(metrics)
}

// addCNP handles ClusterNetworkPolicy ADD events and creates corresponding ClusterNetworkPolicyMetrics objects.
func (a *Aggregator) addCNP(obj interface{}) {
	cnp := obj.(*secv1alpha1.ClusterNetworkPolicy)
	metrics := &metricsv1alpha1.AntreaClusterNetworkPolicyMetrics{
		ObjectMeta: metav1.ObjectMeta{
			Name: cnp.Name,
			UID:  cnp.UID,
			// To indicate the duration that the metrics covers, the CreationTimestamp is set to the time that the stats
			// start, instead of the CreationTimestamp of the ClusterNetworkPolicy.
			CreationTimestamp: metav1.Time{Time: time.Now()},
		},
	}
	a.antreaClusterNetworkPolicyMetrics.Add(metrics)
}

// deleteCNP handles ClusterNetworkPolicy DELETE events and deletes corresponding ClusterNetworkPolicyMetrics objects.
func (a *Aggregator) deleteCNP(obj interface{}) {
	cnp, ok := obj.(*secv1alpha1.ClusterNetworkPolicy)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			klog.Errorf("Error decoding object when deleting Antrea ClusterNetworkPolicy, invalid type: %v", obj)
			return
		}
		cnp, ok = tombstone.Obj.(*secv1alpha1.ClusterNetworkPolicy)
		if !ok {
			klog.Errorf("Error decoding object tombstone when deleting Antrea ClusterNetworkPolicy, invalid type: %v", tombstone.Obj)
			return
		}
	}
	metrics := &metricsv1alpha1.AntreaClusterNetworkPolicyMetrics{
		ObjectMeta: metav1.ObjectMeta{
			Name: cnp.Name,
			UID:  cnp.UID,
		},
	}
	a.antreaClusterNetworkPolicyMetrics.Delete(metrics)
}

// addANP handles Antrea NetworkPolicy ADD events and creates corresponding AntreaNetworkPolicyMetrics objects.
func (a *Aggregator) addANP(obj interface{}) {
	anp := obj.(*secv1alpha1.NetworkPolicy)
	metrics := &metricsv1alpha1.AntreaNetworkPolicyMetrics{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: anp.Namespace,
			Name:      anp.Name,
			UID:       anp.UID,
			// To indicate the duration that the metrics covers, the CreationTimestamp is set to the time that the stats
			// start, instead of the CreationTimestamp of the Antrea NetworkPolicy.
			CreationTimestamp: metav1.Time{Time: time.Now()},
		},
	}
	a.antreaNetworkPolicyMetrics.Add(metrics)
}

// deleteANP handles Antrea NetworkPolicy DELETE events and deletes corresponding AntreaNetworkPolicyMetrics objects.
func (a *Aggregator) deleteANP(obj interface{}) {
	anp, ok := obj.(*secv1alpha1.NetworkPolicy)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			klog.Errorf("Error decoding object when deleting Antrea NetworkPolicy, invalid type: %v", obj)
			return
		}
		anp, ok = tombstone.Obj.(*secv1alpha1.NetworkPolicy)
		if !ok {
			klog.Errorf("Error decoding object tombstone when deleting Antrea NetworkPolicy, invalid type: %v", tombstone.Obj)
			return
		}
	}
	metrics := &metricsv1alpha1.AntreaNetworkPolicyMetrics{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: anp.Namespace,
			Name:      anp.Name,
			UID:       anp.UID,
		},
	}
	a.antreaNetworkPolicyMetrics.Delete(metrics)
}

func (a *Aggregator) ListAntreaClusterNetworkPolicyMetrics() []metricsv1alpha1.AntreaClusterNetworkPolicyMetrics {
	objs := a.antreaClusterNetworkPolicyMetrics.List()
	metrics := make([]metricsv1alpha1.AntreaClusterNetworkPolicyMetrics, len(objs))
	for i, obj := range objs {
		metrics[i] = *(obj.(*metricsv1alpha1.AntreaClusterNetworkPolicyMetrics))
	}
	return metrics
}

func (a *Aggregator) GetAntreaClusterNetworkPolicyMetrics(name string) (*metricsv1alpha1.AntreaClusterNetworkPolicyMetrics, bool) {
	obj, exists, _ := a.antreaClusterNetworkPolicyMetrics.GetByKey(name)
	if !exists {
		return nil, false
	}
	return obj.(*metricsv1alpha1.AntreaClusterNetworkPolicyMetrics), true
}

func (a *Aggregator) ListAntreaNetworkPolicyMetrics(namespace string) []metricsv1alpha1.AntreaNetworkPolicyMetrics {
	var objs []interface{}
	if namespace == "" {
		objs = a.antreaNetworkPolicyMetrics.List()
	} else {
		objs, _ = a.antreaNetworkPolicyMetrics.ByIndex(cache.NamespaceIndex, namespace)
	}

	metrics := make([]metricsv1alpha1.AntreaNetworkPolicyMetrics, len(objs))
	for i, obj := range objs {
		metrics[i] = *(obj.(*metricsv1alpha1.AntreaNetworkPolicyMetrics))
	}
	return metrics
}

func (a *Aggregator) GetAntreaNetworkPolicyMetrics(namespace, name string) (*metricsv1alpha1.AntreaNetworkPolicyMetrics, bool) {
	obj, exists, _ := a.antreaNetworkPolicyMetrics.GetByKey(k8s.NamespacedName(namespace, name))
	if !exists {
		return nil, false
	}
	return obj.(*metricsv1alpha1.AntreaNetworkPolicyMetrics), true
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

func (a *Aggregator) GetNetworkPolicyMetrics(namespace, name string) (*metricsv1alpha1.NetworkPolicyMetrics, bool) {
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
		if !cache.WaitForCacheSync(stopCh, a.cnpListerSynced, a.anpListerSynced) {
			klog.Error("Unable to sync Antrea policy caches for stats aggregator")
			return
		}
	}
	klog.Info("Caches are synced for stats aggregator")

	for {
		select {
		case summary := <-a.dataCh:
			a.doCollect(summary)
		case <-stopCh:
			return
		}
	}
}

func (a *Aggregator) doCollect(summary *controlplane.NodeStatsSummary) {
	for _, stats := range summary.NetworkPolicies {
		// The policy might been removed, skip processing it if missing.
		objs, _ := a.networkPolicyMetrics.ByIndex(uidIndex, string(stats.NetworkPolicy.UID))
		if len(objs) > 0 {
			// The object returned by cache is supposed to be read only, create a new object and update it.
			metrics := objs[0].(*metricsv1alpha1.NetworkPolicyMetrics).DeepCopy()
			addUp(&metrics.TrafficStats, &stats.TrafficStats)
			a.networkPolicyMetrics.Update(metrics)
		}
	}
	if features.DefaultFeatureGate.Enabled(features.AntreaPolicy) {
		for _, stats := range summary.AntreaClusterNetworkPolicies {
			// The policy might been removed, skip processing it if missing.
			objs, _ := a.antreaClusterNetworkPolicyMetrics.ByIndex(uidIndex, string(stats.NetworkPolicy.UID))
			if len(objs) > 0 {
				// The object returned by cache is supposed to be read only, create a new object and update it.
				metrics := objs[0].(*metricsv1alpha1.AntreaClusterNetworkPolicyMetrics).DeepCopy()
				addUp(&metrics.TrafficStats, &stats.TrafficStats)
				a.antreaClusterNetworkPolicyMetrics.Update(metrics)
			}
		}
		for _, stats := range summary.AntreaNetworkPolicies {
			// The policy might been removed, skip processing it if missing.
			objs, _ := a.antreaNetworkPolicyMetrics.ByIndex(uidIndex, string(stats.NetworkPolicy.UID))
			if len(objs) > 0 {
				// The object returned by cache is supposed to be read only, create a new object and update it.
				metrics := objs[0].(*metricsv1alpha1.AntreaNetworkPolicyMetrics).DeepCopy()
				addUp(&metrics.TrafficStats, &stats.TrafficStats)
				a.antreaNetworkPolicyMetrics.Update(metrics)
			}
		}
	}
}

func addUp(stats *metricsv1alpha1.TrafficStats, inc *metricsv1alpha1.TrafficStats) {
	stats.Sessions += inc.Sessions
	stats.Packets += inc.Packets
	stats.Bytes += inc.Bytes
}
