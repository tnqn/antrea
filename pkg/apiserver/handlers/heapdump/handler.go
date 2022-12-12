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

package heapdump

import (
	"net/http"
	"os"
	"runtime/debug"

	"k8s.io/klog/v2"
)

// HandleFunc returns the function which can handle the /loglevel API request.
func HandleFunc() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f, err := os.Create("/root/heapdump")
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			klog.ErrorS(err, "Error when creating heapdump")
			return
		}
		defer f.Close()
		debug.WriteHeapDump(f.Fd())
		w.Write([]byte("Done"))
	}
}
