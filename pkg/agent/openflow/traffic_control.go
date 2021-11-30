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

package openflow

import (
	"antrea.io/antrea/pkg/agent/openflow/cookie"
	"antrea.io/antrea/pkg/apis/crd/v1alpha1"
	binding "antrea.io/antrea/pkg/ovs/openflow"
)

/*
ovs-ofctl del-flows br-int "cookie=0x1/-1"

ovs-ofctl add-flow br-int "cookie=0x1, table=105, priority=300,ct_state=+trk,in_port=8,ip actions=ct(commit,table=HairpinSNAT,zone=65520,exec(load:0x23->NXM_NX_CT_MARK[]))"
ovs-ofctl add-flow br-int "cookie=0x1, table=110, priority=300,ct_state=+trk,ct_mark=0x23 actions=output:32"
ovs-ofctl add-flow br-int "cookie=0x1, table=0, priority=200,in_port=32 actions=load:0x1->NXM_NX_REG0[19],resubmit(,70)"
ovs-ofctl add-flow br-int "cookie=0x1, table=0, priority=200,in_port=33 actions=load:0x1->NXM_NX_REG0[19],resubmit(,70)"

ovs-ofctl add-flow br-int "cookie=0x1, table=105, priority=300,ct_state=+trk,in_port=8,ip actions=ct(commit,table=HairpinSNAT,zone=65520,exec(load:0x23->NXM_NX_CT_MARK[]))"
ovs-ofctl add-flow br-int "cookie=0x1, table=110, priority=300,ct_state=+trk,ct_mark=0x23 actions=output:NXM_NX_REG1[],output:32"


ovs-ofctl add-flow br-int "cookie=0x1, table=0, priority=200,in_port=32 actions=load:0x1->NXM_NX_REG0[19],resubmit(,70)"
ovs-ofctl add-flow br-int "cookie=0x1, table=0, priority=200,in_port=33 actions=load:0x1->NXM_NX_REG0[19],resubmit(,70)"
ovs-ofctl add-flow br-int "cookie=0x1, table=110, priority=300,ct_state=+trk,ct_mark=0x23 actions=output:32"
novs-ofctl add-flow br-int "cookie=0x1, table=110, priority=300,ct_state=+trk+rpl,ct_mark=0x23 actions=output:32"

ovs-ofctl add-flow br-int "cookie=0x1, table=110, priority=300,ct_state=+trk-rpl,ct_mark=0x23 actions=output:NXM_NX_REG1[],output:32"
ovs-ofctl add-flow br-int "cookie=0x1, table=110, priority=300,ct_state=+trk+rpl,ct_mark=0x23 actions=output:NXM_NX_REG1[],output:33"

ovs-ofctl add-flow br-int "cookie=0x1, table=110, priority=300,ct_state=+trk-rpl,ct_mark=0x23 actions=output:32,output:34"
ovs-ofctl add-flow br-int "cookie=0x1, table=110, priority=300,ct_state=+trk+rpl,ct_mark=0x23 actions=output:33,output:34"
*/
func (c *client) InstallTrafficControlRule(name string, srcOfPorts []uint32, direction v1alpha1.Direction, action v1alpha1.Action, dstOfPort uint32) error {
	c.replayMutex.RLock()
	defer c.replayMutex.RUnlock()

	flows := make([]binding.Flow, 0, len(srcOfPorts))
	for _, ofPort := range srcOfPorts {
		// ovs-ofctl add-flow br-int "cookie=0x1, table=110, priority=300,in_port=8 actions=output:16"
		flows = append(flows, L2ForwardingOutTable.BuildFlow(priorityHigh).
			MatchInPort(ofPort).
			Action().Output(dstOfPort).
			Cookie(c.cookieAllocator.Request(cookie.TrafficControl).Raw()).
			Done())
		// ovs-ofctl add-flow br-int "cookie=0x1, table=105, priority=300,ct_state=+trk,in_port=8,ip actions=ct(commit,table=HairpinSNAT,zone=65520,exec(load:0x23->NXM_NX_CT_MARK[]))"
	}
	return c.AddAll(flows)
}