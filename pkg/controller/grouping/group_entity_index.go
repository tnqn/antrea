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
	"k8s.io/klog"
	"reflect"
	"strings"
	"sync"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/vmware-tanzu/antrea/pkg/apis/core/v1alpha2"
	"github.com/vmware-tanzu/antrea/pkg/controller/types"
)

// GroupEntityIndex provides methods to query entities that a given group selects and groups that select a given entity.
// It maintains indexes between groups and entities to make the query efficient. It supports callers to register
// callbacks that will be called when a specific type of groups' entities are updated.
type GroupEntityIndex interface {
	// AddGroup adds or updates a group to the index. The caller can then get entities selected by this group.
	AddGroup(groupType string, name string, selector *types.GroupSelector)
	// DeleteGroup deletes a group from the index.
	DeleteGroup(groupType string, name string)
	// AddEventHandler registers an eventHandler for the given type of groups. When any Pod/ExternelEntity/Namespace
	// update affects the given kind of groups, the eventHandler will be called with the affected groups.
	AddEventHandler(groupType string, handler eventHandler)
	// GetEntities returns the selected Pods or ExternalEntities for the given group.
	GetEntities(groupType string, name string) ([]*v1.Pod, []*v1alpha2.ExternalEntity)
	// GetGroups returns the groups that select the given Pod.
	GetGroupsForPod(namespace, name string) (map[string][]string, bool)
	// AddPod adds or updates a Pod to the index. If any existing groups are affected, eventHandlers will be called with
	// the affected groups.
	AddPod(pod *v1.Pod)
	// DeletePod deletes a Pod from the index. If any existing groups are affected, eventHandlers will be called with
	// the affected groups.
	DeletePod(pod *v1.Pod)
	// AddExternalEntity adds or updates an ExternalEntity to the index. If any existing groups are affected,
	// eventHandlers will be called with the affected groups.
	AddExternalEntity(ee *v1alpha2.ExternalEntity)
	// DeleteExternalEntity deletes an ExternalEntity from the index. If any existing groups are affected, eventHandlers
	// will be called with the affected groups.
	DeleteExternalEntity(ee *v1alpha2.ExternalEntity)
	// AddNamespace adds or updates a Namespace to the index. If any existing groups are affected, eventHandlers will be
	// called with the affected groups.
	AddNamespace(namespace *v1.Namespace)
	// DeleteNamespace deletes a Namespace to the index. If any existing groups are affected, eventHandlers will be
	// called with the affected groups.
	DeleteNamespace(namespace *v1.Namespace)
	// Run starts the index.
	Run(stopCh <-chan struct{})
}

type eventHandler func(group string)

// entityType is an internal type used to differentiate Pod from ExternalEntity.
type entityType string

const (
	podEntityType      entityType = "Pod"
	externalEntityType entityType = "ExternalEntity"
)

// entityItem contains an entity (either Pod or ExternalEntity) and some relevant information.
type entityItem struct {
	// entity is either a Pod or an ExternalEntity.
	entity metav1.Object
	// labelKey is the key of the labelItem that the entityItem is associated with.
	// entityItems will be associated with the same labelItem if they have same Namespace, entityType, and labels.
	labelKey string
}

// labelItem represents an individual label set. It's the actual object that will be matched with label selectors.
// Entities of same type in same Namespace having same labels will share a labelItem.
type labelItem struct {
	// The label set that will be used for matching.
	labels labels.Set
	// The namespace of the entities that share the labelItem.
	namespace string
	// The type of the entities that share the labelItem.
	entityType entityType
	// The keys of the entityItems that share the labelItem.
	entities sets.String
	// The keys of the selectorItems that match the labelItem.
	selectors sets.String
}

// groupItem contains a group's metadata and its selector.
type groupItem struct {
	// The type of the group.
	groupType string
	// The name of the group. It must be unique within its own type.
	name string
	// The selector of the group.
	selector *types.GroupSelector
	// selectorKey is the key of the selectorItem that the groupItem is associated with.
	// groupItems will be associated with the same selectorItem if they have same selector.
	selectorKey string
}

// selectorItem represents an individual label selector. It's the actual object that will be matched with label sets.
// Groups having same label selector will share a selectorItem.
type selectorItem struct {
	// The label selector that will be used for matching.
	selector *types.GroupSelector
	// The keys of the groupItems that share the selectorItem.
	groups sets.String
	// The keys of the labelItems that match the selectorItem.
	labels sets.String
}

var _ GroupEntityIndex = &groupEntityIndex{}

// groupEntityIndex implements GroupEntityIndex.
//
// It abstracts label set and label selector from entities and groups and do actual matching against the formers to
// avoid redundant calculation given that most entities (Pod or ExternalEntity) actually share labels. For example, Pods
// that managed by a deployment controller have same labels.
//
// It maintains indexes between label set and label selector so that querying label sets that match a label selector and
// reversed queries can be performed with a consistent time complexity. Indirectly, querying entities that match a group
// and reversed queries can be performed with same complexity.
//
// The relationship of the four items are like below:
// entityItem <===> labelItem <===> selectorItem <===> groupItem
type groupEntityIndex struct {
	lock sync.RWMutex

	// entityItems stores all entityItems.
	entityItems map[string]*entityItem

	// labelItems stores all labelItems.
	labelItems map[string]*labelItem
	// labelIndex is nested map from entityType to Namespace to keys of labelItems.
	// It's used to filter potential labelItems when matching a namespace scoped selectorItem.
	labelIndex map[entityType]map[string]sets.String

	// groupItems stores all groupItems.
	groupItems map[string]*groupItem

	// selectorItems stores all selectorItems.
	selectorItems map[string]*selectorItem
	// selectorIndex is nested map from entityType to Namespace to keys of selectorItems.
	// It's used to filter potential selectorItems when matching an labelItem.
	selectorIndex map[entityType]map[string]sets.String

	// namespaceLabels stores label sets of all Namespaces.
	namespaceLabels map[string]labels.Set

	// eventHandlers is a map from group type to a list of handlers. When a type of group's updated, the corresponding
	// event handlers will be called with the group name provided.
	eventHandlers map[string][]eventHandler

	// eventChan is channel used for calling eventHandlers asynchronously.
	eventChan chan string
}

// NewPodGroupIndex creates a groupEntityIndex.
func NewPodGroupIndex() *groupEntityIndex {
	index := &groupEntityIndex{
		entityItems:     map[string]*entityItem{},
		groupItems:      map[string]*groupItem{},
		labelItems:      map[string]*labelItem{},
		labelIndex:      map[entityType]map[string]sets.String{podEntityType: {}, externalEntityType: {}},
		selectorItems:   map[string]*selectorItem{},
		selectorIndex:   map[entityType]map[string]sets.String{podEntityType: {}, externalEntityType: {}},
		namespaceLabels: map[string]labels.Set{},
		eventHandlers:   map[string][]eventHandler{},
		eventChan:       make(chan string, 1000),
	}
	return index
}

func (i *groupEntityIndex) GetEntities(groupType string, name string) ([]*v1.Pod, []*v1alpha2.ExternalEntity) {
	groupKey := getGroupKey(groupType, name)

	i.lock.RLock()
	defer i.lock.RUnlock()

	gItem, exists := i.groupItems[groupKey]
	if !exists {
		return nil, nil
	}

	// Get the selectorItem the group is associated with.
	sItem, _ := i.selectorItems[gItem.selectorKey]
	var pods []*v1.Pod
	var externalEntities []*v1alpha2.ExternalEntity
	// Get the keys of the labelItems the selectorItem matches.
	for lKey := range sItem.labels {
		lItem, _ := i.labelItems[lKey]
		// Collect the entityItems that share the labelItem.
		for entityKey := range lItem.entities {
			eItem, _ := i.entityItems[entityKey]
			switch entity := eItem.entity.(type) {
			case *v1.Pod:
				pods = append(pods, entity)
			case *v1alpha2.ExternalEntity:
				externalEntities = append(externalEntities, entity)
			}
		}
	}
	return pods, externalEntities
}

func (i *groupEntityIndex) GetGroupsForPod(namespace, name string) (map[string][]string, bool) {
	return i.getGroups(podEntityType, namespace, name)
}

func (i *groupEntityIndex) getGroups(entityType entityType, namespace, name string) (map[string][]string, bool) {
	entityKey := getEntityKeyByName(entityType, namespace, name)

	i.lock.RLock()
	defer i.lock.RUnlock()

	// Get the selectorItem the group is associated with.
	eItem, exists := i.entityItems[entityKey]
	if !exists {
		return nil, false
	}

	groups := map[string][]string{}
	lItem, _ := i.labelItems[eItem.labelKey]
	// Get the keys of the selectorItems the labelItem matches.
	for sKey := range lItem.selectors {
		sItem, _ := i.selectorItems[sKey]
		// Collect the groupItems that share the selectorItem.
		for groupKey := range sItem.groups {
			gItem, _ := i.groupItems[groupKey]
			groups[gItem.groupType] = append(groups[gItem.groupType], gItem.name)
		}
	}
	return groups, true
}

func (i *groupEntityIndex) AddNamespace(namespace *v1.Namespace) {
	i.lock.Lock()
	defer i.lock.Unlock()

	namespaceLabels, exists := i.namespaceLabels[namespace.Name]
	// Do nothing if labels are not updated.
	if exists && labels.Equals(namespaceLabels, namespace.Labels) {
		return
	}

	i.namespaceLabels[namespace.Name] = namespace.Labels
	// Resync cluster scoped selectors as they may start or stop matching the namespace because of the label update.
	for _, namespaceToSelector := range i.selectorIndex {
		// Cluster scoped selectors are stored under empty namespace in the selectorIndex.
		selectors, exists := namespaceToSelector[""]
		if !exists {
			continue
		}
		for selector := range selectors {
			sItem := i.selectorItems[selector]
			// Notify watchers if the selector is updated.
			if i.syncSelector(sItem) {
				i.notify(selector)
			}
		}
	}

}

func (i *groupEntityIndex) DeleteNamespace(namespace *v1.Namespace) {
	i.lock.Lock()
	defer i.lock.Unlock()

	delete(i.namespaceLabels, namespace.Name)
}

// deletePodFromLabelItem disconnects an entityItem from a labelItem.
// The labelItem will be deleted if it's no longer used by any entityItem.
func (i *groupEntityIndex) deletePodFromLabelItem(label, entity string) *labelItem {
	lItem, _ := i.labelItems[label]
	lItem.entities.Delete(entity)
	// If the labelItem is still used by any entities, keep it. Otherwise delete it.
	if len(lItem.entities) > 0 {
		return lItem
	}
	// Delete the labelItem itself.
	delete(i.labelItems, label)

	// Delete it from the labelIndex.
	i.labelIndex[lItem.entityType][lItem.namespace].Delete(label)
	if len(i.labelIndex[lItem.entityType][lItem.namespace]) == 0 {
		delete(i.labelIndex[lItem.entityType], lItem.namespace)
	}

	// Delete the labelItem from matched selectorItems.
	for selector := range lItem.selectors {
		sItem := i.selectorItems[selector]
		sItem.labels.Delete(label)
	}
	return lItem
}

// createLabelItem creates a labelItem based on the provided entityItem.
// It's called when there is no existing labelItem for a label set.
func (i *groupEntityIndex) createLabelItem(entityType entityType, eItem *entityItem) *labelItem {
	lItem := &labelItem{
		labels:     eItem.entity.GetLabels(),
		namespace:  eItem.entity.GetNamespace(),
		entityType: entityType,
		entities:   sets.NewString(),
		selectors:  sets.NewString(),
	}
	// Create the labelItem.
	i.labelItems[eItem.labelKey] = lItem
	// Add it to the labelIndex.
	labels, exists := i.labelIndex[entityType][lItem.namespace]
	if !exists {
		labels = sets.NewString()
		i.labelIndex[entityType][lItem.namespace] = labels
	}
	labels.Insert(eItem.labelKey)

	// Scan potential selectorItems and associate the new labelItem with the matched ones.
	scanSelectors := func(selectors sets.String) {
		for selector := range selectors {
			sItem := i.selectorItems[selector]
			matched := i.match(lItem.entityType, lItem.labels, lItem.namespace, sItem.selector)
			if matched {
				sItem.labels.Insert(eItem.labelKey)
				lItem.selectors.Insert(selector)
			}
		}
	}
	// SelectorItems in the same namespace may match the labelItem.
	localSelectors, _ := i.selectorIndex[entityType][eItem.entity.GetNamespace()]
	scanSelectors(localSelectors)
	// Cluster scoped selectorItems may match the labelItem.
	clusterSelectors, _ := i.selectorIndex[entityType][""]
	scanSelectors(clusterSelectors)
	return lItem
}

func (i *groupEntityIndex) AddPod(pod *v1.Pod) {
	i.addEntity(podEntityType, pod)
}

func (i *groupEntityIndex) AddExternalEntity(ee *v1alpha2.ExternalEntity) {
	i.addEntity(externalEntityType, ee)
}

func (i *groupEntityIndex) addEntity(entityType entityType, entity metav1.Object) {
	entityKey := getEntityKey(entityType, entity)
	newLabelKey := getLabelKey(entityType, entity)
	var oldLabelItem *labelItem
	var entityUpdated bool

	i.lock.Lock()
	defer i.lock.Unlock()

	eItem, exists := i.entityItems[entityKey]
	if exists {
		entityUpdated = entityAttrsUpdated(eItem.entity, entity)
		eItem.entity = entity
		// If its label doesn't change, its labelItem won't change. We still need to dispatch the updates of the groups
		// that select the entity if the entity's attributes that we care are updated.
		if eItem.labelKey == newLabelKey {
			if entityUpdated {
				lItem := i.labelItems[eItem.labelKey]
				for selector := range lItem.selectors {
					i.notify(selector)
				}
			}
			return
		}
		// Delete the Pod from the previous labelItem as its label is updated.
		oldLabelItem = i.deletePodFromLabelItem(eItem.labelKey, entityKey)
		eItem.labelKey = newLabelKey
	} else {
		entityUpdated = true
		eItem = &entityItem{
			entity:   entity,
			labelKey: newLabelKey,
		}
		i.entityItems[entityKey] = eItem
	}

	// Create a labelItem if it doesn't exist.
	lItem, exists := i.labelItems[newLabelKey]
	if !exists {
		lItem = i.createLabelItem(entityType, eItem)
	}
	lItem.entities.Insert(entityKey)

	// Notify group updates.
	var affectedSelectors sets.String
	if oldLabelItem != nil {
		// If entity is updated, all previously and currently matched selectors are affected. Otherwise only the
		// difference portion are affected.
		if entityUpdated {
			affectedSelectors = lItem.selectors.Union(oldLabelItem.selectors)
		} else {
			affectedSelectors = lItem.selectors.Difference(oldLabelItem.selectors).Union(oldLabelItem.selectors.Difference(lItem.selectors))
		}
	} else {
		affectedSelectors = lItem.selectors
	}
	for selector := range affectedSelectors {
		i.notify(selector)
	}
}

func (i *groupEntityIndex) DeletePod(pod *v1.Pod) {
	i.deleteEntity(podEntityType, pod)
}

func (i *groupEntityIndex) DeleteExternalEntity(ee *v1alpha2.ExternalEntity) {
	i.deleteEntity(externalEntityType, ee)
}

func (i *groupEntityIndex) deleteEntity(entityType entityType, entity metav1.Object) {
	entityKey := getEntityKey(entityType, entity)

	i.lock.Lock()
	defer i.lock.Unlock()

	eItem, exists := i.entityItems[entityKey]
	if !exists {
		return
	}

	// Delete the entity from its associated labelItem and entityItems.
	lItem := i.deletePodFromLabelItem(eItem.labelKey, entityKey)
	delete(i.entityItems, entityKey)

	// All selectorItems that match the labelItem are affected.
	for selector := range lItem.selectors {
		i.notify(selector)
	}
}

// deleteGroupFromSelectorItem disconnects an groupItem from a selectorItem.
// The selectorItem will be deleted if it's no longer used by any groupItem.
func (i *groupEntityIndex) deleteGroupFromSelectorItem(selector, group string) *selectorItem {
	sItem, _ := i.selectorItems[selector]
	sItem.groups.Delete(group)
	// If the selectorItem is still used by any groups, keep it. Otherwise delete it.
	if len(sItem.groups) > 0 {
		return sItem
	}
	// Delete the selectorItem itself.
	delete(i.selectorItems, selector)

	// Delete it from the selectorIndex.
	entityType := podEntityType
	if sItem.selector.ExternalEntitySelector != nil {
		entityType = externalEntityType
	}
	i.selectorIndex[entityType][sItem.selector.Namespace].Delete(sItem.selector.NormalizedName)

	// Delete the selectorItem from matched labelItems.
	for label := range sItem.labels {
		lItem := i.labelItems[label]
		lItem.selectors.Delete(selector)
	}
	return sItem
}

// createSelectorItem creates a selectorItem based on the provided groupItem.
// It's called when there is no existing selectorItem for a group selector.
func (i *groupEntityIndex) createSelectorItem(gItem *groupItem) *selectorItem {
	sItem := &selectorItem{
		selector: gItem.selector,
		groups:   sets.NewString(),
		labels:   sets.NewString(),
	}
	// Create the selectorItem.
	i.selectorItems[gItem.selectorKey] = sItem
	// Add it to the selectorIndex.
	entityType := podEntityType
	if gItem.selector.ExternalEntitySelector != nil {
		entityType = externalEntityType
	}
	selectors, exists := i.selectorIndex[entityType][sItem.selector.Namespace]
	if !exists {
		selectors = sets.NewString()
		i.selectorIndex[entityType][sItem.selector.Namespace] = selectors
	}
	selectors.Insert(gItem.selectorKey)

	i.syncSelector(sItem)
	return sItem
}

// syncSelector scans potential labelItems and associates the new selectorItem with the matched ones.
func (i *groupEntityIndex) syncSelector(sItem *selectorItem) bool {
	updated := false

	scanLabels := func(labels sets.String) {
		for label := range labels {
			lItem := i.labelItems[label]
			if i.match(lItem.entityType, lItem.labels, lItem.namespace, sItem.selector) {
				// Connect the selector and the label if they didn't match before, otherwise do nothing.
				if !sItem.labels.Has(label) {
					sItem.labels.Insert(label)
					lItem.selectors.Insert(sItem.selector.NormalizedName)
					updated = true
				}
			} else {
				// Disconnect the selector and the label if they matched before, otherwise do nothing.
				if sItem.labels.Has(label) {
					sItem.labels.Delete(label)
					lItem.selectors.Delete(sItem.selector.NormalizedName)
					updated = true
				}
			}
		}
	}

	// By default, the selector selects Pods. It selects ExternalEntities only if ExternalEntitySelector is set
	// explicitly.
	entityType := podEntityType
	if sItem.selector.ExternalEntitySelector != nil {
		entityType = externalEntityType
	}
	// If the selector is Namespace scoped, it can only match labelItems in the Namespace. Otherwise, all labelItems
	// should be checked.
	if sItem.selector.Namespace != "" {
		labels, _ := i.labelIndex[entityType][sItem.selector.Namespace]
		scanLabels(labels)
	} else {
		for _, labels := range i.labelIndex[entityType] {
			scanLabels(labels)
		}
	}
	return updated
}

func (i *groupEntityIndex) AddGroup(groupType string, name string, selector *types.GroupSelector) {
	groupKey := getGroupKey(groupType, name)
	newSelectorKey := getSelectorKey(selector)

	i.lock.Lock()
	defer i.lock.Unlock()

	gItem, exists := i.groupItems[groupKey]
	if exists {
		// Its selector doesn't change, do nothing.
		if gItem.selectorKey == newSelectorKey {
			return
		}
		i.deleteGroupFromSelectorItem(gItem.selectorKey, groupKey)
		gItem.selectorKey = newSelectorKey
		gItem.selector = selector
	} else {
		gItem = &groupItem{
			groupType:   groupType,
			name:        name,
			selector:    selector,
			selectorKey: newSelectorKey,
		}
		i.groupItems[groupKey] = gItem
	}

	// Create a selectorItem if it doesn't exist.
	sItem, exists := i.selectorItems[newSelectorKey]
	if !exists {
		sItem = i.createSelectorItem(gItem)
	}
	sItem.groups.Insert(groupKey)
}

func (i *groupEntityIndex) DeleteGroup(groupType string, name string) {
	groupKey := getGroupKey(groupType, name)

	i.lock.Lock()
	defer i.lock.Unlock()

	gItem, exists := i.groupItems[groupKey]
	if !exists {
		return
	}

	// Delete the group from its associated selectorItem and groupItems.
	i.deleteGroupFromSelectorItem(gItem.selectorKey, groupKey)
	delete(i.groupItems, groupKey)
}

func (i *groupEntityIndex) notify(selector string) {
	sItem := i.selectorItems[selector]
	for group := range sItem.groups {
		i.eventChan <- group
	}
}

func (i *groupEntityIndex) Run(stopCh <-chan struct{}) {
	for {
		select {
		case <-stopCh:
			klog.Info("Stopping groupEntityIndex")
			return
		case group := <-i.eventChan:
			parts := strings.Split(group, "/")
			groupType, name := parts[0], parts[1]
			for _, handler := range i.eventHandlers[groupType] {
				handler(name)
			}
		}
	}
}

func (i *groupEntityIndex) AddEventHandler(groupType string, handler eventHandler) {
	i.eventHandlers[groupType] = append(i.eventHandlers[groupType], handler)
}

func (i *groupEntityIndex) match(entityType entityType, label labels.Set, namespace string, sel *types.GroupSelector) bool {
	objSelector := sel.PodSelector
	if entityType == externalEntityType {
		objSelector = sel.ExternalEntitySelector
	}
	if sel.Namespace != "" {
		if sel.Namespace != namespace {
			// Pods or ExternalEntities must be matched within the same Namespace.
			return false
		}
		if objSelector != nil && !objSelector.Matches(label) {
			// podSelector or externalEntitySelector exists but doesn't match the ExternalEntity or Pod's labels.
			return false
		}
		return true
	}
	if sel.NamespaceSelector != nil {
		if !sel.NamespaceSelector.Empty() {
			namespaceLabels, exists := i.namespaceLabels[namespace]
			if !exists {
				return false
			}
			if !sel.NamespaceSelector.Matches(namespaceLabels) {
				// Pod's Namespace do not match namespaceSelector.
				return false
			}
		}
		if objSelector != nil && !objSelector.Matches(label) {
			// ExternalEntity or Pod's Namespace matches namespaceSelector but
			// labels do not match the podSelector or externalEntitySelector.
			return false
		}
		return true
	}
	if objSelector != nil {
		// Selector only has a PodSelector/ExternalEntitySelector and no sel.Namespace.
		// Pods/ExternalEntities must be matched from all Namespaces.
		if !objSelector.Matches(label) {
			// pod/ee labels do not match PodSelector/ExternalEntitySelector.
			return false
		}
		return true
	}
	// TODO: if none of the above selector is set, it means matching all entities or none?
	// This is only possible for ClusterGroup. From previous code, it means matching none.
	return false
}

func entityAttrsUpdated(oldEntity, newEntity metav1.Object) bool {
	switch oldValue := oldEntity.(type) {
	case *v1.Pod:
		// For Pod, we only care about PodIP and NodeName update.
		// Some other attributes we care are immutable, e.g. the named ContainerPort.
		newValue := newEntity.(*v1.Pod)
		if oldValue.Status.PodIP != newValue.Status.PodIP {
			return true
		}
		if oldValue.Spec.NodeName != newValue.Spec.NodeName {
			return true
		}
		return false
	case *v1alpha2.ExternalEntity:
		newValue := newEntity.(*v1alpha2.ExternalEntity)
		if !reflect.DeepEqual(oldValue.Spec, newValue.Spec) {
			return true
		}
		return false
	}
	return false
}

// getEntityKey returns the entity key used in entityItems.
func getEntityKey(entityType entityType, entity metav1.Object) string {
	return string(entityType) + "/" + entity.GetNamespace() + "/" + entity.GetName()
}

// getEntityKeyByName returns the entity key used in entityItems.
func getEntityKeyByName(entityType entityType, namespace, name string) string {
	return string(entityType) + "/" + namespace + "/" + name
}

// getLabelKey returns the label key used in labelItems.
func getLabelKey(entityType entityType, obj metav1.Object) string {
	return string(entityType) + "/" + obj.GetNamespace() + "/" + labels.Set(obj.GetLabels()).String()
}

// getGroupKey returns the group key used in groupItems.
func getGroupKey(groupType string, name string) string {
	return groupType + "/" + name
}

// getSelectorKey returns the selector key used in selectorItems.
func getSelectorKey(selector *types.GroupSelector) string {
	return selector.NormalizedName
}
