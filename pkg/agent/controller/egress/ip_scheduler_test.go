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
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/sets"

	"antrea.io/antrea/pkg/agent/consistenthash"
	"antrea.io/antrea/pkg/agent/memberlist"
	crdv1a2 "antrea.io/antrea/pkg/apis/crd/v1alpha2"
	fakeversioned "antrea.io/antrea/pkg/client/clientset/versioned/fake"
	crdinformers "antrea.io/antrea/pkg/client/informers/externalversions"
)

type fakeMemberlistCluster struct {
	nodes         []string
	hashMap       *consistenthash.Map
	eventHandlers []memberlist.ClusterNodeEventHandler
}

func newFakeMemberlistCluster(nodes []string) *fakeMemberlistCluster {
	hashMap := memberlist.NewNodeConsistentHashMap()
	hashMap.Add(nodes...)
	return &fakeMemberlistCluster{
		nodes:   nodes,
		hashMap: hashMap,
	}
}

func (f *fakeMemberlistCluster) updateNodes(nodes []string) {
	hashMap := memberlist.NewNodeConsistentHashMap()
	hashMap.Add(nodes...)
	f.hashMap = hashMap
	for _, h := range f.eventHandlers {
		h("dummy")
	}
}

func (f *fakeMemberlistCluster) AddClusterEventHandler(h memberlist.ClusterNodeEventHandler) {
	f.eventHandlers = append(f.eventHandlers, h)
}

func (f *fakeMemberlistCluster) AliveNodes() sets.String {
	return sets.NewString(f.nodes...)
}

func (f *fakeMemberlistCluster) SelectNodeForIP(ip, externalIPPool string, filters ...func(string) bool) (string, error) {
	node := f.hashMap.GetWithFilters(ip, filters...)
	if node == "" {
		return "", memberlist.ErrNoNodeAvailable
	}
	return node, nil
}

func (f *fakeMemberlistCluster) ShouldSelectIP(ip string, pool string, filters ...func(node string) bool) (bool, error) {
	return false, nil
}

func TestSchedule(t *testing.T) {
	egresses := []runtime.Object{
		&crdv1a2.Egress{
			ObjectMeta: metav1.ObjectMeta{Name: "egressA", UID: "uidA", CreationTimestamp: metav1.NewTime(time.Unix(1, 0))},
			Spec:       crdv1a2.EgressSpec{EgressIP: "1.1.1.1", ExternalIPPool: "pool1"},
		},
		&crdv1a2.Egress{
			ObjectMeta: metav1.ObjectMeta{Name: "egressB", UID: "uidB", CreationTimestamp: metav1.NewTime(time.Unix(2, 0))},
			Spec:       crdv1a2.EgressSpec{EgressIP: "1.1.1.11", ExternalIPPool: "pool1"},
		},
		&crdv1a2.Egress{
			ObjectMeta: metav1.ObjectMeta{Name: "egressC", UID: "uidC", CreationTimestamp: metav1.NewTime(time.Unix(3, 0))},
			Spec:       crdv1a2.EgressSpec{EgressIP: "1.1.1.21", ExternalIPPool: "pool1"},
		},
	}
	tests := []struct {
		name                string
		nodes               []string
		maxEgressIPsPerNode int
		expectedResults     map[string]*scheduleResult
	}{
		{
			name:                "sufficient capacity",
			nodes:               []string{"node1", "node2", "node3"},
			maxEgressIPsPerNode: 3,
			expectedResults: map[string]*scheduleResult{
				"egressA": {
					node: "node1",
					ip:   "1.1.1.1",
				},
				"egressB": {
					node: "node3",
					ip:   "1.1.1.11",
				},
				"egressC": {
					node: "node1",
					ip:   "1.1.1.21",
				},
			},
		},
		{
			name:                "insufficient node capacity",
			nodes:               []string{"node1", "node2", "node3"},
			maxEgressIPsPerNode: 1,
			// egressC was moved to node2 due to insufficient node capacity.
			expectedResults: map[string]*scheduleResult{
				"egressA": {
					node: "node1",
					ip:   "1.1.1.1",
				},
				"egressB": {
					node: "node3",
					ip:   "1.1.1.11",
				},
				"egressC": {
					node: "node2",
					ip:   "1.1.1.21",
				},
			},
		},
		{
			name:                "insufficient cluster capacity",
			nodes:               []string{"node1", "node3"},
			maxEgressIPsPerNode: 1,
			// egressC was not scheduled to any Node due to insufficient node capacity.
			expectedResults: map[string]*scheduleResult{
				"egressA": {
					node: "node1",
					ip:   "1.1.1.1",
				},
				"egressB": {
					node: "node3",
					ip:   "1.1.1.11",
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeCluster := newFakeMemberlistCluster(tt.nodes)
			crdClient := fakeversioned.NewSimpleClientset(egresses...)
			crdInformerFactory := crdinformers.NewSharedInformerFactory(crdClient, 0)
			egressInformer := crdInformerFactory.Crd().V1alpha2().Egresses()

			s := NewEgressIPScheduler(fakeCluster, egressInformer, tt.maxEgressIPsPerNode)

			stopCh := make(chan struct{})
			defer close(stopCh)
			crdInformerFactory.Start(stopCh)
			crdInformerFactory.WaitForCacheSync(stopCh)

			s.schedule()
			assert.Equal(t, tt.expectedResults, s.scheduleResults)
		})
	}
}

func BenchmarkSchedule(b *testing.B) {
	var egresses []runtime.Object
	for i := 0; i < 1000; i++ {
		egresses = append(egresses, &crdv1a2.Egress{
			ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("egress-%d", i), UID: types.UID(fmt.Sprintf("uid-%d", i)), CreationTimestamp: metav1.NewTime(time.Unix(int64(i), 0))},
			Spec:       crdv1a2.EgressSpec{EgressIP: fmt.Sprintf("1.1.%d.%d", rand.Intn(256), rand.Intn(256)), ExternalIPPool: "pool1"},
		})
	}
	var nodes []string
	for i := 0; i < 1000; i++ {
		nodes = append(nodes, fmt.Sprintf("node-%d", i))
	}
	fakeCluster := newFakeMemberlistCluster(nodes)
	crdClient := fakeversioned.NewSimpleClientset(egresses...)
	crdInformerFactory := crdinformers.NewSharedInformerFactory(crdClient, 0)
	egressInformer := crdInformerFactory.Crd().V1alpha2().Egresses()

	s := NewEgressIPScheduler(fakeCluster, egressInformer, 10)
	stopCh := make(chan struct{})
	defer close(stopCh)
	crdInformerFactory.Start(stopCh)
	crdInformerFactory.WaitForCacheSync(stopCh)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		s.schedule()
	}
}

func TestRun(t *testing.T) {
	ctx := context.Background()
	egresses := []runtime.Object{
		&crdv1a2.Egress{
			ObjectMeta: metav1.ObjectMeta{Name: "egressA", UID: "uidA", CreationTimestamp: metav1.NewTime(time.Unix(1, 0))},
			Spec:       crdv1a2.EgressSpec{EgressIP: "1.1.1.1", ExternalIPPool: "pool1"},
		},
		&crdv1a2.Egress{
			ObjectMeta: metav1.ObjectMeta{Name: "egressB", UID: "uidB", CreationTimestamp: metav1.NewTime(time.Unix(2, 0))},
			Spec:       crdv1a2.EgressSpec{EgressIP: "1.1.1.11", ExternalIPPool: "pool1"},
		},
		&crdv1a2.Egress{
			ObjectMeta: metav1.ObjectMeta{Name: "egressC", UID: "uidC", CreationTimestamp: metav1.NewTime(time.Unix(3, 0))},
			Spec:       crdv1a2.EgressSpec{EgressIP: "1.1.1.21", ExternalIPPool: "pool1"},
		},
	}
	fakeCluster := newFakeMemberlistCluster([]string{"node1", "node2"})
	crdClient := fakeversioned.NewSimpleClientset(egresses...)
	crdInformerFactory := crdinformers.NewSharedInformerFactory(crdClient, 0)
	egressInformer := crdInformerFactory.Crd().V1alpha2().Egresses()

	s := NewEgressIPScheduler(fakeCluster, egressInformer, 2)
	egressUpdates := make(chan string, 10)
	s.AddEventHandler(func(egress string) {
		egressUpdates <- egress
	})
	stopCh := make(chan struct{})
	defer close(stopCh)
	crdInformerFactory.Start(stopCh)
	crdInformerFactory.WaitForCacheSync(stopCh)

	go s.Run(stopCh)

	// The original distribution when the total capacity is sufficient.
	assertReceivedItems(t, egressUpdates, sets.NewString("egressA", "egressB", "egressC"))
	assertScheduleResult(t, s, "egressA", "1.1.1.1", "node1", true)
	assertScheduleResult(t, s, "egressB", "1.1.1.11", "node2", true)
	assertScheduleResult(t, s, "egressC", "1.1.1.21", "node1", true)

	// After egressA is updated, it should be moved to node2 determined by its consistent hash result.
	patch := map[string]interface{}{
		"spec": map[string]string{
			"egressIP": "1.1.1.5",
		},
	}
	patchBytes, _ := json.Marshal(patch)
	crdClient.CrdV1alpha2().Egresses().Patch(context.TODO(), "egressA", types.MergePatchType, patchBytes, metav1.PatchOptions{})
	assertReceivedItems(t, egressUpdates, sets.NewString("egressA"))
	assertScheduleResult(t, s, "egressA", "1.1.1.5", "node2", true)
	assertScheduleResult(t, s, "egressB", "1.1.1.11", "node2", true)
	assertScheduleResult(t, s, "egressC", "1.1.1.21", "node1", true)

	// After node2 leaves, egress A and egressB should be moved to node1 as they were created earlier than egressC.
	// egressC should be left unassigned.
	fakeCluster.updateNodes([]string{"node1"})
	assertReceivedItems(t, egressUpdates, sets.NewString("egressA", "egressB", "egressC"))
	assertScheduleResult(t, s, "egressA", "1.1.1.5", "node1", true)
	assertScheduleResult(t, s, "egressB", "1.1.1.11", "node1", true)
	assertScheduleResult(t, s, "egressC", "", "", false)

	// After egressA is deleted, egressC should be assigned to node1.
	crdClient.CrdV1alpha2().Egresses().Delete(ctx, "egressA", metav1.DeleteOptions{})
	assertReceivedItems(t, egressUpdates, sets.NewString("egressA", "egressC"))
	assertScheduleResult(t, s, "egressA", "", "", false)
	assertScheduleResult(t, s, "egressB", "1.1.1.11", "node1", true)
	assertScheduleResult(t, s, "egressC", "1.1.1.21", "node1", true)

	// After egressD is created, it should be left unassigned as the total capacity is insufficient.
	crdClient.CrdV1alpha2().Egresses().Create(ctx, &crdv1a2.Egress{
		ObjectMeta: metav1.ObjectMeta{Name: "egressD", UID: "uidD", CreationTimestamp: metav1.NewTime(time.Unix(4, 0))},
		Spec:       crdv1a2.EgressSpec{EgressIP: "1.1.1.1", ExternalIPPool: "pool1"},
	}, metav1.CreateOptions{})
	assertReceivedItems(t, egressUpdates, sets.NewString())
	assertScheduleResult(t, s, "egressD", "", "", false)

	// After node2 joins, egressB should be moved to node2 determined by its consistent hash result, and egressD should be assigned to node1.
	fakeCluster.updateNodes([]string{"node1", "node2"})
	assertReceivedItems(t, egressUpdates, sets.NewString("egressB", "egressD"))
	assertScheduleResult(t, s, "egressB", "1.1.1.11", "node2", true)
	assertScheduleResult(t, s, "egressC", "1.1.1.21", "node1", true)
	assertScheduleResult(t, s, "egressD", "1.1.1.1", "node1", true)
}

func assertReceivedItems(t *testing.T, ch <-chan string, expectedItems sets.String) {
	receivedItems := sets.NewString()
	for i := 0; i < expectedItems.Len(); i++ {
		select {
		case <-time.After(2 * time.Second):
			t.Fatalf("Timeout getting item #%d from the channel", i)
		case item := <-ch:
			receivedItems.Insert(item)
		}
	}
	assert.Equal(t, expectedItems, receivedItems)

	select {
	case <-time.After(100 * time.Millisecond):
	case item := <-ch:
		t.Fatalf("Got unexpected item %s from the channel", item)
	}
}

func assertScheduleResult(t *testing.T, s *egressIPScheduler, egress, egressIP, egressNode string, scheduled bool) {
	gotEgressIP, gotEgressNode, gotScheduled := s.GetEgressIPAndNode(egress)
	assert.Equal(t, egressIP, gotEgressIP)
	assert.Equal(t, egressNode, gotEgressNode)
	assert.Equal(t, scheduled, gotScheduled)
}
