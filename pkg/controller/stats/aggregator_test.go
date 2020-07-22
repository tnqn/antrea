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

package stats

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	featuregatetesting "k8s.io/component-base/featuregate/testing"

	cpv1alpha1 "github.com/vmware-tanzu/antrea/pkg/apis/controlplane/v1alpha1"
	metricsv1alpha1 "github.com/vmware-tanzu/antrea/pkg/apis/metrics/v1alpha1"
	secv1alpha1 "github.com/vmware-tanzu/antrea/pkg/apis/security/v1alpha1"
	fakeversioned "github.com/vmware-tanzu/antrea/pkg/client/clientset/versioned/fake"
	crdinformers "github.com/vmware-tanzu/antrea/pkg/client/informers/externalversions"
	"github.com/vmware-tanzu/antrea/pkg/features"
)

var (
	np1 = &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{Namespace: "foo", Name: "bar"},
	}
	np2 = &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{Namespace: "foo", Name: "boo"},
	}
	cnp1 = &secv1alpha1.ClusterNetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{Namespace: "", Name: "bar"},
	}
	cnp2 = &secv1alpha1.ClusterNetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{Namespace: "", Name: "boo"},
	}

	npRef1  = npToRef(np1)
	cnpRef1 = cnpToRef(cnp1)
	cnpRef2 = cnpToRef(cnp2)
)

func npToRef(np *networkingv1.NetworkPolicy) *cpv1alpha1.NetworkPolicyReference {
	return &cpv1alpha1.NetworkPolicyReference{
		Category:  cpv1alpha1.K8sNetworkPolicy,
		Namespace: np.Namespace,
		Name:      np.Name,
	}
}

func cnpToRef(cnp *secv1alpha1.ClusterNetworkPolicy) *cpv1alpha1.NetworkPolicyReference {
	return &cpv1alpha1.NetworkPolicyReference{
		Category:  cpv1alpha1.ClusterNetworkPolicy,
		Namespace: cnp.Namespace,
		Name:      cnp.Name,
	}
}

func TestAggregatorCollectListGet(t *testing.T) {
	tests := []struct {
		name                                string
		summaries                           []*cpv1alpha1.NodeStatsSummary
		existingNetworkPolicies             []runtime.Object
		existingClusterNetworkPolicies      []runtime.Object
		expectedNetworkPolicyMetrics        []metricsv1alpha1.NetworkPolicyMetrics
		expectedClusterNetworkPolicyMetrics []metricsv1alpha1.ClusterNetworkPolicyMetrics
	}{
		{
			name: "multiple Nodes, multiple Policies",
			summaries: []*cpv1alpha1.NodeStatsSummary{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-1",
					},
					NetworkPolicies: []cpv1alpha1.NetworkPolicyStats{
						{
							NetworkPolicy: *npRef1,
							TrafficStats: metricsv1alpha1.TrafficStats{
								Bytes:    10,
								Packets:  1,
								Sessions: 1,
							},
						},
						{
							NetworkPolicy: *cnpRef1,
							TrafficStats: metricsv1alpha1.TrafficStats{
								Bytes:    20,
								Packets:  5,
								Sessions: 2,
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-2",
					},
					NetworkPolicies: []cpv1alpha1.NetworkPolicyStats{
						{
							NetworkPolicy: *npRef1,
							TrafficStats: metricsv1alpha1.TrafficStats{
								Bytes:    20,
								Packets:  10,
								Sessions: 3,
							},
						},
						{
							NetworkPolicy: *cnpRef2,
							TrafficStats: metricsv1alpha1.TrafficStats{
								Bytes:    20,
								Packets:  8,
								Sessions: 5,
							},
						},
					},
				},
			},
			existingNetworkPolicies:        []runtime.Object{np1, np2},
			existingClusterNetworkPolicies: []runtime.Object{cnp1, cnp2},
			expectedNetworkPolicyMetrics: []metricsv1alpha1.NetworkPolicyMetrics{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      np1.Name,
						Namespace: np1.Namespace,
					},
					TrafficStats: metricsv1alpha1.TrafficStats{
						Bytes:    30,
						Packets:  11,
						Sessions: 4,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      np2.Name,
						Namespace: np2.Namespace,
					},
					TrafficStats: metricsv1alpha1.TrafficStats{
						Bytes:    0,
						Packets:  0,
						Sessions: 0,
					},
				},
			},
			expectedClusterNetworkPolicyMetrics: []metricsv1alpha1.ClusterNetworkPolicyMetrics{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: cnp1.Name,
					},
					TrafficStats: metricsv1alpha1.TrafficStats{
						Bytes:    20,
						Packets:  5,
						Sessions: 2,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: cnp2.Name,
					},
					TrafficStats: metricsv1alpha1.TrafficStats{
						Bytes:    20,
						Packets:  8,
						Sessions: 5,
					},
				},
			},
		},
		{
			name: "non existing Policies",
			summaries: []*cpv1alpha1.NodeStatsSummary{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-1",
					},
					NetworkPolicies: []cpv1alpha1.NetworkPolicyStats{
						{
							NetworkPolicy: *npRef1,
							TrafficStats: metricsv1alpha1.TrafficStats{
								Bytes:    10,
								Packets:  1,
								Sessions: 1,
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-2",
					},
					NetworkPolicies: []cpv1alpha1.NetworkPolicyStats{
						{
							NetworkPolicy: *cnpRef1,
							TrafficStats: metricsv1alpha1.TrafficStats{
								Bytes:    20,
								Packets:  8,
								Sessions: 5,
							},
						},
					},
				},
			},
			existingNetworkPolicies:        []runtime.Object{np2},
			existingClusterNetworkPolicies: []runtime.Object{cnp2},
			expectedNetworkPolicyMetrics: []metricsv1alpha1.NetworkPolicyMetrics{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      np2.Name,
						Namespace: np2.Namespace,
					},
					TrafficStats: metricsv1alpha1.TrafficStats{
						Bytes:    0,
						Packets:  0,
						Sessions: 0,
					},
				},
			},
			expectedClusterNetworkPolicyMetrics: []metricsv1alpha1.ClusterNetworkPolicyMetrics{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: cnp2.Name,
					},
					TrafficStats: metricsv1alpha1.TrafficStats{
						Bytes:    0,
						Packets:  0,
						Sessions: 0,
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer featuregatetesting.SetFeatureGateDuringTest(t, features.DefaultFeatureGate, features.AntreaPolicy, true)()

			stopCh := make(chan struct{})
			defer close(stopCh)
			client := fake.NewSimpleClientset(tt.existingNetworkPolicies...)
			informerFactory := informers.NewSharedInformerFactory(client, 12*time.Hour)
			crdClient := fakeversioned.NewSimpleClientset(tt.existingClusterNetworkPolicies...)
			crdInformerFactory := crdinformers.NewSharedInformerFactory(crdClient, 12*time.Hour)
			a := NewAggregator(informerFactory.Networking().V1().NetworkPolicies(), crdInformerFactory.Security().V1alpha1().ClusterNetworkPolicies())
			informerFactory.Start(stopCh)
			crdInformerFactory.Start(stopCh)
			go a.Run(stopCh)

			for _, nodeSummaries := range tt.summaries {
				a.Collect(nodeSummaries)
			}
			// Wait for all summaries being consumed.
			err := wait.PollImmediate(100*time.Millisecond, time.Second, func() (done bool, err error) {
				return len(a.dataCh) == 0, nil
			})
			require.NoError(t, err)
			require.Equal(t, len(tt.expectedNetworkPolicyMetrics), len(a.ListNetworkPolicyMetrics("")))
			for _, metrics := range tt.expectedNetworkPolicyMetrics {
				actualMetrics, exists := a.GetNetworkPolicyMetric(metrics.Namespace, metrics.Name)
				require.True(t, exists)
				require.Equal(t, metrics.TrafficStats, actualMetrics.TrafficStats)
			}
			assert.Equal(t, len(tt.expectedClusterNetworkPolicyMetrics), len(a.ListClusterNetworkPolicyMetrics()))
			for _, metrics := range tt.expectedClusterNetworkPolicyMetrics {
				actualMetrics, exists := a.GetClusterNetworkPolicyMetric(metrics.Name)
				require.True(t, exists)
				require.Equal(t, metrics.TrafficStats, actualMetrics.TrafficStats)
			}
		})
	}
}

func TestDeleteNetworkPolicy(t *testing.T) {
	defer featuregatetesting.SetFeatureGateDuringTest(t, features.DefaultFeatureGate, features.AntreaPolicy, true)()

	stopCh := make(chan struct{})
	defer close(stopCh)
	client := fake.NewSimpleClientset(np1)
	informerFactory := informers.NewSharedInformerFactory(client, 12*time.Hour)
	crdClient := fakeversioned.NewSimpleClientset(cnp1)
	crdInformerFactory := crdinformers.NewSharedInformerFactory(crdClient, 12*time.Hour)
	a := NewAggregator(informerFactory.Networking().V1().NetworkPolicies(), crdInformerFactory.Security().V1alpha1().ClusterNetworkPolicies())
	informerFactory.Start(stopCh)
	crdInformerFactory.Start(stopCh)
	go a.Run(stopCh)

	a.Collect(&cpv1alpha1.NodeStatsSummary{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node-1",
		},
		NetworkPolicies: []cpv1alpha1.NetworkPolicyStats{
			{
				NetworkPolicy: *npRef1,
				TrafficStats: metricsv1alpha1.TrafficStats{
					Bytes:    10,
					Packets:  1,
					Sessions: 1,
				},
			},
			{
				NetworkPolicy: *cnpRef1,
				TrafficStats: metricsv1alpha1.TrafficStats{
					Bytes:    30,
					Packets:  3,
					Sessions: 3,
				},
			},
		},
	})
	// Wait for all summaries being consumed.
	err := wait.PollImmediate(100*time.Millisecond, time.Second, func() (done bool, err error) {
		return len(a.dataCh) == 0, nil
	})
	require.NoError(t, err)
	actualNetworkPolicies := a.ListNetworkPolicyMetrics("")
	require.Equal(t, 1, len(actualNetworkPolicies))
	assert.Equal(t, metricsv1alpha1.TrafficStats{
		Bytes:    10,
		Packets:  1,
		Sessions: 1,
	}, actualNetworkPolicies[0].TrafficStats)
	actualClusterNetworkPolicies := a.ListClusterNetworkPolicyMetrics()
	require.Equal(t, 1, len(actualClusterNetworkPolicies))
	assert.Equal(t, metricsv1alpha1.TrafficStats{
		Bytes:    30,
		Packets:  3,
		Sessions: 3,
	}, actualClusterNetworkPolicies[0].TrafficStats)

	client.NetworkingV1().NetworkPolicies(np1.Namespace).Delete(context.TODO(), np1.Name, metav1.DeleteOptions{})
	crdClient.SecurityV1alpha1().ClusterNetworkPolicies().Delete(context.TODO(), cnp1.Name, metav1.DeleteOptions{})
	// Event handlers are asynchronous, it's supposed to finish very soon.
	err = wait.PollImmediate(100*time.Millisecond, time.Second, func() (done bool, err error) {
		return len(a.ListNetworkPolicyMetrics("")) == 0 && len(a.ListClusterNetworkPolicyMetrics()) == 0, nil
	})
	assert.NoError(t, err)
}
