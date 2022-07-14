// Copyright 2021 Antrea Authors
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

	v1alpha1 "antrea.io/antrea/multicluster/apis/multicluster/v1alpha1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	labels "k8s.io/apimachinery/pkg/labels"
	schema "k8s.io/apimachinery/pkg/runtime/schema"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	testing "k8s.io/client-go/testing"
)

// FakeMemberClusterAnnounces implements MemberClusterAnnounceInterface
type FakeMemberClusterAnnounces struct {
	Fake *FakeMulticlusterV1alpha1
	ns   string
}

var memberclusterannouncesResource = schema.GroupVersionResource{Group: "multicluster.crd.antrea.io", Version: "v1alpha1", Resource: "memberclusterannounces"}

var memberclusterannouncesKind = schema.GroupVersionKind{Group: "multicluster.crd.antrea.io", Version: "v1alpha1", Kind: "MemberClusterAnnounce"}

// Get takes name of the memberClusterAnnounce, and returns the corresponding memberClusterAnnounce object, and an error if there is any.
func (c *FakeMemberClusterAnnounces) Get(ctx context.Context, name string, options v1.GetOptions) (result *v1alpha1.MemberClusterAnnounce, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewGetAction(memberclusterannouncesResource, c.ns, name), &v1alpha1.MemberClusterAnnounce{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.MemberClusterAnnounce), err
}

// List takes label and field selectors, and returns the list of MemberClusterAnnounces that match those selectors.
func (c *FakeMemberClusterAnnounces) List(ctx context.Context, opts v1.ListOptions) (result *v1alpha1.MemberClusterAnnounceList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewListAction(memberclusterannouncesResource, memberclusterannouncesKind, c.ns, opts), &v1alpha1.MemberClusterAnnounceList{})

	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &v1alpha1.MemberClusterAnnounceList{ListMeta: obj.(*v1alpha1.MemberClusterAnnounceList).ListMeta}
	for _, item := range obj.(*v1alpha1.MemberClusterAnnounceList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested memberClusterAnnounces.
func (c *FakeMemberClusterAnnounces) Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewWatchAction(memberclusterannouncesResource, c.ns, opts))

}

// Create takes the representation of a memberClusterAnnounce and creates it.  Returns the server's representation of the memberClusterAnnounce, and an error, if there is any.
func (c *FakeMemberClusterAnnounces) Create(ctx context.Context, memberClusterAnnounce *v1alpha1.MemberClusterAnnounce, opts v1.CreateOptions) (result *v1alpha1.MemberClusterAnnounce, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewCreateAction(memberclusterannouncesResource, c.ns, memberClusterAnnounce), &v1alpha1.MemberClusterAnnounce{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.MemberClusterAnnounce), err
}

// Update takes the representation of a memberClusterAnnounce and updates it. Returns the server's representation of the memberClusterAnnounce, and an error, if there is any.
func (c *FakeMemberClusterAnnounces) Update(ctx context.Context, memberClusterAnnounce *v1alpha1.MemberClusterAnnounce, opts v1.UpdateOptions) (result *v1alpha1.MemberClusterAnnounce, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateAction(memberclusterannouncesResource, c.ns, memberClusterAnnounce), &v1alpha1.MemberClusterAnnounce{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.MemberClusterAnnounce), err
}

// Delete takes name of the memberClusterAnnounce and deletes it. Returns an error if one occurs.
func (c *FakeMemberClusterAnnounces) Delete(ctx context.Context, name string, opts v1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewDeleteActionWithOptions(memberclusterannouncesResource, c.ns, name, opts), &v1alpha1.MemberClusterAnnounce{})

	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeMemberClusterAnnounces) DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error {
	action := testing.NewDeleteCollectionAction(memberclusterannouncesResource, c.ns, listOpts)

	_, err := c.Fake.Invokes(action, &v1alpha1.MemberClusterAnnounceList{})
	return err
}

// Patch applies the patch and returns the patched memberClusterAnnounce.
func (c *FakeMemberClusterAnnounces) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *v1alpha1.MemberClusterAnnounce, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewPatchSubresourceAction(memberclusterannouncesResource, c.ns, name, pt, data, subresources...), &v1alpha1.MemberClusterAnnounce{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.MemberClusterAnnounce), err
}