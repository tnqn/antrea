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

package e2e

import (
	"crypto/tls"
	"fmt"
	"net"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"antrea.io/antrea/pkg/apis"
	agentconfig "antrea.io/antrea/pkg/config/agent"
	controllerconfig "antrea.io/antrea/pkg/config/controller"
)

const (
	cipherSuite    = tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256 // a TLS1.2 Cipher Suite
	cipherSuiteStr = "ECDHE-RSA-AES128-GCM-SHA256"
)

var (
	cipherSuites             = []uint16{cipherSuite}
	opensslTLS13CipherSuites = []string{"TLS_AES_128_GCM_SHA256", "TLS_AES_256_GCM_SHA384", "TLS_CHACHA20_POLY1305_SHA256"}
)

// TestAntreaApiserverTLSConfig tests Cipher Suite and TLSVersion config on Antrea apiserver, Controller side or Agent side.
func TestAntreaApiserverTLSConfig(t *testing.T) {
	skipIfHasWindowsNodes(t)
	skipIfNotRequired(t, "mode-irrelevant")

	data, err := setupTest(t)
	if err != nil {
		t.Fatalf("Error when setting up test: %v", err)
	}
	defer teardownTest(t, data)

	data.configureTLS(t, cipherSuites, "VersionTLS12")

	controllerPod, err := data.getAntreaController()
	assert.NoError(t, err, "failed to get Antrea Controller Pod")
	controllerPodName := controllerPod.Name
	controlPlaneNode := controlPlaneNodeName()
	agentPodName, err := data.getAntreaPodOnNode(controlPlaneNode)
	assert.NoError(t, err, "failed to get Antrea Agent Pod Name on Control Plane Node")

	tests := []struct {
		name          string
		podName       string
		containerName string
		apiserver     int
		apiserverStr  string
	}{
		{"ControllerApiserver", controllerPodName, controllerContainerName, apis.AntreaControllerAPIPort, "Controller"},
		{"AgentApiserver", agentPodName, agentContainerName, apis.AntreaAgentAPIPort, "Agent"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data.checkTLS(t, tc.podName, tc.containerName, tc.apiserver, tc.apiserverStr)
		})
	}
}

func (data *TestData) configureTLS(t *testing.T, cipherSuites []uint16, tlsMinVersion string) {
	var cipherSuitesStr string
	for i, cs := range cipherSuites {
		cipherSuitesStr = fmt.Sprintf("%s%s", cipherSuitesStr, tls.CipherSuiteName(cs))
		if i != len(cipherSuites)-1 {
			cipherSuitesStr = fmt.Sprintf("%s,", cipherSuitesStr)
		}
	}

	cc := func(config *controllerconfig.ControllerConfig) {
		config.TLSCipherSuites = cipherSuitesStr
		config.TLSMinVersion = tlsMinVersion
	}
	ac := func(config *agentconfig.AgentConfig) {
		config.TLSCipherSuites = cipherSuitesStr
		config.TLSMinVersion = tlsMinVersion
	}
	if err := data.mutateAntreaConfigMap(cc, ac, true, true); err != nil {
		t.Fatalf("Failed to configure Cipher Suites and TLSMinVersion: %v", err)
	}
}

func (data *TestData) checkTLS(t *testing.T, podName string, containerName string, apiserver int, apiserverStr string) {
	// Set TLSMaxVersion to TLS1.2, then TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256 should be used
	stdouts := data.opensslConnect(t, podName, containerName, true, apiserver)
	matchMsg := "New, TLSv1/SSLv3, Cipher is %s"
	oldOpenssl := data.isOldOpenssl(t, podName, containerName)
	if !oldOpenssl {
		matchMsg = "New, TLSv1.2, Cipher is %s"
	}
	for _, stdout := range stdouts {
		assert.True(t, strings.Contains(stdout, fmt.Sprintf(matchMsg, cipherSuiteStr)),
			"Cipher Suite used by %s apiserver should be the TLS1.2 one '%s', output: %s", apiserverStr, cipherSuiteStr, stdout)
	}
}

func (data *TestData) opensslConnect(t *testing.T, pod string, container string, tls12 bool, port int) []string {
	var stdouts []string
	opensslConnectCommands := []struct {
		enabled bool
		ip      string
	}{
		{
			clusterInfo.podV4NetworkCIDR != "",
			"127.0.0.1",
		},
	}
	for _, c := range opensslConnectCommands {
		if !c.enabled {
			continue
		}
		cmd := []string{"timeout", "1", "openssl", "s_client", "-connect", net.JoinHostPort(c.ip, fmt.Sprint(port))}
		if tls12 {
			cmd = append(cmd, "-tls1_2")
		}
		stdout, stderr, err := data.RunCommandFromPod(antreaNamespace, pod, container, cmd)
		assert.NoError(t, err, "failed to run openssl command on Pod '%s'\nstderr: %s", pod, stderr)
		t.Logf("Ran '%s' on Pod %s", strings.Join(cmd, " "), pod)
		stdouts = append(stdouts, stdout)
	}
	return stdouts
}

func (data *TestData) isOldOpenssl(t *testing.T, pod string, container string) bool {
	cmd := []string{"openssl", "version"}
	stdout, stderr, err := data.RunCommandFromPod(antreaNamespace, pod, container, cmd)
	// sample output: OpenSSL 1.1.1  11 Sep 2018
	if err != nil {
		t.Fatalf("Failed to get openssl version, error: %v", err)
	}
	if stderr != "" {
		t.Fatalf("Failed to get openssl version, stderr: %s", stderr)
	}

	outSlice := strings.Split(stdout, " ")
	if len(outSlice) < 2 {
		t.Fatalf("Failed to get openssl version, version output has incorrect length")
	}
	verDetail := strings.Split(outSlice[1], ".")
	if len(verDetail) < 2 {
		t.Fatalf("Failed to get openssl version, version number output has incorrect length")
	}
	firstDigit, err := strconv.Atoi(verDetail[0])
	if err != nil {
		t.Fatalf("Failed to convert string to int: %v", err)
	}
	secondDigit, err := strconv.Atoi(verDetail[1])
	if err != nil {
		t.Fatalf("Failed to convert string to int: %v", err)
	}
	if (firstDigit == 1 && secondDigit >= 1) || firstDigit > 1 {
		return false
	}
	return true
}
