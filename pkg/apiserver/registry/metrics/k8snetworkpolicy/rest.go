package k8snetworkpolicy

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/internalversion"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/registry/rest"

	cpv1alpha1 "github.com/vmware-tanzu/antrea/pkg/apis/controlplane/v1alpha1"
	metricsv1alpha1 "github.com/vmware-tanzu/antrea/pkg/apis/metrics/v1alpha1"
)

type StatsProvider interface {
	List() []*cpv1alpha1.NetworkPolicyStats
}

type REST struct {
	statsProvider StatsProvider
}

// NewREST returns a REST object that will work against API services.
func NewREST(p StatsProvider) *REST {
	return &REST{p}
}

var (
	_ rest.Storage = &REST{}
	_ rest.Scoper  = &REST{}
	_ rest.Getter  = &REST{}
	_ rest.Lister  = &REST{}
)

func (m *REST) New() runtime.Object {
	return &metricsv1alpha1.K8sNetworkPolicyMetric{}
}

func (m *REST) NewList() runtime.Object {
	return &metricsv1alpha1.K8sNetworkPolicyMetricList{}
}

func (m *REST) List(ctx context.Context, options *internalversion.ListOptions) (runtime.Object, error) {
	statsList := m.statsProvider.List()

	items := make([]metricsv1alpha1.K8sNetworkPolicyMetric, len(statsList))
	for i, stats := range statsList {
		items[i].Name = stats.NetworkPolicy.Name
		items[i].Stats.Packets = stats.RuleStats.Packets
		items[i].Stats.Bytes = stats.RuleStats.Bytes
		items[i].Stats.Sessions = stats.RuleStats.Sessions
	}

	metricList := &metricsv1alpha1.K8sNetworkPolicyMetricList{
		Items: items,
	}
	return metricList, nil
}

func (m *REST) ConvertToTable(ctx context.Context, object runtime.Object, tableOptions runtime.Object) (*v1.Table, error) {
	panic("implement me")
}

func (m *REST) Get(ctx context.Context, name string, options *v1.GetOptions) (runtime.Object, error) {
	panic("implement me")
}

func (m *REST) NamespaceScoped() bool {
	return true
}