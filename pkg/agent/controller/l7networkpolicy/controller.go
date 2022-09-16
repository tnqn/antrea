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

package l7networkpolicy

import (
	"antrea.io/antrea/pkg/agent/controller/l7networkpolicy/suricata"
	"antrea.io/antrea/pkg/agent/controller/networkpolicy"
	"k8s.io/klog/v2"
	"time"
)

const (
	controllerName = "AntreaAgentL7NetworkPolicyController"
)

type Controller struct {
	ruleEnforcer networkpolicy.RuleEnforcer
}

func New() *Controller {
	ruleEnforcer := suricata.NewEnforcer()
	c := &Controller{
		ruleEnforcer: ruleEnforcer,
	}
	return c
}

func (c *Controller) Run(stopCh <-chan struct{}) {
	klog.InfoS("Starting controller", "controller", controllerName)
	defer klog.InfoS("Shutting down controller", "controller", controllerName)

	time.Sleep(5 * time.Second)

	go c.ruleEnforcer.Run(stopCh)

	//err := c.ruleEnforcer.AddRule("foo", &types.Rule{
	//	From:     sets.NewString("192.168.0.246"),
	//	To:       sets.NewString("192.168.1.38"),
	//	Protocol: nil,
	//	Action:   "",
	//})
	//if err != nil {
	//	klog.ErrorS(err, "Failed to add test rule")
	//}

	<-stopCh
}
