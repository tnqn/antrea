// Copyright 2022 Antrea Authors
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

package suricata

import (
	v1beta "antrea.io/antrea/pkg/apis/controlplane/v1beta2"
	"bytes"
	"fmt"
	"k8s.io/klog/v2"
	"net"
	"os"
	"os/exec"
	"strings"
)

const (
	signaturePath = "/etc/suricata/rules/antrea-l7-networkpolicy.rules"
)

type Enforcer struct {
	rules map[string]string
}

func NewEnforcer() *Enforcer {
	e := &Enforcer{
		rules: map[string]string{},
	}
	return e
}

func (e *Enforcer) Run(stopCh <-chan struct{}) {
	// suricata -c suricata.yaml --af-packet
	// ovs-vsctl add-port br-int
	afPacketConf := `%YAML 1.1
---
af-packet:
  - interface: suricata0
    threads: 4
    cluster-id: 98
    cluster-type: cluster_flow
    defrag: no
    use-mmap: yes
    copy-mode: ips
    copy-iface: suricata1
  - interface: suricata1
    threads: 4
    cluster-id: 99
    cluster-type: cluster_flow
    defrag: no
    use-mmap: yes
    copy-mode: ips
    copy-iface: suricata0
default-rule-path: /etc/suricata/rules
rule-files:
  - antrea-l7-networkpolicy.rules
`
	if err := os.WriteFile("/etc/suricata/antrea.yaml", []byte(afPacketConf), 0600); err != nil {
		klog.ErrorS(err, "Failed to write af-packet conf file")
		return
	}
	f, err := os.OpenFile("/etc/suricata/suricata.yaml", os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		klog.ErrorS(err, "Failed to open Suricata conf file")
		return
	}
	defer f.Close()
	_, err = f.WriteString("include: /etc/suricata/antrea.yaml\n")
	if err != nil {
		klog.ErrorS(err, "Failed to update Suricata conf file")
		return
	}
	cmd := exec.Command("suricata", "-c", "/etc/suricata/suricata.yaml", "--af-packet")
	err = cmd.Start()
	if err != nil {
		klog.ErrorS(err, "Failed to run Suricata instance")
	}
	klog.InfoS("Started Suricata instance")
	return
}

func (e *Enforcer) syncRules() error {
	signatures := bytes.NewBuffer(nil)
	for _, rule := range e.rules {
		signatures.WriteString(rule)
		signatures.WriteByte('\n')
	}
	f, err := os.OpenFile(signaturePath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(signatures.Bytes())
	if err != nil {
		return err
	}

	cmd := exec.Command("suricatasc", "-c", "reload-rules")
	output, err := cmd.CombinedOutput()
	if err != nil {
		klog.ErrorS(err, "Failed to reload Suricata rules", "output", output)
	}
	klog.InfoS("Reloaded Suricata rules")
	// ...
	return nil
}

func (e *Enforcer) AddRule(name string, from v1beta.GroupMemberSet, target v1beta.GroupMemberSet, services []v1beta.Service) error {
	//action := "drop"
	protocol := "http"
	srcIPs := "[]"
	if len(from) > 0 {
		ips := make([]string, 0, len(from))
		for _, a := range from {
			for _, ip := range a.IPs {
				ips = append(ips, net.IP(ip).String())
			}
		}
		srcIPs = fmt.Sprintf("[%s]", strings.Join(ips, ","))
	}
	srcPorts := "any"
	dstIPs := "any"
	if len(target) > 0 {
		ips := make([]string, 0, len(target))
		for _, a := range target {
			for _, ip := range a.IPs {
				ips = append(ips, net.IP(ip).String())
			}
		}
		dstIPs = fmt.Sprintf("[%s]", strings.Join(ips, ","))
	}
	dstPorts := "any"
	uri := ""
	method := ""
	host := ""
	if len(services) > 0 {
		service := services[0]
		if service.Path != "" {
			uri = fmt.Sprintf("http.uri; content:\"%s\"; ", service.Path)
		}
		if service.Method != "" {
			method = fmt.Sprintf("http.method; content:\"%s\"; ", service.Method)
		}
		if service.Host != "" {
			host = fmt.Sprintf("http.host; content:\"%s\"; ", service.Host)
		}
	}
	signature := fmt.Sprintf("%s %s %s %s -> %s %s (%s%s%ssid:1; rev:1;)", "pass", protocol, srcIPs, srcPorts, dstIPs, dstPorts, uri, method, host)
	defaultDropSignature := fmt.Sprintf("%s %s any any -> %s %s (sid:2; rev:1;)", "drop", protocol, dstIPs, dstPorts)
	e.rules[name] = signature + "\n" + defaultDropSignature

	err := e.syncRules()
	if err != nil {
		return err
	}
	return nil
}

func (e *Enforcer) DeleteRule(name string) error {
	delete(e.rules, name)
	err := e.syncRules()
	if err != nil {
		return err
	}
	return nil
}
