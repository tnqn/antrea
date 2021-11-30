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

package tc

import (
	"antrea.io/antrea/pkg/agent/interfacestore"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	coreinformers "k8s.io/client-go/informers/core/v1"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/klog/v2"

	"antrea.io/antrea/pkg/agent/openflow"
	"antrea.io/antrea/pkg/apis/crd/v1alpha1"
)

type Controller struct {
	podInformer coreinformers.PodInformer
	podLister corelisters.PodLister

	ofClient openflow.Client
	interfaceStore interfacestore.InterfaceStore

	destOfID uint32
}

func NewController() *Controller {
	controller := &Controller{
		destOfID: 100,
	}
	return controller
}

func (c *Controller) syncTrafficControlConfiguration(key string) {
	var tcRule *v1alpha1.TrafficControlRule

	podSelector, err := metav1.LabelSelectorAsSelector(tcRule.PodSelector)
	if err != nil {
		klog.ErrorS(err, "Failed to convert PodSelector", "podSelector", podSelector)
		return
	}
	pods, _ := c.podLister.List(podSelector)

	var podOfPorts []uint32
	for _, pod := range pods {
		podInterfaces := c.interfaceStore.GetContainerInterfacesByPod(pod.Name, pod.Namespace)
		if len(podInterfaces) == 0 {
			continue
		}
		podOfPorts = append(podOfPorts, uint32(podInterfaces[0].OFPort))
	}

	err = c.ofClient.InstallTrafficControlRule(tcRule.Name, podOfPorts, tcRule.Direction, tcRule.Action, c.destOfID)
	if err != nil {
		klog.ErrorS(err, "Failed to install traffic control rule", "rule", tcRule.Name)
		return
	}
	return
}