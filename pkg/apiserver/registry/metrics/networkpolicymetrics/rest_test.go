// Copyright 2020 Antrea Authors
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

package networkpolicymetrics

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/endpoints/request"
	featuregatetesting "k8s.io/component-base/featuregate/testing"

	metricsv1alpha1 "github.com/vmware-tanzu/antrea/pkg/apis/metrics/v1alpha1"
	"github.com/vmware-tanzu/antrea/pkg/features"
)

type fakeMetricsProvider struct {
	metrics map[string]map[string]metricsv1alpha1.NetworkPolicyMetrics
}

func (p *fakeMetricsProvider) ListNetworkPolicyMetrics(namespace string) []metricsv1alpha1.NetworkPolicyMetrics {
	var list []metricsv1alpha1.NetworkPolicyMetrics
	if namespace == "" {
		for _, m1 := range p.metrics {
			for _, m2 := range m1 {
				list = append(list, m2)
			}
		}
	} else {
		m1, _ := p.metrics[namespace]
		for _, m2 := range m1 {
			list = append(list, m2)
		}
	}
	return list
}

func (p *fakeMetricsProvider) GetNetworkPolicyMetrics(namespace, name string) (*metricsv1alpha1.NetworkPolicyMetrics, bool) {
	m, exists := p.metrics[namespace][name]
	if !exists {
		return nil, false
	}
	return &m, true
}

func TestRESTGet(t *testing.T) {
	tests := []struct {
		name                        string
		networkPolicyMetricsEnabled bool
		metrics                     map[string]map[string]metricsv1alpha1.NetworkPolicyMetrics
		npNamespace                 string
		npName                      string
		expectedObj                 runtime.Object
		expectedErr                 bool
	}{
		{
			name:                        "NetworkPolicyMetrics feature disabled",
			networkPolicyMetricsEnabled: false,
			expectedErr:                 true,
		},
		{
			name:                        "np not found",
			networkPolicyMetricsEnabled: true,
			metrics: map[string]map[string]metricsv1alpha1.NetworkPolicyMetrics{
				"foo": {
					"bar": {
						ObjectMeta: metav1.ObjectMeta{
							Namespace: "foo",
							Name:      "foo",
						},
					},
				},
			},
			npNamespace: "non existing namespace",
			npName:      "non existing name",
			expectedErr: true,
		},
		{
			name:                        "np found",
			networkPolicyMetricsEnabled: true,
			metrics: map[string]map[string]metricsv1alpha1.NetworkPolicyMetrics{
				"foo": {
					"bar": {
						ObjectMeta: metav1.ObjectMeta{
							Namespace: "foo",
							Name:      "bar",
						},
					},
				},
			},
			npNamespace: "foo",
			npName:      "bar",
			expectedObj: &metricsv1alpha1.NetworkPolicyMetrics{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "foo",
					Name:      "bar",
				},
			},
			expectedErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer featuregatetesting.SetFeatureGateDuringTest(t, features.DefaultFeatureGate, features.NetworkPolicyMetrics, tt.networkPolicyMetricsEnabled)()

			r := &REST{
				metricsProvider: &fakeMetricsProvider{metrics: tt.metrics},
			}
			ctx := request.WithNamespace(context.TODO(), tt.npNamespace)
			actualObj, err := r.Get(ctx, tt.npName, &metav1.GetOptions{})
			if tt.expectedErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.expectedObj, actualObj)
		})
	}
}

func TestRESTList(t *testing.T) {
	tests := []struct {
		name                        string
		networkPolicyMetricsEnabled bool
		metrics                     map[string]map[string]metricsv1alpha1.NetworkPolicyMetrics
		npNamespace                 string
		expectedObj                 runtime.Object
		expectedErr                 bool
	}{
		{
			name:                        "NetworkPolicyMetrics feature disabled",
			networkPolicyMetricsEnabled: false,
			expectedErr:                 true,
		},
		{
			name:                        "all namespaces",
			networkPolicyMetricsEnabled: true,
			metrics: map[string]map[string]metricsv1alpha1.NetworkPolicyMetrics{
				"foo": {
					"bar": {
						ObjectMeta: metav1.ObjectMeta{
							Namespace: "foo",
							Name:      "bar",
						},
					},
				},
				"foo1": {
					"bar1": {
						ObjectMeta: metav1.ObjectMeta{
							Namespace: "foo1",
							Name:      "bar1",
						},
					},
				},
			},
			npNamespace: "",
			expectedObj: &metricsv1alpha1.NetworkPolicyMetricsList{
				Items: []metricsv1alpha1.NetworkPolicyMetrics{
					{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: "foo",
							Name:      "bar",
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: "foo1",
							Name:      "bar1",
						},
					},
				},
			},
			expectedErr: false,
		},
		{
			name:                        "one namespace",
			networkPolicyMetricsEnabled: true,
			metrics: map[string]map[string]metricsv1alpha1.NetworkPolicyMetrics{
				"foo": {
					"bar": {
						ObjectMeta: metav1.ObjectMeta{
							Namespace: "foo",
							Name:      "bar",
						},
					},
				},
				"foo1": {
					"bar1": {
						ObjectMeta: metav1.ObjectMeta{
							Namespace: "foo1",
							Name:      "bar1",
						},
					},
				},
			},
			npNamespace: "foo",
			expectedObj: &metricsv1alpha1.NetworkPolicyMetricsList{
				Items: []metricsv1alpha1.NetworkPolicyMetrics{
					{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: "foo",
							Name:      "bar",
						},
					},
				},
			},
			expectedErr: false,
		},
		{
			name:                        "empty metrics",
			networkPolicyMetricsEnabled: true,
			metrics:                     map[string]map[string]metricsv1alpha1.NetworkPolicyMetrics{},
			npNamespace:                 "",
			expectedObj: &metricsv1alpha1.NetworkPolicyMetricsList{
				Items: []metricsv1alpha1.NetworkPolicyMetrics{},
			},
			expectedErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer featuregatetesting.SetFeatureGateDuringTest(t, features.DefaultFeatureGate, features.NetworkPolicyMetrics, tt.networkPolicyMetricsEnabled)()

			r := &REST{
				metricsProvider: &fakeMetricsProvider{metrics: tt.metrics},
			}
			ctx := request.WithNamespace(context.TODO(), tt.npNamespace)
			actualObj, err := r.List(ctx, &internalversion.ListOptions{})
			if tt.expectedErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			if tt.expectedObj == nil {
				assert.Nil(t, actualObj)
			} else {
				assert.ElementsMatch(t, tt.expectedObj.(*metricsv1alpha1.NetworkPolicyMetricsList).Items, actualObj.(*metricsv1alpha1.NetworkPolicyMetricsList).Items)
			}
		})
	}
}
