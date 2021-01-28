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

package grouping

import (
	"time"

	v1 "k8s.io/api/core/v1"
	coreinformers "k8s.io/client-go/informers/core/v1"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"

	"github.com/vmware-tanzu/antrea/pkg/apis/core/v1alpha2"
	corev1a2informers "github.com/vmware-tanzu/antrea/pkg/client/informers/externalversions/core/v1alpha2"
	corev1a2listers "github.com/vmware-tanzu/antrea/pkg/client/listers/core/v1alpha2"
	"github.com/vmware-tanzu/antrea/pkg/features"
)

const (
	controllerName = "GroupController"
	// NetworkPolicyController is the only writer of the antrea network policy
	// storages and will keep re-enqueuing failed items until they succeed.
	// Set resyncPeriod to 0 to disable resyncing.
	resyncPeriod time.Duration = 0
)

type Controller struct {
	podInformer coreinformers.PodInformer
	// podLister is able to list/get Pods and is populated by the shared informer passed to
	// NewNetworkPolicyController.
	podLister corelisters.PodLister
	// podListerSynced is a function which returns true if the Pod shared informer has been synced at least once.
	podListerSynced cache.InformerSynced

	externalEntityInformer corev1a2informers.ExternalEntityInformer
	// externalEntityLister is able to list/get ExternalEntities and is populated by the shared informer passed to
	// NewNetworkPolicyController.
	externalEntityLister corev1a2listers.ExternalEntityLister
	// externalEntityListerSynced is a function which returns true if the ExternalEntity shared informer has been synced at least once.
	externalEntityListerSynced cache.InformerSynced

	namespaceInformer coreinformers.NamespaceInformer
	// namespaceLister is able to list/get Namespaces and is populated by the shared informer passed to
	// NewNetworkPolicyController.
	namespaceLister corelisters.NamespaceLister
	// namespaceListerSynced is a function which returns true if the Namespace shared informer has been synced at least once.
	namespaceListerSynced cache.InformerSynced

	groupEntityIndex GroupEntityIndex
}

func NewController(groupEntityIndex GroupEntityIndex,
	podInformer coreinformers.PodInformer,
	namespaceInformer coreinformers.NamespaceInformer,
	externalEntityInformer corev1a2informers.ExternalEntityInformer) *Controller {
	c := &Controller{
		groupEntityIndex:           groupEntityIndex,
		podInformer:                podInformer,
		podLister:                  podInformer.Lister(),
		podListerSynced:            podInformer.Informer().HasSynced,
		namespaceInformer:          namespaceInformer,
		namespaceLister:            namespaceInformer.Lister(),
		namespaceListerSynced:      namespaceInformer.Informer().HasSynced,
		externalEntityInformer:     externalEntityInformer,
		externalEntityLister:       externalEntityInformer.Lister(),
		externalEntityListerSynced: externalEntityInformer.Informer().HasSynced,
	}
	// Add handlers for Pod events.
	podInformer.Informer().AddEventHandlerWithResyncPeriod(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    c.addPod,
			UpdateFunc: c.updatePod,
			DeleteFunc: c.deletePod,
		},
		resyncPeriod,
	)
	// Add handlers for Namespace events.
	namespaceInformer.Informer().AddEventHandlerWithResyncPeriod(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    c.addNamespace,
			UpdateFunc: c.updateNamespace,
			DeleteFunc: c.deleteNamespace,
		},
		resyncPeriod,
	)
	if features.DefaultFeatureGate.Enabled(features.AntreaPolicy) {
		// Add handlers for ExternalEntity events.
		externalEntityInformer.Informer().AddEventHandlerWithResyncPeriod(
			cache.ResourceEventHandlerFuncs{
				AddFunc:    c.addExternalEntity,
				UpdateFunc: c.updateExternalEntity,
				DeleteFunc: c.deleteExternalEntity,
			},
			resyncPeriod,
		)
	}
	return c
}

func (n *Controller) Run(stopCh <-chan struct{}) {
	klog.Infof("Starting %s", controllerName)
	defer klog.Infof("Shutting down %s", controllerName)

	cacheSyncs := []cache.InformerSynced{n.podListerSynced, n.namespaceListerSynced}
	// Wait for externalEntityListerSynced when AntreaPolicy feature gate is enabled.
	if features.DefaultFeatureGate.Enabled(features.AntreaPolicy) {
		cacheSyncs = append(cacheSyncs, n.externalEntityListerSynced)
	}
	if !cache.WaitForNamedCacheSync(controllerName, stopCh, cacheSyncs...) {
		return
	}

	go n.groupEntityIndex.Run(stopCh)

	<-stopCh
}

// addPod retrieves all AddressGroups and AppliedToGroups which match the Pod's
// labels and enqueues the groups key for further processing.
func (n *Controller) addPod(obj interface{}) {
	pod := obj.(*v1.Pod)
	klog.V(2).Infof("Processing Pod %s/%s ADD event, labels: %v", pod.Namespace, pod.Name, pod.Labels)
	n.groupEntityIndex.AddPod(pod)
}

// updatePod retrieves all AddressGroups and AppliedToGroups which match the
// updated and old Pod's labels and enqueues the group keys for further
// processing.
func (n *Controller) updatePod(_, curObj interface{}) {
	curPod := curObj.(*v1.Pod)
	klog.V(2).Infof("Processing Pod %s/%s UPDATE event, labels: %v", curPod.Namespace, curPod.Name, curPod.Labels)
	n.groupEntityIndex.AddPod(curPod)
}

// deletePod retrieves all AddressGroups and AppliedToGroups which match the Pod's
// labels and enqueues the groups key for further processing.
func (n *Controller) deletePod(old interface{}) {
	pod, ok := old.(*v1.Pod)
	if !ok {
		tombstone, ok := old.(cache.DeletedFinalStateUnknown)
		if !ok {
			klog.Errorf("Error decoding object when deleting Pod, invalid type: %v", old)
			return
		}
		pod, ok = tombstone.Obj.(*v1.Pod)
		if !ok {
			klog.Errorf("Error decoding object tombstone when deleting Pod, invalid type: %v", tombstone.Obj)
			return
		}
	}
	n.groupEntityIndex.DeletePod(pod)
}

// addNamespace retrieves all AddressGroups which match the Namespace
// labels and enqueues the group keys for further processing.
func (n *Controller) addNamespace(obj interface{}) {
	namespace := obj.(*v1.Namespace)
	klog.V(2).Infof("Processing Namespace %s ADD event, labels: %v", namespace.Name, namespace.Labels)
	n.groupEntityIndex.AddNamespace(namespace)
}

// updateNamespace retrieves all AddressGroups which match the current and old
// Namespace labels and enqueues the group keys for further processing.
func (n *Controller) updateNamespace(_, curObj interface{}) {
	curNamespace := curObj.(*v1.Namespace)
	klog.V(2).Infof("Processing Namespace %s UPDATE event, labels: %v", curNamespace.Name, curNamespace.Labels)
	n.groupEntityIndex.AddNamespace(curNamespace)
}

// deleteNamespace retrieves all AddressGroups which match the Namespace's
// labels and enqueues the group keys for further processing.
func (n *Controller) deleteNamespace(old interface{}) {
	namespace, ok := old.(*v1.Namespace)
	if !ok {
		tombstone, ok := old.(cache.DeletedFinalStateUnknown)
		if !ok {
			klog.Errorf("Error decoding object when deleting Namespace, invalid type: %v", old)
			return
		}
		namespace, ok = tombstone.Obj.(*v1.Namespace)
		if !ok {
			klog.Errorf("Error decoding object tombstone when deleting Namespace, invalid type: %v", tombstone.Obj)
			return
		}
	}
	klog.V(2).Infof("Processing Namespace %s DELETE event, labels: %v", namespace.Name, namespace.Labels)
	n.groupEntityIndex.DeleteNamespace(namespace)
}

// addExternalEntity retrieves all AddressGroups and AppliedToGroups which match the ExternalEnitty's
// labels and enqueues the groups key for further processing.
func (n *Controller) addExternalEntity(obj interface{}) {
	ee := obj.(*v1alpha2.ExternalEntity)
	klog.V(2).Infof("Processing ExternalEntity %s/%s ADD event, labels: %v", ee.GetNamespace(), ee.GetName(), ee.GetLabels())
	n.groupEntityIndex.AddExternalEntity(ee)
}

// updateExternalEntity retrieves all AddressGroups and AppliedToGroups which match the
// updated and old ExternalEntity's labels and enqueues the group keys for further
// processing.
func (n *Controller) updateExternalEntity(_, curObj interface{}) {
	curEE := curObj.(*v1alpha2.ExternalEntity)
	klog.V(2).Infof("Processing ExternalEntity %s/%s UPDATE event, labels: %v", curEE.GetNamespace(), curEE.GetName(), curEE.GetLabels())
	n.groupEntityIndex.AddExternalEntity(curEE)
}

// deleteExternalEntity retrieves all AddressGroups and AppliedToGroups which match the ExternalEntity's
// labels and enqueues the groups key for further processing.
func (n *Controller) deleteExternalEntity(old interface{}) {
	ee, ok := old.(*v1alpha2.ExternalEntity)
	if !ok {
		tombstone, ok := old.(cache.DeletedFinalStateUnknown)
		if !ok {
			klog.Errorf("Error decoding object when deleting ExternalEntity, invalid type: %v", old)
			return
		}
		ee, ok = tombstone.Obj.(*v1alpha2.ExternalEntity)
		if !ok {
			klog.Errorf("Error decoding object tombstone when deleting ExternalEntity, invalid type: %v", tombstone.Obj)
			return
		}
	}

	klog.V(2).Infof("Processing ExternalEntity %s/%s DELETE event, labels: %v", ee.GetNamespace(), ee.GetName(), ee.GetLabels())
	n.groupEntityIndex.DeleteExternalEntity(ee)
}
