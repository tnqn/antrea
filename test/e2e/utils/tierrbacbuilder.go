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

package utils

import (
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	crdv1a1 "github.com/vmware-tanzu/antrea/pkg/apis/crd/v1alpha1"
)

type TierEntitlementSpecBuilder struct {
	Spec crdv1a1.TierEntitlementSpec
	Name string
}

func (t *TierEntitlementSpecBuilder) Get() *crdv1a1.TierEntitlement {
	if t.Spec.Tiers == nil {
		t.Spec.Tiers = []string{}
	}
	return &crdv1a1.TierEntitlement{
		ObjectMeta: metav1.ObjectMeta{
			Name: t.Name,
		},
		Spec: t.Spec,
	}
}

func (t *TierEntitlementSpecBuilder) SetName(name string) *TierEntitlementSpecBuilder {
	t.Name = name
	return t
}

func (t *TierEntitlementSpecBuilder) SetPriorityEdit() *TierEntitlementSpecBuilder {
	t.Spec.Permission = "edit"
	return t
}

func (t *TierEntitlementSpecBuilder) AddTier(tier string) *TierEntitlementSpecBuilder {
	if tier == "" {
		return t
	}
	if t.Spec.Tiers == nil {
		t.Spec.Tiers = []string{}
	}
	t.Spec.Tiers = append(t.Spec.Tiers, tier)
	return t
}

type TierEntitlementBindingSpecBuilder struct {
	Spec crdv1a1.TierEntitlementBindingSpec
	Name string
}

func (t *TierEntitlementBindingSpecBuilder) Get() *crdv1a1.TierEntitlementBinding {
	if t.Spec.Subjects == nil {
		t.Spec.Subjects = []rbacv1.Subject{}
	}
	return &crdv1a1.TierEntitlementBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: t.Name,
		},
		Spec: t.Spec,
	}
}

func (t *TierEntitlementBindingSpecBuilder) SetName(name string) *TierEntitlementBindingSpecBuilder {
	t.Name = name
	return t
}

func (t *TierEntitlementBindingSpecBuilder) SetTierEntitlement(name string) *TierEntitlementBindingSpecBuilder {
	t.Spec.TierEntitlement = name
	return t
}

func (t *TierEntitlementBindingSpecBuilder) AddSubject(user rbacv1.Subject) *TierEntitlementBindingSpecBuilder {
	if t.Spec.Subjects == nil {
		t.Spec.Subjects = []rbacv1.Subject{}
	}
	t.Spec.Subjects = append(t.Spec.Subjects, user)
	return t
}
