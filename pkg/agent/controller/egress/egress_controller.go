package egress

import (
	"context"
	"github.com/vmware-tanzu/antrea/pkg/agent/route"
	"k8s.io/apimachinery/pkg/labels"
	"net"
	"reflect"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"

	"github.com/vmware-tanzu/antrea/pkg/agent"
	"github.com/vmware-tanzu/antrea/pkg/agent/interfacestore"
	"github.com/vmware-tanzu/antrea/pkg/agent/openflow"
	v1beta "github.com/vmware-tanzu/antrea/pkg/apis/controlplane/v1beta1"
	egressv1a1 "github.com/vmware-tanzu/antrea/pkg/apis/egress/v1alpha1"
	opsinformers "github.com/vmware-tanzu/antrea/pkg/client/informers/externalversions/egress/v1alpha1"
	opslisters "github.com/vmware-tanzu/antrea/pkg/client/listers/egress/v1alpha1"
)

const (
	controllerName = "AntreaAgentEgressController"
	// How long to wait before retrying the processing of a network policy change.
	minRetryDelay = 5 * time.Second
	maxRetryDelay = 300 * time.Second
	// Default number of workers processing a rule change.
	defaultWorkers               = 4
	resyncPeriod   time.Duration = 0
)

var emptyWatch = watch.NewEmptyWatch()

type LocalIPDetector interface {
	IsLocalIP(ip string) bool
}

type Controller struct {
	antreaClientProvider     agent.AntreaClientProvider
	egressPolicyInformer     opsinformers.EgressInformer
	egressPolicyLister       opslisters.EgressLister
	ofClient                 openflow.Client
	routeClient              route.Interface
	egressPolicyListerSynced cache.InformerSynced
	queue                    workqueue.RateLimitingInterface
	egressMarks              map[string]uint32
	egressGroups             map[string]v1beta.GroupMemberSet
	localIPDetector          LocalIPDetector
	ifaceStore               interfacestore.InterfaceStore
	nodeName                 string
}

type fakeLocalIPDetector struct {
	nodeName string
}

func (f fakeLocalIPDetector) IsLocalIP(ip string) bool {
	return f.nodeName == "k8s01"
}

var _ LocalIPDetector = &fakeLocalIPDetector{}

func NewEgressController(
	ofClient openflow.Client,
	egressInformer opsinformers.EgressInformer,
	antreaClientGetter agent.AntreaClientProvider,
	ifaceStore interfacestore.InterfaceStore,
	routeClient route.Interface,
	nodeName string,
) *Controller {
	c := &Controller{
		ofClient:                 ofClient,
		routeClient:              routeClient,
		antreaClientProvider:     antreaClientGetter,
		queue:                    workqueue.NewNamedRateLimitingQueue(workqueue.NewItemExponentialFailureRateLimiter(minRetryDelay, maxRetryDelay), "egressgroup"),
		egressPolicyInformer:     egressInformer,
		egressPolicyLister:       egressInformer.Lister(),
		egressPolicyListerSynced: egressInformer.Informer().HasSynced,
		nodeName:                 nodeName,
		ifaceStore:               ifaceStore,
		egressGroups:             map[string]v1beta.GroupMemberSet{},
		egressMarks:              map[string]uint32{},
		localIPDetector:          &fakeLocalIPDetector{nodeName: nodeName},
	}

	egressInformer.Informer().AddEventHandlerWithResyncPeriod(
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(cur interface{}) {
				c.enqueueEgress(cur)
			},
			UpdateFunc: func(old, cur interface{}) {
				c.enqueueEgress(cur)
			},
			DeleteFunc: func(old interface{}) {
				c.enqueueEgress(old)
			},
		},
		resyncPeriod,
	)
	return c
}

func (c *Controller) enqueueEgress(obj interface{}) {
	egress, isEgress := obj.(*egressv1a1.Egress)
	if !isEgress {
		deletedState, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			klog.Errorf("Received unexpected object: %v", obj)
			return
		}
		egress, ok = deletedState.Obj.(*egressv1a1.Egress)
		if !ok {
			klog.Errorf("DeletedFinalStateUnknown contains non-Egress object: %v", deletedState.Obj)
			return
		}
	}
	c.queue.Add(egress.Name)
}

// Run will create defaultWorkers workers (go routines) which will process the Node events from the
// workqueue.
func (c *Controller) Run(stopCh <-chan struct{}) {
	defer c.queue.ShutDown()

	klog.Infof("Starting %s", controllerName)
	defer klog.Infof("Shutting down %s", controllerName)

	if !cache.WaitForNamedCacheSync(controllerName, stopCh, c.egressPolicyListerSynced) {
		return
	}

	go wait.NonSlidingUntil(c.watch, 5*time.Second, stopCh)

	for i := 0; i < defaultWorkers; i++ {
		go wait.Until(c.worker, time.Second, stopCh)
	}
	<-stopCh
}

// worker is a long-running function that will continually call the processNextWorkItem function in
// order to read and process a message on the workqueue.
func (c *Controller) worker() {
	for c.processNextWorkItem() {
	}
}

// processNextWorkItem processes an item in the "node" work queue, by calling syncNodeRoute after
// casting the item to a string (Node name). If syncNodeRoute returns an error, this function
// handles it by requeueing the item so that it can be processed again later. If syncNodeRoute is
// successful, the Node is removed from the queue until we get notified of a new change. This
// function returns false if and only if the work queue was shutdown (no more items will be
// processed).
func (c *Controller) processNextWorkItem() bool {
	obj, quit := c.queue.Get()
	if quit {
		return false
	}
	// We call Done here so the workqueue knows we have finished processing this item. We also
	// must remember to call Forget if we do not want this work item being re-queued. For
	// example, we do not call Forget if a transient error occurs, instead the item is put back
	// on the workqueue and attempted again after a back-off period.
	defer c.queue.Done(obj)

	// We expect strings (Node name) to come off the workqueue.
	if key, ok := obj.(string); !ok {
		// As the item in the workqueue is actually invalid, we call Forget here else we'd
		// go into a loop of attempting to process a work item that is invalid.
		// This should not happen: enqueueNode only enqueues strings.
		c.queue.Forget(obj)
		klog.Errorf("Expected string in work queue but got %#v", obj)
		return true
	} else if err := c.syncEgress(key); err == nil {
		// If no error occurs we Forget this item so it does not get queued again until
		// another change happens.
		c.queue.Forget(key)
	} else {
		// Put the item back on the workqueue to handle any transient errors.
		c.queue.AddRateLimited(key)
		klog.Errorf("Error syncing Node %s, requeuing. Error: %v", key, err)
	}
	return true
}

func (c *Controller) syncEgress(egressName string) error {
	startTime := time.Now()
	defer func() {
		klog.V(4).Infof("Finished syncing Egress for %s. (%v)", egressName, time.Since(startTime))
	}()

	// The work queue guarantees that concurrent goroutines cannot call syncNodeRoute on the
	// same Node, which is required by the InstallNodeFlows / UninstallNodeFlows OF Client
	// methods.

	egresses, err := c.egressPolicyLister.List(labels.Everything())
	egress := egresses[0]
	if err != nil {
		if !errors.IsNotFound(err) {
			return err
		}
		klog.V(2).Infof("Egress %s not found", egressName)
		mark, exists := c.egressMarks[egressName]
		// It's not a local SNAT IP, do nothing.
		if !exists {
			return nil
		}
		if err := c.ofClient.UninstallSNATMarkFlows(mark); err != nil {
			return err
		}
		if err := c.routeClient.DeleteSNATRule(mark); err != nil {
			return err
		}
		delete(c.egressMarks, egressName)
		// TODO: release the mark
		return nil
	}

	egressIP := net.ParseIP(egress.Spec.EgressIP)
	var mark uint32
	if c.localIPDetector.IsLocalIP(egress.Spec.EgressIP) {
		var exists bool
		mark, exists = c.egressMarks[egressName]
		if !exists {
			// TODO: Allocate dynamically.
			mark = 1
			if err := c.ofClient.InstallSNATMarkFlows(egressIP, mark); err != nil {
				return err
			}
			c.routeClient.AddSNATRule(egressIP, mark)
		}
	}
	groupMembers := c.egressGroups[egressName]
	for _, groupMember := range groupMembers {
		ifaces := c.ifaceStore.GetContainerInterfacesByPod(groupMember.Pod.Name, groupMember.Pod.Namespace)
		if len(ifaces) == 0 {
			klog.V(2).Infof("Interfaces of Pod %s/%s not found")
			continue
		}
		ofPort := ifaces[0].OFPort
		if err := c.ofClient.InstallPodSNATFlows(uint32(ofPort), egressIP, mark); err != nil {
			return err
		}
	}
	return nil
}

func (c *Controller) watch() {
	klog.Info("Starting watch for EgressGroup")
	antreaClient, err := c.antreaClientProvider.GetAntreaClient()
	if err != nil {
		klog.Errorf("")
		return
	}
	options := metav1.ListOptions{
		FieldSelector: fields.OneTermEqualSelector("nodeName", c.nodeName).String(),
	}
	watcher, err := antreaClient.ControlplaneV1beta1().EgressGroups().Watch(context.TODO(), options)
	if err != nil {
		klog.Errorf("")
		return
	}
	// Watch method doesn't return error but "emptyWatch" in case of some partial data errors,
	// e.g. timeout error. Make sure that watcher is not empty and log warning otherwise.
	if reflect.TypeOf(watcher) == reflect.TypeOf(emptyWatch) {
		klog.Warning("Failed to start watch for EgressGroup, please ensure antrea service is reachable for the agent")
		return
	}

	klog.Info("Started watch for EgressGroup")
	eventCount := 0
	defer func() {
		klog.Infof("Stopped watch for EgressGroup, total items received: %d", eventCount)
		watcher.Stop()
	}()

	// First receive init events from the result channel and buffer them until
	// a Bookmark event is received, indicating that all init events have been
	// received.
	var initObjects []*v1beta.EgressGroup
loop:
	for {
		select {
		case event, ok := <-watcher.ResultChan():
			if !ok {
				klog.Warningf("Result channel for EgressGroup was closed")
				return
			}
			switch event.Type {
			case watch.Added:
				klog.V(2).Infof("Added EgressGroup (%#v)", event.Object)
				initObjects = append(initObjects, event.Object.(*v1beta.EgressGroup))
			case watch.Bookmark:
				break loop
			}
		}
	}
	klog.Infof("Received %d init events for EgressGroup", len(initObjects))

	eventCount += len(initObjects)
	for _, egressGroup := range initObjects {
		groupMemberSet := v1beta.NewGroupMemberSet()
		for i := range egressGroup.GroupMembers {
			groupMemberSet.Insert(&egressGroup.GroupMembers[i])
		}
		c.egressGroups[egressGroup.Name] = groupMemberSet
		c.queue.Add(egressGroup.Name)
	}

	for {
		select {
		case event, ok := <-watcher.ResultChan():
			if !ok {
				return
			}
			switch event.Type {
			case watch.Added:
				//if err := w.AddFunc(event.Object); err != nil {
				//	klog.Errorf("Failed to handle added event: %v", err)
				//	return
				//}
				klog.V(2).Infof("Added EgressGroup (%#v)", event.Object)
			case watch.Modified:
				//if err := w.UpdateFunc(event.Object); err != nil {
				//	klog.Errorf("Failed to handle modified event: %v", err)
				//	return
				//}
				klog.V(2).Infof("Updated EgressGroup (%#v)", event.Object)
			case watch.Deleted:
				//if err := w.DeleteFunc(event.Object); err != nil {
				//	klog.Errorf("Failed to handle deleted event: %v", err)
				//	return
				//}
				klog.V(2).Infof("Removed EgressGroup (%#v)", event.Object)
			default:
				klog.Errorf("Unknown event: %v", event)
				return
			}
			eventCount++
		}
	}
}
