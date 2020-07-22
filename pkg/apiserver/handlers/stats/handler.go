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

package stats

import (
	"encoding/json"
	"net/http"

	"k8s.io/klog"

	"github.com/vmware-tanzu/antrea/pkg/apis/controlplane/v1alpha1"
)

// statsCollector is the interface required by the handler.
// Aggregator implements it.
type statsCollector interface {
	Collect(summary *v1alpha1.NodeStatsSummary)
}

func HandleFunc(c statsCollector) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var summary v1alpha1.NodeStatsSummary

		err := json.NewDecoder(r.Body).Decode(&summary)
		if err != nil {
			klog.Errorf("Error decoding NodeStatsSummary body: %v", err)
			return
		}
		c.Collect(&summary)
	}
}
