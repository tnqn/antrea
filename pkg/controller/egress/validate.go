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
		poolsValidate := func(pools []string) bool {
			specEgressIPs := make(map[string]bool)
			for _, pool := range pools {
				if pool == "" {
					return false
				}
				if specEgressIPs[pool] {
					return false
				}
				specEgressIPs[pool] = true
			}
			return true
		}
		// Only validate whether the specified Egress IP is in the Pool when they are both set.
		if (newEgress.Spec.EgressIP == "" || newEgress.Spec.ExternalIPPool == "") && ((len(newEgress.Spec.EgressIPs) == 0 && poolsValidate(newEgress.Spec.ExternalIPPools)) || (len(newEgress.Spec.ExternalIPPools) == 0 && len(newEgress.Spec.EgressIPs) == 1)) {
			return true, ""
		}
		specEgressIPs := make(map[string]string)
		defineMultiExternalIPPool := false
		if newEgress.Spec.ExternalIPPool != "" {
			specEgressIPs[newEgress.Spec.ExternalIPPool] = newEgress.Spec.EgressIP
		} else {
			defineMultiExternalIPPool = true
			if len(newEgress.Spec.ExternalIPPools) == 0 {
				return false, fmt.Sprintf("Invalid EgressIPs %+v, only one EgressIP in EgressIPs is supported while ExternalIPPools num is 0", newEgress.Spec.EgressIPs)
			}
			if len(newEgress.Spec.EgressIPs) > len(newEgress.Spec.ExternalIPPools) {
				return false, fmt.Sprintf("IPs number %+v is larger than ExternalIPPools %+v number", newEgress.Spec.EgressIPs, newEgress.Spec.ExternalIPPools)
			}
			for i, pool := range newEgress.Spec.ExternalIPPools {
				if pool == "" {
					return false, fmt.Sprintf("Invalid ExternalIPPool: %s", pool)
				}
				if _, has := specEgressIPs[pool]; has {
					return false, fmt.Sprintf("Duplicate ExternalIPPool %s in ExternalIPPools", pool)
				}
				if i < len(newEgress.Spec.EgressIPs) {
					specEgressIPs[pool] = newEgress.Spec.EgressIPs[i]
				} else {
					specEgressIPs[pool] = ""
				}
			}
		}
		for pool, ipStr := range specEgressIPs {
			ip := net.ParseIP(ipStr)
			if !c.externalIPAllocator.IPPoolExists(pool) {
				return false, fmt.Sprintf("ExternalIPPool %s does not exist", pool)
			}
			if ip == nil {
				if defineMultiExternalIPPool {
					continue
				}
				return false, fmt.Sprintf("IP %s is not valid", ipStr)
			}
			if !c.externalIPAllocator.IPPoolHasIP(pool, ip) {
				return false, fmt.Sprintf("IP %s is not within the IP range", ipStr)
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
