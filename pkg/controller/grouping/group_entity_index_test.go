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

package grouping

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/vmware-tanzu/antrea/pkg/apis/core/v1alpha2"
	"github.com/vmware-tanzu/antrea/pkg/controller/types"
)

const (
	groupType1 = "fakeGroup1"
	groupType2 = "fakeGroup2"
)

var (
	// Fake Pods
	podFoo1                 = newPod("default", "podFoo1", map[string]string{"app": "foo"})
	podFoo2                 = newPod("default", "podFoo2", map[string]string{"app": "foo"})
	podBar1                 = newPod("default", "podBar1", map[string]string{"app": "bar"})
	podFoo1InOtherNamespace = newPod("other", "podFoo1", map[string]string{"app": "foo"})
	// Fake ExternalEntities
	eeFoo1                 = newExternalEntity("default", "eeFoo1", map[string]string{"app": "foo"})
	eeFoo2                 = newExternalEntity("default", "eeFoo2", map[string]string{"app": "foo"})
	eeBar1                 = newExternalEntity("default", "eeBar1", map[string]string{"app": "bar"})
	eeFoo1InOtherNamespace = newExternalEntity("other", "eeFoo1", map[string]string{"app": "foo"})
	// Fake Namespaces
	nsDefault = newNamespace("default", map[string]string{"name": "default"})
	nsOther   = newNamespace("other", map[string]string{"name": "other"})
	// Fake groups
	groupPodFooType1             = &group{groupType: groupType1, groupName: "groupPodFooType1", groupSelector: types.NewGroupSelector("default", &metav1.LabelSelector{MatchLabels: map[string]string{"app": "foo"}}, nil, nil)}
	groupPodFooType2             = &group{groupType: groupType2, groupName: "groupPodFooType2", groupSelector: types.NewGroupSelector("default", &metav1.LabelSelector{MatchLabels: map[string]string{"app": "foo"}}, nil, nil)}
	groupPodBarType1             = &group{groupType: groupType1, groupName: "groupPodBarType1", groupSelector: types.NewGroupSelector("default", &metav1.LabelSelector{MatchLabels: map[string]string{"app": "bar"}}, nil, nil)}
	groupEEFooType1              = &group{groupType: groupType1, groupName: "groupEEFooType1", groupSelector: types.NewGroupSelector("default", nil, nil, &metav1.LabelSelector{MatchLabels: map[string]string{"app": "foo"}})}
	groupPodFooAllNamespaceType1 = &group{groupType: groupType1, groupName: "groupPodFooAllNamespaceType1", groupSelector: types.NewGroupSelector("", &metav1.LabelSelector{MatchLabels: map[string]string{"app": "foo"}}, nil, nil)}
	groupPodAllNamespaceType1    = &group{groupType: groupType1, groupName: "groupPodAllNamespaceType1", groupSelector: types.NewGroupSelector("", nil, &metav1.LabelSelector{}, nil)}
)

type group struct {
	groupType     string
	groupName     string
	groupSelector *types.GroupSelector
}

func copyAndMutatePod(pod *v1.Pod, mutateFunc func(*v1.Pod)) *v1.Pod {
	newPod := pod.DeepCopy()
	mutateFunc(newPod)
	return newPod
}

func copyAndMutateNamespace(ns *v1.Namespace, mutateFunc func(*v1.Namespace)) *v1.Namespace {
	newNS := ns.DeepCopy()
	mutateFunc(newNS)
	return newNS
}

func copyAndMutateGroup(g *group, mutateFunc func(*group)) *group {
	newGroup := &group{
		groupType:     g.groupType,
		groupName:     g.groupName,
		groupSelector: g.groupSelector,
	}
	mutateFunc(newGroup)
	return newGroup
}

func newNamespace(name string, labels map[string]string) *v1.Namespace {
	return &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
	}
}

func newPod(namespace, name string, labels map[string]string) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			Labels:    labels,
		},
	}
}

func newExternalEntity(namespace, name string, labels map[string]string) *v1alpha2.ExternalEntity {
	return &v1alpha2.ExternalEntity{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			Labels:    labels,
		},
	}
}

func TestGroupEntityIndexGetEntities(t *testing.T) {
	tests := []struct {
		name                     string
		existingPods             []*v1.Pod
		existingExternalEntities []*v1alpha2.ExternalEntity
		existingNamespaces       []*v1.Namespace
		inputGroupSelector       *types.GroupSelector
		expectedPods             []*v1.Pod
		expectedExternalEntities []*v1alpha2.ExternalEntity
	}{
		{
			name:                     "namespace scoped pod selector",
			existingPods:             []*v1.Pod{podFoo1, podFoo2, podBar1, podFoo1InOtherNamespace},
			existingExternalEntities: []*v1alpha2.ExternalEntity{eeFoo1, eeFoo2, eeBar1, eeFoo1InOtherNamespace},
			inputGroupSelector:       types.NewGroupSelector("default", &metav1.LabelSelector{MatchLabels: map[string]string{"app": "foo"}}, nil, nil),
			expectedPods:             []*v1.Pod{podFoo1, podFoo2},
		},
		{
			name:                     "namespace scoped externalEntity selector",
			existingPods:             []*v1.Pod{podFoo1, podFoo2, podBar1, podFoo1InOtherNamespace},
			existingExternalEntities: []*v1alpha2.ExternalEntity{eeFoo1, eeFoo2, eeBar1, eeFoo1InOtherNamespace},
			inputGroupSelector:       types.NewGroupSelector("default", nil, nil, &metav1.LabelSelector{MatchLabels: map[string]string{"app": "foo"}}),
			expectedExternalEntities: []*v1alpha2.ExternalEntity{eeFoo1, eeFoo2},
		},
		{
			name:                     "cluster scoped pod selector",
			existingPods:             []*v1.Pod{podFoo1, podFoo2, podBar1, podFoo1InOtherNamespace},
			existingExternalEntities: []*v1alpha2.ExternalEntity{eeFoo1, eeFoo2, eeBar1, eeFoo1InOtherNamespace},
			inputGroupSelector:       types.NewGroupSelector("", &metav1.LabelSelector{MatchLabels: map[string]string{"app": "foo"}}, nil, nil),
			expectedPods:             []*v1.Pod{podFoo1, podFoo2, podFoo1InOtherNamespace},
		},
		{
			name:                     "cluster scoped externalEntity selector",
			existingPods:             []*v1.Pod{podFoo1, podFoo2, podBar1, podFoo1InOtherNamespace},
			existingExternalEntities: []*v1alpha2.ExternalEntity{eeFoo1, eeFoo2, eeBar1, eeFoo1InOtherNamespace},
			inputGroupSelector:       types.NewGroupSelector("", nil, nil, &metav1.LabelSelector{MatchLabels: map[string]string{"app": "foo"}}),
			expectedExternalEntities: []*v1alpha2.ExternalEntity{eeFoo1, eeFoo2, eeFoo1InOtherNamespace},
		},
		{
			name:                     "cluster scoped pod selector with namespaceSelector",
			existingNamespaces:       []*v1.Namespace{nsDefault, nsOther},
			existingPods:             []*v1.Pod{podFoo1, podFoo2, podBar1, podFoo1InOtherNamespace},
			existingExternalEntities: []*v1alpha2.ExternalEntity{eeFoo1, eeFoo2, eeBar1, eeFoo1InOtherNamespace},
			inputGroupSelector:       types.NewGroupSelector("", &metav1.LabelSelector{MatchLabels: map[string]string{"app": "foo"}}, &metav1.LabelSelector{MatchLabels: map[string]string{"name": "other"}}, nil),
			expectedPods:             []*v1.Pod{podFoo1InOtherNamespace},
		},
		{
			name:                     "cluster scoped pod selector with namespaceSelector but no namespaces",
			existingPods:             []*v1.Pod{podFoo1, podFoo2, podBar1, podFoo1InOtherNamespace},
			existingExternalEntities: []*v1alpha2.ExternalEntity{eeFoo1, eeFoo2, eeBar1, eeFoo1InOtherNamespace},
			inputGroupSelector:       types.NewGroupSelector("", &metav1.LabelSelector{MatchLabels: map[string]string{"app": "foo"}}, &metav1.LabelSelector{MatchLabels: map[string]string{"name": "other"}}, nil),
			expectedPods:             []*v1.Pod{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			index := NewPodGroupIndex()

			for _, pod := range tt.existingPods {
				index.AddPod(pod)
			}
			for _, ns := range tt.existingNamespaces {
				index.AddNamespace(ns)
			}
			for _, ee := range tt.existingExternalEntities {
				index.AddExternalEntity(ee)
			}
			index.AddGroup(groupType1, "group", tt.inputGroupSelector)

			pods, ees := index.GetEntities(groupType1, "group")
			assert.ElementsMatch(t, tt.expectedPods, pods)
			assert.ElementsMatch(t, tt.expectedExternalEntities, ees)
		})
	}
}

func TestGroupEntityIndexEventHandlers(t *testing.T) {
	tests := []struct {
		name                     string
		existingPods             []*v1.Pod
		existingNamespaces       []*v1.Namespace
		existingExternalEntities []*v1alpha2.ExternalEntity
		existingGroups           []*group
		addedPod                 *v1.Pod
		deletedPod               *v1.Pod
		addedNamespace           *v1.Namespace
		expectedGroupsCalled     map[string][]string
	}{
		{
			name:                     "add a new pod",
			existingPods:             []*v1.Pod{podFoo1, podBar1, podFoo1InOtherNamespace},
			existingExternalEntities: []*v1alpha2.ExternalEntity{eeFoo1, eeBar1, eeFoo1InOtherNamespace},
			existingGroups:           []*group{groupPodFooType1, groupPodBarType1, groupPodFooType2, groupPodFooAllNamespaceType1, groupEEFooType1},
			addedPod:                 podFoo2,
			expectedGroupsCalled:     map[string][]string{groupType1: {groupPodFooType1.groupName, groupPodFooAllNamespaceType1.groupName}, groupType2: {groupPodFooType2.groupName}},
		},
		{
			name:                     "update an existing pod's labels",
			existingPods:             []*v1.Pod{podFoo1, podBar1, podFoo1InOtherNamespace},
			existingExternalEntities: []*v1alpha2.ExternalEntity{eeFoo1, eeBar1, eeFoo1InOtherNamespace},
			existingGroups:           []*group{groupPodFooType1, groupPodFooAllNamespaceType1, groupEEFooType1, groupPodAllNamespaceType1},
			addedPod: copyAndMutatePod(podFoo1, func(pod *v1.Pod) {
				pod.Labels = map[string]string{"app": "bar"}
			}),
			expectedGroupsCalled: map[string][]string{groupType1: {groupPodFooType1.groupName, groupPodFooAllNamespaceType1.groupName}},
		},
		{
			name:                     "update an existing pod's attributes",
			existingPods:             []*v1.Pod{podFoo1, podBar1, podFoo1InOtherNamespace},
			existingExternalEntities: []*v1alpha2.ExternalEntity{eeFoo1, eeBar1, eeFoo1InOtherNamespace},
			existingGroups:           []*group{groupPodFooType1, groupPodFooAllNamespaceType1, groupEEFooType1, groupPodAllNamespaceType1},
			addedPod: copyAndMutatePod(podFoo1, func(pod *v1.Pod) {
				pod.Status.PodIP = "2.2.2.2"
			}),
			expectedGroupsCalled: map[string][]string{groupType1: {groupPodFooType1.groupName, groupPodFooAllNamespaceType1.groupName, groupPodAllNamespaceType1.groupName}},
		},
		{
			name:                     "update an existing pod's annotations",
			existingPods:             []*v1.Pod{podFoo1, podBar1, podFoo1InOtherNamespace},
			existingExternalEntities: []*v1alpha2.ExternalEntity{eeFoo1, eeBar1, eeFoo1InOtherNamespace},
			existingGroups:           []*group{groupPodFooType1, groupPodFooAllNamespaceType1, groupEEFooType1, groupPodAllNamespaceType1},
			addedPod: copyAndMutatePod(podFoo1, func(pod *v1.Pod) {
				pod.Annotations = map[string]string{"foo": "bar"}
			}),
			expectedGroupsCalled: map[string][]string{},
		},
		{
			name:                     "delete an existing pod",
			existingPods:             []*v1.Pod{podFoo1, podBar1, podFoo1InOtherNamespace},
			existingExternalEntities: []*v1alpha2.ExternalEntity{eeFoo1, eeBar1, eeFoo1InOtherNamespace},
			existingGroups:           []*group{groupPodFooType1, groupPodFooAllNamespaceType1, groupEEFooType1, groupPodAllNamespaceType1},
			deletedPod:               podFoo1,
			expectedGroupsCalled:     map[string][]string{groupType1: {groupPodFooType1.groupName, groupPodFooAllNamespaceType1.groupName, groupPodAllNamespaceType1.groupName}},
		},
		{
			name:                     "update an existing namespace's labels",
			existingPods:             []*v1.Pod{podFoo1, podBar1, podFoo1InOtherNamespace},
			existingExternalEntities: []*v1alpha2.ExternalEntity{eeFoo1, eeBar1, eeFoo1InOtherNamespace},
			existingGroups: []*group{groupPodFooType1, groupPodFooAllNamespaceType1, groupPodAllNamespaceType1, {
				groupType:     groupType1,
				groupName:     "groupWithNamespaceSelector",
				groupSelector: types.NewGroupSelector("", nil, &metav1.LabelSelector{MatchLabels: map[string]string{"foo": "bar"}}, nil),
			}},
			addedNamespace: copyAndMutateNamespace(nsDefault, func(namespace *v1.Namespace) {
				namespace.Labels["foo"] = "bar"
			}),
			expectedGroupsCalled: map[string][]string{groupType1: {"groupWithNamespaceSelector"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stopCh := make(chan struct{})
			defer close(stopCh)

			index := NewPodGroupIndex()
			go index.Run(stopCh)

			var lock sync.Mutex
			actualGroupsCalled := map[string][]string{}
			for groupType := range tt.expectedGroupsCalled {
				index.AddEventHandler(groupType, func(gType string) eventHandler {
					return func(group string) {
						lock.Lock()
						defer lock.Unlock()
						actualGroupsCalled[gType] = append(actualGroupsCalled[gType], group)
					}
				}(groupType))
			}
			for _, pod := range tt.existingPods {
				index.AddPod(pod)
			}
			for _, ns := range tt.existingNamespaces {
				index.AddNamespace(ns)
			}
			for _, ee := range tt.existingExternalEntities {
				index.AddExternalEntity(ee)
			}
			for _, group := range tt.existingGroups {
				index.AddGroup(group.groupType, group.groupName, group.groupSelector)
			}
			if tt.addedPod != nil {
				index.AddPod(tt.addedPod)
			}
			if tt.deletedPod != nil {
				index.DeletePod(tt.deletedPod)
			}
			if tt.addedNamespace != nil {
				index.AddNamespace(tt.addedNamespace)
			}

			time.Sleep(10 * time.Millisecond)
			lock.Lock()
			defer lock.Unlock()
			assert.Equal(t, len(tt.expectedGroupsCalled), len(actualGroupsCalled))
			for groupType, expected := range tt.expectedGroupsCalled {
				assert.ElementsMatch(t, expected, actualGroupsCalled[groupType])
			}
		})
	}
}
