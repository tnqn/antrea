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

package v1alpha1

import (
	"context"
	"time"

	v1alpha1 "antrea.io/antrea/pkg/apis/crd/v1alpha1"
	scheme "antrea.io/antrea/pkg/client/clientset/versioned/scheme"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	rest "k8s.io/client-go/rest"
)

// TrafficControlConfigurationsGetter has a method to return a TrafficControlConfigurationInterface.
// A group's client should implement this interface.
type TrafficControlConfigurationsGetter interface {
	TrafficControlConfigurations() TrafficControlConfigurationInterface
}

// TrafficControlConfigurationInterface has methods to work with TrafficControlConfiguration resources.
type TrafficControlConfigurationInterface interface {
	Create(ctx context.Context, trafficControlConfiguration *v1alpha1.TrafficControlConfiguration, opts v1.CreateOptions) (*v1alpha1.TrafficControlConfiguration, error)
	Update(ctx context.Context, trafficControlConfiguration *v1alpha1.TrafficControlConfiguration, opts v1.UpdateOptions) (*v1alpha1.TrafficControlConfiguration, error)
	Delete(ctx context.Context, name string, opts v1.DeleteOptions) error
	DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error
	Get(ctx context.Context, name string, opts v1.GetOptions) (*v1alpha1.TrafficControlConfiguration, error)
	List(ctx context.Context, opts v1.ListOptions) (*v1alpha1.TrafficControlConfigurationList, error)
	Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error)
	Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *v1alpha1.TrafficControlConfiguration, err error)
	TrafficControlConfigurationExpansion
}

// trafficControlConfigurations implements TrafficControlConfigurationInterface
type trafficControlConfigurations struct {
	client rest.Interface
}

// newTrafficControlConfigurations returns a TrafficControlConfigurations
func newTrafficControlConfigurations(c *CrdV1alpha1Client) *trafficControlConfigurations {
	return &trafficControlConfigurations{
		client: c.RESTClient(),
	}
}

// Get takes name of the trafficControlConfiguration, and returns the corresponding trafficControlConfiguration object, and an error if there is any.
func (c *trafficControlConfigurations) Get(ctx context.Context, name string, options v1.GetOptions) (result *v1alpha1.TrafficControlConfiguration, err error) {
	result = &v1alpha1.TrafficControlConfiguration{}
	err = c.client.Get().
		Resource("trafficcontrolconfigurations").
		Name(name).
		VersionedParams(&options, scheme.ParameterCodec).
		Do(ctx).
		Into(result)
	return
}

// List takes label and field selectors, and returns the list of TrafficControlConfigurations that match those selectors.
func (c *trafficControlConfigurations) List(ctx context.Context, opts v1.ListOptions) (result *v1alpha1.TrafficControlConfigurationList, err error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	result = &v1alpha1.TrafficControlConfigurationList{}
	err = c.client.Get().
		Resource("trafficcontrolconfigurations").
		VersionedParams(&opts, scheme.ParameterCodec).
		Timeout(timeout).
		Do(ctx).
		Into(result)
	return
}

// Watch returns a watch.Interface that watches the requested trafficControlConfigurations.
func (c *trafficControlConfigurations) Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	opts.Watch = true
	return c.client.Get().
		Resource("trafficcontrolconfigurations").
		VersionedParams(&opts, scheme.ParameterCodec).
		Timeout(timeout).
		Watch(ctx)
}

// Create takes the representation of a trafficControlConfiguration and creates it.  Returns the server's representation of the trafficControlConfiguration, and an error, if there is any.
func (c *trafficControlConfigurations) Create(ctx context.Context, trafficControlConfiguration *v1alpha1.TrafficControlConfiguration, opts v1.CreateOptions) (result *v1alpha1.TrafficControlConfiguration, err error) {
	result = &v1alpha1.TrafficControlConfiguration{}
	err = c.client.Post().
		Resource("trafficcontrolconfigurations").
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(trafficControlConfiguration).
		Do(ctx).
		Into(result)
	return
}

// Update takes the representation of a trafficControlConfiguration and updates it. Returns the server's representation of the trafficControlConfiguration, and an error, if there is any.
func (c *trafficControlConfigurations) Update(ctx context.Context, trafficControlConfiguration *v1alpha1.TrafficControlConfiguration, opts v1.UpdateOptions) (result *v1alpha1.TrafficControlConfiguration, err error) {
	result = &v1alpha1.TrafficControlConfiguration{}
	err = c.client.Put().
		Resource("trafficcontrolconfigurations").
		Name(trafficControlConfiguration.Name).
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(trafficControlConfiguration).
		Do(ctx).
		Into(result)
	return
}

// Delete takes name of the trafficControlConfiguration and deletes it. Returns an error if one occurs.
func (c *trafficControlConfigurations) Delete(ctx context.Context, name string, opts v1.DeleteOptions) error {
	return c.client.Delete().
		Resource("trafficcontrolconfigurations").
		Name(name).
		Body(&opts).
		Do(ctx).
		Error()
}

// DeleteCollection deletes a collection of objects.
func (c *trafficControlConfigurations) DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error {
	var timeout time.Duration
	if listOpts.TimeoutSeconds != nil {
		timeout = time.Duration(*listOpts.TimeoutSeconds) * time.Second
	}
	return c.client.Delete().
		Resource("trafficcontrolconfigurations").
		VersionedParams(&listOpts, scheme.ParameterCodec).
		Timeout(timeout).
		Body(&opts).
		Do(ctx).
		Error()
}

// Patch applies the patch and returns the patched trafficControlConfiguration.
func (c *trafficControlConfigurations) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *v1alpha1.TrafficControlConfiguration, err error) {
	result = &v1alpha1.TrafficControlConfiguration{}
	err = c.client.Patch(pt).
		Resource("trafficcontrolconfigurations").
		Name(name).
		SubResource(subresources...).
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(data).
		Do(ctx).
		Into(result)
	return
}
