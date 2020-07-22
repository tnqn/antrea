package stats

import (
	"encoding/json"
	"net/http"

	"k8s.io/klog"

	"github.com/vmware-tanzu/antrea/pkg/apis/controlplane/v1alpha1"
)


type StatsCollector interface {
	Collect(statsList []v1alpha1.NetworkPolicyStats)
}

func HandleFunc(c StatsCollector) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var statsList []v1alpha1.NetworkPolicyStats

		err := json.NewDecoder(r.Body).Decode(&statsList)
		if err != nil {
			klog.Errorf("Error decoding body: %v", err)
		}
		c.Collect(statsList)
	}
}