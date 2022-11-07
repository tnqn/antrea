// Copyright 2022 Antrea Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Code generated by informer-gen. DO NOT EDIT.

package v1alpha1

import (
	"context"
	time "time"

	crdv1alpha1 "antrea.io/antrea/pkg/apis/crd/v1alpha1"
	versioned "antrea.io/antrea/pkg/client/clientset/versioned"
	internalinterfaces "antrea.io/antrea/pkg/client/informers/externalversions/internalinterfaces"
	v1alpha1 "antrea.io/antrea/pkg/client/listers/crd/v1alpha1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	watch "k8s.io/apimachinery/pkg/watch"
	cache "k8s.io/client-go/tools/cache"
)

// L7NetworkPolicyInformer provides access to a shared informer and lister for
// L7NetworkPolicies.
type L7NetworkPolicyInformer interface {
	Informer() cache.SharedIndexInformer
	Lister() v1alpha1.L7NetworkPolicyLister
}

type l7NetworkPolicyInformer struct {
	factory          internalinterfaces.SharedInformerFactory
	tweakListOptions internalinterfaces.TweakListOptionsFunc
	namespace        string
}

// NewL7NetworkPolicyInformer constructs a new informer for L7NetworkPolicy type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewL7NetworkPolicyInformer(client versioned.Interface, namespace string, resyncPeriod time.Duration, indexers cache.Indexers) cache.SharedIndexInformer {
	return NewFilteredL7NetworkPolicyInformer(client, namespace, resyncPeriod, indexers, nil)
}

// NewFilteredL7NetworkPolicyInformer constructs a new informer for L7NetworkPolicy type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewFilteredL7NetworkPolicyInformer(client versioned.Interface, namespace string, resyncPeriod time.Duration, indexers cache.Indexers, tweakListOptions internalinterfaces.TweakListOptionsFunc) cache.SharedIndexInformer {
	return cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options v1.ListOptions) (runtime.Object, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.CrdV1alpha1().L7NetworkPolicies(namespace).List(context.TODO(), options)
			},
			WatchFunc: func(options v1.ListOptions) (watch.Interface, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.CrdV1alpha1().L7NetworkPolicies(namespace).Watch(context.TODO(), options)
			},
		},
		&crdv1alpha1.L7NetworkPolicy{},
		resyncPeriod,
		indexers,
	)
}

func (f *l7NetworkPolicyInformer) defaultInformer(client versioned.Interface, resyncPeriod time.Duration) cache.SharedIndexInformer {
	return NewFilteredL7NetworkPolicyInformer(client, f.namespace, resyncPeriod, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}, f.tweakListOptions)
}

func (f *l7NetworkPolicyInformer) Informer() cache.SharedIndexInformer {
	return f.factory.InformerFor(&crdv1alpha1.L7NetworkPolicy{}, f.defaultInformer)
}

func (f *l7NetworkPolicyInformer) Lister() v1alpha1.L7NetworkPolicyLister {
	return v1alpha1.NewL7NetworkPolicyLister(f.Informer().GetIndexer())
}
