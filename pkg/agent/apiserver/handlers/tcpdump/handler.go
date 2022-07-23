package tcpdump

import (
	"antrea.io/antrea/pkg/agent/querier"
	"k8s.io/klog/v2"
	"net/http"
	"os/exec"
	"time"

	"k8s.io/apiserver/pkg/util/flushwriter"
)

// HandleFunc returns the function which can handle queries issued by agentinfo commands.
// The handler function populates Antrea agent information to the response.
func HandleFunc(aq querier.AgentQuerier) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("name")
		ns := r.URL.Query().Get("namespace")

		interfaces := aq.GetInterfaceStore().GetContainerInterfacesByPod(name, ns)
		if len(interfaces) == 0 {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		interfaceName := interfaces[0].InterfaceName

		ctx := r.Context()
		fw := flushwriter.Wrap(w)
		w.Header().Set("Transfer-Encoding", "chunked")

		cmd := exec.CommandContext(ctx, "tcpdump", "-i", interfaceName, "-l", "-n")
		cmd.Stdout = fw
		cmd.Stderr = fw
		if err := cmd.Run(); err != nil {
			klog.ErrorS(err, "Failed to run tcpdump")
		}

		time.Sleep(5 * time.Second)
	}
}
