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
	"antrea.io/libOpenflow/protocol"
	"encoding/binary"
	"net"

	"antrea.io/libOpenflow/openflow15"

	"antrea.io/antrea/pkg/agent/config"
	"antrea.io/antrea/pkg/agent/openflow/cookie"
	"antrea.io/antrea/pkg/apis/crd/v1alpha2"
	binding "antrea.io/antrea/pkg/ovs/openflow"
	"antrea.io/antrea/pkg/util/runtime"
)

type featurePodConnectivity struct {
	cookieAllocator cookie.Allocator
	ipProtocols     []binding.Protocol

	nodeCachedFlows *flowCategoryCache
	podCachedFlows  *flowCategoryCache
	tcCachedFlows   *flowCategoryCache

	gatewayIPs    map[binding.Protocol]net.IP
	gatewayPort   uint32
	uplinkPort    uint32
	hostIfacePort uint32
	tunnelPort    uint32
	ctZones       map[binding.Protocol]int
	localCIDRs    map[binding.Protocol]net.IPNet
	nodeIPs       map[binding.Protocol]net.IP
	nodeConfig    *config.NodeConfig
	networkConfig *config.NetworkConfig

	connectUplinkToBridge bool
	ctZoneSrcField        *binding.RegField
	ipCtZoneTypeRegMarks  map[binding.Protocol]*binding.RegMark
	enableMulticast       bool
	proxyAll              bool
	enableDSR             bool
	enableTrafficControl  bool
	enableL7FlowExporter  bool

	category cookie.Category
}

func (f *featurePodConnectivity) getFeatureName() string {
	return "PodConnectivity"
}

func newFeaturePodConnectivity(
	cookieAllocator cookie.Allocator,
	ipProtocols []binding.Protocol,
	nodeConfig *config.NodeConfig,
	networkConfig *config.NetworkConfig,
	connectUplinkToBridge bool,
	enableMulticast bool,
	proxyAll bool,
	enableDSR bool,
	enableTrafficControl bool,
	enableL7FlowExporter bool) *featurePodConnectivity {
	ctZones := make(map[binding.Protocol]int)
	gatewayIPs := make(map[binding.Protocol]net.IP)
	localCIDRs := make(map[binding.Protocol]net.IPNet)
	nodeIPs := make(map[binding.Protocol]net.IP)
	ipCtZoneTypeRegMarks := make(map[binding.Protocol]*binding.RegMark)
	for _, ipProtocol := range ipProtocols {
		if ipProtocol == binding.ProtocolIP {
			ctZones[ipProtocol] = CtZone
			gatewayIPs[ipProtocol] = nodeConfig.GatewayConfig.IPv4
			nodeIPs[ipProtocol] = nodeConfig.NodeIPv4Addr.IP
			if nodeConfig.PodIPv4CIDR != nil {
				localCIDRs[ipProtocol] = *nodeConfig.PodIPv4CIDR
			}
			ipCtZoneTypeRegMarks[ipProtocol] = IPCtZoneTypeRegMark
		} else if ipProtocol == binding.ProtocolIPv6 {
			ctZones[ipProtocol] = CtZoneV6
			gatewayIPs[ipProtocol] = nodeConfig.GatewayConfig.IPv6
			nodeIPs[ipProtocol] = nodeConfig.NodeIPv6Addr.IP
			if nodeConfig.PodIPv6CIDR != nil {
				localCIDRs[ipProtocol] = *nodeConfig.PodIPv6CIDR
			}
			ipCtZoneTypeRegMarks[ipProtocol] = IPv6CtZoneTypeRegMark
		}
	}

	gatewayPort := uint32(config.HostGatewayOFPort)
	if nodeConfig.GatewayConfig != nil {
		gatewayPort = nodeConfig.GatewayConfig.OFPort
	}
	uplinkPort := uint32(0)
	if nodeConfig.UplinkNetConfig != nil {
		uplinkPort = nodeConfig.UplinkNetConfig.OFPort
	}

	return &featurePodConnectivity{
		cookieAllocator:       cookieAllocator,
		ipProtocols:           ipProtocols,
		nodeCachedFlows:       newFlowCategoryCache(),
		podCachedFlows:        newFlowCategoryCache(),
		tcCachedFlows:         newFlowCategoryCache(),
		gatewayIPs:            gatewayIPs,
		gatewayPort:           gatewayPort,
		uplinkPort:            uplinkPort,
		hostIfacePort:         nodeConfig.HostInterfaceOFPort,
		tunnelPort:            nodeConfig.TunnelOFPort,
		ctZones:               ctZones,
		localCIDRs:            localCIDRs,
		nodeIPs:               nodeIPs,
		nodeConfig:            nodeConfig,
		networkConfig:         networkConfig,
		connectUplinkToBridge: connectUplinkToBridge,
		enableTrafficControl:  enableTrafficControl,
		enableL7FlowExporter:  enableL7FlowExporter,
		ipCtZoneTypeRegMarks:  ipCtZoneTypeRegMarks,
		ctZoneSrcField:        getZoneSrcField(connectUplinkToBridge),
		enableMulticast:       enableMulticast,
		proxyAll:              proxyAll,
		enableDSR:             enableDSR,
		category:              cookie.PodConnectivity,
	}
}

func (f *featurePodConnectivity) initFlows() []*openflow15.FlowMod {
	var flows []binding.Flow
	gatewayMAC := f.nodeConfig.GatewayConfig.MAC

	for _, ipProtocol := range f.ipProtocols {
		if ipProtocol == binding.ProtocolIPv6 {
			flows = append(flows, f.ipv6Flows()...)
		} else if ipProtocol == binding.ProtocolIP {
			flows = append(flows, f.arpNormalFlow())
			flows = append(flows, f.arpSpoofGuardFlow(f.gatewayIPs[ipProtocol], gatewayMAC, f.gatewayPort))
			if f.connectUplinkToBridge {
				flows = append(flows, f.arpResponderFlow(f.gatewayIPs[ipProtocol], gatewayMAC))
				flows = append(flows, f.hostBridgeUplinkVLANFlows()...)
			}
			if runtime.IsWindowsPlatform() || f.connectUplinkToBridge {
				// This installs the flows between bridge local port and uplink port to support host networking.
				flows = append(flows, f.hostBridgeUplinkFlows()...)
			}
		}
	}
	if f.connectUplinkToBridge {
		flows = append(flows, f.l3FwdFlowToNode()...)
	}
	flows = append(flows, f.l3FwdFlowToExternal())
	flows = append(flows, f.decTTLFlows()...)
	flows = append(flows, f.conntrackFlows()...)
	flows = append(flows, f.l2ForwardOutputFlow())
	flows = append(flows, f.gatewayClassifierFlows()...)
	flows = append(flows, f.l2ForwardCalcFlow(gatewayMAC, f.gatewayPort))
	flows = append(flows, f.gatewayIPSpoofGuardFlows()...)
	flows = append(flows, f.l3FwdFlowToGateway()...)
	// Add flow to ensure the liveliness check packet could be forwarded correctly.
	flows = append(flows, f.localProbeFlows()...)

	if f.tunnelPort != 0 {
		flows = append(flows, f.tunnelClassifierFlow(f.tunnelPort))
		flows = append(flows, f.l2ForwardCalcFlow(GlobalVirtualMAC, f.tunnelPort))
	}

	if f.networkConfig.TrafficEncapMode.IsNetworkPolicyOnly() {
		flows = append(flows, f.l3FwdFlowRouteToGW()...)
		// If IPv6 is enabled, this flow will never get hit. Replies any ARP request with the same global virtual MAC.
		if f.networkConfig.IPv4Enabled {
			flows = append(flows, f.arpResponderStaticFlow())
		}
	} else {
		// If NetworkPolicyOnly mode is enabled, IPAM is implemented by the primary CNI, which may not use the Pod CIDR
		// of the Node. Therefore, it doesn't make sense to install flows for the Pod CIDR. Individual flow for each local
		// Pod IP will take care of routing the traffic to destination Pod.
		flows = append(flows, f.l3FwdFlowToLocalPodCIDR()...)
	}
	if f.enableTrafficControl || f.enableL7FlowExporter {
		flows = append(flows, f.trafficControlCommonFlows()...)
	}
	return GetFlowModMessages(flows, binding.AddMessage)
}

func (f *featurePodConnectivity) replayFlows() []*openflow15.FlowMod {
	var flows []*openflow15.FlowMod

	// Get cached flows.
	for _, cachedFlows := range []*flowCategoryCache{f.nodeCachedFlows, f.podCachedFlows, f.tcCachedFlows} {
		flows = append(flows, getCachedFlowMessages(cachedFlows)...)
	}

	return flows
}

// trafficControlMarkFlows generates the flows to mark the packets that need to be redirected or mirrored.
func (f *featurePodConnectivity) trafficControlMarkFlows(sourceOFPorts []uint32,
	targetOFPort uint32,
	direction v1alpha2.Direction,
	action v1alpha2.TrafficControlAction,
	priority uint16) []binding.Flow {
	cookieID := f.cookieAllocator.Request(f.category).Raw()
	var actionRegMark *binding.RegMark
	if action == v1alpha2.ActionRedirect {
		actionRegMark = TrafficControlRedirectRegMark
	} else if action == v1alpha2.ActionMirror {
		actionRegMark = TrafficControlMirrorRegMark
	}
	var flows []binding.Flow
	for _, port := range sourceOFPorts {
		if direction == v1alpha2.DirectionIngress || direction == v1alpha2.DirectionBoth {
			// This generates the flow to mark the packets destined for a provided port.
			flows = append(flows, TrafficControlTable.ofTable.BuildFlow(priority).
				Cookie(cookieID).
				MatchRegFieldWithValue(TargetOFPortField, port).
				Action().LoadToRegField(TrafficControlTargetOFPortField, targetOFPort).
				Action().LoadRegMark(actionRegMark).
				Action().NextTable().
				Done())
		}
		// This generates the flow to mark the packets sourced from a provided port.
		if direction == v1alpha2.DirectionEgress || direction == v1alpha2.DirectionBoth {
			flows = append(flows, TrafficControlTable.ofTable.BuildFlow(priority).
				Cookie(cookieID).
				MatchInPort(port).
				Action().LoadToRegField(TrafficControlTargetOFPortField, targetOFPort).
				Action().LoadRegMark(actionRegMark).
				Action().NextTable().
				Done())
		}
	}
	return flows
}

// trafficControlReturnClassifierFlow generates the flow to mark the packets from traffic control return port and forward
// the packets to stageRouting directly. Note that, for the packets which are originally to be output to a tunnel port,
// value of NXM_NX_TUN_IPV4_DST for the returned packets needs to be loaded in stageRouting.
func (f *featurePodConnectivity) trafficControlReturnClassifierFlow(returnOFPort uint32) binding.Flow {
	return ClassifierTable.ofTable.BuildFlow(priorityNormal).
		Cookie(f.cookieAllocator.Request(f.category).Raw()).
		MatchInPort(returnOFPort).
		Action().LoadRegMark(FromTCReturnRegMark).
		Action().GotoStage(stageRouting).
		Done()
}

// trafficControlCommonFlows generates the common flows for traffic control.
func (f *featurePodConnectivity) trafficControlCommonFlows() []binding.Flow {
	cookieID := f.cookieAllocator.Request(f.category).Raw()
	return []binding.Flow{
		// This generates the flow to output packets to the original target port as well as mirror the packets to the target
		// traffic control port.
		OutputTable.ofTable.BuildFlow(priorityHigh+1).
			Cookie(cookieID).
			MatchRegMark(OutputToOFPortRegMark, TrafficControlMirrorRegMark).
			Action().OutputToRegField(TargetOFPortField).
			Action().OutputToRegField(TrafficControlTargetOFPortField).
			Done(),
		// This generates the flow to output the packets to be redirected to the target traffic control port.
		OutputTable.ofTable.BuildFlow(priorityHigh+1).
			Cookie(cookieID).
			MatchRegMark(OutputToOFPortRegMark, TrafficControlRedirectRegMark).
			Action().OutputToRegField(TrafficControlTargetOFPortField).
			Done(),
		// This generates the flow to forward the returned packets (with FromTCReturnRegMark) to stageOutput directly
		// after loading output port number to reg1 in L2ForwardingCalcTable.
		TrafficControlTable.ofTable.BuildFlow(priorityHigh).
			Cookie(cookieID).
			MatchRegMark(OutputToOFPortRegMark, FromTCReturnRegMark).
			Action().GotoStage(stageOutput).
			Done(),
	}
}

// tunnelClassifierFlow generates the flow to mark the packets from tunnel port.
func (f *featurePodConnectivity) tunnelClassifierFlow(tunnelOFPort uint32) binding.Flow {
	return ClassifierTable.ofTable.BuildFlow(priorityNormal).
		Cookie(f.cookieAllocator.Request(f.category).Raw()).
		MatchInPort(tunnelOFPort).
		Action().LoadRegMark(FromTunnelRegMark, RewriteMACRegMark).
		Action().GotoStage(stageConntrackState).
		Done()
}

// gatewayClassifierFlows generates the flow to mark the packets from the Antrea gateway port.
func (f *featurePodConnectivity) gatewayClassifierFlows() []binding.Flow {
	var flows []binding.Flow
	for ipProtocol, gatewayIP := range f.gatewayIPs {
		flows = append(flows, ClassifierTable.ofTable.BuildFlow(priorityHigh).
			Cookie(f.cookieAllocator.Request(f.category).Raw()).
			MatchInPort(f.gatewayPort).
			MatchProtocol(ipProtocol).
			MatchSrcIP(gatewayIP).
			Action().LoadRegMark(FromGatewayRegMark).
			Action().GotoStage(stageValidation).
			Done())
	}
	// If the packet is from gateway but its source IP is not the gateway IP, it's considered external sourced traffic.
	flows = append(flows, ClassifierTable.ofTable.BuildFlow(priorityNormal).
		Cookie(f.cookieAllocator.Request(f.category).Raw()).
		MatchInPort(f.gatewayPort).
		Action().LoadRegMark(FromGatewayRegMark, FromExternalRegMark).
		Action().GotoStage(stageValidation).
		Done())
	return flows
}

// podClassifierFlow generates the flow to mark the packets from a local Pod port.
// If multi-cluster is enabled, also load podLabelID into LabelIDField.
func (f *featurePodConnectivity) podClassifierFlow(podOFPort uint32, isAntreaFlexibleIPAM bool, podLabelID *uint32) binding.Flow {
	regMarksToLoad := []*binding.RegMark{FromLocalRegMark}
	if isAntreaFlexibleIPAM {
		regMarksToLoad = append(regMarksToLoad, AntreaFlexibleIPAMRegMark, RewriteMACRegMark)
	}
	if podLabelID != nil {
		return ClassifierTable.ofTable.BuildFlow(priorityLow).
			Cookie(f.cookieAllocator.Request(f.category).Raw()).
			MatchInPort(podOFPort).
			Action().LoadRegMark(regMarksToLoad...).
			Action().SetTunnelID(uint64(*podLabelID)).
			Action().GotoStage(stageValidation).
			Done()
	}
	return ClassifierTable.ofTable.BuildFlow(priorityLow).
		Cookie(f.cookieAllocator.Request(f.category).Raw()).
		MatchInPort(podOFPort).
		Action().LoadRegMark(regMarksToLoad...).
		Action().GotoStage(stageValidation).
		Done()
}

// podUplinkClassifierFlows generates the flows to mark the packets with target destination MAC address from uplink/bridge
// port, which are needed when uplink is connected to OVS bridge and Antrea IPAM is configured.
func (f *featurePodConnectivity) podUplinkClassifierFlows(dstMAC net.HardwareAddr, vlanID uint16) []binding.Flow {
	cookieID := f.cookieAllocator.Request(f.category).Raw()
	var flows []binding.Flow
	nonVLAN := true
	if vlanID > 0 {
		nonVLAN = false
	}
	for _, ipProtocol := range f.ipProtocols {
		flows = append(flows,
			// This generates the flow to mark the packets from uplink port.
			ClassifierTable.ofTable.BuildFlow(priorityHigh).
				Cookie(cookieID).
				MatchInPort(f.uplinkPort).
				MatchDstMAC(dstMAC).
				MatchVLAN(nonVLAN, vlanID, nil).
				MatchProtocol(ipProtocol).
				Action().LoadRegMark(f.ipCtZoneTypeRegMarks[ipProtocol], FromUplinkRegMark).
				Action().LoadToRegField(VLANIDField, uint32(vlanID)).
				Action().GotoStage(stageConntrackState).
				Done(),
		)
		if vlanID == 0 {
			flows = append(flows,
				// This generates the flow to mark the packets from bridge local port.
				ClassifierTable.ofTable.BuildFlow(priorityHigh).
					Cookie(cookieID).
					MatchInPort(f.hostIfacePort).
					MatchDstMAC(dstMAC).
					MatchVLAN(true, 0, nil).
					MatchProtocol(ipProtocol).
					Action().LoadRegMark(f.ipCtZoneTypeRegMarks[ipProtocol], FromBridgeRegMark).
					Action().GotoStage(stageConntrackState).
					Done(),
			)
		}
	}
	return flows
}

// conntrackFlows generates the flows about conntrack for feature PodConnectivity.
func (f *featurePodConnectivity) conntrackFlows() []binding.Flow {
	cookieID := f.cookieAllocator.Request(f.category).Raw()
	var flows []binding.Flow
	for _, ipProtocol := range f.ipProtocols {
		flows = append(flows,
			// This generates the flow to transform the destination IP of request packets or source IP of reply packets
			// from tracked connections in CT zone.
			ConntrackTable.ofTable.BuildFlow(priorityNormal).
				Cookie(cookieID).
				MatchProtocol(ipProtocol).
				Action().CT(false, ConntrackTable.GetNext(), f.ctZones[ipProtocol], f.ctZoneSrcField).
				NAT().
				CTDone().
				Done(),
			// This generates the flow to match the packets of tracked non-Service connection and forward them to
			// stageEgressSecurity directly to bypass stagePreRouting. The first packet of non-Service connection passes
			// through stagePreRouting, and the subsequent packets go to stageEgressSecurity directly.
			ConntrackStateTable.ofTable.BuildFlow(priorityLow).
				Cookie(cookieID).
				MatchProtocol(ipProtocol).
				MatchCTStateNew(false).
				MatchCTStateTrk(true).
				MatchCTMark(NotServiceCTMark).
				Action().GotoStage(stageEgressSecurity).
				Done(),
			// This generates the flow to drop invalid packets.
			ConntrackStateTable.ofTable.BuildFlow(priorityNormal).
				Cookie(cookieID).
				MatchProtocol(ipProtocol).
				MatchCTStateInv(true).
				MatchCTStateTrk(true).
				Action().Drop().
				Done(),
			// This generates the flow to match the first packet of non-Service connection and mark the source of the connection
			// by copying PktSourceField to ConnSourceCTMarkField.
			ConntrackCommitTable.ofTable.BuildFlow(priorityNormal).
				Cookie(cookieID).
				MatchProtocol(ipProtocol).
				MatchCTStateNew(true).
				MatchCTStateTrk(true).
				MatchCTStateSNAT(false).
				MatchCTMark(NotServiceCTMark).
				Action().CT(true, ConntrackCommitTable.GetNext(), f.ctZones[ipProtocol], f.ctZoneSrcField).
				MoveToCtMarkField(PktSourceField, ConnSourceCTMarkField).
				CTDone().
				Done(),
		)
	}
	// This generates default flow to match the first packet of a new connection and forward it to stagePreRouting.
	flows = append(flows, ConntrackStateTable.ofTable.BuildFlow(priorityMiss).
		Cookie(cookieID).
		Action().GotoStage(stagePreRouting).
		Done())

	return flows
}

// l2ForwardCalcFlow generates the flow to match the destination MAC and load the target ofPort to TargetOFPortField.
func (f *featurePodConnectivity) l2ForwardCalcFlow(dstMAC net.HardwareAddr, ofPort uint32) binding.Flow {
	return L2ForwardingCalcTable.ofTable.BuildFlow(priorityNormal).
		Cookie(f.cookieAllocator.Request(f.category).Raw()).
		MatchDstMAC(dstMAC).
		Action().LoadToRegField(TargetOFPortField, ofPort).
		Action().LoadRegMark(OutputToOFPortRegMark).
		Action().NextTable().
		Done()
}

// l2ForwardOutputFlow generates the flow to output the packets to target OVS port according to the value of TargetOFPortField.
func (f *featurePodConnectivity) l2ForwardOutputFlow() binding.Flow {
	return OutputTable.ofTable.BuildFlow(priorityNormal).
		Cookie(f.cookieAllocator.Request(f.category).Raw()).
		MatchRegMark(OutputToOFPortRegMark).
		Action().OutputToRegField(TargetOFPortField).
		Done()
}

// l3FwdFlowToPod generates the flows to match the packets destined for a local Pod. For a per-Node IPAM Pod, the flow
// rewrites destination MAC to the Pod interface's MAC, and rewrites source MAC to Antrea gateway interface's MAC. For
// an Antrea IPAM Pod, the flow only rewrites the destination MAC to the Pod interface's MAC.
func (f *featurePodConnectivity) l3FwdFlowToPod(localGatewayMAC net.HardwareAddr,
	podInterfaceIPs []net.IP,
	podInterfaceMAC net.HardwareAddr,
	isAntreaFlexibleIPAM bool,
	vlanID uint16) []binding.Flow {
	cookieID := f.cookieAllocator.Request(f.category).Raw()
	var flows []binding.Flow
	for _, ip := range podInterfaceIPs {
		ipProtocol := getIPProtocol(ip)
		if isAntreaFlexibleIPAM {
			// This generates the flow to match the packets destined for a local Antrea IPAM Pod.
			flows = append(flows, L3ForwardingTable.ofTable.BuildFlow(priorityNormal).
				Cookie(cookieID).
				MatchRegFieldWithValue(VLANIDField, uint32(vlanID)).
				MatchProtocol(ipProtocol).
				MatchDstIP(ip).
				Action().SetDstMAC(podInterfaceMAC).
				Action().GotoTable(L3DecTTLTable.GetID()).
				Done())
		} else {
			// This generates the flow to match the packets with RewriteMACRegMark and destined for a local per-Node IPAM Pod.
			regMarksToMatch := []*binding.RegMark{RewriteMACRegMark}
			if f.connectUplinkToBridge {
				// Only overwrite MAC for untagged traffic which destination is a local per-Node IPAM Pod.
				regMarksToMatch = append(regMarksToMatch, binding.NewRegMark(VLANIDField, 0))
			}
			flows = append(flows, L3ForwardingTable.ofTable.BuildFlow(priorityNormal).
				Cookie(cookieID).
				MatchProtocol(ipProtocol).
				MatchRegMark(regMarksToMatch...).
				MatchDstIP(ip).
				Action().SetSrcMAC(localGatewayMAC).
				Action().SetDstMAC(podInterfaceMAC).
				Action().GotoTable(L3DecTTLTable.GetID()).
				Done())
		}
	}
	return flows
}

// l3FwdFlowRouteToPod generates the flows to match the packets destined for a Pod based on the destination IPs. It rewrites
// destination MAC to the Pod interface's MAC. The flows are only used in networkPolicyOnly mode.
func (f *featurePodConnectivity) l3FwdFlowRouteToPod(podInterfaceIPs []net.IP, podInterfaceMAC net.HardwareAddr) []binding.Flow {
	cookieID := f.cookieAllocator.Request(f.category).Raw()
	var flows []binding.Flow
	for _, ip := range podInterfaceIPs {
		ipProtocol := getIPProtocol(ip)
		flows = append(flows, L3ForwardingTable.ofTable.BuildFlow(priorityNormal).
			Cookie(cookieID).
			MatchProtocol(ipProtocol).
			MatchDstIP(ip).
			Action().SetDstMAC(podInterfaceMAC).
			Action().GotoTable(L3DecTTLTable.GetID()).
			Done())
	}
	return flows
}

// l3FwdFlowRouteToGW generates the flows to match the packets destined for the Antrea gateway. It rewrites destination MAC
// to the Antrea gateway interface's MAC. The flows are used in networkPolicyOnly mode to match the packets sourced from a
// local Pod and destined for remote Pods, Nodes, or external network.
func (f *featurePodConnectivity) l3FwdFlowRouteToGW() []binding.Flow {
	cookieID := f.cookieAllocator.Request(f.category).Raw()
	var flows []binding.Flow
	for _, ipProtocol := range f.ipProtocols {
		flows = append(flows, L3ForwardingTable.ofTable.BuildFlow(priorityLow).
			Cookie(cookieID).
			MatchProtocol(ipProtocol).
			Action().SetDstMAC(f.nodeConfig.GatewayConfig.MAC).
			Action().LoadRegMark(ToGatewayRegMark).
			Action().GotoTable(L3DecTTLTable.GetID()).
			Done(),
		)
	}
	return flows
}

// l3FwdFlowToGateway generates the flows to match the packets destined for the Antrea gateway.
func (f *featurePodConnectivity) l3FwdFlowToGateway() []binding.Flow {
	cookieID := f.cookieAllocator.Request(f.category).Raw()
	var flows []binding.Flow
	for ipProtocol, gatewayIP := range f.gatewayIPs {
		flows = append(flows,
			// This generates the flow to match the packets destined for Antrea gateway.
			L3ForwardingTable.ofTable.BuildFlow(priorityHigh).
				Cookie(cookieID).
				MatchProtocol(ipProtocol).
				MatchDstIP(gatewayIP).
				Action().SetDstMAC(f.nodeConfig.GatewayConfig.MAC).
				Action().LoadRegMark(ToGatewayRegMark).
				Action().GotoTable(L3DecTTLTable.GetID()).
				Done(),
			// This generates the flow to match the reply packets of connection with FromGatewayCTMark.
			L3ForwardingTable.ofTable.BuildFlow(priorityHigh).
				Cookie(cookieID).
				MatchProtocol(ipProtocol).
				MatchCTMark(FromGatewayCTMark).
				MatchCTStateRpl(true).
				MatchCTStateTrk(true).
				Action().SetDstMAC(f.nodeConfig.GatewayConfig.MAC).
				Action().LoadRegMark(ToGatewayRegMark).
				Action().GotoTable(L3DecTTLTable.GetID()).
				Done(),
		)
	}
	return flows
}

// l3FwdFlowsToRemoteViaTun generates the flows to match the packets destined for remote Pods via tunnel.
func (f *featurePodConnectivity) l3FwdFlowsToRemoteViaTun(localGatewayMAC net.HardwareAddr, peerSubnet net.IPNet, tunnelPeer net.IP) []binding.Flow {
	ipProtocol := getIPProtocol(peerSubnet.IP)
	buildFlow := func(matcher func(b binding.FlowBuilder) binding.FlowBuilder) binding.Flow {
		builder := L3ForwardingTable.ofTable.BuildFlow(priorityNormal).
			Cookie(f.cookieAllocator.Request(f.category).Raw()).
			MatchProtocol(ipProtocol)
		builder = matcher(builder)
		return builder.
			Action().SetSrcMAC(localGatewayMAC).  // Rewrite src MAC to local gateway MAC.
			Action().SetDstMAC(GlobalVirtualMAC). // Rewrite dst MAC to virtual MAC.
			Action().SetTunnelDst(tunnelPeer).    // Flow based tunnel. Set tunnel destination.
			Action().LoadRegMark(ToTunnelRegMark).
			Action().GotoTable(L3DecTTLTable.GetID()).
			Done()
	}
	flows := []binding.Flow{
		// The flow handles packets whose destination IP is in the peer subnet.
		buildFlow(func(b binding.FlowBuilder) binding.FlowBuilder {
			return b.MatchDstIPNet(peerSubnet)
		}),
	}
	// If DSR is enabled, packets accessing a DSR Service will not be DNATed on the ingress Node, but EndpointIPField
	// holds the selected backend Pod IP, we match it and DSRServiceRegMark to send these packets to corresponding Nodes.
	if f.enableDSR {
		// Like matching destination IP, we only check if the prefix of the EndpointIP stored in EndpointIPField is in
		// the subnet. For example, if the peerSubnet is 10.10.1.0/24, we will check reg3=0xa0a0100/0xffffff00.
		ones, bits := peerSubnet.Mask.Size()
		if ipProtocol == binding.ProtocolIP {
			maskedEndpointIPField := binding.NewRegField(EndpointIPField.GetRegID(), uint32(bits-ones), 31)
			maskedEndpointIPValue := binary.BigEndian.Uint32(peerSubnet.IP.To4()) >> (bits - ones)
			flows = append(flows, buildFlow(func(b binding.FlowBuilder) binding.FlowBuilder {
				return b.MatchRegMark(DSRServiceRegMark).MatchRegFieldWithValue(maskedEndpointIPField, maskedEndpointIPValue)
			}))
		}
		// TODO: MatchXXReg must support mask to support IPv6.
	}
	return flows
}

// l3FwdFlowToRemoteViaGW generates the flow to match the packets destined for remote Pods via the Antrea gateway. It is
// used when the cross-Node connections that do not require encapsulation (in noEncap, networkPolicyOnly, or hybrid mode).
func (f *featurePodConnectivity) l3FwdFlowToRemoteViaGW(localGatewayMAC net.HardwareAddr, peerSubnet net.IPNet) binding.Flow {
	cookieID := f.cookieAllocator.Request(f.category).Raw()
	ipProtocol := getIPProtocol(peerSubnet.IP)
	var regMarksToMatch []*binding.RegMark
	if f.connectUplinkToBridge {
		regMarksToMatch = append(regMarksToMatch, NotAntreaFlexibleIPAMRegMark) // Exclude the packets from Antrea IPAM Pods.
	}
	// This generates the flow to match the packets destined for remote Pods. Note that, this flow is installed in Linux Nodes
	// or Windows Nodes whose remote Node's transport interface MAC is unknown.
	return L3ForwardingTable.ofTable.BuildFlow(priorityNormal).
		Cookie(cookieID).
		MatchProtocol(ipProtocol).
		MatchDstIPNet(peerSubnet).
		MatchRegMark(regMarksToMatch...).
		Action().SetDstMAC(localGatewayMAC).
		Action().LoadRegMark(ToGatewayRegMark).
		Action().GotoTable(L3DecTTLTable.GetID()). // Traffic to in-cluster destination should skip EgressMark table.
		Done()
}

// l3FwdFlowToRemoteViaUplink generates the flow to match the packets destined for remote Pods via uplink. It is used
// when the cross-Node connections that do not require encapsulation (in noEncap, networkPolicyOnly, hybrid mode).
func (f *featurePodConnectivity) l3FwdFlowToRemoteViaUplink(remoteGatewayMAC net.HardwareAddr,
	peerSubnet net.IPNet,
	isAntreaFlexibleIPAM bool) binding.Flow {
	cookieID := f.cookieAllocator.Request(f.category).Raw()
	ipProtocol := getIPProtocol(peerSubnet.IP)
	if !isAntreaFlexibleIPAM {
		// This generates the flow to match the packets destined for remote Pods via uplink directly without passing
		// through the Antrea gateway by rewriting destination MAC to remote Node Antrea gateway's MAC. Note that,
		// this flow is only installed in Windows Nodes。
		return L3ForwardingTable.ofTable.BuildFlow(priorityNormal).
			Cookie(cookieID).
			MatchProtocol(ipProtocol).
			MatchRegMark(NotAntreaFlexibleIPAMRegMark).
			MatchDstIPNet(peerSubnet).
			Action().SetSrcMAC(f.nodeConfig.UplinkNetConfig.MAC).
			Action().SetDstMAC(remoteGatewayMAC).
			Action().LoadRegMark(ToUplinkRegMark).
			Action().GotoTable(L3DecTTLTable.GetID()).
			Done()
	}
	// This generates the flow to match the packets sourced Antrea IPAM Pods and destined for remote Pods, and rewrite
	// the destination MAC to remote Node Antrea gateway's MAC. Note that, this flow is only used in Linux when AntreaIPAM
	// is enabled.
	return L3ForwardingTable.ofTable.BuildFlow(priorityNormal).
		Cookie(cookieID).
		MatchRegFieldWithValue(VLANIDField, 0).
		MatchProtocol(ipProtocol).
		MatchRegMark(AntreaFlexibleIPAMRegMark).
		MatchDstIPNet(peerSubnet).
		Action().SetDstMAC(remoteGatewayMAC).
		Action().LoadRegMark(ToUplinkRegMark).
		Action().GotoTable(L3DecTTLTable.GetID()).
		Done()
}

// arpResponderFlow generates the flow to reply to the ARP request with a MAC address for the target IP address.
func (f *featurePodConnectivity) arpResponderFlow(ipAddr net.IP, macAddr net.HardwareAddr) binding.Flow {
	return ARPResponderTable.ofTable.BuildFlow(priorityNormal).
		Cookie(f.cookieAllocator.Request(f.category).Raw()).
		MatchProtocol(binding.ProtocolARP).
		MatchARPOp(arpOpRequest).
		MatchARPTpa(ipAddr).
		Action().Move(binding.NxmFieldSrcMAC, binding.NxmFieldDstMAC).
		Action().SetSrcMAC(macAddr).
		Action().LoadARPOperation(arpOpReply).
		Action().Move(binding.NxmFieldARPSha, binding.NxmFieldARPTha).
		Action().SetARPSha(macAddr).
		Action().Move(binding.NxmFieldARPSpa, binding.NxmFieldARPTpa).
		Action().SetARPSpa(ipAddr).
		Action().OutputInPort().
		Done()
}

// arpResponderStaticFlow generates the flow to reply to any ARP request with the same global virtual MAC. It is used
// in policy-only mode, where traffic are routed via IP not MAC.
func (f *featurePodConnectivity) arpResponderStaticFlow() binding.Flow {
	return ARPResponderTable.ofTable.BuildFlow(priorityNormal).
		Cookie(f.cookieAllocator.Request(f.category).Raw()).
		MatchProtocol(binding.ProtocolARP).
		MatchARPOp(arpOpRequest).
		Action().Move(binding.NxmFieldSrcMAC, binding.NxmFieldDstMAC).
		Action().SetSrcMAC(GlobalVirtualMAC).
		Action().LoadARPOperation(arpOpReply).
		Action().Move(binding.NxmFieldARPSha, binding.NxmFieldARPTha).
		Action().SetARPSha(GlobalVirtualMAC).
		Action().Move(binding.NxmFieldARPTpa, SwapField.GetNXFieldName()).
		Action().Move(binding.NxmFieldARPSpa, binding.NxmFieldARPTpa).
		Action().Move(SwapField.GetNXFieldName(), binding.NxmFieldARPSpa).
		Action().OutputInPort().
		Done()
}

// podIPSpoofGuardFlow generates the flow to check IP packets from local Pods. Packets from the Antrea gateway will not be
// checked, since it might be Pod to Service connection or host namespace connection.
func (f *featurePodConnectivity) podIPSpoofGuardFlow(ifIPs []net.IP, ifMAC net.HardwareAddr, ifOFPort uint32, vlanID uint16) []binding.Flow {
	cookieID := f.cookieAllocator.Request(f.category).Raw()
	var flows []binding.Flow
	targetTables := make(map[binding.Protocol]uint8)
	// - When IPv4 is enabled only, IPv6Table is not initialized. All packets should be forwarded to the next table of
	//   SpoofGuardTable.
	// - When IPv6 is enabled only, IPv6Table is initialized, and it is the next table of SpoofGuardTable. All packets
	//   should be to IPv6Table.
	// - When both IPv4 and IPv6 are enabled, IPv4 packets should skip IPv6Table (which is the next table of SpoofGuardTable)
	//   to avoid unnecessary overhead.
	if len(f.ipProtocols) == 1 {
		targetTables[f.ipProtocols[0]] = SpoofGuardTable.GetNext()
	} else {
		targetTables[binding.ProtocolIP] = IPv6Table.GetNext()
		targetTables[binding.ProtocolIPv6] = IPv6Table.GetID()
	}

	for _, ifIP := range ifIPs {
		var regMarksToLoad []*binding.RegMark
		ipProtocol := getIPProtocol(ifIP)
		if f.connectUplinkToBridge {
			regMarksToLoad = append(regMarksToLoad, f.ipCtZoneTypeRegMarks[ipProtocol], binding.NewRegMark(VLANIDField, uint32(vlanID)))
		}
		flows = append(flows, SpoofGuardTable.ofTable.BuildFlow(priorityNormal).
			Cookie(cookieID).
			MatchProtocol(ipProtocol).
			MatchInPort(ifOFPort).
			MatchSrcMAC(ifMAC).
			MatchSrcIP(ifIP).
			Action().LoadRegMark(regMarksToLoad...).
			Action().GotoTable(targetTables[ipProtocol]).
			Done())
	}
	return flows
}

// arpSpoofGuardFlow generates the flow to check the ARP packets sourced from local Pods or the Antrea gateway.
func (f *featurePodConnectivity) arpSpoofGuardFlow(ifIP net.IP, ifMAC net.HardwareAddr, ifOFPort uint32) binding.Flow {
	return ARPSpoofGuardTable.ofTable.BuildFlow(priorityNormal).
		Cookie(f.cookieAllocator.Request(f.category).Raw()).
		MatchProtocol(binding.ProtocolARP).
		MatchInPort(ifOFPort).
		MatchARPSha(ifMAC).
		MatchARPSpa(ifIP).
		Action().NextTable().
		Done()
}

// gatewayIPSpoofGuardFlows generates the flow to skip spoof guard checking for packets from the Antrea gateway.
func (f *featurePodConnectivity) gatewayIPSpoofGuardFlows() []binding.Flow {
	cookieID := f.cookieAllocator.Request(f.category).Raw()
	var flows []binding.Flow
	targetTables := make(map[binding.Protocol]uint8)
	// - When IPv4 is enabled only, IPv6Table is not initialized. All packets should be forwarded to the next table of
	//   SpoofGuardTable.
	// - When IPv6 is enabled only, IPv6Table is initialized, and it is the next table of SpoofGuardTable. All packets
	//   should be to IPv6Table.
	// - When both IPv4 and IPv6 are enabled, IPv4 packets should skip IPv6Table (which is the next table of SpoofGuardTable)
	//   to avoid unnecessary overhead.
	if len(f.ipProtocols) == 1 {
		targetTables[f.ipProtocols[0]] = SpoofGuardTable.GetNext()
	} else {
		targetTables[binding.ProtocolIP] = IPv6Table.GetNext()
		targetTables[binding.ProtocolIPv6] = IPv6Table.GetID()
	}

	for _, ipProtocol := range f.ipProtocols {
		var regMarksToLoad []*binding.RegMark
		// Set CtZoneTypeField based on ipProtocol and keep VLANIDField=0
		if f.connectUplinkToBridge {
			regMarksToLoad = append(regMarksToLoad, f.ipCtZoneTypeRegMarks[ipProtocol])
		}
		flows = append(flows, SpoofGuardTable.ofTable.BuildFlow(priorityNormal).
			Cookie(cookieID).
			MatchProtocol(ipProtocol).
			MatchInPort(f.gatewayPort).
			Action().LoadRegMark(regMarksToLoad...).
			Action().GotoTable(targetTables[ipProtocol]).
			Done(),
		)
	}
	return flows
}

// arpNormalFlow generates the flow to reply to the ARP request packets in normal way if no flow in ARPResponderTable is matched.
func (f *featurePodConnectivity) arpNormalFlow() binding.Flow {
	return ARPResponderTable.ofTable.BuildFlow(priorityLow).
		Cookie(f.cookieAllocator.Request(f.category).Raw()).
		MatchProtocol(binding.ProtocolARP).
		Action().Normal().
		Done()
}

// ipv6Flows generates the flows to allow IPv6 packets from link-local addresses and handle multicast packets, Neighbor
// Solicitation and ND Advertisement packets properly.
func (f *featurePodConnectivity) ipv6Flows() []binding.Flow {
	cookieID := f.cookieAllocator.Request(f.category).Raw()
	var flows []binding.Flow
	_, ipv6LinkLocalIpnet, _ := net.ParseCIDR(ipv6LinkLocalAddr)
	_, ipv6MulticastIpnet, _ := net.ParseCIDR(ipv6MulticastAddr)
	flows = append(flows,
		// Allow IPv6 packets (e.g. Multicast Listener Report Message V2) which are sent from link-local addresses in
		// SpoofGuardTable, so that these packets will not be dropped.
		SpoofGuardTable.ofTable.BuildFlow(priorityNormal).
			Cookie(cookieID).
			MatchProtocol(binding.ProtocolIPv6).
			MatchSrcIPNet(*ipv6LinkLocalIpnet).
			Action().GotoTable(IPv6Table.GetID()).
			Done(),
		// Handle IPv6 Neighbor Solicitation and Neighbor Advertisement as a regular L2 learning Switch by using normal.
		IPv6Table.ofTable.BuildFlow(priorityNormal).
			Cookie(cookieID).
			MatchProtocol(binding.ProtocolICMPv6).
			MatchICMPv6Type(135).
			MatchICMPv6Code(0).
			Action().Normal().
			Done(),
		IPv6Table.ofTable.BuildFlow(priorityNormal).
			Cookie(cookieID).
			MatchProtocol(binding.ProtocolICMPv6).
			MatchICMPv6Type(136).
			MatchICMPv6Code(0).
			Action().Normal().
			Done(),
		// Handle IPv6 multicast packets as a regular L2 learning Switch by using normal.
		// It is used to ensure that all kinds of IPv6 multicast packets are properly handled (e.g. Multicast Listener
		// Report Message V2).
		IPv6Table.ofTable.BuildFlow(priorityNormal).
			Cookie(cookieID).
			MatchProtocol(binding.ProtocolIPv6).
			MatchDstIPNet(*ipv6MulticastIpnet).
			Action().Normal().
			Done(),
	)
	return flows
}

// localProbeFlows generates the flows to forward locally generated request packets to stageConntrack directly, bypassing
// ingress rule of Network Policies. The packets are sent by kubelet to probe the liveness/readiness of local Pods.
// On Linux and when OVS kernel datapath is used, the probe packets are identified by matching the HostLocalSourceMark.
// On Windows or when OVS userspace (netdev) datapath is used, we need a different approach because:
//  1. On Windows, kube-proxy userspace mode is used, and currently there is no way to distinguish kubelet generated traffic
//     from kube-proxy proxied traffic.
//  2. pkt_mark field is not properly supported for OVS userspace (netdev) datapath.
//
// When proxyAll is disabled, the probe packets are identified by matching the source IP is the Antrea gateway IP;
// otherwise, the packets are identified by matching both the Antrea gateway IP and NotServiceCTMark. Note that, when
// proxyAll is disabled, currently there is no way to distinguish kubelet generated traffic from kube-proxy proxied traffic
// only by matching the Antrea gateway IP. There is a defect that NodePort Service access by external clients will be
// masqueraded as the Antrea gateway IP to bypass NetworkPolicies. See https://github.com/antrea-io/antrea/issues/280.
func (f *featurePodConnectivity) localProbeFlows() []binding.Flow {
	cookieID := f.cookieAllocator.Request(f.category).Raw()
	var flows []binding.Flow
	if runtime.IsWindowsPlatform() {
		var ctMarksToMatch []*binding.CtMark
		if f.proxyAll {
			ctMarksToMatch = append(ctMarksToMatch, NotServiceCTMark)
		}
		for ipProtocol, gatewayIP := range f.gatewayIPs {
			flows = append(flows, IngressSecurityClassifierTable.ofTable.BuildFlow(priorityHigh).
				Cookie(cookieID).
				MatchProtocol(ipProtocol).
				MatchCTStateRpl(false).
				MatchCTStateTrk(true).
				MatchSrcIP(gatewayIP).
				MatchCTMark(ctMarksToMatch...).
				Action().GotoStage(stageConntrack).
				Done())
		}
	} else {
		for _, ipProtocol := range f.ipProtocols {
			flows = append(flows, IngressSecurityClassifierTable.ofTable.BuildFlow(priorityHigh).
				Cookie(cookieID).
				MatchProtocol(ipProtocol).
				MatchCTStateRpl(false).
				MatchCTStateTrk(true).
				MatchPktMark(types.HostLocalSourceMark, &types.HostLocalSourceMark).
				Action().GotoStage(stageConntrack).
				Done())
		}
	}
	return flows
}

// decTTLFlows generates the flow to process TTL. For the packets forwarded across Nodes, TTL should be decremented by one;
// for packets which enter OVS pipeline from the Antrea gateway, as the host IP stack should have decremented the TTL
// already for such packets, TTL should not be decremented again.
func (f *featurePodConnectivity) decTTLFlows() []binding.Flow {
	cookieID := f.cookieAllocator.Request(f.category).Raw()
	var flows []binding.Flow
	for _, ipProtocol := range f.ipProtocols {
		flows = append(flows,
			// Skip packets from the gateway interface.
			L3DecTTLTable.ofTable.BuildFlow(priorityHigh).
				Cookie(cookieID).
				MatchProtocol(ipProtocol).
				MatchRegMark(FromGatewayRegMark).
				Action().NextTable().
				Done(),
			L3DecTTLTable.ofTable.BuildFlow(priorityNormal).
				Cookie(cookieID).
				MatchProtocol(ipProtocol).
				Action().DecTTL().
				Action().NextTable().
				Done(),
		)
	}
	return flows
}

// l3FwdFlowToLocalPodCIDR generates the flow to match the packets to local per-Node IPAM Pods.
func (f *featurePodConnectivity) l3FwdFlowToLocalPodCIDR() []binding.Flow {
	cookieID := f.cookieAllocator.Request(f.category).Raw()
	var flows []binding.Flow
	regMarksToMatch := []*binding.RegMark{NotRewriteMACRegMark}
	if f.connectUplinkToBridge {
		regMarksToMatch = append(regMarksToMatch, binding.NewRegMark(VLANIDField, 0))
	}
	for ipProtocol, cidr := range f.localCIDRs {
		// This generates the flow to match the packets destined for local Pods without RewriteMACRegMark.
		flows = append(flows, L3ForwardingTable.ofTable.BuildFlow(priorityNormal).
			Cookie(cookieID).
			MatchProtocol(ipProtocol).
			MatchDstIPNet(cidr).
			MatchRegMark(regMarksToMatch...).
			Action().GotoStage(stageSwitching).
			Done())
	}
	return flows
}

// l3FwdFlowToNode generates the flows to match the packets destined for local Node.
func (f *featurePodConnectivity) l3FwdFlowToNode() []binding.Flow {
	cookieID := f.cookieAllocator.Request(f.category).Raw()
	var regMarksToMatch []*binding.RegMark
	if f.connectUplinkToBridge {
		regMarksToMatch = append(regMarksToMatch, binding.NewRegMark(VLANIDField, 0))
	}
	var flows []binding.Flow
	for ipProtocol, nodeIP := range f.nodeIPs {
		flows = append(flows,
			// This generates the flow to match the packets sourced from local Antrea Pods and destined for local Node
			// via bridge local port.
			L3ForwardingTable.ofTable.BuildFlow(priorityHigh).
				Cookie(cookieID).
				MatchProtocol(ipProtocol).
				MatchDstIP(nodeIP).
				MatchRegMark(AntreaFlexibleIPAMRegMark).
				MatchRegMark(regMarksToMatch...).
				Action().SetDstMAC(f.nodeConfig.UplinkNetConfig.MAC).
				Action().GotoStage(stageSwitching).
				Done(),
			// When Node bridge local port and uplink port connect to OVS, this generates the flow to match the reply
			// packets of connection initiated through the bridge local port with FromBridgeCTMark.
			L3ForwardingTable.ofTable.BuildFlow(priorityHigh).
				Cookie(cookieID).
				MatchProtocol(ipProtocol).
				MatchCTMark(FromBridgeCTMark).
				MatchRegMark(regMarksToMatch...).
				MatchCTStateRpl(true).
				MatchCTStateTrk(true).
				Action().SetDstMAC(f.nodeConfig.UplinkNetConfig.MAC).
				Action().GotoStage(stageSwitching).
				Done())
	}
	return flows
}

// l3FwdFlowToExternal generates the flow to forward packets destined for external network. Corresponding cases are listed
// in the follows:
//   - when Egress is disabled, request packets of connections sourced from local Pods and destined for external network.
//   - when AntreaIPAM is enabled, request packets of connections sourced from local AntreaIPAM Pods and destined for external network.
//
// TODO: load ToUplinkRegMark to packets sourced from AntreaIPAM Pods and destined for external network.
//
// Due to the lack of defined variables of flow priority, there are not enough flow priority to install the flows to
// differentiate the packets sourced from AntreaIPAM Pods and non-AntreaIPAM Pods. For the packets sourced from AntreaIPAM
// Pods and destined for external network, they are forwarded via uplink port, not Antrea gateway. Apparently, loading
// ToGatewayRegMark to such packets is not right. However, packets sourced from AntreaIPAM Pods with ToGatewayRegMark
// don't cause unexpected effects to the consumers of ToGatewayRegMark and ToUplinkRegMark. Consumers of these two
// marks are listed in the follows:
//   - In IngressSecurityClassifierTable, flows are installed to forward the packets with ToGatewayRegMark, ToGatewayRegMark
//     or ToUplinkRegMark to IngressMetricTable directly.
//   - In ServiceMarkTable, ToGatewayRegMark is used with FromGatewayRegMark together.
//   - In ServiceMarkTable, ToUplinkRegMark is only used in noEncap mode + Windows.
func (f *featurePodConnectivity) l3FwdFlowToExternal() binding.Flow {
	return L3ForwardingTable.ofTable.BuildFlow(priorityMiss).
		Cookie(f.cookieAllocator.Request(f.category).Raw()).
		Action().LoadRegMark(ToGatewayRegMark).
		Action().GotoStage(stageSwitching).
		Done()
}

// hostBridgeLocalFlows generates the flows to match the packets forwarded between bridge local port and uplink port.
func (f *featurePodConnectivity) hostBridgeLocalFlows() []binding.Flow {
	cookieID := f.cookieAllocator.Request(f.category).Raw()
	return []binding.Flow{
		// This generates the flow to forward the packets from uplink port to bridge local port.
		ClassifierTable.ofTable.BuildFlow(priorityNormal).
			Cookie(cookieID).
			MatchInPort(f.uplinkPort).
			Action().Output(f.hostIfacePort).
			Done(),
		// This generates the flow to forward the packets from bridge local port to uplink port.
		ClassifierTable.ofTable.BuildFlow(priorityNormal).
			Cookie(cookieID).
			MatchInPort(f.hostIfacePort).
			Action().Output(f.uplinkPort).
			Done(),
	}
}

// hostBridgeUplinkVLANFlows generates the flows to match VLAN packets from uplink port.
func (f *featurePodConnectivity) hostBridgeUplinkVLANFlows() []binding.Flow {
	vlanMask := uint16(openflow15.OFPVID_PRESENT)
	return []binding.Flow{
		VLANTable.ofTable.BuildFlow(priorityLow).
			Cookie(f.cookieAllocator.Request(f.category).Raw()).
			MatchInPort(f.uplinkPort).
			MatchVLAN(false, 0, &vlanMask).
			Action().PopVLAN().
			Action().NextTable().
			Done(),
	}
}

// podVLANFlows generates the flows to match the packets from Pod and set VLAN ID.
func (f *featurePodConnectivity) podVLANFlow(podOFPort uint32, vlanID uint16) binding.Flow {
	return VLANTable.ofTable.BuildFlow(priorityLow).
		Cookie(f.cookieAllocator.Request(f.category).Raw()).
		MatchInPort(podOFPort).
		MatchRegFieldWithValue(TargetOFPortField, f.uplinkPort).
		Action().PushVLAN(EtherTypeDot1q).
		Action().SetVLAN(vlanID).
		Action().NextTable().
		Done()
}

// TODO: Use DuplicateToBuilder or integrate this function into original one to avoid unexpected difference.
// flowsToTrace generates Traceflow specific flows in the connectionTrackStateTable or L2ForwardingCalcTable for featurePodConnectivity.
// When packet is not provided, the flows bypass the drop flow in conntrackStateFlow to avoid unexpected drop of the
// injected Traceflow packet, and to drop any Traceflow packet that has ct_state +rpl, which may happen when the Traceflow
// request destination is the Node's IP. When packet is provided, a flow is added to mark - the first packet of the first
// connection that matches the provided packet - as the Traceflow packet. The flow is added in connectionTrackStateTable
// when receiverOnly is false and it also matches in_port to be the provided ofPort (the sender Pod); otherwise when
// receiverOnly is true, the flow is added into L2ForwardingCalcTable and matches the destination MAC (the receiver Pod MAC).
func (f *featurePodConnectivity) flowsToTrace(dataplaneTag uint8,
	ovsMetersAreSupported,
	liveTraffic,
	droppedOnly,
	receiverOnly bool,
	packet *binding.Packet,
	ofPort uint32,
	timeout uint16) []binding.Flow {
	cookieID := f.cookieAllocator.Request(cookie.Traceflow).Raw()
	var flows []binding.Flow
	if packet == nil {
		for _, ipProtocol := range f.ipProtocols {
			flows = append(flows,
				ConntrackStateTable.ofTable.BuildFlow(priorityLow+1).
					Cookie(cookieID).
					MatchProtocol(ipProtocol).
					MatchIPDSCP(dataplaneTag).
					SetHardTimeout(timeout).
					Action().GotoStage(stagePreRouting).
					Done(),
				ConntrackStateTable.ofTable.BuildFlow(priorityLow+2).
					Cookie(cookieID).
					MatchProtocol(ipProtocol).
					MatchCTStateTrk(true).
					MatchCTStateRpl(true).
					MatchIPDSCP(dataplaneTag).
					SetHardTimeout(timeout).
					Action().Drop().
					Done(),
			)
		}
	} else {
		var flowBuilder binding.FlowBuilder
		if !receiverOnly {
			flowBuilder = ConntrackStateTable.ofTable.BuildFlow(priorityLow).
				Cookie(cookieID).
				MatchInPort(ofPort).
				MatchCTStateNew(true).
				MatchCTStateTrk(true).
				Action().LoadIPDSCP(dataplaneTag).
				SetHardTimeout(timeout).
				Action().GotoStage(stagePreRouting)
			if packet.DestinationIP != nil {
				flowBuilder = flowBuilder.MatchDstIP(packet.DestinationIP)
			}
		} else {
			flowBuilder = L2ForwardingCalcTable.ofTable.BuildFlow(priorityHigh).
				Cookie(cookieID).
				MatchCTStateNew(true).
				MatchCTStateTrk(true).
				MatchDstMAC(packet.DestinationMAC).
				Action().LoadToRegField(TargetOFPortField, ofPort).
				Action().LoadRegMark(OutputToOFPortRegMark).
				Action().LoadIPDSCP(dataplaneTag).
				SetHardTimeout(timeout).
				Action().GotoStage(stageIngressSecurity)
			if packet.SourceIP != nil {
				flowBuilder = flowBuilder.MatchSrcIP(packet.SourceIP)
			}
		}
		// Match transport header
		switch packet.IPProto {
		case protocol.Type_ICMP:
			flowBuilder = flowBuilder.MatchProtocol(binding.ProtocolICMP)
		case protocol.Type_IPv6ICMP:
			flowBuilder = flowBuilder.MatchProtocol(binding.ProtocolICMPv6)
		case protocol.Type_TCP:
			if packet.IsIPv6 {
				flowBuilder = flowBuilder.MatchProtocol(binding.ProtocolTCPv6)
			} else {
				flowBuilder = flowBuilder.MatchProtocol(binding.ProtocolTCP)
			}
		case protocol.Type_UDP:
			if packet.IsIPv6 {
				flowBuilder = flowBuilder.MatchProtocol(binding.ProtocolUDPv6)
			} else {
				flowBuilder = flowBuilder.MatchProtocol(binding.ProtocolUDP)
			}
		default:
			flowBuilder = flowBuilder.MatchIPProtocolValue(packet.IsIPv6, packet.IPProto)
		}
		if packet.IPProto == protocol.Type_TCP || packet.IPProto == protocol.Type_UDP {
			if packet.DestinationPort != 0 {
				flowBuilder = flowBuilder.MatchDstPort(packet.DestinationPort, nil)
			}
			if packet.SourcePort != 0 {
				flowBuilder = flowBuilder.MatchSrcPort(packet.SourcePort, nil)
			}
		}
		flows = append(flows, flowBuilder.Done())
	}

	// Do not send to controller if captures only dropped packet.
	ifDroppedOnly := func(fb binding.FlowBuilder) binding.FlowBuilder {
		if !droppedOnly {
			if ovsMetersAreSupported {
				fb = fb.Action().Meter(PacketInMeterIDTF)
			}
			fb = fb.Action().SendToController([]byte{uint8(PacketInCategoryTF)}, false)
		}
		return fb
	}
	// Clear the loaded DSCP bits before output.
	ifLiveTraffic := func(fb binding.FlowBuilder) binding.FlowBuilder {
		if liveTraffic {
			return fb.Action().LoadIPDSCP(0).
				Action().OutputToRegField(TargetOFPortField)
		}
		return fb
	}

	// This generates Traceflow specific flows that outputs traceflow non-hairpin packets to OVS port and Antrea Agent after
	// L2 forwarding calculation.
	for _, ipProtocol := range f.ipProtocols {
		if f.networkConfig.TrafficEncapMode.SupportsEncap() {
			if f.tunnelPort != 0 {
				// SendToController and Output if output port is tunnel port.
				fb := OutputTable.ofTable.BuildFlow(priorityNormal+3).
					Cookie(cookieID).
					MatchRegFieldWithValue(TargetOFPortField, f.tunnelPort).
					MatchProtocol(ipProtocol).
					MatchRegMark(OutputToOFPortRegMark).
					MatchIPDSCP(dataplaneTag).
					SetHardTimeout(timeout).
					Action().OutputToRegField(TargetOFPortField)
				fb = ifDroppedOnly(fb)
				flows = append(flows, fb.Done())
			}
			// For injected packets, only SendToController if output port is local gateway. In encapMode, a Traceflow
			// packet going out of the gateway port (i.e. exiting the overlay) essentially means that the Traceflow
			// request is complete.
			fb := OutputTable.ofTable.BuildFlow(priorityNormal+2).
				Cookie(cookieID).
				MatchRegFieldWithValue(TargetOFPortField, f.gatewayPort).
				MatchProtocol(ipProtocol).
				MatchRegMark(OutputToOFPortRegMark).
				MatchIPDSCP(dataplaneTag).
				SetHardTimeout(timeout)
			fb = ifDroppedOnly(fb)
			fb = ifLiveTraffic(fb)
			flows = append(flows, fb.Done())
		} else {
			// SendToController and Output if output port is local gateway. Unlike in encapMode, inter-Node Pod-to-Pod
			// traffic is expected to go out of the gateway port on the way to its destination.
			fb := OutputTable.ofTable.BuildFlow(priorityNormal+2).
				Cookie(cookieID).
				MatchRegFieldWithValue(TargetOFPortField, f.gatewayPort).
				MatchProtocol(ipProtocol).
				MatchRegMark(OutputToOFPortRegMark).
				MatchIPDSCP(dataplaneTag).
				SetHardTimeout(timeout).
				Action().OutputToRegField(TargetOFPortField)
			fb = ifDroppedOnly(fb)
			flows = append(flows, fb.Done())
		}
		// Only SendToController if output port is local gateway and destination IP is gateway.
		gatewayIP := f.gatewayIPs[ipProtocol]
		if gatewayIP != nil {
			fb := OutputTable.ofTable.BuildFlow(priorityNormal+3).
				Cookie(cookieID).
				MatchRegFieldWithValue(TargetOFPortField, f.gatewayPort).
				MatchProtocol(ipProtocol).
				MatchDstIP(gatewayIP).
				MatchRegMark(OutputToOFPortRegMark).
				MatchIPDSCP(dataplaneTag).
				SetHardTimeout(timeout)
			fb = ifDroppedOnly(fb)
			fb = ifLiveTraffic(fb)
			flows = append(flows, fb.Done())
		}
		// Only SendToController if output port is Pod port.
		fb := OutputTable.ofTable.BuildFlow(priorityNormal + 2).
			Cookie(cookieID).
			MatchProtocol(ipProtocol).
			MatchRegMark(OutputToOFPortRegMark).
			MatchIPDSCP(dataplaneTag).
			SetHardTimeout(timeout)
		fb = ifDroppedOnly(fb)
		fb = ifLiveTraffic(fb)
		flows = append(flows, fb.Done())
	}

	return flows
}

func (f *featurePodConnectivity) initGroups() []binding.OFEntry {
	return nil
}

func (f *featurePodConnectivity) replayGroups() []binding.OFEntry {
	return nil
}

func (f *featurePodConnectivity) replayMeters() []binding.OFEntry {
	return nil
}

func (f *featurePodConnectivity) getRequiredTables() []*Table {
	tables := []*Table{
		ClassifierTable,
		SpoofGuardTable,
		ConntrackTable,
		ConntrackStateTable,
		L3ForwardingTable,
		L3DecTTLTable,
		L2ForwardingCalcTable,
		ConntrackCommitTable,
		OutputTable,
	}

	for _, ipProtocol := range f.ipProtocols {
		switch ipProtocol {
		case binding.ProtocolIPv6:
			tables = append(tables, IPv6Table)
		case binding.ProtocolIP:
			tables = append(tables,
				ARPSpoofGuardTable,
				ARPResponderTable)
			if f.enableMulticast {
				tables = append(tables, PipelineIPClassifierTable)
			}
			if f.connectUplinkToBridge {
				tables = append(tables, VLANTable)
			}
		}
	}
	if f.enableTrafficControl || f.enableL7FlowExporter {
		tables = append(tables, TrafficControlTable)
	}

	return tables
}
