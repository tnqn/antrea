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

package egress

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	admv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"

	crdv1alpha2 "antrea.io/antrea/pkg/apis/crd/v1alpha2"
)

func marshal(object runtime.Object) []byte {
	raw, _ := json.Marshal(object)
	return raw
}

func TestEgressControllerValidateEgress(t *testing.T) {
	tests := []struct {
		name                    string
		existingExternalIPPools []*crdv1alpha2.ExternalIPPool
		request                 *admv1.AdmissionRequest
		expectedResponse        *admv1.AdmissionResponse
	}{
		{
			name:                    "Requesting IP from non-existing ExternalIPPool should not be allowed",
			existingExternalIPPools: nil,
			request: &admv1.AdmissionRequest{
				Name:      "foo",
				Operation: "CREATE",
				Object:    runtime.RawExtension{Raw: marshal(newEgress("foo", "10.10.10.1", "nonExistingPool", nil, nil))},
			},
			expectedResponse: &admv1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Message: "ExternalIPPool nonExistingPool does not exist",
				},
			},
		},
		{
			name: "Requesting IP from non-existing ExternalIPPool should not be allowed[multi-Pools]",
			existingExternalIPPools: []*crdv1alpha2.ExternalIPPool{
				newExternalIPPool("bar", "10.10.10.0/24", "", ""),
			},
			request: &admv1.AdmissionRequest{
				Name:      "foo",
				Operation: "CREATE",
				Object:    runtime.RawExtension{Raw: marshal(newEgressWithMultiExternalIPPools("foo", "", "", []string{"1.1.1.1", "10.10.10.1"}, []string{"nonExistingPool", "bar"}, nil, nil))},
			},
			expectedResponse: &admv1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Message: "ExternalIPPool nonExistingPool does not exist",
				},
			},
		},
		{
			name:                    "Requesting IP out of range should not be allowed",
			existingExternalIPPools: []*crdv1alpha2.ExternalIPPool{newExternalIPPool("bar", "10.10.10.0/24", "", "")},
			request: &admv1.AdmissionRequest{
				Name:      "foo",
				Operation: "CREATE",
				Object:    runtime.RawExtension{Raw: marshal(newEgress("foo", "10.10.11.1", "bar", nil, nil))},
			},
			expectedResponse: &admv1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Message: "IP 10.10.11.1 is not within the IP range",
				},
			},
		},
		{
			name: "Requesting IP out of range should not be allowed[multi-pools]",
			existingExternalIPPools: []*crdv1alpha2.ExternalIPPool{
				newExternalIPPool("bar", "10.10.10.0/24", "", ""),
				newExternalIPPool("bar1", "20.20.20.0/24", "", ""),
			},
			request: &admv1.AdmissionRequest{
				Name:      "foo",
				Operation: "CREATE",
				Object:    runtime.RawExtension{Raw: marshal(newEgressWithMultiExternalIPPools("foo", "", "", []string{"10.10.11.1", "20.20.20.1"}, []string{"bar", "bar1"}, nil, nil))},
			},
			expectedResponse: &admv1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Message: "IP 10.10.11.1 is not within the IP range",
				},
			},
		},
		{
			name:                    "Requesting normal IP should be allowed",
			existingExternalIPPools: []*crdv1alpha2.ExternalIPPool{newExternalIPPool("bar", "10.10.10.0/24", "", "")},
			request: &admv1.AdmissionRequest{
				Name:      "foo",
				Operation: "CREATE",
				Object:    runtime.RawExtension{Raw: marshal(newEgress("foo", "10.10.10.1", "bar", nil, nil))},
			},
			expectedResponse: &admv1.AdmissionResponse{Allowed: true},
		},
		{
			name:                    "Requesting EgressIPs nums larger than ExternalIPPools should not be allowed[multi-pools]",
			existingExternalIPPools: []*crdv1alpha2.ExternalIPPool{newExternalIPPool("bar", "10.10.10.0/24", "", "")},
			request: &admv1.AdmissionRequest{
				Name:      "foo",
				Operation: "CREATE",
				Object:    runtime.RawExtension{Raw: marshal(newEgressWithMultiExternalIPPools("foo", "", "", []string{"10.10.10.1", "2.2.2.2"}, []string{"bar"}, nil, nil))},
			},
			expectedResponse: &admv1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Message: "IPs number [10.10.10.1 2.2.2.2] is larger than ExternalIPPools [bar] number",
				},
			},
		},
		{
			name:                    "Requesting ExternalIPPools is empty and EgressIPs num larger than 1 should not be allowed[multi-pools]",
			existingExternalIPPools: nil,
			request: &admv1.AdmissionRequest{
				Name:      "foo",
				Operation: "CREATE",
				Object:    runtime.RawExtension{Raw: marshal(newEgressWithMultiExternalIPPools("foo", "", "", []string{"10.10.10.1", "2.2.2.2"}, nil, nil, nil))},
			},
			expectedResponse: &admv1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Message: "Invalid EgressIPs [10.10.10.1 2.2.2.2], only one EgressIP in EgressIPs is supported while ExternalIPPools num is 0",
				},
			},
		},
		{
			name: "Requesting normal Egress with multiple ExternalIPPools should be allowed[multi-pools]",
			existingExternalIPPools: []*crdv1alpha2.ExternalIPPool{
				newExternalIPPool("bar", "10.10.10.0/24", "", ""),
				newExternalIPPool("bar1", "20.20.20.0/24", "", ""),
			},
			request: &admv1.AdmissionRequest{
				Name:      "foo",
				Operation: "CREATE",
				Object:    runtime.RawExtension{Raw: marshal(newEgressWithMultiExternalIPPools("foo", "", "", []string{"10.10.10.1", "20.20.20.1"}, []string{"bar", "bar1"}, nil, nil))},
			},
			expectedResponse: &admv1.AdmissionResponse{Allowed: true},
		},
		{
			name: "Requesting Egress with multiple ExternalIPPools num larger than EgressIPs num should be allowed[multi-pools]",
			existingExternalIPPools: []*crdv1alpha2.ExternalIPPool{
				newExternalIPPool("bar", "10.10.10.0/24", "", ""),
				newExternalIPPool("bar1", "20.20.20.0/24", "", ""),
			},
			request: &admv1.AdmissionRequest{
				Name:      "foo",
				Operation: "CREATE",
				Object:    runtime.RawExtension{Raw: marshal(newEgressWithMultiExternalIPPools("foo", "", "", []string{"10.10.10.1"}, []string{"bar", "bar1"}, nil, nil))},
			},
			expectedResponse: &admv1.AdmissionResponse{Allowed: true},
		},
		{
			name: "Requesting Egress with multiple ExternalIPPools and nil EgressIPs should be allowed[multi-pools]",
			existingExternalIPPools: []*crdv1alpha2.ExternalIPPool{
				newExternalIPPool("bar", "10.10.10.0/24", "", ""),
				newExternalIPPool("bar1", "20.20.20.0/24", "", ""),
			},
			request: &admv1.AdmissionRequest{
				Name:      "foo",
				Operation: "CREATE",
				Object:    runtime.RawExtension{Raw: marshal(newEgressWithMultiExternalIPPools("foo", "", "", nil, []string{"bar", "bar1"}, nil, nil))},
			},
			expectedResponse: &admv1.AdmissionResponse{Allowed: true},
		},
		{
			name: "Requesting Egress with multiple ExternalIPPools(with '' pool) and nil EgressIPs should not be allowed[multi-pools]",
			existingExternalIPPools: []*crdv1alpha2.ExternalIPPool{
				newExternalIPPool("bar", "10.10.10.0/24", "", ""),
				newExternalIPPool("bar1", "20.20.20.0/24", "", ""),
			},
			request: &admv1.AdmissionRequest{
				Name:      "foo",
				Operation: "CREATE",
				Object:    runtime.RawExtension{Raw: marshal(newEgressWithMultiExternalIPPools("foo", "", "", nil, []string{"bar", "bar1", ""}, nil, nil))},
			},
			expectedResponse: &admv1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Message: "Invalid ExternalIPPool: ",
				},
			},
		},
		{
			name: "Requesting Egress with multiple ExternalIPPools(with duplicate pool name) and nil EgressIPs should not be allowed[multi-pools]",
			existingExternalIPPools: []*crdv1alpha2.ExternalIPPool{
				newExternalIPPool("bar", "10.10.10.0/24", "", ""),
			},
			request: &admv1.AdmissionRequest{
				Name:      "foo",
				Operation: "CREATE",
				Object:    runtime.RawExtension{Raw: marshal(newEgressWithMultiExternalIPPools("foo", "", "", nil, []string{"bar", "bar"}, nil, nil))},
			},
			expectedResponse: &admv1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Message: "Duplicate ExternalIPPool bar in ExternalIPPools",
				},
			},
		},
		{
			name:                    "Updating EgressIP to invalid one should not be allowed",
			existingExternalIPPools: []*crdv1alpha2.ExternalIPPool{newExternalIPPool("bar", "10.10.10.0/24", "", "")},
			request: &admv1.AdmissionRequest{
				Name:      "foo",
				Operation: "UPDATE",
				OldObject: runtime.RawExtension{Raw: marshal(newEgress("foo", "10.10.10.1", "bar", nil, nil))},
				Object:    runtime.RawExtension{Raw: marshal(newEgress("foo", "10.10.11.1", "bar", nil, nil))},
			},
			expectedResponse: &admv1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Message: "IP 10.10.11.1 is not within the IP range",
				},
			},
		},
		{
			name:                    "Updating EgressIP to valid one should be allowed",
			existingExternalIPPools: []*crdv1alpha2.ExternalIPPool{newExternalIPPool("bar", "10.10.10.0/24", "", "")},
			request: &admv1.AdmissionRequest{
				Name:      "foo",
				Operation: "UPDATE",
				OldObject: runtime.RawExtension{Raw: marshal(newEgress("foo", "10.10.10.1", "bar", nil, nil))},
				Object:    runtime.RawExtension{Raw: marshal(newEgress("foo", "10.10.10.2", "bar", nil, nil))},
			},
			expectedResponse: &admv1.AdmissionResponse{Allowed: true},
		},
		{
			name:                    "Updating podSelector should be allowed",
			existingExternalIPPools: []*crdv1alpha2.ExternalIPPool{newExternalIPPool("bar", "10.10.10.0/24", "", "")},
			request: &admv1.AdmissionRequest{
				Name:      "foo",
				Operation: "UPDATE",
				OldObject: runtime.RawExtension{Raw: marshal(newEgress("foo", "10.10.10.1", "bar", nil, nil))},
				Object: runtime.RawExtension{Raw: marshal(newEgress("foo", "10.10.10.2", "bar", &metav1.LabelSelector{
					MatchLabels: map[string]string{"foo": "bar"},
				}, nil))},
			},
			expectedResponse: &admv1.AdmissionResponse{Allowed: true},
		},
		{
			name: "DELETE operation should be allowed",
			request: &admv1.AdmissionRequest{
				Name:      "foo",
				Operation: "DELETE",
				Object:    runtime.RawExtension{Raw: marshal(newEgress("foo", "10.10.10.2", "bar", nil, nil))},
			},
			expectedResponse: &admv1.AdmissionResponse{Allowed: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stopCh := make(chan struct{})
			defer close(stopCh)
			var objs []runtime.Object
			for _, pool := range tt.existingExternalIPPools {
				objs = append(objs, pool)
			}
			controller := newController(nil, objs)
			controller.informerFactory.Start(stopCh)
			controller.crdInformerFactory.Start(stopCh)
			controller.informerFactory.WaitForCacheSync(stopCh)
			controller.crdInformerFactory.WaitForCacheSync(stopCh)
			go controller.externalIPAllocator.Run(stopCh)
			require.True(t, cache.WaitForCacheSync(stopCh, controller.externalIPAllocator.HasSynced))
			controller.externalIPAllocator.RestoreIPAllocations(nil)
			review := &admv1.AdmissionReview{
				Request: tt.request,
			}
			gotResponse := controller.ValidateEgress(review)
			assert.Equal(t, tt.expectedResponse, gotResponse)
		})
	}
}
