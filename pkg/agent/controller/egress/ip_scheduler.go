// Copyright 2023 Antrea Authors
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

package egress

import (
	"sort"
	"sync"
	"sync/atomic"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	"antrea.io/antrea/pkg/agent/memberlist"
	crdv1a2 "antrea.io/antrea/pkg/apis/crd/v1alpha2"
	crdinformers "antrea.io/antrea/pkg/client/informers/externalversions/crd/v1alpha2"
	crdlisters "antrea.io/antrea/pkg/client/listers/crd/v1alpha2"
)

const (
	// workItem is the only item that will be enqueued, used to trigger Egress IP scheduling.
	workItem = "key"
)

// scheduleEventHandler is a callback when an Egress is rescheduled.
type scheduleEventHandler func(egress string)

// scheduleResult is the schedule result of an Egress, including the effective Egress IP and Node.
type scheduleResult struct {
	ip   string
	node string
}

// egressIPScheduler is responsible for scheduling Egress IPs to appropriate Nodes according to the Node selector of the
// IP pool, taking Node's capacity into consideration.
type egressIPScheduler struct {
	// cluster is responsible for selecting a Node for a given IP and pool.
	cluster memberlist.Interface

	egressLister       crdlisters.EgressLister
	egressListerSynced cache.InformerSynced

	// queue is used to trigger scheduling. Triggering multiple times before the item is consumed will only cause one
	// execution of scheduling.
	queue workqueue.Interface

	// mutex is used to protect scheduleResults.
	mutex           sync.RWMutex
	scheduleResults map[string]*scheduleResult
	// scheduledOnce indicates whether scheduling has been executed at lease once.
	scheduledOnce *atomic.Bool

	// eventHandlers is the registered callbacks.
	eventHandlers []scheduleEventHandler

	// The maximum number of Egress IPs a Node can accommodate.
	maxEgressIPsPerNode int
}

func NewEgressIPScheduler(cluster memberlist.Interface, egressInformer crdinformers.EgressInformer, maxEgressIPsPerNode int) *egressIPScheduler {
	s := &egressIPScheduler{
		cluster:             cluster,
		egressLister:        egressInformer.Lister(),
		egressListerSynced:  egressInformer.Informer().HasSynced,
		scheduleResults:     map[string]*scheduleResult{},
		scheduledOnce:       &atomic.Bool{},
		maxEgressIPsPerNode: maxEgressIPsPerNode,
		queue:               workqueue.New(),
	}
	egressInformer.Informer().AddEventHandlerWithResyncPeriod(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    s.addEgress,
			UpdateFunc: s.updateEgress,
			DeleteFunc: s.deleteEgress,
		},
		resyncPeriod,
	)

	s.cluster.AddClusterEventHandler(func(poolName string) {
		// Trigger scheduling regardless of which pool is changed.
		s.queue.Add(workItem)
	})
	return s
}

// addEgress processes Egress ADD events.
func (s *egressIPScheduler) addEgress(obj interface{}) {
	egress := obj.(*crdv1a2.Egress)
	if !isEgressSchedulable(egress) {
		return
	}
	s.queue.Add(workItem)
	klog.V(2).InfoS("Egress ADD event triggered Egress IP scheduling", "egress", klog.KObj(egress))
}

// updateEgress processes Egress UPDATE events.
func (s *egressIPScheduler) updateEgress(old, cur interface{}) {
	oldEgress := old.(*crdv1a2.Egress)
	curEgress := cur.(*crdv1a2.Egress)
	if !isEgressSchedulable(oldEgress) && !isEgressSchedulable(curEgress) {
		return
	}
	if oldEgress.Spec.EgressIP == curEgress.Spec.EgressIP && oldEgress.Spec.ExternalIPPool == curEgress.Spec.ExternalIPPool {
		return
	}
	s.queue.Add(workItem)
	klog.V(2).InfoS("Egress UPDATE event triggered Egress IP scheduling", "egress", klog.KObj(curEgress))
}

// deleteEgress processes Egress DELETE events.
func (s *egressIPScheduler) deleteEgress(obj interface{}) {
	egress, ok := obj.(*crdv1a2.Egress)
	if !ok {
		deletedState, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			klog.Errorf("Received unexpected object: %v", obj)
			return
		}
		egress, ok = deletedState.Obj.(*crdv1a2.Egress)
		if !ok {
			klog.Errorf("DeletedFinalStateUnknown contains non-Egress object: %v", deletedState.Obj)
			return
		}
	}
	if !isEgressSchedulable(egress) {
		return
	}
	s.queue.Add(workItem)
	klog.V(2).InfoS("Egress DELETE event triggered Egress IP scheduling", "egress", klog.KObj(egress))
}

func (s *egressIPScheduler) Run(stopCh <-chan struct{}) {
	klog.InfoS("Starting Egress IP scheduler")
	defer klog.InfoS("Shutting down Egress IP scheduler")
	defer s.queue.ShutDown()

	if !cache.WaitForCacheSync(stopCh, s.egressListerSynced) {
		return
	}

	// Schedule at least once even if there is no Egress to unblock clients waiting for HasScheduled to return true.
	s.queue.Add(workItem)

	go func() {
		for {
			obj, quit := s.queue.Get()
			if quit {
				return
			}
			s.schedule()
			s.queue.Done(obj)
		}
	}()

	<-stopCh
}

func (s *egressIPScheduler) HasScheduled() bool {
	return s.scheduledOnce.Load()
}

func (s *egressIPScheduler) AddEventHandler(handler scheduleEventHandler) {
	s.eventHandlers = append(s.eventHandlers, handler)
}

func (s *egressIPScheduler) GetEgressIPAndNode(egress string) (string, string, bool) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	result, exists := s.scheduleResults[egress]
	if !exists {
		return "", "", false
	}
	return result.ip, result.node, true
}

// EgressesByCreationTimestamp sorts a list of Egresses by creation timestamp.
type EgressesByCreationTimestamp []*crdv1a2.Egress

func (o EgressesByCreationTimestamp) Len() int      { return len(o) }
func (o EgressesByCreationTimestamp) Swap(i, j int) { o[i], o[j] = o[j], o[i] }
func (o EgressesByCreationTimestamp) Less(i, j int) bool {
	if o[i].CreationTimestamp.Equal(&o[j].CreationTimestamp) {
		return o[i].Name < o[j].Name
	}
	return o[i].CreationTimestamp.Before(&o[j].CreationTimestamp)
}

// schedule takes the spec of Egress and ExternalIPPool and the state of memberlist cluster as inputs, generates
// scheduling results deterministically. When every Node's capacity is sufficient, each Egress's schedule is independent
// and is only determined by the consistent hash map. When any Node's capacity is insufficient, one Egress's schedule
// may be affected by Egresses created before it. It will be triggerred when any schedulable Egress changes or the state
// of memberlist cluster changes, and will notify Egress schedule event subscribers of Egresses that are rescheduled.
//
// Note that it's possible that different agents decide different IP - Node assignment because their caches of Egress or
// the states of memberlist cluster are inconsistent at a moment. But all agents should get the same schedule results
// and correct IP assignment when their caches converge.
func (s *egressIPScheduler) schedule() {
	var egressesToUpdate []string
	newResults := map[string]*scheduleResult{}
	nodeToIPs := map[string]sets.String{}
	egresses, _ := s.egressLister.List(labels.Everything())
	// Sort Egresses by creation timestamp to make the result deterministic and prioritize objected created earlier
	// when the total capacity is insufficient.
	sort.Sort(EgressesByCreationTimestamp(egresses))
	for _, egress := range egresses {
		// Ignore Egresses that shouldn't be scheduled.
		if !isEgressSchedulable(egress) {
			continue
		}

		maxEgressIPsFilter := func(node string) bool {
			// Count the Egress IPs that are already assigned to this Node.
			ipsOnNode, _ := nodeToIPs[node]
			numIPs := ipsOnNode.Len()
			// Check if this Node can accommodate the new Egress IP.
			if !ipsOnNode.Has(egress.Spec.EgressIP) {
				numIPs += 1
			}
			return numIPs <= s.maxEgressIPsPerNode
		}
		node, err := s.cluster.SelectNodeForIP(egress.Spec.EgressIP, egress.Spec.ExternalIPPool, maxEgressIPsFilter)
		if err != nil {
			if err == memberlist.ErrNoNodeAvailable {
				klog.InfoS("No Node is eligible for Egress", "egress", klog.KObj(egress))
			} else {
				klog.ErrorS(err, "Failed to select Node for Egress", "egress", klog.KObj(egress))
			}
			continue
		}
		result := &scheduleResult{
			ip:   egress.Spec.EgressIP,
			node: node,
		}
		newResults[egress.Name] = result

		ips, exists := nodeToIPs[node]
		if !exists {
			ips = sets.NewString()
			nodeToIPs[node] = ips
		}
		ips.Insert(egress.Spec.EgressIP)
	}

	func() {
		s.mutex.Lock()
		defer s.mutex.Unlock()

		// Identify Egresses whose schedule results are updated.
		prevResults := s.scheduleResults
		for egress, result := range newResults {
			prevResult, exists := prevResults[egress]
			if !exists || prevResult.ip != result.ip || prevResult.node != result.node {
				egressesToUpdate = append(egressesToUpdate, egress)
			}
			delete(prevResults, egress)
		}
		for egress := range prevResults {
			egressesToUpdate = append(egressesToUpdate, egress)
		}

		// Record the new results.
		s.scheduleResults = newResults
	}()

	for _, egress := range egressesToUpdate {
		for _, handler := range s.eventHandlers {
			handler(egress)
		}
	}

	s.scheduledOnce.Store(true)
}
