package tcpdump

import (
	"antrea.io/antrea/pkg/antctl/raw"
	"antrea.io/antrea/pkg/antctl/runtime"
	"context"
	"fmt"
	"github.com/spf13/cobra"
	"io"
	"k8s.io/client-go/rest"
	"os"
	"strings"
	"time"
)

var Command *cobra.Command

type tcpdumpOptions struct {
	podName      string
	podNamespace string

	staticDir     string
	staticPrefix  string
	apiPrefix     string
	acceptPaths   string
	rejectPaths   string
	acceptHosts   string
	rejectMethods string
	port          int
	address       string
	disableFilter bool
	unixSocket    string
	keepalive     time.Duration

	agentNodeName string
}

var options *tcpdumpOptions

// validateAndComplete checks the tcpdumpOptions to see if there is sufficient information to run the
// command, and adds default values when needed.
func (o *tcpdumpOptions) validateAndComplete(args []string) error {
	o.podName = args[0]
	return nil
}

var tcpdumpCommandExample = strings.Trim(`
  Start a reverse proxy for the Antrea Controller API
  $ antctl tcpdump <Pod Name> -n <Pod Namespace>
`, "\n")

func init() {
	Command = &cobra.Command{
		Use:     "tcpdump",
		Short:   "Run a reverse proxy to access Antrea API",
		Long:    "Run a reverse proxy to access Antrea API (Controller or Agent). Command only supports remote mode. HTTPS connections between the proxy and the Antrea API will not be secure (no certificate verification).",
		Example: tcpdumpCommandExample,
		RunE:    runE,
		Args:    cobra.ExactArgs(1),
	}

	// Options are the same as for "kubectl proxy"
	// https://github.com/kubernetes/kubectl/blob/v0.19.0/pkg/cmd/proxy/proxy.go
	o := &tcpdumpOptions{}
	options = o
	Command.Flags().StringVarP(&o.podNamespace, "namespace", "n", "", "Also serve static files from the given directory under the specified prefix.")
}

func runE(cmd *cobra.Command, args []string) error {
	if runtime.Mode != runtime.ModeController || runtime.InPod {
		return fmt.Errorf("only remote mode is supported for this command")
	}

	if err := options.validateAndComplete(args); err != nil {
		return err
	}

	kubeconfig, err := raw.ResolveKubeconfig(cmd)
	if err != nil {
		return err
	}
	restconfigTmpl := rest.CopyConfig(kubeconfig)
	raw.SetupKubeconfig(restconfigTmpl)
	if server, err := Command.Flags().GetString("server"); err != nil {
		kubeconfig.Host = server
	}

	k8sClientset, antreaClientset, err := raw.SetupClients(kubeconfig)
	if err != nil {
		return fmt.Errorf("failed to create clientset: %w", err)
	}

	var clientCfg *rest.Config
	if !true {
		clientCfg, err = raw.CreateControllerClientCfg(k8sClientset, antreaClientset, restconfigTmpl)
		if err != nil {
			return fmt.Errorf("error when creating Controller client config: %w", err)
		}
	} else {
		clientCfg, err = raw.CreateAgentClientCfg(k8sClientset, antreaClientset, restconfigTmpl, "bm01")
		if err != nil {
			return fmt.Errorf("error when creating Agent client config: %w", err)
		}
	}

	client, err := rest.UnversionedRESTClientFor(clientCfg)
	if err != nil {
		return err
	}
	stream, err := client.Get().Prefix("/tcpdump").Param("name", options.podName).Param("namespace", options.podNamespace).Stream(context.TODO())
	if err != nil {
		return err
	}
	defer stream.Close()
	if _, err := io.Copy(os.Stdout, stream); err != nil {
		return fmt.Errorf("error when dumping: %w", err)
	}
	return nil
}
