// Copyright 2021 Antrea Authors
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
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	cgtesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"

	"antrea.io/antrea/pkg/apis/controlplane"
	"antrea.io/antrea/pkg/apis/crd/v1alpha2"
	fakeversioned "antrea.io/antrea/pkg/client/clientset/versioned/fake"
	crdinformers "antrea.io/antrea/pkg/client/informers/externalversions"
	"antrea.io/antrea/pkg/controller/egress/store"
	"antrea.io/antrea/pkg/controller/externalippool"
	"antrea.io/antrea/pkg/controller/grouping"
)

var (
	node1 = "node1"
	node2 = "node2"
	node3 = "node3"
	// Fake Pods
	podFoo1                 = newPod("default", "podFoo1", map[string]string{"app": "foo"}, node1, "1.1.1.1", false)
	podFoo2                 = newPod("default", "podFoo2", map[string]string{"app": "foo"}, node2, "1.1.2.1", false)
	podBar1                 = newPod("default", "podBar1", map[string]string{"app": "bar"}, node1, "1.1.1.2", false)
	podFoo1InOtherNamespace = newPod("other", "podFoo1", map[string]string{"app": "foo"}, node1, "1.1.1.3", false)
	podUnscheduled          = newPod("default", "podUnscheduled", map[string]string{"app": "foo"}, "", "", false)
	podNonIP                = newPod("default", "podNonIP", map[string]string{"app": "foo"}, node1, "", false)
	podWithHostNetwork      = newPod("default", "podHostNetwork", map[string]string{"app": "bar"}, node1, "172.16.100.1", true)
	// Fake Namespaces
	nsDefault = newNamespace("default", map[string]string{"company": "default"})
	nsOther   = newNamespace("other", map[string]string{"company": "other"})
	// Fake ExternalIPPools
	eipFoo1 = newExternalIPPool("pool1", "1.1.1.0/24", "", "")
	eipFoo2 = newExternalIPPool("pool2", "", "2.2.2.10", "2.2.2.20")
)

func newEgress(name, egressIP, externalIPPool string, podSelector, namespaceSelector *metav1.LabelSelector) *v1alpha2.Egress {
	egress := &v1alpha2.Egress{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: v1alpha2.EgressSpec{
			AppliedTo: v1alpha2.AppliedTo{
				PodSelector:       podSelector,
				NamespaceSelector: namespaceSelector,
			},
			EgressIP:       egressIP,
			ExternalIPPool: externalIPPool,
		},
	}
	return egress
}

func newEgressWithMultiExternalIPPools(name, egressIP, externalIPPool string, egressIPs, externalIPPools []string, podSelector, namespaceSelector *metav1.LabelSelector) *v1alpha2.Egress {
	egress := newEgress(name, egressIP, externalIPPool, podSelector, namespaceSelector)
	egress.Spec.EgressIPs = egressIPs
	egress.Spec.ExternalIPPools = externalIPPools
	return egress
}

func newExternalIPPool(name, cidr, start, end string) *v1alpha2.ExternalIPPool {
	pool := &v1alpha2.ExternalIPPool{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	if len(cidr) > 0 {
		pool.Spec.IPRanges = append(pool.Spec.IPRanges, v1alpha2.IPRange{CIDR: cidr})
	}
	if len(start) > 0 && len(end) > 0 {
		pool.Spec.IPRanges = append(pool.Spec.IPRanges, v1alpha2.IPRange{Start: start, End: end})
	}
	return pool
}

func newNamespace(name string, labels map[string]string) *v1.Namespace {
	return &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
	}
}

func newPod(namespace, name string, labels map[string]string, nodeName string, ip string, hostNetwork bool) *v1.Pod {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			Labels:    labels,
		},
		Spec: v1.PodSpec{
			NodeName:    nodeName,
			HostNetwork: hostNetwork,
		},
	}
	if len(ip) > 0 {
		pod.Status.PodIP = ip
		pod.Status.PodIPs = []v1.PodIP{{IP: ip}}
	}
	return pod
}

type egressController struct {
	*EgressController
	client              kubernetes.Interface
	crdClient           *fakeversioned.Clientset
	informerFactory     informers.SharedInformerFactory
	crdInformerFactory  crdinformers.SharedInformerFactory
	groupingController  *grouping.GroupEntityController
	externalIPAllocator *externalippool.ExternalIPPoolController
}

// objects is an initial set of K8s objects that is exposed through the client.
func newController(objects, crdObjects []runtime.Object) *egressController {
	client := fake.NewSimpleClientset(objects...)
	crdClient := fakeversioned.NewSimpleClientset(crdObjects...)
	informerFactory := informers.NewSharedInformerFactory(client, resyncPeriod)
	crdInformerFactory := crdinformers.NewSharedInformerFactory(crdClient, resyncPeriod)
	externalIPAllocator := externalippool.NewExternalIPPoolController(crdClient, crdInformerFactory.Crd().V1alpha2().ExternalIPPools())
	egressGroupStore := store.NewEgressGroupStore()
	egressInformer := crdInformerFactory.Crd().V1alpha2().Egresses()
	groupEntityIndex := grouping.NewGroupEntityIndex()
	groupingController := grouping.NewGroupEntityController(groupEntityIndex,
		informerFactory.Core().V1().Pods(),
		informerFactory.Core().V1().Namespaces(),
		crdInformerFactory.Crd().V1alpha2().ExternalEntities())
	controller := NewEgressController(crdClient, groupEntityIndex, egressInformer, externalIPAllocator, egressGroupStore)
	return &egressController{
		controller,
		client,
		crdClient,
		informerFactory,
		crdInformerFactory,
		groupingController,
		externalIPAllocator,
	}
}

func TestAddEgress(t *testing.T) {
	tests := []struct {
		name                 string
		inputEgress          *v1alpha2.Egress
		expectedEgressIP     string
		expectedEgressIPs    []string
		expectedEgressGroups map[string]*controlplane.EgressGroup
	}{
		{
			name: "Egress with podSelector and namespaceSelector",
			inputEgress: &v1alpha2.Egress{
				ObjectMeta: metav1.ObjectMeta{Name: "egressA", UID: "uidA"},
				Spec: v1alpha2.EgressSpec{
					AppliedTo: v1alpha2.AppliedTo{
						PodSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "foo"},
						},
						NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: nsDefault.Labels,
						},
					},
					EgressIP: "1.1.1.1",
				},
			},
			expectedEgressIP: "1.1.1.1",
			expectedEgressGroups: map[string]*controlplane.EgressGroup{
				node1: {
					ObjectMeta: metav1.ObjectMeta{Name: "egressA", UID: "uidA"},
					GroupMembers: []controlplane.GroupMember{
						{Pod: &controlplane.PodReference{Name: podFoo1.Name, Namespace: podFoo1.Namespace}},
						{Pod: &controlplane.PodReference{Name: podNonIP.Name, Namespace: podNonIP.Namespace}},
					},
				},
				node2: {
					ObjectMeta: metav1.ObjectMeta{Name: "egressA", UID: "uidA"},
					GroupMembers: []controlplane.GroupMember{
						{Pod: &controlplane.PodReference{Name: podFoo2.Name, Namespace: podFoo2.Namespace}},
					},
				},
				node3: nil,
			},
		},
		{
			name: "Egress with namespaceSelector",
			inputEgress: &v1alpha2.Egress{
				ObjectMeta: metav1.ObjectMeta{Name: "egressA", UID: "uidA"},
				Spec: v1alpha2.EgressSpec{
					AppliedTo: v1alpha2.AppliedTo{
						NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: nsDefault.Labels,
						},
					},
					EgressIP: "1.1.1.1",
				},
			},
			expectedEgressIP: "1.1.1.1",
			expectedEgressGroups: map[string]*controlplane.EgressGroup{
				node1: {
					ObjectMeta: metav1.ObjectMeta{Name: "egressA", UID: "uidA"},
					GroupMembers: []controlplane.GroupMember{
						{Pod: &controlplane.PodReference{Name: podFoo1.Name, Namespace: podFoo1.Namespace}},
						{Pod: &controlplane.PodReference{Name: podBar1.Name, Namespace: podBar1.Namespace}},
						{Pod: &controlplane.PodReference{Name: podNonIP.Name, Namespace: podNonIP.Namespace}},
					},
				},
				node2: {
					ObjectMeta: metav1.ObjectMeta{Name: "egressA", UID: "uidA"},
					GroupMembers: []controlplane.GroupMember{
						{Pod: &controlplane.PodReference{Name: podFoo2.Name, Namespace: podFoo2.Namespace}},
					},
				},
				node3: nil,
			},
		},
		{
			name: "Egress with podSelector",
			inputEgress: &v1alpha2.Egress{
				ObjectMeta: metav1.ObjectMeta{Name: "egressA", UID: "uidA"},
				Spec: v1alpha2.EgressSpec{
					AppliedTo: v1alpha2.AppliedTo{
						PodSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "foo"},
						},
					},
					EgressIP: "1.1.1.1",
				},
			},
			expectedEgressIP: "1.1.1.1",
			expectedEgressGroups: map[string]*controlplane.EgressGroup{
				node1: {
					ObjectMeta: metav1.ObjectMeta{Name: "egressA", UID: "uidA"},
					GroupMembers: []controlplane.GroupMember{
						{Pod: &controlplane.PodReference{Name: podFoo1.Name, Namespace: podFoo1.Namespace}},
						{Pod: &controlplane.PodReference{Name: podFoo1InOtherNamespace.Name, Namespace: podFoo1InOtherNamespace.Namespace}},
						{Pod: &controlplane.PodReference{Name: podNonIP.Name, Namespace: podNonIP.Namespace}},
					},
				},
				node2: {
					ObjectMeta: metav1.ObjectMeta{Name: "egressA", UID: "uidA"},
					GroupMembers: []controlplane.GroupMember{
						{Pod: &controlplane.PodReference{Name: podFoo2.Name, Namespace: podFoo2.Namespace}},
					},
				},
				node3: nil,
			},
		},
		{
			name: "Egress with podSelector and empty EgressIP",
			inputEgress: &v1alpha2.Egress{
				ObjectMeta: metav1.ObjectMeta{Name: "egressA", UID: "uidA"},
				Spec: v1alpha2.EgressSpec{
					AppliedTo: v1alpha2.AppliedTo{
						PodSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "foo"},
						},
					},
					EgressIP:       "",
					ExternalIPPool: eipFoo1.Name,
				},
			},
			expectedEgressIP: "1.1.1.1",
			expectedEgressGroups: map[string]*controlplane.EgressGroup{
				node1: {
					ObjectMeta: metav1.ObjectMeta{Name: "egressA", UID: "uidA"},
					GroupMembers: []controlplane.GroupMember{
						{Pod: &controlplane.PodReference{Name: podFoo1.Name, Namespace: podFoo1.Namespace}},
						{Pod: &controlplane.PodReference{Name: podFoo1InOtherNamespace.Name, Namespace: podFoo1InOtherNamespace.Namespace}},
						{Pod: &controlplane.PodReference{Name: podNonIP.Name, Namespace: podNonIP.Namespace}},
					},
				},
				node2: {
					ObjectMeta: metav1.ObjectMeta{Name: "egressA", UID: "uidA"},
					GroupMembers: []controlplane.GroupMember{
						{Pod: &controlplane.PodReference{Name: podFoo2.Name, Namespace: podFoo2.Namespace}},
					},
				},
				node3: nil,
			},
		},
		{
			name: "Egress with multiple ExternalIPPool",
			inputEgress: &v1alpha2.Egress{
				ObjectMeta: metav1.ObjectMeta{Name: "egressA", UID: "uidA"},
				Spec: v1alpha2.EgressSpec{
					AppliedTo: v1alpha2.AppliedTo{
						PodSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "foo"},
						},
					},
					EgressIP:        "",
					ExternalIPPool:  "",
					EgressIPs:       []string{},
					ExternalIPPools: []string{eipFoo2.Name, eipFoo1.Name},
				},
			},
			expectedEgressIP:  "",
			expectedEgressIPs: []string{"2.2.2.10", "1.1.1.1"},
			expectedEgressGroups: map[string]*controlplane.EgressGroup{
				node1: {
					ObjectMeta: metav1.ObjectMeta{Name: "egressA", UID: "uidA"},
					GroupMembers: []controlplane.GroupMember{
						{Pod: &controlplane.PodReference{Name: podFoo1.Name, Namespace: podFoo1.Namespace}},
						{Pod: &controlplane.PodReference{Name: podFoo1InOtherNamespace.Name, Namespace: podFoo1InOtherNamespace.Namespace}},
						{Pod: &controlplane.PodReference{Name: podNonIP.Name, Namespace: podNonIP.Namespace}},
					},
				},
				node2: {
					ObjectMeta: metav1.ObjectMeta{Name: "egressA", UID: "uidA"},
					GroupMembers: []controlplane.GroupMember{
						{Pod: &controlplane.PodReference{Name: podFoo2.Name, Namespace: podFoo2.Namespace}},
					},
				},
				node3: nil,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stopCh := make(chan struct{})
			defer close(stopCh)
			var fakeObjects []runtime.Object
			fakeObjects = append(fakeObjects, nsDefault, nsOther)
			fakeObjects = append(fakeObjects, podFoo1, podFoo2, podBar1, podFoo1InOtherNamespace, podUnscheduled, podNonIP, podWithHostNetwork)
			var fakeCRDObjects []runtime.Object
			fakeCRDObjects = append(fakeCRDObjects, eipFoo1)
			fakeCRDObjects = append(fakeCRDObjects, eipFoo2)
			controller := newController(fakeObjects, fakeCRDObjects)
			controller.informerFactory.Start(stopCh)
			controller.crdInformerFactory.Start(stopCh)
			controller.informerFactory.WaitForCacheSync(stopCh)
			controller.crdInformerFactory.WaitForCacheSync(stopCh)
			go controller.externalIPAllocator.Run(stopCh)
			require.True(t, cache.WaitForCacheSync(stopCh, controller.externalIPAllocator.HasSynced))
			go controller.groupingInterface.Run(stopCh)
			go controller.groupingController.Run(stopCh)
			go controller.Run(stopCh)

			controller.crdClient.CrdV1alpha2().Egresses().Create(context.TODO(), tt.inputEgress, metav1.CreateOptions{})

			for nodeName, expectedEgressGroup := range tt.expectedEgressGroups {
				watcher, err := controller.egressGroupStore.Watch(context.TODO(), "", nil, fields.ParseSelectorOrDie(fmt.Sprintf("nodeName=%s", nodeName)))
				require.NoError(t, err)
				gotEgressGroup := func() *controlplane.EgressGroup {
					for {
						select {
						case <-stopCh:
							return nil
						case <-time.After(500 * time.Millisecond):
							return nil
						case event := <-watcher.ResultChan():
							if event.Type == watch.Added {
								return event.Object.(*controlplane.EgressGroup)
							}
						}
					}
				}()

				if expectedEgressGroup == nil {
					assert.Nil(t, gotEgressGroup)
				} else {
					require.NotNil(t, gotEgressGroup)
					assert.Equal(t, expectedEgressGroup.ObjectMeta, gotEgressGroup.ObjectMeta)
					assert.ElementsMatch(t, expectedEgressGroup.GroupMembers, gotEgressGroup.GroupMembers)
				}
			}

			gotEgress, err := controller.crdClient.CrdV1alpha2().Egresses().Get(context.TODO(), tt.inputEgress.Name, metav1.GetOptions{})
			require.NoError(t, err)
			assert.Equal(t, tt.expectedEgressIP, gotEgress.Spec.EgressIP)
			assert.Equal(t, tt.expectedEgressIPs, gotEgress.Spec.EgressIPs)
			if gotEgress.Spec.ExternalIPPool != "" && gotEgress.Spec.EgressIP != "" {
				checkExternalIPPoolUsed(t, controller, gotEgress.Spec.ExternalIPPool, 1)
			}
		})
	}
}

func TestUpdateEgress(t *testing.T) {
	stopCh := make(chan struct{})
	defer close(stopCh)
	controller := newController([]runtime.Object{nsDefault, podFoo1}, []runtime.Object{eipFoo1, eipFoo2})
	controller.informerFactory.Start(stopCh)
	controller.crdInformerFactory.Start(stopCh)
	controller.informerFactory.WaitForCacheSync(stopCh)
	controller.crdInformerFactory.WaitForCacheSync(stopCh)
	go controller.externalIPAllocator.Run(stopCh)
	require.True(t, cache.WaitForCacheSync(stopCh, controller.externalIPAllocator.HasSynced))
	go controller.groupingInterface.Run(stopCh)
	go controller.groupingController.Run(stopCh)
	go controller.Run(stopCh)

	egress := &v1alpha2.Egress{
		ObjectMeta: metav1.ObjectMeta{Name: "egressA", UID: "uidA"},
		Spec: v1alpha2.EgressSpec{
			AppliedTo: v1alpha2.AppliedTo{
				PodSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "foo"},
				},
			},
			EgressIP:       "",
			ExternalIPPool: eipFoo1.Name,
		},
	}
	controller.crdClient.CrdV1alpha2().Egresses().Create(context.TODO(), egress, metav1.CreateOptions{})

	watcher, err := controller.egressGroupStore.Watch(context.TODO(), "", nil, fields.ParseSelectorOrDie(fmt.Sprintf("nodeName=%s", node1)))
	assert.NoError(t, err)

	getEvent := func() *watch.Event {
		for {
			select {
			case <-stopCh:
				return nil
			case <-time.After(500 * time.Millisecond):
				return nil
			case event := <-watcher.ResultChan():
				if event.Type != watch.Bookmark {
					return &event
				}
			}
		}
	}

	gotEgressIP := func() string {
		var err error
		egress, err = controller.crdClient.CrdV1alpha2().Egresses().Get(context.TODO(), egress.Name, metav1.GetOptions{})
		if err != nil {
			return ""
		}
		return egress.Spec.EgressIP
	}

	gotEgressIPs := func() []string {
		var err error
		egress, err = controller.crdClient.CrdV1alpha2().Egresses().Get(context.TODO(), egress.Name, metav1.GetOptions{})
		if err != nil {
			return nil
		}
		return egress.Spec.EgressIPs
	}

	assert.Equal(t, &watch.Event{
		Type: watch.Added,
		Object: &controlplane.EgressGroup{
			ObjectMeta: metav1.ObjectMeta{Name: "egressA", UID: "uidA"},
			GroupMembers: []controlplane.GroupMember{
				{Pod: &controlplane.PodReference{Name: podFoo1.Name, Namespace: podFoo1.Namespace}},
			},
		},
	}, getEvent())
	assert.Equal(t, "1.1.1.1", gotEgressIP())
	checkExternalIPPoolUsed(t, controller, eipFoo1.Name, 1)

	// Add a Pod matching the Egress's selector and running on this Node.
	controller.client.CoreV1().Pods(podFoo1InOtherNamespace.Namespace).Create(context.TODO(), podFoo1InOtherNamespace, metav1.CreateOptions{})
	assert.Equal(t, &watch.Event{
		Type: watch.Modified,
		Object: &controlplane.EgressGroupPatch{
			ObjectMeta: metav1.ObjectMeta{Name: "egressA", UID: "uidA"},
			AddedGroupMembers: []controlplane.GroupMember{
				{Pod: &controlplane.PodReference{Name: podFoo1InOtherNamespace.Name, Namespace: podFoo1InOtherNamespace.Namespace}},
			},
		},
	}, getEvent())

	// Delete the above Pod.
	controller.client.CoreV1().Pods(podFoo1InOtherNamespace.Namespace).Delete(context.TODO(), podFoo1InOtherNamespace.Name, metav1.DeleteOptions{})
	assert.Equal(t, &watch.Event{
		Type: watch.Modified,
		Object: &controlplane.EgressGroupPatch{
			ObjectMeta: metav1.ObjectMeta{Name: "egressA", UID: "uidA"},
			RemovedGroupMembers: []controlplane.GroupMember{
				{Pod: &controlplane.PodReference{Name: podFoo1InOtherNamespace.Name, Namespace: podFoo1InOtherNamespace.Namespace}},
			},
		},
	}, getEvent())

	// Updating the Egress's spec to make it match no Pods on this Node and use a new ExternalIPPool.
	egress.Spec.AppliedTo = v1alpha2.AppliedTo{
		PodSelector: &metav1.LabelSelector{
			MatchLabels: map[string]string{"app": "non-existing-app"},
		},
	}
	egress.Spec.ExternalIPPool = eipFoo2.Name
	egress.Spec.EgressIP = ""
	egress.Generation += 1
	controller.crdClient.CrdV1alpha2().Egresses().Update(context.TODO(), egress, metav1.UpdateOptions{})
	assert.Equal(t, &watch.Event{
		Type: watch.Deleted,
		Object: &controlplane.EgressGroup{
			ObjectMeta: metav1.ObjectMeta{Name: "egressA", UID: "uidA"},
		},
	}, getEvent())
	assert.Equal(t, "2.2.2.10", gotEgressIP())
	checkExternalIPPoolUsed(t, controller, eipFoo1.Name, 0)
	checkExternalIPPoolUsed(t, controller, eipFoo2.Name, 1)

	// Delete the IPPool in use. The EgressIP should be released.
	controller.crdClient.CrdV1alpha2().ExternalIPPools().Delete(context.TODO(), eipFoo2.Name, metav1.DeleteOptions{})
	assert.Eventually(t, func() bool {
		allocationMap := controller.getIPAllocation(egress.Name)
		if allocationMap != nil {
			return false
		}
		ip := gotEgressIP()
		if ip != "" {
			return false
		}
		return true
	}, time.Second, 50*time.Millisecond, "EgressIP was not deleted after the ExternalIPPool was deleted")

	// Recreate the ExternalIPPool. An EgressIP should be allocated.
	controller.crdClient.CrdV1alpha2().ExternalIPPools().Create(context.TODO(), eipFoo2, metav1.CreateOptions{})
	assert.Eventually(t, func() bool {
		return controller.getIPAllocation(egress.Name) != nil
	}, time.Second, 50*time.Millisecond, "IP was not allocated after the ExternalIPPool was created")
	checkExternalIPPoolUsed(t, controller, eipFoo2.Name, 1)

	// Test the egress with externalIPPools
	egress.Spec.ExternalIPPools = []string{eipFoo2.Name, eipFoo1.Name}
	egress.Spec.ExternalIPPool = ""
	egress.Spec.EgressIP = ""
	egress.Generation += 1
	controller.crdClient.CrdV1alpha2().Egresses().Update(context.TODO(), egress, metav1.UpdateOptions{})
	assert.Eventually(t, func() bool {
		return reflect.DeepEqual([]string{"2.2.2.10", "1.1.1.1"}, gotEgressIPs())
	}, time.Second, 50*time.Millisecond, "IP was not allocated after the ExternalIPPool was created")

	checkExternalIPPoolUsed(t, controller, eipFoo1.Name, 1)
	checkExternalIPPoolUsed(t, controller, eipFoo2.Name, 1)

	// Delete the IPPool in use. The EgressIP should be released.
	controller.crdClient.CrdV1alpha2().ExternalIPPools().Delete(context.TODO(), eipFoo2.Name, metav1.DeleteOptions{})
	require.Eventually(t, func() bool {
		if controller.getIPAllocation(egress.Name) == nil {
			return false
		}
		ips := gotEgressIPs()
		expectedEgressIPs := []string{"", "1.1.1.1"}
		if !reflect.DeepEqual(ips, expectedEgressIPs) {
			return false
		}
		return true
	}, time.Second, 50*time.Millisecond, "EgressIPs was not deleted after the ExternalIPPool was deleted")

	controller.crdClient.CrdV1alpha2().ExternalIPPools().Delete(context.TODO(), eipFoo1.Name, metav1.DeleteOptions{})
	require.Eventually(t, func() bool {
		if controller.getIPAllocation(egress.Name) != nil {
			return false
		}
		ips := gotEgressIPs()
		expectedEgressIPs := []string{"", ""}
		if !reflect.DeepEqual(ips, expectedEgressIPs) {
			return false
		}
		return true
	}, time.Second, 50*time.Millisecond, "EgressIPs was not deleted after the ExternalIPPool was deleted")

	// Recreate the ExternalIPPool. An EgressIP should be allocated.
	controller.crdClient.CrdV1alpha2().ExternalIPPools().Create(context.TODO(), eipFoo2, metav1.CreateOptions{})
	controller.crdClient.CrdV1alpha2().ExternalIPPools().Create(context.TODO(), eipFoo1, metav1.CreateOptions{})
	require.Eventually(t, func() bool {
		return controller.getIPAllocation(egress.Name) != nil
	}, time.Second, 50*time.Millisecond, "IP was not allocated after the ExternalIPPool was created")
	checkExternalIPPoolUsed(t, controller, eipFoo2.Name, 1)
	checkExternalIPPoolUsed(t, controller, eipFoo1.Name, 1)

	// Delete the Egress. The EgressIP should be released.
	controller.crdClient.CrdV1alpha2().Egresses().Delete(context.TODO(), egress.Name, metav1.DeleteOptions{})
	require.Eventually(t, func() bool {
		return controller.getIPAllocation(egress.Name) == nil
	}, time.Second, 50*time.Millisecond, "IP allocation was not deleted after the Egress was deleted")
	checkExternalIPPoolUsed(t, controller, eipFoo2.Name, 0)
	checkExternalIPPoolUsed(t, controller, eipFoo1.Name, 0)
}

func TestSyncEgressIP(t *testing.T) {
	tests := []struct {
		name                       string
		existingEgresses           []*v1alpha2.Egress
		existingExternalIPPool     *v1alpha2.ExternalIPPool
		inputEgress                *v1alpha2.Egress
		expectedEgressIP           string
		expectedExternalIPPoolUsed int
		expectErr                  bool
	}{
		{
			name: "Egress with empty EgressIP and existing ExternalIPPool",
			existingEgresses: []*v1alpha2.Egress{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "egressA", UID: "uidA"},
					Spec: v1alpha2.EgressSpec{
						EgressIP:       "1.1.1.1",
						ExternalIPPool: "ipPoolA",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "egressB", UID: "uidB"},
					Spec: v1alpha2.EgressSpec{
						EgressIP:       "1.1.1.2",
						ExternalIPPool: "ipPoolA",
					},
				},
			},
			// The first IPRange 1.1.1.0/30 should be occupied by the existing Egresses. The input Egress's IP should
			// be allocated from the second IPRange 1.1.2.10-1.1.2.20.
			existingExternalIPPool: newExternalIPPool("ipPoolA", "1.1.1.0/30", "1.1.2.10", "1.1.2.20"),
			inputEgress: &v1alpha2.Egress{
				ObjectMeta: metav1.ObjectMeta{Name: "egressC", UID: "uidC"},
				Spec: v1alpha2.EgressSpec{
					EgressIP:       "",
					ExternalIPPool: "ipPoolA",
				},
			},
			expectedEgressIP:           "1.1.2.10",
			expectedExternalIPPoolUsed: 3,
			expectErr:                  false,
		},
		{
			name:                   "Egress with empty EgressIP and non-existing ExternalIPPool",
			existingExternalIPPool: newExternalIPPool("ipPoolA", "1.1.1.0/24", "", ""),
			inputEgress: &v1alpha2.Egress{
				ObjectMeta: metav1.ObjectMeta{Name: "egressA", UID: "uidA"},
				Spec: v1alpha2.EgressSpec{
					EgressIP:       "",
					ExternalIPPool: "ipPoolB",
				},
			},
			expectedEgressIP:           "",
			expectedExternalIPPoolUsed: 0,
			expectErr:                  false,
		},
		{
			name:                   "[IPv6]Egress with empty EgressIP and proper ExternalIPPool",
			existingExternalIPPool: newExternalIPPool("ipPoolA", "2021:2::aaa0/124", "", ""),
			inputEgress: &v1alpha2.Egress{
				ObjectMeta: metav1.ObjectMeta{Name: "egressA", UID: "uidA"},
				Spec: v1alpha2.EgressSpec{
					ExternalIPPool: "ipPoolA",
				},
			},
			expectedEgressIP:           "2021:2::aaa1",
			expectedExternalIPPoolUsed: 1,
			expectErr:                  false,
		},
		{
			name:                   "Egress with non-empty EgressIP and proper ExternalIPPool",
			existingExternalIPPool: newExternalIPPool("ipPoolA", "1.1.1.0/24", "", ""),
			inputEgress: &v1alpha2.Egress{
				ObjectMeta: metav1.ObjectMeta{Name: "egressA", UID: "uidA"},
				Spec: v1alpha2.EgressSpec{
					EgressIP:       "1.1.1.2",
					ExternalIPPool: "ipPoolA",
				},
			},
			expectedEgressIP:           "1.1.1.2",
			expectedExternalIPPoolUsed: 1,
			expectErr:                  false,
		},
		{
			name:                   "Egress with non-empty EgressIP and improper ExternalIPPool",
			existingExternalIPPool: newExternalIPPool("ipPoolA", "1.1.1.0/24", "", ""),
			inputEgress: &v1alpha2.Egress{
				ObjectMeta: metav1.ObjectMeta{Name: "egressA", UID: "uidA"},
				Spec: v1alpha2.EgressSpec{
					EgressIP:       "1.1.2.2",
					ExternalIPPool: "ipPoolA",
				},
			},
			expectedEgressIP:           "1.1.2.2",
			expectedExternalIPPoolUsed: 0,
			expectErr:                  false,
		},
		{
			name: "Egress with updated EgressIP",
			existingEgresses: []*v1alpha2.Egress{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "egressA", UID: "uidA"},
					Spec: v1alpha2.EgressSpec{
						EgressIP:       "1.1.1.2",
						ExternalIPPool: "ipPoolA",
					},
				},
			},
			existingExternalIPPool: newExternalIPPool("ipPoolA", "1.1.1.0/24", "", ""),
			inputEgress: &v1alpha2.Egress{
				ObjectMeta: metav1.ObjectMeta{Name: "egressA", UID: "uidA"},
				Spec: v1alpha2.EgressSpec{
					EgressIP:       "1.1.1.3",
					ExternalIPPool: "ipPoolA",
				},
			},
			expectedEgressIP:           "1.1.1.3",
			expectedExternalIPPoolUsed: 1,
			expectErr:                  false,
		},
		{
			name: "Egress with unchanged EgressIP",
			existingEgresses: []*v1alpha2.Egress{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "egressA", UID: "uidA"},
					Spec: v1alpha2.EgressSpec{
						EgressIP:       "1.1.1.2",
						ExternalIPPool: "ipPoolA",
					},
				},
			},
			existingExternalIPPool: newExternalIPPool("ipPoolA", "1.1.1.0/24", "", ""),
			inputEgress: &v1alpha2.Egress{
				ObjectMeta: metav1.ObjectMeta{Name: "egressA", UID: "uidA"},
				Spec: v1alpha2.EgressSpec{
					EgressIP:       "1.1.1.2",
					ExternalIPPool: "ipPoolA",
				},
			},
			expectedEgressIP:           "1.1.1.2",
			expectedExternalIPPoolUsed: 1,
			expectErr:                  false,
		},
		{
			name: "Egress with conflicting EgressIP",
			existingEgresses: []*v1alpha2.Egress{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "egressA", UID: "uidA"},
					Spec: v1alpha2.EgressSpec{
						EgressIP:       "1.1.1.2",
						ExternalIPPool: "ipPoolA",
					},
				},
			},
			existingExternalIPPool: newExternalIPPool("ipPoolA", "1.1.1.0/24", "", ""),
			inputEgress: &v1alpha2.Egress{
				ObjectMeta: metav1.ObjectMeta{Name: "egressB", UID: "uidB"},
				Spec: v1alpha2.EgressSpec{
					EgressIP:       "1.1.1.2",
					ExternalIPPool: "ipPoolA",
				},
			},
			expectedEgressIP:           "1.1.1.2",
			expectedExternalIPPoolUsed: 1,
			expectErr:                  false,
		},
		{
			name: "Egress with empty ExternalIPPool",
			existingEgresses: []*v1alpha2.Egress{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "egressA", UID: "uidA"},
					Spec: v1alpha2.EgressSpec{
						EgressIP:       "1.1.1.2",
						ExternalIPPool: "ipPoolA",
					},
				},
			},
			existingExternalIPPool: newExternalIPPool("ipPoolA", "1.1.1.0/24", "", ""),
			inputEgress: &v1alpha2.Egress{
				ObjectMeta: metav1.ObjectMeta{Name: "egressA", UID: "uidA"},
				Spec: v1alpha2.EgressSpec{
					EgressIP: "10.10.10.10",
				},
			},
			expectedEgressIP:           "10.10.10.10",
			expectedExternalIPPoolUsed: 0,
			expectErr:                  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stopCh := make(chan struct{})
			defer close(stopCh)
			var fakeObjects []runtime.Object
			fakeObjects = append(fakeObjects, tt.inputEgress, tt.existingExternalIPPool)
			controller := newController(nil, fakeObjects)
			controller.informerFactory.Start(stopCh)
			controller.crdInformerFactory.Start(stopCh)
			controller.informerFactory.WaitForCacheSync(stopCh)
			controller.crdInformerFactory.WaitForCacheSync(stopCh)
			go controller.externalIPAllocator.Run(stopCh)
			require.True(t, cache.WaitForCacheSync(stopCh, controller.externalIPAllocator.HasSynced))
			controller.restoreIPAllocations(tt.existingEgresses)
			err := controller.syncEgressIP(tt.inputEgress)
			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			egress, err := controller.crdClient.CrdV1alpha2().Egresses().Get(context.TODO(), tt.inputEgress.Name, metav1.GetOptions{})
			assert.NoError(t, err)
			assert.Equal(t, tt.expectedEgressIP, egress.Spec.EgressIP)
			checkExternalIPPoolUsed(t, controller, tt.existingExternalIPPool.Name, tt.expectedExternalIPPoolUsed)
		})
	}
}

func TestSyncEgressIPWithMultiplePools(t *testing.T) {
	tests := []struct {
		name                        string
		existingEgresses            []*v1alpha2.Egress
		existingExternalIPPools     []*v1alpha2.ExternalIPPool
		inputEgress                 *v1alpha2.Egress
		patchEgressFail             bool
		expectedEgressIP            string
		expectedEgressIPs           []string
		expectedIPAllocations       map[string]ipAllocation
		expectedExternalIPPoolUseds []int
		expectErr                   bool
	}{
		{
			name: "Egress with ExternalIPPools",
			existingExternalIPPools: []*v1alpha2.ExternalIPPool{
				newExternalIPPool("ipPoolA", "1.1.1.0/24", "", ""),
				newExternalIPPool("ipPoolB", "2.2.2.0/24", "", ""),
				newExternalIPPool("ipPoolC", "3.3.3.0/24", "", "")},
			inputEgress: &v1alpha2.Egress{
				ObjectMeta: metav1.ObjectMeta{Name: "egressA", UID: "uidA"},
				Spec: v1alpha2.EgressSpec{
					ExternalIPPools: []string{"ipPoolA", "ipPoolB", "ipPoolC"},
				},
			},
			expectedEgressIPs: []string{"1.1.1.1", "2.2.2.1", "3.3.3.1"},
			expectedIPAllocations: map[string]ipAllocation{
				"egressA": map[string]string{"ipPoolA": "1.1.1.1", "ipPoolB": "2.2.2.1", "ipPoolC": "3.3.3.1"},
			},
			expectedExternalIPPoolUseds: []int{1, 1, 1},
			expectErr:                   false,
		},
		{
			name: "Egress with invalid ExternalIPPools",
			existingEgresses: []*v1alpha2.Egress{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "egressA", UID: "uidA"},
					Spec: v1alpha2.EgressSpec{
						ExternalIPPools: []string{"ipPoolA", "ipPoolC"},
						EgressIPs:       []string{"1.1.1.1", "3.3.3.1"},
					},
				},
			},
			existingExternalIPPools: []*v1alpha2.ExternalIPPool{
				newExternalIPPool("ipPoolA", "", "1.1.1.1", "1.1.1.1"),
				newExternalIPPool("ipPoolC", "3.3.3.0/24", "", "")},
			inputEgress: &v1alpha2.Egress{
				ObjectMeta: metav1.ObjectMeta{Name: "egressB", UID: "uidB"},
				Spec: v1alpha2.EgressSpec{
					ExternalIPPools: []string{"ipPoolA", "ipPoolB", "ipPoolC"},
				},
			},
			expectedIPAllocations: map[string]ipAllocation{
				"egressA": map[string]string{"ipPoolA": "1.1.1.1", "ipPoolC": "3.3.3.1"},
				"egressB": map[string]string{"ipPoolC": "3.3.3.2"},
			},
			expectedEgressIPs:           []string{"", "", "3.3.3.2"},
			expectedExternalIPPoolUseds: []int{1, 2},
			expectErr:                   false,
		},
		{
			name: "Egress with ExternalIPPool change to ExternalIPPools",
			existingEgresses: []*v1alpha2.Egress{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "egressA", UID: "uidA"},
					Spec: v1alpha2.EgressSpec{
						ExternalIPPool: "ipPoolA",
					},
				},
			},
			existingExternalIPPools: []*v1alpha2.ExternalIPPool{
				newExternalIPPool("ipPoolA", "1.1.1.0/24", "", ""),
				newExternalIPPool("ipPoolB", "2.2.2.0/24", "", "")},
			inputEgress: &v1alpha2.Egress{
				ObjectMeta: metav1.ObjectMeta{Name: "egressA", UID: "uidA"},
				Spec: v1alpha2.EgressSpec{
					ExternalIPPools: []string{"ipPoolB", "ipPoolA"},
				},
			},
			expectedIPAllocations: map[string]ipAllocation{
				"egressA": map[string]string{"ipPoolA": "1.1.1.1", "ipPoolB": "2.2.2.1"},
			},
			expectedEgressIPs:           []string{"2.2.2.1", "1.1.1.1"},
			expectedExternalIPPoolUseds: []int{1, 1},
			expectErr:                   false,
		},
		{
			name: "Egress with ExternalIPPool and EgressIP change to ExternalIPPools",
			existingEgresses: []*v1alpha2.Egress{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "egressA", UID: "uidA"},
					Spec: v1alpha2.EgressSpec{
						ExternalIPPool: "ipPoolA",
						EgressIP:       "1.1.1.10",
					},
				},
			},
			existingExternalIPPools: []*v1alpha2.ExternalIPPool{
				newExternalIPPool("ipPoolA", "1.1.1.0/24", "", ""),
				newExternalIPPool("ipPoolB", "2.2.2.0/24", "", ""),
			},
			inputEgress: &v1alpha2.Egress{
				ObjectMeta: metav1.ObjectMeta{Name: "egressA", UID: "uidA"},
				Spec: v1alpha2.EgressSpec{
					ExternalIPPools: []string{"ipPoolB", "ipPoolA"},
				},
			},
			expectedIPAllocations: map[string]ipAllocation{
				"egressA": map[string]string{"ipPoolA": "1.1.1.10", "ipPoolB": "2.2.2.1"},
			},
			expectedEgressIPs:           []string{"2.2.2.1", "1.1.1.10"},
			expectedExternalIPPoolUseds: []int{1, 1},
			expectErr:                   false,
		},
		{
			name: "Egress with ExternalIPPool and EgressIP change to ExternalIPPools but fail to update",
			existingEgresses: []*v1alpha2.Egress{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "egressA", UID: "uidA"},
					Spec: v1alpha2.EgressSpec{
						ExternalIPPool: "ipPoolA",
						EgressIP:       "1.1.1.10",
					},
				},
			},
			existingExternalIPPools: []*v1alpha2.ExternalIPPool{
				newExternalIPPool("ipPoolA", "1.1.1.0/24", "", ""),
				newExternalIPPool("ipPoolB", "2.2.2.0/24", "", ""),
			},
			inputEgress: &v1alpha2.Egress{
				ObjectMeta: metav1.ObjectMeta{Name: "egressA", UID: "uidA"},
				Spec: v1alpha2.EgressSpec{
					EgressIP:        "1.1.1.10",
					ExternalIPPools: []string{"ipPoolB", "ipPoolA"},
				},
			},
			patchEgressFail: true,
			expectedIPAllocations: map[string]ipAllocation{
				"egressA": map[string]string{"ipPoolA": "1.1.1.10"},
			},
			expectedEgressIP:            "1.1.1.10",
			expectedExternalIPPoolUseds: []int{1, 0},
			expectErr:                   true,
		},
		{
			name: "Egress with ExternalIPPools change to ExternalIPPool",
			existingEgresses: []*v1alpha2.Egress{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "egressA", UID: "uidA"},
					Spec: v1alpha2.EgressSpec{
						EgressIPs:       []string{"2.2.2.20", "1.1.1.10"},
						ExternalIPPools: []string{"ipPoolB", "ipPoolA"},
					},
				},
			},
			existingExternalIPPools: []*v1alpha2.ExternalIPPool{
				newExternalIPPool("ipPoolA", "1.1.1.0/24", "", ""),
				newExternalIPPool("ipPoolB", "2.2.2.0/24", "", "")},
			inputEgress: &v1alpha2.Egress{
				ObjectMeta: metav1.ObjectMeta{Name: "egressA", UID: "uidA"},
				Spec: v1alpha2.EgressSpec{
					ExternalIPPool: "ipPoolA",
				},
			},
			expectedIPAllocations: map[string]ipAllocation{
				"egressA": map[string]string{"ipPoolA": "1.1.1.10"},
			},
			expectedEgressIP:            "1.1.1.10",
			expectedExternalIPPoolUseds: []int{1, 0},
			expectErr:                   false,
		},
		{
			name: "Egress with ExternalIPPools change to ExternalIPPool and EgressIP",
			existingEgresses: []*v1alpha2.Egress{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "egressA", UID: "uidA"},
					Spec: v1alpha2.EgressSpec{
						EgressIPs:       []string{"2.2.2.20", "1.1.1.10"},
						ExternalIPPools: []string{"ipPoolB", "ipPoolA"},
					},
				},
			},
			existingExternalIPPools: []*v1alpha2.ExternalIPPool{
				newExternalIPPool("ipPoolA", "1.1.1.0/24", "", ""),
				newExternalIPPool("ipPoolB", "2.2.2.0/24", "", "")},
			inputEgress: &v1alpha2.Egress{
				ObjectMeta: metav1.ObjectMeta{Name: "egressA", UID: "uidA"},
				Spec: v1alpha2.EgressSpec{
					ExternalIPPool: "ipPoolA",
					EgressIP:       "1.1.1.100",
				},
			},
			expectedIPAllocations: map[string]ipAllocation{
				"egressA": map[string]string{"ipPoolA": "1.1.1.100"},
			},
			expectedEgressIP:            "1.1.1.100",
			expectedExternalIPPoolUseds: []int{1, 0},
			expectErr:                   false,
		},
		{
			name: "Egress with ExternalIPPools change to EgressIP",
			existingEgresses: []*v1alpha2.Egress{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "egressA", UID: "uidA"},
					Spec: v1alpha2.EgressSpec{
						EgressIPs:       []string{"2.2.2.20", "1.1.1.10"},
						ExternalIPPools: []string{"ipPoolB", "ipPoolA"},
					},
				},
			},
			existingExternalIPPools: []*v1alpha2.ExternalIPPool{
				newExternalIPPool("ipPoolA", "1.1.1.0/24", "", ""),
				newExternalIPPool("ipPoolB", "2.2.2.0/24", "", ""),
			},
			inputEgress: &v1alpha2.Egress{
				ObjectMeta: metav1.ObjectMeta{Name: "egressA", UID: "uidA"},
				Spec:       v1alpha2.EgressSpec{EgressIP: "5.5.5.5"},
			},
			expectedIPAllocations:       map[string]ipAllocation{},
			expectedEgressIP:            "5.5.5.5",
			expectedExternalIPPoolUseds: []int{0, 0},
			expectErr:                   false,
		},
		{
			name: "Insert one ExternalIPPool",
			existingEgresses: []*v1alpha2.Egress{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "egressA", UID: "uidA"},
					Spec: v1alpha2.EgressSpec{
						EgressIPs:       []string{"2.2.2.20", "1.1.1.10"},
						ExternalIPPools: []string{"ipPoolB", "ipPoolA"},
					},
				},
			},
			existingExternalIPPools: []*v1alpha2.ExternalIPPool{
				newExternalIPPool("ipPoolA", "1.1.1.0/24", "", ""),
				newExternalIPPool("ipPoolB", "2.2.2.0/24", "", ""),
				newExternalIPPool("ipPoolC", "3.3.3.0/24", "", ""),
			},
			inputEgress: &v1alpha2.Egress{
				ObjectMeta: metav1.ObjectMeta{Name: "egressA", UID: "uidA"},
				Spec: v1alpha2.EgressSpec{
					ExternalIPPools: []string{"ipPoolB", "ipPoolC", "ipPoolA"},
				},
			},
			expectedIPAllocations: map[string]ipAllocation{
				"egressA": map[string]string{"ipPoolA": "1.1.1.10", "ipPoolB": "2.2.2.20", "ipPoolC": "3.3.3.1"},
			},
			expectedEgressIPs:           []string{"2.2.2.20", "3.3.3.1", "1.1.1.10"},
			expectedExternalIPPoolUseds: []int{1, 1, 1},
			expectErr:                   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stopCh := make(chan struct{})
			defer close(stopCh)
			var fakeObjects []runtime.Object
			fakeObjects = append(fakeObjects, tt.inputEgress)
			for _, eip := range tt.existingExternalIPPools {
				fakeObjects = append(fakeObjects, eip)
			}
			controller := newController(nil, fakeObjects)
			if tt.patchEgressFail {
				controller.crdClient.PrependReactor("patch", "egresses", func(action cgtesting.Action) (handled bool, ret runtime.Object, err error) {
					return true, nil, errors.New("error patching egress")
				})
			}
			controller.informerFactory.Start(stopCh)
			controller.crdInformerFactory.Start(stopCh)
			controller.informerFactory.WaitForCacheSync(stopCh)
			controller.crdInformerFactory.WaitForCacheSync(stopCh)
			go controller.externalIPAllocator.Run(stopCh)
			require.True(t, cache.WaitForCacheSync(stopCh, controller.externalIPAllocator.HasSynced))
			controller.restoreIPAllocations(tt.existingEgresses)
			err := controller.syncEgressIP(tt.inputEgress)
			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			egress, err := controller.crdClient.CrdV1alpha2().Egresses().Get(context.TODO(), tt.inputEgress.Name, metav1.GetOptions{})
			require.NoError(t, err)
			assert.Equal(t, tt.expectedEgressIP, egress.Spec.EgressIP)
			assert.Equal(t, tt.expectedEgressIPs, egress.Spec.EgressIPs)
			assert.Equal(t, tt.expectedIPAllocations, controller.ipAllocationMap)
			for i, pool := range tt.existingExternalIPPools {
				checkExternalIPPoolUsed(t, controller, pool.Name, tt.expectedExternalIPPoolUseds[i])
			}
		})
	}
}

func checkExternalIPPoolUsed(t *testing.T, controller *egressController, poolName string, used int) {
	exists := controller.externalIPAllocator.IPPoolExists(poolName)
	require.True(t, exists)
	err := wait.PollImmediate(50*time.Millisecond, 2*time.Second, func() (found bool, err error) {
		eip, err := controller.crdClient.CrdV1alpha2().ExternalIPPools().Get(context.TODO(), poolName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		return eip.Status.Usage.Used == used, nil
	})
	assert.NoError(t, err)
}
