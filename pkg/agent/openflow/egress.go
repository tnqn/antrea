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

package openflow

import (
	"antrea.io/antrea/pkg/agent/types"
	"net"
	"sync"

	"antrea.io/libOpenflow/openflow15"

	"antrea.io/antrea/pkg/agent/config"
	"antrea.io/antrea/pkg/agent/openflow/cookie"
	binding "antrea.io/antrea/pkg/ovs/openflow"
)

type featureEgress struct {
	cookieAllocator cookie.Allocator
	ipProtocols     []binding.Protocol

	cachedFlows *flowCategoryCache
	cachedMeter sync.Map

	exceptCIDRs map[binding.Protocol][]net.IPNet
	nodeIPs     map[binding.Protocol]net.IP
	gatewayMAC  net.HardwareAddr

	category                   cookie.Category
	enableEgressTrafficShaping bool
}

func (f *featureEgress) getFeatureName() string {
	return "Egress"
}

func newFeatureEgress(cookieAllocator cookie.Allocator,
	ipProtocols []binding.Protocol,
	nodeConfig *config.NodeConfig,
	egressConfig *config.EgressConfig,
	enableEgressTrafficShaping bool) *featureEgress {
	exceptCIDRs := make(map[binding.Protocol][]net.IPNet)
	for _, cidr := range egressConfig.ExceptCIDRs {
		if cidr.IP.To4() == nil {
			exceptCIDRs[binding.ProtocolIPv6] = append(exceptCIDRs[binding.ProtocolIPv6], cidr)
		} else {
			exceptCIDRs[binding.ProtocolIP] = append(exceptCIDRs[binding.ProtocolIP], cidr)
		}
	}

	nodeIPs := make(map[binding.Protocol]net.IP)
	for _, ipProtocol := range ipProtocols {
		if ipProtocol == binding.ProtocolIP {
			nodeIPs[ipProtocol] = nodeConfig.NodeIPv4Addr.IP
		} else if ipProtocol == binding.ProtocolIPv6 {
			nodeIPs[ipProtocol] = nodeConfig.NodeIPv6Addr.IP
		}
	}
	return &featureEgress{
		cachedFlows:                newFlowCategoryCache(),
		cachedMeter:                sync.Map{},
		cookieAllocator:            cookieAllocator,
		exceptCIDRs:                exceptCIDRs,
		ipProtocols:                ipProtocols,
		nodeIPs:                    nodeIPs,
		gatewayMAC:                 nodeConfig.GatewayConfig.MAC,
		category:                   cookie.Egress,
		enableEgressTrafficShaping: enableEgressTrafficShaping,
	}
}

// snatSkipCIDRFlow generates the flow to skip SNAT for connection destined for the provided CIDR.
func (f *featureEgress) snatSkipCIDRFlow(cidr net.IPNet) binding.Flow {
	ipProtocol := getIPProtocol(cidr.IP)
	return EgressMarkTable.ofTable.BuildFlow(priorityHigh).
		Cookie(f.cookieAllocator.Request(f.category).Raw()).
		MatchProtocol(ipProtocol).
		MatchDstIPNet(cidr).
		Action().LoadRegMark(ToGatewayRegMark).
		Action().GotoStage(stageSwitching).
		Done()
}

// snatSkipNodeFlow generates the flow to skip SNAT for connection destined for the transport IP of a remote Node.
func (f *featureEgress) snatSkipNodeFlow(nodeIP net.IP) binding.Flow {
	ipProtocol := getIPProtocol(nodeIP)
	return EgressMarkTable.ofTable.BuildFlow(priorityHigh).
		Cookie(f.cookieAllocator.Request(f.category).Raw()).
		MatchProtocol(ipProtocol).
		MatchDstIP(nodeIP).
		Action().LoadRegMark(ToGatewayRegMark).
		Action().GotoStage(stageSwitching).
		Done()
}

// snatIPFromTunnelFlow generates the flow that marks SNAT packets tunnelled from remote Nodes. The SNAT IP matches the
// packet's tunnel destination IP.
func (f *featureEgress) snatIPFromTunnelFlow(snatIP net.IP, mark uint32) binding.Flow {
	ipProtocol := getIPProtocol(snatIP)
	fb := EgressMarkTable.ofTable.BuildFlow(priorityNormal).
		Cookie(f.cookieAllocator.Request(f.category).Raw()).
		MatchProtocol(ipProtocol).
		MatchCTStateTrk(true).
		MatchTunnelDst(snatIP).
		Action().LoadPktMarkRange(mark, snatPktMarkRange).
		Action().LoadRegMark(ToGatewayRegMark)
	if f.enableEgressTrafficShaping {
		// To apply rate-limit on all traffic.
		fb = fb.Action().GotoTable(EgressQoSTable.GetID())
	} else {
		fb = fb.Action().GotoStage(stageSwitching)
	}
	return fb.Done()
}

// snatRuleFlow generates the flow that applies the SNAT rule for a local Pod. If the SNAT IP exists on the local Node,
// it sets the packet mark with the ID of the SNAT IP, for the traffic from local Pods to external; if the SNAT IP is
// on a remote Node, it tunnels the packets to the remote Node.
func (f *featureEgress) snatRuleFlow(ofPort uint32, snatIP net.IP, snatMark uint32, localGatewayMAC net.HardwareAddr) binding.Flow {
	cookieID := f.cookieAllocator.Request(f.category).Raw()
	ipProtocol := getIPProtocol(snatIP)
	if snatMark != 0 {
		// Local SNAT IP.
		fb := EgressMarkTable.ofTable.BuildFlow(priorityNormal).
			Cookie(cookieID).
			MatchProtocol(ipProtocol).
			MatchCTStateTrk(true).
			MatchInPort(ofPort).
			Action().LoadPktMarkRange(snatMark, snatPktMarkRange).
			Action().LoadRegMark(ToGatewayRegMark)
		if f.enableEgressTrafficShaping {
			// To apply rate-limit on all traffic.
			fb = fb.Action().GotoTable(EgressQoSTable.GetID())
		} else {
			fb = fb.Action().GotoStage(stageSwitching)
		}
		return fb.Done()
	}
	// SNAT IP should be on a remote Node.
	return EgressMarkTable.ofTable.BuildFlow(priorityNormal).
		Cookie(cookieID).
		MatchProtocol(ipProtocol).
		MatchInPort(ofPort).
		Action().SetSrcMAC(localGatewayMAC).
		Action().SetDstMAC(GlobalVirtualMAC).
		Action().SetTunnelDst(snatIP). // Set tunnel destination to the SNAT IP.
		Action().LoadRegMark(ToTunnelRegMark, RemoteSNATRegMark).
		Action().GotoStage(stageSwitching).
		Done()
}

func (f *featureEgress) egressQoSFlow(mark uint32) binding.Flow {
	return EgressQoSTable.ofTable.BuildFlow(priorityNormal).
		Cookie(f.cookieAllocator.Request(f.category).Raw()).
		MatchPktMark(mark, &types.SNATIPMarkMask).
		Action().Meter(mark).
		Action().GotoStage(stageSwitching).
		Done()
}

func (f *featureEgress) egressQoSDefaultFlow() binding.Flow {
	return EgressQoSTable.ofTable.BuildFlow(priorityLow).
		Cookie(f.cookieAllocator.Request(f.category).Raw()).
		Action().GotoStage(stageSwitching).
		Done()
}

// externalFlows generates the flows to perform SNAT for the packets of connection to the external network. The flows identify
// the packets to external network, and send them to EgressMarkTable, where SNAT IPs are looked up for the packets.
func (f *featureEgress) externalFlows() []binding.Flow {
	cookieID := f.cookieAllocator.Request(f.category).Raw()
	var flows []binding.Flow
	for _, ipProtocol := range f.ipProtocols {
		flows = append(flows,
			// This generates the flow to match the packets sourced from local Pods and destined for external network, then
			// forward them to EgressMarkTable.
			L3ForwardingTable.ofTable.BuildFlow(priorityLow).
				Cookie(cookieID).
				MatchProtocol(ipProtocol).
				MatchCTStateRpl(false).
				MatchCTStateTrk(true).
				MatchRegMark(FromLocalRegMark, NotAntreaFlexibleIPAMRegMark).
				Action().GotoTable(EgressMarkTable.GetID()).
				Done(),
			// This generates the flow to match the packets sourced from tunnel and destined for external network, then
			// forward them to EgressMarkTable.
			L3ForwardingTable.ofTable.BuildFlow(priorityLow).
				Cookie(cookieID).
				MatchProtocol(ipProtocol).
				MatchCTStateRpl(false).
				MatchCTStateTrk(true).
				MatchRegMark(FromTunnelRegMark).
				Action().SetDstMAC(f.gatewayMAC).
				Action().GotoTable(EgressMarkTable.GetID()).
				Done(),
			// This generates the default flow to drop the packets from remote Nodes and there is no matched SNAT policy.
			EgressMarkTable.ofTable.BuildFlow(priorityLow).
				Cookie(cookieID).
				MatchProtocol(ipProtocol).
				MatchCTStateNew(true).
				MatchCTStateTrk(true).
				MatchRegMark(FromTunnelRegMark).
				Action().Drop().
				Done(),
			// This generates the flow to bypass the packets destined for local Node.
			f.snatSkipNodeFlow(f.nodeIPs[ipProtocol]),
		)
		// This generates the flows to bypass the packets sourced from local Pods and destined for the except CIDRs for Egress.
		for _, cidr := range f.exceptCIDRs[ipProtocol] {
			flows = append(flows, f.snatSkipCIDRFlow(cidr))
		}
	}
	// This generates the flow to match the packets of tracked Egress connection and forward them to stageSwitching.
	flows = append(flows, EgressMarkTable.ofTable.BuildFlow(priorityMiss).
		Cookie(cookieID).
		Action().LoadRegMark(ToGatewayRegMark).
		Action().GotoStage(stageSwitching).
		Done())

	return flows
}

func (f *featureEgress) initFlows() []*openflow15.FlowMod {
	// This installs the flows to enable Pods to communicate to the external IP addresses. The flows identify the packets
	// from local Pods to the external IP address, and mark the packets to be SNAT'd with the configured SNAT IPs.
	initialFlows := f.externalFlows()
	if f.enableEgressTrafficShaping {
		initialFlows = append(initialFlows, f.egressQoSDefaultFlow())
	}
	return GetFlowModMessages(initialFlows, binding.AddMessage)
}

func (f *featureEgress) replayFlows() []*openflow15.FlowMod {
	return getCachedFlowMessages(f.cachedFlows)
}

func (f *featureEgress) initGroups() []binding.OFEntry {
	return nil
}

func (f *featureEgress) replayGroups() []binding.OFEntry {
	return nil
}

func (f *featureEgress) replayMeters() []binding.OFEntry {
	var meters []binding.OFEntry
	f.cachedMeter.Range(func(id, value interface{}) bool {
		meter := value.(binding.Meter)
		meter.Reset()
		meters = append(meters, meter)
		return true
	})
	return meters
}

func (f *featureEgress) getRequiredTables() []*Table {
	tables := []*Table{
		L3ForwardingTable,
		EgressMarkTable,
	}
	if f.enableEgressTrafficShaping {
		tables = append(tables, EgressQoSTable)
	}
	return tables
}
