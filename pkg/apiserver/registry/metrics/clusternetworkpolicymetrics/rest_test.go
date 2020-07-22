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

package clusternetworkpolicymetrics

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	featuregatetesting "k8s.io/component-base/featuregate/testing"

	metricsv1alpha1 "github.com/vmware-tanzu/antrea/pkg/apis/metrics/v1alpha1"
	"github.com/vmware-tanzu/antrea/pkg/features"
)

type fakeMetricsProvider struct {
	metrics map[string]metricsv1alpha1.ClusterNetworkPolicyMetrics
}

func (p *fakeMetricsProvider) ListClusterNetworkPolicyMetrics() []metricsv1alpha1.ClusterNetworkPolicyMetrics {
	list := make([]metricsv1alpha1.ClusterNetworkPolicyMetrics, 0, len(p.metrics))
	for _, m := range p.metrics {
		list = append(list, m)
	}
	return list
}

func (p *fakeMetricsProvider) GetClusterNetworkPolicyMetric(name string) (*metricsv1alpha1.ClusterNetworkPolicyMetrics, bool) {
	m, exists := p.metrics[name]
	if !exists {
		return nil, false
	}
	return &m, true
}

func TestRESTGet(t *testing.T) {
	tests := []struct {
		name                        string
		networkPolicyMetricsEnabled bool
		antreaPolicyEnabled         bool
		metrics                     map[string]metricsv1alpha1.ClusterNetworkPolicyMetrics
		cnp                         string
		expectedObj                 runtime.Object
		expectedErr                 bool
	}{
		{
			name:                        "NetworkPolicyMetrics feature disabled",
			networkPolicyMetricsEnabled: false,
			antreaPolicyEnabled:         true,
			expectedErr:                 true,
		},
		{
			name:                        "AntreaPolicy feature disabled",
			networkPolicyMetricsEnabled: true,
			antreaPolicyEnabled:         false,
			expectedErr:                 true,
		},
		{
			name:                        "cnp not found",
			networkPolicyMetricsEnabled: true,
			antreaPolicyEnabled:         true,
			metrics: map[string]metricsv1alpha1.ClusterNetworkPolicyMetrics{
				"foo": {
					ObjectMeta: metav1.ObjectMeta{
						Name: "foo",
					},
				},
			},
			cnp:         "bar",
			expectedErr: true,
		},
		{
			name:                        "cnp found",
			networkPolicyMetricsEnabled: true,
			antreaPolicyEnabled:         true,
			metrics: map[string]metricsv1alpha1.ClusterNetworkPolicyMetrics{
				"foo": {
					ObjectMeta: metav1.ObjectMeta{
						Name: "foo",
					},
				},
			},
			cnp: "foo",
			expectedObj: &metricsv1alpha1.ClusterNetworkPolicyMetrics{
				ObjectMeta: metav1.ObjectMeta{
					Name: "foo",
				},
			},
			expectedErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer featuregatetesting.SetFeatureGateDuringTest(t, features.DefaultFeatureGate, features.NetworkPolicyMetrics, tt.networkPolicyMetricsEnabled)()
			defer featuregatetesting.SetFeatureGateDuringTest(t, features.DefaultFeatureGate, features.AntreaPolicy, tt.antreaPolicyEnabled)()

			r := &REST{
				metricsProvider: &fakeMetricsProvider{metrics: tt.metrics},
			}
			actualObj, err := r.Get(context.TODO(), tt.cnp, &metav1.GetOptions{})
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
		antreaPolicyEnabled         bool
		metrics                     map[string]metricsv1alpha1.ClusterNetworkPolicyMetrics
		expectedObj                 runtime.Object
		expectedErr                 bool
	}{
		{
			name:                        "NetworkPolicyMetrics feature disabled",
			networkPolicyMetricsEnabled: false,
			antreaPolicyEnabled:         true,
			expectedErr:                 true,
		},
		{
			name:                        "AntreaPolicy feature disabled",
			networkPolicyMetricsEnabled: true,
			antreaPolicyEnabled:         false,
			expectedErr:                 true,
		},
		{
			name:                        "empty metrics",
			networkPolicyMetricsEnabled: true,
			antreaPolicyEnabled:         true,
			metrics:                     map[string]metricsv1alpha1.ClusterNetworkPolicyMetrics{},
			expectedObj: &metricsv1alpha1.ClusterNetworkPolicyMetricsList{
				Items: []metricsv1alpha1.ClusterNetworkPolicyMetrics{},
			},
			expectedErr: false,
		},
		{
			name:                        "a few metrics",
			networkPolicyMetricsEnabled: true,
			antreaPolicyEnabled:         true,
			metrics: map[string]metricsv1alpha1.ClusterNetworkPolicyMetrics{
				"foo": {
					ObjectMeta: metav1.ObjectMeta{
						Name: "foo",
					},
				},
				"bar": {
					ObjectMeta: metav1.ObjectMeta{
						Name: "bar",
					},
				},
			},
			expectedObj: &metricsv1alpha1.ClusterNetworkPolicyMetricsList{
				Items: []metricsv1alpha1.ClusterNetworkPolicyMetrics{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "foo",
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "bar",
						},
					},
				},
			},
			expectedErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer featuregatetesting.SetFeatureGateDuringTest(t, features.DefaultFeatureGate, features.NetworkPolicyMetrics, tt.networkPolicyMetricsEnabled)()
			defer featuregatetesting.SetFeatureGateDuringTest(t, features.DefaultFeatureGate, features.AntreaPolicy, tt.antreaPolicyEnabled)()

			r := &REST{
				metricsProvider: &fakeMetricsProvider{metrics: tt.metrics},
			}
			actualObj, err := r.List(context.TODO(), &internalversion.ListOptions{})
			if tt.expectedErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			if tt.expectedObj == nil {
				assert.Nil(t, actualObj)
			} else {
				assert.ElementsMatch(t, tt.expectedObj.(*metricsv1alpha1.ClusterNetworkPolicyMetricsList).Items, actualObj.(*metricsv1alpha1.ClusterNetworkPolicyMetricsList).Items)
			}
		})
	}
}
