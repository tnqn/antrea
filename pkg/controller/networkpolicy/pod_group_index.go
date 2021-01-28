package networkpolicy

import (
	"github.com/vmware-tanzu/antrea/pkg/apis/core/v1alpha2"
	"github.com/vmware-tanzu/antrea/pkg/controller/types"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"
	"strings"
	"sync"
)

type GroupType string

const (
	AppliedToGroupType GroupType = "AppliedToGroup"
	AddressGroupType GroupType = "AddressGroup"
	ClusterGroupType GroupType = "ClusterGroup"
)

type EntityType string

const (
	PodType EntityType = "Pod"
	ExternalEntityType EntityType = "ExternalEntity"
)

type GroupReference struct {
	Type GroupType
	Name string
}

type PodGroupIndex interface {
	GetEntities(group *GroupReference) ([]*v1.Pod, []*v1alpha2.ExternalEntity)

	GetGroups(pod *v1.Pod) []*GroupReference

	AddEntity(entityType EntityType, entity metav1.Object)

	DeleteEntity(entityType EntityType, entity metav1.Object)

	AddNamespace(namespace *v1.Namespace)

	DeleteNamespace(namespace *v1.Namespace)

	AddGroup(group *GroupReference, selector *types.GroupSelector)

	DeleteGroup(group *GroupReference)

	AddEventHandler(handler eventHandler)
}

var _ PodGroupIndex = &podGroupIndex{}

type podItem struct {
	entityType     EntityType
	entity         metav1.Object
	labelKey       string
}

type groupItem struct {
	group *GroupReference
	selector *types.GroupSelector
}

type labelItem struct {
	labels labels.Set

	namespace string
	// The names of the Pods that share the labels and the namespace.
	entities sets.String
	// The names of the selectors that match the labels.
	selectors sets.String
}

type selectorItem struct {
	selector *types.GroupSelector
	// The names of the Groups that share the selector.
	groups sets.String
	// The names of the labels that the selector matches.
	labels sets.String
}

type eventHandler func(group *GroupReference)

type podGroupIndex struct {
	lock sync.RWMutex

	podIndex map[string]*podItem

	labelIndex map[string]*labelItem
	namespaceToLabel map[string]sets.String

	selectorIndex map[string]*selectorItem
	namespaceToSelector map[string]sets.String

	groupIndex map[string]*groupItem

	namespaceLabelsIndex map[string]labels.Set

	eventHandlers []eventHandler

	notifyChan chan *GroupReference
}

func NewPodGroupIndex() *podGroupIndex {
	index :=  &podGroupIndex{
		podIndex:             map[string]*podItem{},
		groupIndex:           map[string]*groupItem{},
		labelIndex:           map[string]*labelItem{},
		namespaceToLabel:     map[string]sets.String{},
		selectorIndex:        map[string]*selectorItem{},
		namespaceToSelector:  map[string]sets.String{},
		namespaceLabelsIndex: map[string]labels.Set{},
		notifyChan:           make(chan *GroupReference, 1000),
	}
	go index.Run()
	return index
}

func (i *podGroupIndex) GetEntities(group *GroupReference) ([]*v1.Pod, []*v1alpha2.ExternalEntity) {
	i.lock.RLock()
	defer i.lock.RUnlock()
	gItem, exists := i.groupIndex[getGroupKey(group)]
	if !exists {
		return nil, nil
	}
	sItem, _ := i.selectorIndex[gItem.selector.NormalizedName]
	var pods []*v1.Pod
	var externalEntities []*v1alpha2.ExternalEntity
	for lKey := range sItem.labels {
		lItem, _ := i.labelIndex[lKey]
		for pod := range lItem.entities {
			pItem, _ := i.podIndex[pod]
			if pItem.entityType == PodType {
				pods = append(pods, pItem.entity.(*v1.Pod))
			} else if pItem.entityType == ExternalEntityType {
				externalEntities = append(externalEntities, pItem.entity.(*v1alpha2.ExternalEntity))
			}
		}
	}
	return pods, externalEntities
}

func (i *podGroupIndex) GetGroups(pod *v1.Pod) []*GroupReference {
	i.lock.RLock()
	defer i.lock.RUnlock()
	panic("implement me")
}

func (i *podGroupIndex) AddNamespace(namespace *v1.Namespace) {
	i.lock.Lock()
	defer i.lock.Unlock()

	namespaceLabels, exists := i.namespaceLabelsIndex[namespace.Name]
	// Do nothing if labels are not updated.
	if exists && labels.Equals(namespaceLabels, namespace.Labels){
		return
	}

	i.namespaceLabelsIndex[namespace.Name] = namespace.Labels

	// As the Namespace's labels are updated, cluster scoped selectors may be affected.
	selectors, exists := i.namespaceToSelector[""]
	if !exists {
		return
	}
	for selector := range selectors {
		sItem := i.selectorIndex[selector]
		if i.syncSelector(sItem) {
			i.notify(selector)
		}
	}
}

func (i *podGroupIndex) DeleteNamespace(namespace *v1.Namespace) {
	panic("implement me")
}

func getLabelKey(obj metav1.Object) string {
	return obj.GetNamespace() + "/" + labels.Set(obj.GetLabels()).String()
}

func (i *podGroupIndex) deletePodFromLabelItem(label, pod string) *labelItem {
	lItem, _ := i.labelIndex[label]
	lItem.entities.Delete(pod)
	if len(lItem.entities) > 0 {
		return lItem
	}
	// If the label is no longer used by any Pods, remove it.
	delete(i.labelIndex, label)
	for selector := range lItem.selectors {
		sItem := i.selectorIndex[selector]
		sItem.labels.Delete(label)
	}
	return lItem
}

func (i *podGroupIndex) createLabelItem(pItem *podItem) *labelItem {
	lItem := &labelItem{
		labels:    pItem.entity.GetLabels(),
		namespace: pItem.entity.GetNamespace(),
		entities:  sets.NewString(),
		selectors: sets.NewString(),
	}
	i.labelIndex[pItem.labelKey] = lItem

	labels, exists := i.namespaceToLabel[lItem.namespace]
	if !exists {
		labels = sets.NewString()
		i.namespaceToLabel[lItem.namespace] = labels
	}
	labels.Insert(pItem.labelKey)
	return lItem
}

func getEntityKey(entityType EntityType, entity metav1.Object) string {
	return string(entityType) +"/" + entity.GetNamespace() + "/" + entity.GetName()
}

func (i *podGroupIndex) AddEntity(entityType EntityType, entity metav1.Object) {
	entityKey := getEntityKey(entityType, entity)
	newLabelKey := getLabelKey(entity)
	var oldSelectors sets.String

	i.lock.Lock()
	defer i.lock.Unlock()

	pItem, exists := i.podIndex[entityKey]
	if exists {
		pItem.entity = entity
		// Its label doesn't change, use the cached result directly.
		if pItem.labelKey == newLabelKey {
			// TODO: It should notify group changes if pod's status is updated.
			return
		}
		// Delete the Pod from the previous labelItem as its label is updated.
		lItem := i.deletePodFromLabelItem(pItem.labelKey, entityKey)
		oldSelectors = lItem.selectors
		pItem.labelKey = newLabelKey
	} else {
		pItem = &podItem{
			entityType: entityType,
			entity:   entity,
			labelKey: newLabelKey,
		}
		i.podIndex[entityKey] = pItem
	}

	lItem, exists := i.labelIndex[newLabelKey]
	if exists {
		// The labelItem already exists, use the cached result directly.
		lItem.entities.Insert(entityKey)
		// TODO: It should notify group changes if pod's status is updated.
		return
	}
	lItem = i.createLabelItem(pItem)
	lItem.entities.Insert(entityKey)

	scanSelectors := func(selectors sets.String) {
		for selector := range selectors {
			sItem := i.selectorIndex[selector]
			matched := i.match(lItem.labels, lItem.namespace, sItem.selector)
			if matched {
				sItem.labels.Insert(newLabelKey)
				lItem.selectors.Insert(selector)
			}
		}
	}

	localSelectors, _ := i.namespaceToSelector[entity.GetNamespace()]
	scanSelectors(localSelectors)

	clusterSelectors, _ := i.namespaceToSelector[""]
	scanSelectors(clusterSelectors)

	for selector := range lItem.selectors.Difference(oldSelectors) {
		i.notify(selector)
	}
	for selector := range oldSelectors.Difference(lItem.selectors) {
		i.notify(selector)
	}
}

func (i *podGroupIndex) notify(selector string) {
	sItem := i.selectorIndex[selector]
	for group := range sItem.groups {
		i.notifyChan <- getGroupReference(group)
	}
}

func (i *podGroupIndex) Run() {
	for group := range i.notifyChan {
		for _, handler := range i.eventHandlers {
			handler(group)
		}
	}
}

func (i *podGroupIndex) DeleteEntity(entityType EntityType, entity metav1.Object) {
	panic("implement me")
}

func (i *podGroupIndex) deleteGroupFromSelectorItem(selector, group string) *selectorItem {
	sItem, _ := i.selectorIndex[selector]
	sItem.groups.Delete(group)
	if len(sItem.groups) > 0 {
		return sItem
	}
	// If the selector is no longer used by any Groups, remove it.
	delete(i.selectorIndex, selector)
	for label := range sItem.labels {
		lItem := i.labelIndex[label]
		lItem.selectors.Delete(selector)
	}
	return sItem
}

func (i *podGroupIndex) createSelectorItem(gItem *groupItem) *selectorItem {
	sItem := &selectorItem{
		selector:  gItem.selector,
		groups:    sets.NewString(),
		labels: sets.NewString(),
	}
	i.selectorIndex[gItem.selector.NormalizedName] = sItem

	selectors, exists := i.namespaceToSelector[sItem.selector.Namespace]
	if !exists {
		selectors = sets.NewString()
		i.namespaceToSelector[sItem.selector.Namespace] = selectors
	}
	selectors.Insert(gItem.selector.NormalizedName)
	return sItem
}

func (i *podGroupIndex) AddGroup(group *GroupReference, selector *types.GroupSelector) {
	gKey := getGroupKey(group)

	i.lock.Lock()
	defer i.lock.Unlock()

	gItem, exists := i.groupIndex[gKey]
	if exists {
		// There is no change, do nothing.
		if gItem.selector.NormalizedName == selector.NormalizedName {
			return
		}
		i.deleteGroupFromSelectorItem(gItem.selector.NormalizedName, gKey)
		gItem.selector = selector
	} else {
		gItem = &groupItem{
			group:    group,
			selector: selector,
		}
		i.groupIndex[gKey] = gItem
	}

	sItem, exists := i.selectorIndex[selector.NormalizedName]
	if exists {
		// The selectorItem already exists, no need to match labels.
		sItem.groups.Insert(gKey)
		return
	}
	sItem = i.createSelectorItem(gItem)
	sItem.groups.Insert(gKey)
	i.syncSelector(sItem)
}

func (i *podGroupIndex) syncSelector(sItem *selectorItem) bool {
	updated := false

	processOneLabelItem := func(lKey string, lItem *labelItem) {
		if i.match(lItem.labels, lItem.namespace, sItem.selector) {
			// Do nothing if the selector selected the label before.
			if sItem.labels.Has(lKey) {
				return
			}
			// Connect the selector and the label.
			sItem.labels.Insert(lKey)
			lItem.selectors.Insert(sItem.selector.NormalizedName)
			updated = true
			return
		}
		// Do nothing if the selector didn't select the label before.
		if !sItem.labels.Has(lKey) {
			return
		}
		// Disconnect the selector and the label.
		sItem.labels.Delete(lKey)
		lItem.selectors.Delete(sItem.selector.NormalizedName)
		updated = true
	}

	if sItem.selector.Namespace != "" {
		// If the selector is Namespace scoped, it can only match labels in the Namespace.
		labels, _ := i.namespaceToLabel[sItem.selector.Namespace]
		for label := range labels {
			lItem := i.labelIndex[label]
			processOneLabelItem(label, lItem)
		}
	} else {
		for label, lItem := range i.labelIndex {
			processOneLabelItem(label, lItem)
		}
	}
	return updated
}

func (i *podGroupIndex) DeleteGroup(group *GroupReference) {
	panic("implement me")
}

func (i *podGroupIndex) AddEventHandler(handler eventHandler) {
	i.eventHandlers = append(i.eventHandlers, handler)
}

func getGroupKey(group *GroupReference) string {
	return string(group.Type) +"/" + group.Name
}

func getGroupReference(group string) *GroupReference {
	parts := strings.Split(group, "/")
	return &GroupReference{Type: GroupType(parts[0]), Name: parts[1]}
}

func (i *podGroupIndex) match(label labels.Set, namespace string, sel *types.GroupSelector) bool {
	objSelector := sel.PodSelector
	if sel.Namespace != "" {
		if sel.Namespace != namespace{
			// Pods or ExternalEntities must be matched within the same Namespace.
			return false
		}
		if objSelector != nil && objSelector.Matches(label) {
			// podSelector or externalEntitySelector matches the ExternalEntity or Pod's labels.
			return true
		}
		// selector does not match the ExternalEntity or Pod's labels.
		return false
	}
	if sel.NamespaceSelector != nil {
		namespaceLabels, exists := i.namespaceLabelsIndex[namespace]
		if !exists {
			return false
		}
		if !sel.NamespaceSelector.Matches(namespaceLabels) {
			// Pod's Namespace do not match namespaceSelector.
			return false
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
	return false
}