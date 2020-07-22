package clusternetworkpolicy

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/internalversion"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/registry/rest"

	metricsv1alpha1 "github.com/vmware-tanzu/antrea/pkg/apis/metrics/v1alpha1"
	"github.com/vmware-tanzu/antrea/pkg/controller/stats"
)

type REST struct {
	metricProvider stats.MetricProvider
}

// NewREST returns a REST object that will work against API services.
func NewREST(metricProvider stats.MetricProvider) *REST {
	return &REST{metricProvider}
}

var (
	_ rest.Storage = &REST{}
	_ rest.Scoper  = &REST{}
	_ rest.Getter  = &REST{}
	_ rest.Lister  = &REST{}
)

func (m *REST) New() runtime.Object {
	return &metricsv1alpha1.ClusterNetworkPolicyMetric{}
}

func (m *REST) NewList() runtime.Object {
	return &metricsv1alpha1.ClusterNetworkPolicyMetricList{}
}

func (m *REST) List(ctx context.Context, options *internalversion.ListOptions) (runtime.Object, error) {
	metricList := m.metricProvider.List()
	list := &metricsv1alpha1.ClusterNetworkPolicyMetricList{
		Items: metricList,
	}
	return list, nil
}

func (m *REST) ConvertToTable(ctx context.Context, object runtime.Object, tableOptions runtime.Object) (*v1.Table, error) {
	panic("implement me")
}

func (m *REST) Get(ctx context.Context, name string, options *v1.GetOptions) (runtime.Object, error) {
	panic("implement me")
}

func (m *REST) NamespaceScoped() bool {
	return false
}