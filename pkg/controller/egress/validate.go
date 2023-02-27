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
	"fmt"
	"k8s.io/apimachinery/pkg/util/sets"
	"net"
	"reflect"

	admv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	crdv1alpha2 "antrea.io/antrea/pkg/apis/crd/v1alpha2"
)

func (c *EgressController) ValidateEgress(review *admv1.AdmissionReview) *admv1.AdmissionResponse {
	var result *metav1.Status
	var msg string
	allowed := true

	klog.V(2).Info("Validating Egress", "request", review.Request)
	var newObj, oldObj crdv1alpha2.Egress
	if review.Request.Object.Raw != nil {
		if err := json.Unmarshal(review.Request.Object.Raw, &newObj); err != nil {
			klog.ErrorS(err, "Error de-serializing current Egress")
			return newAdmissionResponseForErr(err)
		}
	}
	if review.Request.OldObject.Raw != nil {
		if err := json.Unmarshal(review.Request.OldObject.Raw, &oldObj); err != nil {
			klog.ErrorS(err, "Error de-serializing old Egress")
			return newAdmissionResponseForErr(err)
		}
	}

	shouldAllow := func(oldEgress, newEgress *crdv1alpha2.Egress) (bool, string) {
		// Allow it if EgressIP and ExternalIPPool don't change.
		if newEgress.Spec.EgressIP == oldEgress.Spec.EgressIP &&
			newEgress.Spec.ExternalIPPool == oldEgress.Spec.ExternalIPPool &&
			reflect.DeepEqual(newEgress.Spec.EgressIPs, oldEgress.Spec.EgressIPs) &&
			reflect.DeepEqual(newEgress.Spec.ExternalIPPools, oldEgress.Spec.ExternalIPPools) {
			return true, ""
		}
		checkIPAndPool := func(ipStr, pool string) (bool, string) {
			ip := net.ParseIP(ipStr)
			if ip == nil {
				return false, fmt.Sprintf("IP %s is not valid", ipStr)
			}
			if !c.externalIPAllocator.IPPoolExists(pool) {
				return false, fmt.Sprintf("ExternalIPPool %s does not exist", pool)
			}
			if !c.externalIPAllocator.IPPoolHasIP(pool, ip) {
				return false, fmt.Sprintf("IP %s is not within the IP range of ExternalIPPool %s", ipStr, pool)
			}
			return true, ""
		}
		singleEgressIP := newEgress.Spec.EgressIP != "" || newEgress.Spec.ExternalIPPool != ""
		if singleEgressIP {
			// Only validate whether the specified Egress IP is in the Pool when they are both set.
			if newEgress.Spec.EgressIP == "" || newEgress.Spec.ExternalIPPool == "" {
				return true, ""
			}
			if allowed, message := checkIPAndPool(newEgress.Spec.EgressIP, newEgress.Spec.ExternalIPPool); !allowed {
				return false, message
			}
		} else {
			if len(newEgress.Spec.ExternalIPPools) == 0 {
				return false, fmt.Sprintf("EgressIP, ExternalIPPool, and ExternalIPPools must not be empty at the same time")
			}
			if len(newEgress.Spec.EgressIPs) > len(newEgress.Spec.ExternalIPPools) {
				return false, fmt.Sprintf("The count of EgressIPs %d must not be greater than the count of ExternalIPPools %d", len(newEgress.Spec.EgressIPs), len(newEgress.Spec.ExternalIPPools))
			}
			visitedPools := sets.NewString()
			for i, pool := range newEgress.Spec.ExternalIPPools {
				if pool == "" {
					return false, fmt.Sprintf("The items of ExternalIPPools must not be empty")
				}
				if visitedPools.Has(pool) {
					return false, fmt.Sprintf("The items of ExternalIPPools must be unique")
				}
				visitedPools.Insert(pool)
				if len(newEgress.Spec.EgressIPs) <= i {
					continue
				}
				ipStr := newEgress.Spec.EgressIPs[i]
				// Allow empty IP in EgressIPs as IP allocation may fail for some pools but succeed for other pools.
				if ipStr == "" {
					continue
				}
				if allowed, message := checkIPAndPool(ipStr, pool); !allowed {
					return false, message
				}
			}
		}
		return true, ""
	}

	switch review.Request.Operation {
	case admv1.Create:
		klog.V(2).Info("Validating CREATE request for Egress")
		allowed, msg = shouldAllow(&oldObj, &newObj)
	case admv1.Update:
		klog.V(2).Info("Validating UPDATE request for Egress")
		allowed, msg = shouldAllow(&oldObj, &newObj)
	case admv1.Delete:
		// This shouldn't happen with the webhook configuration we include in the Antrea YAML manifests.
		klog.V(2).Info("Validating DELETE request for Egress")
		// Always allow DELETE request.
	}

	if msg != "" {
		result = &metav1.Status{
			Message: msg,
		}
	}
	return &admv1.AdmissionResponse{
		Allowed: allowed,
		Result:  result,
	}
}

func newAdmissionResponseForErr(err error) *admv1.AdmissionResponse {
	return &admv1.AdmissionResponse{
		Result: &metav1.Status{
			Message: err.Error(),
		},
	}
}
