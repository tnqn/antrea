package networkpolicy

import (
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"testing"
)

func newNamespace(name string, labels map[string]string) *v1.Namespace {
	return &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: labels,
		},
	}
}

func newPod(namespace, name string, labels map[string]string) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name: name,
			Labels: labels,
		},
	}
}

func TestPodGroupIndexGetPods(t *testing.T) {
	type args struct {
		group *GroupReference
	}
	tests := []struct {
		name   string
		args   args
		want   []*v1.Pod
		want1  bool
	}{
		{

		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			index := NewPodGroupIndex()

			index.AddEventHandler(func(group *GroupReference) {
				t.Logf("Got %v", group)
			})
			namespace := newNamespace("default", nil)
			pod1 := newPod("default", "pod1", map[string]string{"app": "foo"})
			pod2 := newPod("default", "pod2", map[string]string{"app": "foo"})
			pod3 := newPod("default", "pod3", map[string]string{"app": "bar"})
			group1 := &GroupReference{
				Type: AppliedToGroupType,
				Name: "group1",
			}
			group2 := &GroupReference{
				Type: AppliedToGroupType,
				Name: "group2",
			}

			index.AddEntity(PodType, pod1)
			index.AddEntity(PodType, pod2)
			index.AddEntity(PodType, pod3)

			index.AddGroup(group1, toGroupSelector("default", &metav1.LabelSelector{MatchLabels: map[string]string{"app": "foo"}}, nil, nil))
			index.AddGroup(group2, toGroupSelector("default", &metav1.LabelSelector{MatchLabels: map[string]string{"app": "bar"}}, nil, nil))

			index.AddNamespace(namespace)

			pods, _ := index.GetEntities(group1)
			assert.ElementsMatch(t, []*v1.Pod{pod1, pod2}, pods)
			pods, _ = index.GetEntities(group2)
			assert.ElementsMatch(t, []*v1.Pod{pod3}, pods)
			index.AddEntity(PodType, newPod("default", "pod4", map[string]string{"app": "foo"}))
		})
	}
}