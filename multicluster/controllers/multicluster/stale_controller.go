/*
Copyright 2021 Antrea Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package multicluster

import (
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	k8smcsv1alpha1 "sigs.k8s.io/mcs-api/pkg/apis/v1alpha1"

	mcsv1alpha1 "antrea.io/antrea/multicluster/apis/multicluster/v1alpha1"
	"antrea.io/antrea/multicluster/controllers/multicluster/common"
	"antrea.io/antrea/multicluster/controllers/multicluster/commonarea"
	crdv1alpha1 "antrea.io/antrea/pkg/apis/crd/v1alpha1"
)

// StaleResCleanupController will clean up ServiceImport, MC Service, ACNP, ClusterInfoImport resources
// if no corresponding ResourceImports in the leader cluster and remove stale ResourceExports
// in the leader cluster if no corresponding ServiceExport or Gateway in the member cluster.
// It will only run in the member cluster.
type StaleResCleanupController struct {
	client.Client
	Scheme           *runtime.Scheme
	localClusterID   string
	commonAreaGetter RemoteCommonAreaGetter
	namespace        string
	// queue only ever has one item, but it has nice error handling backoff/retry semantics
	queue workqueue.RateLimitingInterface
}

func NewStaleResCleanupController(
	Client client.Client,
	Scheme *runtime.Scheme,
	namespace string,
	commonAreaGetter RemoteCommonAreaGetter) *StaleResCleanupController {
	reconciler := &StaleResCleanupController{
		Client:           Client,
		Scheme:           Scheme,
		namespace:        namespace,
		commonAreaGetter: commonAreaGetter,
		queue:            workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "StaleResCleanupController"),
	}
	return reconciler
}

//+kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;delete
//+kubebuilder:rbac:groups=multicluster.x-k8s.io,resources=serviceimports,verbs=get;list;watch;delete
//+kubebuilder:rbac:groups=multicluster.crd.antrea.io,resources=resourceimports,verbs=get;list;watch;
//+kubebuilder:rbac:groups=multicluster.crd.antrea.io,resources=resourceexports,verbs=get;list;watch;delete

func (c *StaleResCleanupController) cleanup() error {
	var err error
	var commonArea commonarea.RemoteCommonArea
	commonArea, c.localClusterID, err = c.commonAreaGetter.GetRemoteCommonAreaAndLocalID()
	if err != nil {
		return err
	}

	resImpList := &mcsv1alpha1.ResourceImportList{}
	if err := commonArea.List(ctx, resImpList, &client.ListOptions{Namespace: commonArea.GetNamespace()}); err != nil {
		return err
	}
	if err := c.cleanupStaleServiceResources(commonArea, resImpList); err != nil {
		return err
	}

	if err := c.cleanupACNPResources(resImpList); err != nil {
		return err
	}
	if err := c.cleanupClusterInfoImport(resImpList); err != nil {
		return err
	}

	// Clean up stale ResourceExports in the leader cluster.
	resExpList := &mcsv1alpha1.ResourceExportList{}
	if err := commonArea.List(ctx, resExpList, &client.ListOptions{Namespace: commonArea.GetNamespace()}); err != nil {
		return err
	}

	if len(resExpList.Items) == 0 {
		return nil
	}
	if err := c.cleanupServiceResourceExport(commonArea, resExpList); err != nil {
		return err
	}
	if err := c.cleanupClusterInfoResourceExport(commonArea, resExpList); err != nil {
		return err
	}
	return nil
}

func (c *StaleResCleanupController) cleanupStaleServiceResources(commonArea commonarea.RemoteCommonArea,
	resImpList *mcsv1alpha1.ResourceImportList) error {
	svcImpList := &k8smcsv1alpha1.ServiceImportList{}
	if err := c.List(ctx, svcImpList, &client.ListOptions{}); err != nil {
		return err
	}

	svcList := &corev1.ServiceList{}
	if err := c.List(ctx, svcList, &client.ListOptions{}); err != nil {
		return err
	}

	svcImpItems := map[string]k8smcsv1alpha1.ServiceImport{}
	for _, svcImp := range svcImpList.Items {
		svcImpItems[svcImp.Namespace+"/"+svcImp.Name] = svcImp
	}

	mcsSvcItems := map[string]corev1.Service{}
	for _, svc := range svcList.Items {
		if _, ok := svc.Annotations[common.AntreaMCServiceAnnotation]; ok {
			mcsSvcItems[svc.Namespace+"/"+svc.Name] = svc
		}
	}

	for _, resImp := range resImpList.Items {
		if resImp.Spec.Kind == common.ServiceImportKind {
			delete(mcsSvcItems, resImp.Spec.Namespace+"/"+common.AntreaMCSPrefix+resImp.Spec.Name)
			delete(svcImpItems, resImp.Spec.Namespace+"/"+resImp.Spec.Name)
		}
	}

	for _, staleSvc := range mcsSvcItems {
		svc := staleSvc
		klog.InfoS("Cleaning up stale Service", "service", klog.KObj(&svc))
		if err := c.Client.Delete(ctx, &svc, &client.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	for _, staleSvcImp := range svcImpItems {
		svcImp := staleSvcImp
		klog.InfoS("Cleaning up stale ServiceImport", "serviceimport", klog.KObj(&svcImp))
		if err := c.Client.Delete(ctx, &svcImp, &client.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

func (c *StaleResCleanupController) cleanupACNPResources(resImpList *mcsv1alpha1.ResourceImportList) error {
	acnpList := &crdv1alpha1.ClusterNetworkPolicyList{}
	if err := c.List(ctx, acnpList, &client.ListOptions{}); err != nil {
		return err
	}
	staleMCACNPItems := map[string]crdv1alpha1.ClusterNetworkPolicy{}
	for _, acnp := range acnpList.Items {
		if _, ok := acnp.Annotations[common.AntreaMCACNPAnnotation]; ok {
			staleMCACNPItems[acnp.Name] = acnp
		}
	}
	for _, resImp := range resImpList.Items {
		if resImp.Spec.Kind == common.AntreaClusterNetworkPolicyKind {
			acnpNameFromResImp := common.AntreaMCSPrefix + resImp.Spec.Name
			delete(staleMCACNPItems, acnpNameFromResImp)
		}
	}
	for _, stalePolicy := range staleMCACNPItems {
		acnp := stalePolicy
		klog.InfoS("Cleaning up stale ACNP", "acnp", klog.KObj(&acnp))
		if err := c.Client.Delete(ctx, &acnp, &client.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

func (c *StaleResCleanupController) cleanupClusterInfoImport(resImpList *mcsv1alpha1.ResourceImportList) error {
	ciImpList := &mcsv1alpha1.ClusterInfoImportList{}
	if err := c.List(ctx, ciImpList, &client.ListOptions{}); err != nil {
		return err
	}

	staleCIImps := map[string]mcsv1alpha1.ClusterInfoImport{}
	for _, item := range ciImpList.Items {
		staleCIImps[item.Name] = item
	}
	for _, resImp := range resImpList.Items {
		if resImp.Spec.Kind == common.ClusterInfoKind {
			delete(staleCIImps, resImp.Name)
		}
	}
	for _, staleCIImp := range staleCIImps {
		ciImp := staleCIImp
		klog.InfoS("Cleaning up stale ClusterInfoImport", "clusterinfoimport", klog.KObj(&ciImp))
		if err := c.Client.Delete(ctx, &ciImp, &client.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

// cleanupServiceResourceExport removes any Service/Endpoint kind of ResourceExports when there is no
// corresponding ServiceExport in the local cluster.
func (c *StaleResCleanupController) cleanupServiceResourceExport(commonArea commonarea.RemoteCommonArea,
	resExpList *mcsv1alpha1.ResourceExportList) error {
	svcExpList := &k8smcsv1alpha1.ServiceExportList{}
	if err := c.List(ctx, svcExpList, &client.ListOptions{}); err != nil {
		return err
	}
	allResExpItems := resExpList.Items
	svcExpItems := svcExpList.Items
	staleResExpItems := map[string]mcsv1alpha1.ResourceExport{}

	for _, resExp := range allResExpItems {
		if resExp.Spec.Kind == common.ServiceKind && resExp.Labels[common.SourceClusterID] == c.localClusterID {
			staleResExpItems[resExp.Spec.Namespace+"/"+resExp.Spec.Name+"service"] = resExp
		}
		if resExp.Spec.Kind == common.EndpointsKind && resExp.Labels[common.SourceClusterID] == c.localClusterID {
			staleResExpItems[resExp.Spec.Namespace+"/"+resExp.Spec.Name+"endpoint"] = resExp
		}
	}

	for _, se := range svcExpItems {
		delete(staleResExpItems, se.Namespace+"/"+se.Name+"service")
		delete(staleResExpItems, se.Namespace+"/"+se.Name+"endpoint")
	}

	for _, r := range staleResExpItems {
		re := r
		klog.InfoS("Cleaning up ResourceExport", "ResourceExport", klog.KObj(&re))
		if err := commonArea.Delete(ctx, &re, &client.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

// cleanupClusterInfoResourceExport removes any ClusterInfo kind of ResourceExports when there is no
// Gateway in the local cluster.
func (c *StaleResCleanupController) cleanupClusterInfoResourceExport(commonArea commonarea.RemoteCommonArea,
	resExpList *mcsv1alpha1.ResourceExportList) error {
	var gws mcsv1alpha1.GatewayList
	if err := c.Client.List(ctx, &gws, &client.ListOptions{}); err != nil {
		return err
	}

	if len(gws.Items) == 0 {
		ciExport := &mcsv1alpha1.ResourceExport{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: commonArea.GetNamespace(),
				Name:      newClusterInfoResourceExportName(c.localClusterID),
			},
		}
		klog.InfoS("Cleaning up stale ClusterInfo kind of ResourceExport", "resourceexport", klog.KObj(ciExport))
		if err := commonArea.Delete(ctx, ciExport, &client.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

// Enqueue will be called after StaleResCleanupController is initialized.
func (c *StaleResCleanupController) Enqueue() {
	// The key can be anything as we only have single item.
	c.queue.Add("key")
}

// Run starts the StaleResCleanupController and blocks until stopCh is closed.
// it will run only once to clean up stale resources if no error happens.
func (c *StaleResCleanupController) Run(stopCh <-chan struct{}) {
	defer c.queue.ShutDown()

	klog.InfoS("Starting StaleResCleanupController")
	defer klog.InfoS("Shutting down StaleResCleanupController")

	if err := c.RunOnce(); err != nil {
		c.Enqueue()
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	<-stopCh
}

func (c *StaleResCleanupController) runWorker() {
	for c.processNextWorkItem() {
	}
}

func (c *StaleResCleanupController) processNextWorkItem() bool {
	key, quit := c.queue.Get()
	if quit {
		return false
	}
	defer c.queue.Done(key)

	err := c.cleanup()
	if err == nil {
		c.queue.Forget(key)
		return true
	}

	klog.ErrorS(err, "Error removing stale resources, requeuing it")
	c.queue.AddRateLimited(key)
	return true
}

func (c *StaleResCleanupController) RunOnce() error {
	err := c.cleanup()
	if err != nil {
		return err
	}
	return nil
}