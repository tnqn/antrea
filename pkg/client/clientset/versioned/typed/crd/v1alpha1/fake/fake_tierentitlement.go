// Copyright 2020 Antrea Authors
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

// Code generated by client-gen. DO NOT EDIT.

package fake

import (
	"context"

	v1alpha1 "github.com/vmware-tanzu/antrea/pkg/apis/crd/v1alpha1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	labels "k8s.io/apimachinery/pkg/labels"
	schema "k8s.io/apimachinery/pkg/runtime/schema"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	testing "k8s.io/client-go/testing"
)

// FakeTierEntitlements implements TierEntitlementInterface
type FakeTierEntitlements struct {
	Fake *FakeCrdV1alpha1
}

var tierentitlementsResource = schema.GroupVersionResource{Group: "crd.antrea.tanzu.vmware.com", Version: "v1alpha1", Resource: "tierentitlements"}

var tierentitlementsKind = schema.GroupVersionKind{Group: "crd.antrea.tanzu.vmware.com", Version: "v1alpha1", Kind: "TierEntitlement"}

// Get takes name of the tierEntitlement, and returns the corresponding tierEntitlement object, and an error if there is any.
func (c *FakeTierEntitlements) Get(ctx context.Context, name string, options v1.GetOptions) (result *v1alpha1.TierEntitlement, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootGetAction(tierentitlementsResource, name), &v1alpha1.TierEntitlement{})
	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.TierEntitlement), err
}

// List takes label and field selectors, and returns the list of TierEntitlements that match those selectors.
func (c *FakeTierEntitlements) List(ctx context.Context, opts v1.ListOptions) (result *v1alpha1.TierEntitlementList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootListAction(tierentitlementsResource, tierentitlementsKind, opts), &v1alpha1.TierEntitlementList{})
	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &v1alpha1.TierEntitlementList{ListMeta: obj.(*v1alpha1.TierEntitlementList).ListMeta}
	for _, item := range obj.(*v1alpha1.TierEntitlementList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested tierEntitlements.
func (c *FakeTierEntitlements) Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewRootWatchAction(tierentitlementsResource, opts))
}

// Create takes the representation of a tierEntitlement and creates it.  Returns the server's representation of the tierEntitlement, and an error, if there is any.
func (c *FakeTierEntitlements) Create(ctx context.Context, tierEntitlement *v1alpha1.TierEntitlement, opts v1.CreateOptions) (result *v1alpha1.TierEntitlement, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootCreateAction(tierentitlementsResource, tierEntitlement), &v1alpha1.TierEntitlement{})
	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.TierEntitlement), err
}

// Update takes the representation of a tierEntitlement and updates it. Returns the server's representation of the tierEntitlement, and an error, if there is any.
func (c *FakeTierEntitlements) Update(ctx context.Context, tierEntitlement *v1alpha1.TierEntitlement, opts v1.UpdateOptions) (result *v1alpha1.TierEntitlement, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootUpdateAction(tierentitlementsResource, tierEntitlement), &v1alpha1.TierEntitlement{})
	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.TierEntitlement), err
}

// Delete takes name of the tierEntitlement and deletes it. Returns an error if one occurs.
func (c *FakeTierEntitlements) Delete(ctx context.Context, name string, opts v1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewRootDeleteAction(tierentitlementsResource, name), &v1alpha1.TierEntitlement{})
	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeTierEntitlements) DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error {
	action := testing.NewRootDeleteCollectionAction(tierentitlementsResource, listOpts)

	_, err := c.Fake.Invokes(action, &v1alpha1.TierEntitlementList{})
	return err
}

// Patch applies the patch and returns the patched tierEntitlement.
func (c *FakeTierEntitlements) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *v1alpha1.TierEntitlement, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootPatchSubresourceAction(tierentitlementsResource, name, pt, data, subresources...), &v1alpha1.TierEntitlement{})
	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.TierEntitlement), err
}
