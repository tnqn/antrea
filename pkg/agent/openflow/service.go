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
	"antrea.io/antrea/pkg/agent/util"
	"antrea.io/antrea/pkg/util/runtime"
	"antrea.io/antrea/third_party/proxy"
	"encoding/binary"
	netutils "k8s.io/utils/net"
	"net"
	"sync"

	"antrea.io/libOpenflow/openflow15"

	"antrea.io/antrea/pkg/agent/config"
	"antrea.io/antrea/pkg/agent/nodeip"
	"antrea.io/antrea/pkg/agent/openflow/cookie"
	binding "antrea.io/antrea/pkg/ovs/openflow"
)

type featureService struct {
	cookieAllocator cookie.Allocator
	nodeIPChecker   nodeip.Checker
	ipProtocols     []binding.Protocol
	bridge          binding.Bridge

	cachedFlows *flowCategoryCache
	groupCache  sync.Map

	gatewayIPs             map[binding.Protocol]net.IP
	virtualIPs             map[binding.Protocol]net.IP
	virtualNodePortDNATIPs map[binding.Protocol]net.IP
	dnatCtZones            map[binding.Protocol]int
	snatCtZones            map[binding.Protocol]int
	gatewayMAC             net.HardwareAddr
	nodePortAddresses      map[binding.Protocol][]net.IP
	serviceCIDRs           map[binding.Protocol]net.IPNet
	localCIDRs             map[binding.Protocol]net.IPNet
	networkConfig          *config.NetworkConfig
	gatewayPort            uint32

	enableAntreaPolicy    bool
	enableProxy           bool
	proxyAll              bool
	enableDSR             bool
	connectUplinkToBridge bool
	ctZoneSrcField        *binding.RegField

	category cookie.Category
}

func (f *featureService) getFeatureName() string {
	return "Service"
}

func newFeatureService(
	cookieAllocator cookie.Allocator,
	nodeIPChecker nodeip.Checker,
	ipProtocols []binding.Protocol,
	nodeConfig *config.NodeConfig,
	networkConfig *config.NetworkConfig,
	serviceConfig *config.ServiceConfig,
	bridge binding.Bridge,
	enableAntreaPolicy,
	enableProxy,
	proxyAll,
	enableDSR,
	connectUplinkToBridge bool) *featureService {
	gatewayIPs := make(map[binding.Protocol]net.IP)
	virtualIPs := make(map[binding.Protocol]net.IP)
	virtualNodePortDNATIPs := make(map[binding.Protocol]net.IP)
	dnatCtZones := make(map[binding.Protocol]int)
	snatCtZones := make(map[binding.Protocol]int)
	nodePortAddresses := make(map[binding.Protocol][]net.IP)
	serviceCIDRs := make(map[binding.Protocol]net.IPNet)
	localCIDRs := make(map[binding.Protocol]net.IPNet)
	for _, ipProtocol := range ipProtocols {
		if ipProtocol == binding.ProtocolIP {
			gatewayIPs[ipProtocol] = nodeConfig.GatewayConfig.IPv4
			virtualIPs[ipProtocol] = config.VirtualServiceIPv4
			virtualNodePortDNATIPs[ipProtocol] = config.VirtualNodePortDNATIPv4
			dnatCtZones[ipProtocol] = CtZone
			snatCtZones[ipProtocol] = SNATCtZone
			nodePortAddresses[ipProtocol] = serviceConfig.NodePortAddressesIPv4
			if serviceConfig.ServiceCIDR != nil {
				serviceCIDRs[ipProtocol] = *serviceConfig.ServiceCIDR
			}
			if nodeConfig.PodIPv4CIDR != nil {
				localCIDRs[ipProtocol] = *nodeConfig.PodIPv4CIDR
			}
		} else if ipProtocol == binding.ProtocolIPv6 {
			gatewayIPs[ipProtocol] = nodeConfig.GatewayConfig.IPv6
			virtualIPs[ipProtocol] = config.VirtualServiceIPv6
			virtualNodePortDNATIPs[ipProtocol] = config.VirtualNodePortDNATIPv6
			dnatCtZones[ipProtocol] = CtZoneV6
			snatCtZones[ipProtocol] = SNATCtZoneV6
			nodePortAddresses[ipProtocol] = serviceConfig.NodePortAddressesIPv6
			if serviceConfig.ServiceCIDRv6 != nil {
				serviceCIDRs[ipProtocol] = *serviceConfig.ServiceCIDRv6
			}
			if nodeConfig.PodIPv6CIDR != nil {
				localCIDRs[ipProtocol] = *nodeConfig.PodIPv6CIDR
			}
		}
	}

	return &featureService{
		cookieAllocator:        cookieAllocator,
		nodeIPChecker:          nodeIPChecker,
		ipProtocols:            ipProtocols,
		bridge:                 bridge,
		cachedFlows:            newFlowCategoryCache(),
		groupCache:             sync.Map{},
		gatewayIPs:             gatewayIPs,
		virtualIPs:             virtualIPs,
		virtualNodePortDNATIPs: virtualNodePortDNATIPs,
		dnatCtZones:            dnatCtZones,
		snatCtZones:            snatCtZones,
		nodePortAddresses:      nodePortAddresses,
		serviceCIDRs:           serviceCIDRs,
		localCIDRs:             localCIDRs,
		gatewayMAC:             nodeConfig.GatewayConfig.MAC,
		gatewayPort:            nodeConfig.GatewayConfig.OFPort,
		networkConfig:          networkConfig,
		enableAntreaPolicy:     enableAntreaPolicy,
		enableProxy:            enableProxy,
		proxyAll:               proxyAll,
		enableDSR:              enableDSR,
		connectUplinkToBridge:  connectUplinkToBridge,
		ctZoneSrcField:         getZoneSrcField(connectUplinkToBridge),
		category:               cookie.Service,
	}
}

// serviceNoEndpointFlow generates the flow to match the packets to Service without Endpoint and send them to controller.
func (f *featureService) serviceNoEndpointFlow() binding.Flow {
	return EndpointDNATTable.ofTable.BuildFlow(priorityNormal).
		Cookie(f.cookieAllocator.Request(f.category).Raw()).
		MatchRegMark(SvcNoEpRegMark).
		Action().SendToController([]byte{uint8(PacketInCategorySvcReject)}, false).
		Done()
}

func (f *featureService) initFlows() []*openflow15.FlowMod {
	var flows []binding.Flow
	if f.enableProxy {
		flows = append(flows, f.conntrackFlows()...)
		flows = append(flows, f.preRoutingClassifierFlows()...)
		flows = append(flows, f.l3FwdFlowToExternalEndpoint())
		flows = append(flows, f.gatewaySNATFlows()...)
		flows = append(flows, f.snatConntrackFlows()...)
		flows = append(flows, f.serviceNeedLBFlow())
		flows = append(flows, f.sessionAffinityReselectFlow())
		flows = append(flows, f.serviceNoEndpointFlow())
		flows = append(flows, f.l2ForwardOutputHairpinServiceFlow())
		if f.proxyAll {
			// This installs the flows to match the first packet of NodePort connection. The flows set a bit of a register
			// to mark the Service type of the packet as NodePort, and the mark is consumed in table serviceLBTable.
			flows = append(flows, f.nodePortMarkFlows()...)
		}
		if f.enableDSR {
			flows = append(flows, f.dsrServiceNoDNATFlows()...)
		}
	} else {
		// This installs the flows to enable Service connectivity. Upstream kube-proxy is leveraged to provide load-balancing,
		// and the flows installed by this method ensure that traffic sent from local Pods to any Service address can be
		// forwarded to the host gateway interface correctly, otherwise packets might be dropped by egress rules before
		// they are DNATed to backend Pods.
		flows = append(flows, f.serviceCIDRDNATFlows()...)
	}
	return GetFlowModMessages(flows, binding.AddMessage)
}

// conntrackFlows generates the flows about conntrack for feature Service.
func (f *featureService) conntrackFlows() []binding.Flow {
	cookieID := f.cookieAllocator.Request(f.category).Raw()
	var flows []binding.Flow
	for _, ipProtocol := range f.ipProtocols {
		flows = append(flows,
			// This generates the flow to mark tracked DNATed Service connection with RewriteMACRegMark (load-balanced by
			// AntreaProxy) and forward the packets to stageEgressSecurity directly to bypass stagePreRouting.
			ConntrackStateTable.ofTable.BuildFlow(priorityLow).
				Cookie(cookieID).
				MatchProtocol(ipProtocol).
				MatchCTMark(ServiceCTMark).
				MatchCTStateNew(false).
				MatchCTStateTrk(true).
				Action().LoadRegMark(RewriteMACRegMark).
				Action().GotoStage(stageEgressSecurity).
				Done(),
		)
	}
	// If DSR is enabled, traffic working in DSR mode will be in invalid state on ingress Node.
	// We forward externally originated packets of invalid connections to stagePreRouting to see if it could be
	// marked as DSR traffic. If not, they will be dropped in DSRServiceMarkTable.
	if f.enableDSR {
		flows = append(flows,
			ConntrackStateTable.ofTable.BuildFlow(priorityHigh).
				Cookie(cookieID).
				MatchRegMark(FromExternalRegMark).
				MatchCTStateInv(true).
				MatchCTStateTrk(true).
				Action().GotoStage(stagePreRouting).
				Done(),
			DSRServiceMarkTable.ofTable.BuildFlow(priorityLow).
				Cookie(cookieID).
				MatchRegMark(NotDSRServiceRegMark).
				MatchCTStateInv(true).
				MatchCTStateTrk(true).
				Action().Drop().
				Done(),
		)
	}
	return flows
}

// snatConntrackFlows generates the flows about conntrack of SNAT connection for feature Service.
func (f *featureService) snatConntrackFlows() []binding.Flow {
	cookieID := f.cookieAllocator.Request(f.category).Raw()
	var flows []binding.Flow
	for _, ipProtocol := range f.ipProtocols {
		gatewayIP := f.gatewayIPs[ipProtocol]
		// virtualIP is used as SNAT IP when a request's source IP is gateway IP and we need to forward it back to
		// gateway interface to avoid asymmetry path.
		virtualIP := f.virtualIPs[ipProtocol]
		flows = append(flows,
			// SNAT should be performed for the following connections:
			// - Hairpin Service connection initiated through a local Pod, and SNAT should be performed with the Antrea
			//   gateway IP.
			// - Hairpin Service connection initiated through the Antrea gateway, and SNAT should be performed with a
			//   virtual IP.
			// - Nodeport / LoadBalancer connection initiated through the Antrea gateway and externalTrafficPolicy is
			//   Cluster, if the selected Endpoint is not on local Node, then SNAT should be performed with the Antrea
			//   gateway IP.
			// Note that, for Service connections that require SNAT, ServiceCTMark is loaded in SNAT CT zone when performing
			// SNAT since ServiceCTMark loaded in DNAT CT zone cannot be read in SNAT CT zone. For Service connections,
			// ServiceCTMark (loaded in DNAT / SNAT CT zone) is used to bypass ConntrackCommitTable which is used to commit
			// non-Service connections. For hairpin connections, HairpinCTMark is also loaded in SNAT CT zone when performing
			// SNAT since HairpinCTMark loaded in DNAT CT zone also cannot be read in SNAT CT zone. HairpinCTMark is used
			// to output packets of hairpin connections in OutputTable.

			// This generates the flow to match the first packet of hairpin Service connection initiated through the Antrea
			// gateway with ConnSNATCTMark and HairpinCTMark, then perform SNAT in SNAT CT zone with a virtual IP.
			SNATTable.ofTable.BuildFlow(priorityNormal).
				Cookie(cookieID).
				MatchProtocol(ipProtocol).
				MatchCTStateNew(true).
				MatchCTStateTrk(true).
				MatchRegMark(FromGatewayRegMark).
				MatchCTMark(HairpinCTMark).
				Action().CT(true, SNATTable.GetNext(), f.snatCtZones[ipProtocol], nil).
				SNAT(&binding.IPRange{StartIP: virtualIP, EndIP: virtualIP}, nil).
				LoadToCtMark(ServiceCTMark, HairpinCTMark).
				CTDone().
				Done(),
			// This generates the flow to unSNAT reply packets of connections committed in SNAT CT zone by the above flow.
			UnSNATTable.ofTable.BuildFlow(priorityNormal).
				Cookie(cookieID).
				MatchProtocol(ipProtocol).
				MatchDstIP(virtualIP).
				Action().CT(false, UnSNATTable.GetNext(), f.snatCtZones[ipProtocol], nil).
				NAT().
				CTDone().
				Done(),

			// This generates the flow to match the first packet of hairpin Service connection initiated through a Pod with
			// ConnSNATCTMark and HairpinCTMark, then perform SNAT in SNAT CT zone with the Antrea gateway IP.
			SNATTable.ofTable.BuildFlow(priorityNormal).
				Cookie(cookieID).
				MatchProtocol(ipProtocol).
				MatchCTStateNew(true).
				MatchCTStateTrk(true).
				MatchRegMark(FromLocalRegMark).
				MatchCTMark(HairpinCTMark).
				Action().CT(true, SNATTable.GetNext(), f.snatCtZones[ipProtocol], nil).
				SNAT(&binding.IPRange{StartIP: gatewayIP, EndIP: gatewayIP}, nil).
				LoadToCtMark(ServiceCTMark, HairpinCTMark).
				CTDone().
				Done(),
			// This generates the flow to match the first packet of NodePort / LoadBalancer connection (non-hairpin) initiated
			// through the Antrea gateway with ConnSNATCTMark, then perform SNAT in SNAT CT zone with the Antrea gateway IP.
			SNATTable.ofTable.BuildFlow(priorityLow).
				Cookie(cookieID).
				MatchProtocol(ipProtocol).
				MatchCTStateNew(true).
				MatchCTStateTrk(true).
				MatchRegMark(FromGatewayRegMark).
				MatchCTMark(ConnSNATCTMark).
				Action().CT(true, SNATTable.GetNext(), f.snatCtZones[ipProtocol], nil).
				SNAT(&binding.IPRange{StartIP: gatewayIP, EndIP: gatewayIP}, nil).
				LoadToCtMark(ServiceCTMark).
				CTDone().
				Done(),
			// This generates the flow to unSNAT reply packets of connections committed in SNAT CT zone by the above flows.
			UnSNATTable.ofTable.BuildFlow(priorityNormal).
				Cookie(cookieID).
				MatchProtocol(ipProtocol).
				MatchDstIP(gatewayIP).
				Action().CT(false, UnSNATTable.GetNext(), f.snatCtZones[ipProtocol], nil).
				NAT().
				CTDone().
				Done(),
			// This generates the flow to match the subsequent request packets of connection whose first request packet has
			// been committed in SNAT CT zone, then commit the packets in SNAT CT zone again to perform SNAT.
			// For example:
			/*
				* 192.168.77.1 is the IP address of client.
				* 192.168.77.100 is the IP address of K8s Node.
				* 30001 is the NodePort port.
				* 10.10.0.1 is the IP address of Antrea gateway.
				* 10.10.0.3 is the IP of NodePort Service Endpoint.

				* packet 1 (request)
					* client                     192.168.77.1:12345->192.168.77.100:30001
					* CT zone SNAT 65521         192.168.77.1:12345->192.168.77.100:30001
					* CT zone DNAT 65520         192.168.77.1:12345->192.168.77.100:30001
					* CT commit DNAT zone 65520  192.168.77.1:12345->192.168.77.100:30001  =>  192.168.77.1:12345->10.10.0.3:80
					* CT commit SNAT zone 65521  192.168.77.1:12345->10.10.0.3:80          =>  10.10.0.1:12345->10.10.0.3:80
					* output
				  * packet 2 (reply)
					* Pod                         10.10.0.3:80->10.10.0.1:12345
					* CT zone SNAT 65521          10.10.0.3:80->10.10.0.1:12345            =>  10.10.0.3:80->192.168.77.1:12345
					* CT zone DNAT 65520          10.10.0.3:80->192.168.77.1:12345         =>  192.168.77.1:30001->192.168.77.1:12345
					* output
				  * packet 3 (request)
					* client                     192.168.77.1:12345->192.168.77.100:30001
					* CT zone SNAT 65521         192.168.77.1:12345->192.168.77.100:30001
					* CT zone DNAT 65520         192.168.77.1:12345->10.10.0.3:80
					* CT zone SNAT 65521         192.168.77.1:12345->10.10.0.3:80          =>  10.10.0.1:12345->10.10.0.3:80
					* output
				  * packet ...
			*/
			// As a result, subsequent request packets like packet 3 will only perform SNAT when they pass through SNAT
			// CT zone the second time, after they are DNATed in DNAT CT zone.
			SNATTable.ofTable.BuildFlow(priorityNormal).
				Cookie(cookieID).
				MatchProtocol(ipProtocol).
				MatchCTMark(ConnSNATCTMark).
				MatchCTStateNew(false).
				MatchCTStateTrk(true).
				MatchCTStateRpl(false).
				Action().CT(false, SNATTable.GetNext(), f.snatCtZones[ipProtocol], nil).
				NAT().
				CTDone().
				Done(),
		)
	}
	return flows
}

// flowsToTrace is used to generate flows for Traceflow in featureService.
func (f *featureService) flowsToTrace(dataplaneTag uint8,
	ovsMetersAreSupported,
	liveTraffic,
	droppedOnly,
	receiverOnly bool,
	packet *binding.Packet,
	ofPort uint32,
	timeout uint16) []binding.Flow {
	cookieID := f.cookieAllocator.Request(cookie.Traceflow).Raw()
	var flows []binding.Flow
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

	// This generates Traceflow specific flows that outputs hairpin traceflow packets to OVS port and Antrea Agent after
	// L2forwarding calculation.
	for _, ipProtocol := range f.ipProtocols {
		if f.enableProxy {
			// Only SendToController for hairpin traffic.
			// This flow must have higher priority than the one installed by l2ForwardOutputHairpinServiceFlow.
			fb := OutputTable.ofTable.BuildFlow(priorityHigh + 2).
				Cookie(cookieID).
				MatchProtocol(ipProtocol).
				MatchCTMark(HairpinCTMark).
				MatchIPDSCP(dataplaneTag).
				SetHardTimeout(timeout)
			fb = ifDroppedOnly(fb)
			fb = ifLiveTraffic(fb)
			flows = append(flows, fb.Done())
		}
	}
	return flows
}

// l2ForwardOutputHairpinServiceFlow generates the flow to output the packet of hairpin Service connection with IN_PORT
// action.
func (f *featureService) l2ForwardOutputHairpinServiceFlow() binding.Flow {
	return OutputTable.ofTable.BuildFlow(priorityHigh).
		Cookie(f.cookieAllocator.Request(f.category).Raw()).
		MatchCTMark(HairpinCTMark).
		Action().OutputInPort().
		Done()
}

// sessionAffinityReselectFlow generates the flow which resubmits the Service accessing packet back to ServiceLBTable
// if there is no endpointDNAT flow matched. This case will occur if an Endpoint is removed and is the learned Endpoint
// selection of the Service.
func (f *featureService) sessionAffinityReselectFlow() binding.Flow {
	return EndpointDNATTable.ofTable.BuildFlow(priorityLow).
		Cookie(f.cookieAllocator.Request(f.category).Raw()).
		MatchRegMark(EpSelectedRegMark).
		Action().LoadRegMark(EpToSelectRegMark).
		Action().ResubmitToTables(ServiceLBTable.GetID()).
		Done()
}

// serviceCIDRDNATFlows generates the flows to match destination IP in Service CIDR and output to the Antrea gateway directly.
func (f *featureService) serviceCIDRDNATFlows() []binding.Flow {
	cookieID := f.cookieAllocator.Request(f.category).Raw()
	var flows []binding.Flow
	for ipProtocol, serviceCIDR := range f.serviceCIDRs {
		flows = append(flows, DNATTable.ofTable.BuildFlow(priorityNormal).
			Cookie(cookieID).
			MatchProtocol(ipProtocol).
			MatchDstIPNet(serviceCIDR).
			Action().LoadToRegField(TargetOFPortField, f.gatewayPort).
			Action().LoadRegMark(OutputToOFPortRegMark).
			Action().GotoStage(stageConntrack).
			Done())
	}
	return flows
}

// serviceNeedLBFlow generates the default flow to mark packets with EpToSelectRegMark.
func (f *featureService) serviceNeedLBFlow() binding.Flow {
	return SessionAffinityTable.ofTable.BuildFlow(priorityMiss).
		Cookie(f.cookieAllocator.Request(f.category).Raw()).
		Action().LoadRegMark(EpToSelectRegMark).
		Done()
}

// nodePortMarkFlows generates the flows to mark the first packet of Service NodePort connection with ToNodePortAddressRegMark,
// which indicates the Service type is NodePort.
func (f *featureService) nodePortMarkFlows() []binding.Flow {
	cookieID := f.cookieAllocator.Request(f.category).Raw()
	var flows []binding.Flow
	for ipProtocol, nodePortAddresses := range f.nodePortAddresses {
		// This generates a flow for every NodePort IP. The flows are used to mark the first packet of NodePort connection
		// from a local Pod.
		for i := range nodePortAddresses {
			// From the perspective of a local Pod, the traffic destined to loopback is not NodePort traffic, so we skip
			// the loopback address.
			if nodePortAddresses[i].IsLoopback() {
				continue
			}
			flows = append(flows,
				NodePortMarkTable.ofTable.BuildFlow(priorityNormal).
					Cookie(cookieID).
					MatchProtocol(ipProtocol).
					MatchDstIP(nodePortAddresses[i]).
					Action().LoadRegMark(ToNodePortAddressRegMark).
					Done())
		}
		// This generates the flow for the virtual NodePort DNAT IP. The flow is used to mark the first packet of NodePort
		// connection sourced from the Antrea gateway (the connection is performed DNAT with the virtual IP in host netns).
		flows = append(flows,
			NodePortMarkTable.ofTable.BuildFlow(priorityNormal).
				Cookie(cookieID).
				MatchProtocol(ipProtocol).
				MatchDstIP(f.virtualNodePortDNATIPs[ipProtocol]).
				Action().LoadRegMark(ToNodePortAddressRegMark).
				Done())
	}

	return flows
}

// serviceLearnFlow generates the flow with learn action which adds new flows in SessionAffinityTable according to the
// Endpoint selection decision.
func (f *featureService) serviceLearnFlow(config *types.ServiceConfig) binding.Flow {
	// Using unique cookie ID here to avoid learned flow cascade deletion.
	cookieID := f.cookieAllocator.RequestWithObjectID(f.category, uint32(config.TrafficPolicyGroupID())).Raw()
	flowBuilder := ServiceLBTable.ofTable.BuildFlow(priorityLow).
		Cookie(cookieID).
		MatchProtocol(config.Protocol).
		MatchDstPort(config.ServicePort, nil)

	// EpToLearnRegMark is required to match the packets that have done Endpoint selection.
	regMarksToMatch := []*binding.RegMark{EpToLearnRegMark}
	// ToNodePortAddressRegMark is required to match the packets if the flow is for NodePort address, otherwise Service IP
	// is used.
	if config.IsNodePort {
		regMarksToMatch = append(regMarksToMatch, ToNodePortAddressRegMark)
	} else {
		flowBuilder = flowBuilder.MatchDstIP(config.ServiceIP)
	}
	flowBuilder = flowBuilder.MatchRegMark(regMarksToMatch...)

	// affinityTimeout is used as the OpenFlow "hard timeout": learned flow will be removed from
	// OVS after that time regarding of whether traffic is still hitting the flow. This is the
	// desired behavior based on the K8s spec. Note that existing connections will keep going to
	// the same endpoint because of connection tracking; and that is also the desired behavior.
	isIPv6 := netutils.IsIPv6(config.ServiceIP)
	learnFlowBuilderLearnAction := flowBuilder.
		Action().Learn(SessionAffinityTable.GetID(), priorityNormal, 0, config.AffinityTimeout, 0, 0, cookieID).
		DeleteLearned().
		MatchEthernetProtocol(isIPv6).
		MatchIPProtocol(config.Protocol).
		MatchLearnedDstPort(config.Protocol).
		MatchLearnedDstIP(isIPv6).
		MatchLearnedSrcIP(isIPv6).
		LoadFieldToField(EndpointPortField, EndpointPortField).
		LoadFieldToField(RemoteEndpointRegMark.GetField(), RemoteEndpointRegMark.GetField())
	if isIPv6 {
		learnFlowBuilderLearnAction = learnFlowBuilderLearnAction.LoadXXRegToXXReg(EndpointIP6Field, EndpointIP6Field)
	} else {
		learnFlowBuilderLearnAction = learnFlowBuilderLearnAction.LoadFieldToField(EndpointIPField, EndpointIPField)
	}

	// Loading the EpSelectedRegMark indicates that the Endpoint selection is completed. RewriteMACRegMark must be loaded
	// for Service packets.
	regMarksToLoad := []*binding.RegMark{EpSelectedRegMark, RewriteMACRegMark}
	// If the flow is for external Service IP, which means the Service is accessible externally, the ToExternalAddressRegMark
	// should be loaded to determine whether SNAT is required for the connection.
	if config.IsExternal {
		regMarksToLoad = append(regMarksToLoad, ToExternalAddressRegMark)
	}
	return learnFlowBuilderLearnAction.LoadRegMark(regMarksToLoad...).
		Done().
		Action().LoadRegMark(EpSelectedRegMark).
		Action().NextTable().
		Done()
}

// serviceLBFlows generates the flows which use the specific groups to do Endpoint selection.
func (f *featureService) serviceLBFlows(config *types.ServiceConfig) []binding.Flow {
	buildFlow := func(priority uint16, groupID binding.GroupIDType, extraMatcher func(b binding.FlowBuilder) binding.FlowBuilder) binding.Flow {
		flowBuilder := ServiceLBTable.ofTable.BuildFlow(priority).
			Cookie(f.cookieAllocator.Request(f.category).Raw()).
			MatchProtocol(config.Protocol).
			MatchDstPort(config.ServicePort, nil).
			MatchRegMark(EpToSelectRegMark) // EpToSelectRegMark is required to match the packets that haven't undergone Endpoint selection yet.
		// ToNodePortAddressRegMark is required to match the packets if the flow is for NodePort address, otherwise Service IP
		// is used.
		if config.IsNodePort {
			flowBuilder = flowBuilder.MatchRegMark(ToNodePortAddressRegMark)
		} else {
			flowBuilder = flowBuilder.MatchDstIP(config.ServiceIP)
		}
		if extraMatcher != nil {
			flowBuilder = extraMatcher(flowBuilder)
		}

		// RewriteMACRegMark must be loaded for Service packets.
		regMarksToLoad := []*binding.RegMark{RewriteMACRegMark}
		if config.AffinityTimeout != 0 {
			regMarksToLoad = append(regMarksToLoad, EpToLearnRegMark)
		} else {
			regMarksToLoad = append(regMarksToLoad, EpSelectedRegMark)
		}
		// If the flow is for external Service IP, which means the Service is accessible externally, the ToExternalAddressRegMark
		// should be loaded to determine whether SNAT is required for the connection.
		if config.IsExternal {
			regMarksToLoad = append(regMarksToLoad, ToExternalAddressRegMark)
		}
		if f.enableAntreaPolicy {
			regMarksToLoad = append(regMarksToLoad, binding.NewRegMark(ServiceGroupIDField, uint32(groupID)))
		}
		if config.IsNested {
			regMarksToLoad = append(regMarksToLoad, NestedServiceRegMark)
		}
		return flowBuilder.
			Action().LoadRegMark(regMarksToLoad...).
			Action().Group(groupID).Done()
	}
	flows := []binding.Flow{
		buildFlow(priorityNormal, config.TrafficPolicyGroupID(), nil),
	}
	if config.IsExternal && config.TrafficPolicyLocal {
		// For short-circuiting flow, an extra match condition matching packet from local Pod CIDR is added.
		flows = append(flows, buildFlow(priorityHigh, config.ClusterGroupID, func(b binding.FlowBuilder) binding.FlowBuilder {
			return b.MatchSrcIPNet(f.localCIDRs[getIPProtocol(config.ServiceIP)])
		}))
	}
	if config.IsDSR {
		// For DSR Service, we add a flow to match packets received from tunnel device, which means it has been
		// load-balanced once in ingress Node, and we must select a local Endpoint on this Node.
		flows = append(flows, buildFlow(priorityHigh, config.LocalGroupID, func(b binding.FlowBuilder) binding.FlowBuilder {
			return b.MatchRegMark(FromTunnelRegMark)
		}))
	}
	return flows
}

// dsrServiceMarkFlow generates the flow which matches the packets with the following attributes:
//  1. It's accessing the DSR Service's IP and port.
//  2. It's externally originated.
//  3. It's going to be sent to a remote Endpoint.
//  4. It doesn't already have the DSRServiceRegMark.
//
// And it will perform the following actions:
//  1. Load DSRServiceRegMark to indicate this packet uses DSR mode.
//  2. Generate a learned flow which matches the 5-tuple of the connection, to ensure the same Endpoint will be selected
//     for subsequent packets of the connection.
func (f *featureService) dsrServiceMarkFlow(config *types.ServiceConfig) binding.Flow {
	// Using unique cookie ID here to avoid learned flow cascade deletion.
	cookieID := f.cookieAllocator.RequestWithObjectID(f.category, uint32(config.ClusterGroupID)).Raw()
	isIPv6 := netutils.IsIPv6(config.ServiceIP)
	learnFlowBuilderLearnAction := DSRServiceMarkTable.ofTable.BuildFlow(priorityNormal).
		Cookie(cookieID).
		MatchProtocol(config.Protocol).
		MatchDstIP(config.ServiceIP).
		MatchDstPort(config.ServicePort, nil).
		MatchRegMark(FromExternalRegMark, RemoteEndpointRegMark, NotDSRServiceRegMark).
		// This learned flow has higher priority than the learned flow generated for ClientIP session affinity because
		// we need this connection's traffic to hit this flow to reset the idle duration and its FIN/RST packet to reset
		// the idle timeout.
		Action().Learn(SessionAffinityTable.GetID(), priorityHigh, dsrServiceConnectionIdleTimeout, 0, dsrServiceConnectionFinIdleTimeout, 0, cookieID).
		DeleteLearned().
		MatchEthernetProtocol(isIPv6).
		MatchIPProtocol(config.Protocol).
		MatchLearnedSrcPort(config.Protocol).
		MatchLearnedDstPort(config.Protocol).
		MatchLearnedSrcIP(isIPv6).
		MatchLearnedDstIP(isIPv6).
		LoadFieldToField(EndpointPortField, EndpointPortField).
		LoadRegMark(EpSelectedRegMark, DSRServiceRegMark)
	if isIPv6 {
		learnFlowBuilderLearnAction = learnFlowBuilderLearnAction.LoadXXRegToXXReg(EndpointIP6Field, EndpointIP6Field)
	} else {
		learnFlowBuilderLearnAction = learnFlowBuilderLearnAction.LoadFieldToField(EndpointIPField, EndpointIPField)
	}
	return learnFlowBuilderLearnAction.Done().
		Action().LoadRegMark(DSRServiceRegMark).
		Action().NextTable().
		Done()
}

// endpointRedirectFlowForServiceIP generates the flow which uses the specific group for a Service's ClusterIP
// to do final Endpoint selection.
func (f *featureService) endpointRedirectFlowForServiceIP(config *types.ServiceConfig) binding.Flow {
	unionVal := (EpSelectedRegMark.GetValue() << EndpointPortField.GetRange().Length()) + uint32(config.ServicePort)
	flowBuilder := EndpointDNATTable.ofTable.BuildFlow(priorityHigh).
		MatchProtocol(config.Protocol).
		Cookie(f.cookieAllocator.Request(f.category).Raw()).
		MatchRegFieldWithValue(EpUnionField, unionVal).
		MatchRegMark(NestedServiceRegMark)
	ipProtocol := getIPProtocol(config.ServiceIP)

	if ipProtocol == binding.ProtocolIP {
		ipVal := binary.BigEndian.Uint32(config.ServiceIP.To4())
		flowBuilder = flowBuilder.MatchRegFieldWithValue(EndpointIPField, ipVal)
	} else {
		ipVal := []byte(config.ServiceIP)
		flowBuilder = flowBuilder.MatchXXReg(EndpointIP6Field.GetRegID(), ipVal)
	}
	return flowBuilder.Action().
		Group(config.TrafficPolicyGroupID()).
		Done()
}

// endpointDNATFlow generates the flow which transforms the Service Cluster IP to the Endpoint IP according to the Endpoint
// selection decision which is stored in regs.
func (f *featureService) endpointDNATFlow(endpointIP net.IP, endpointPort uint16, protocol binding.Protocol) binding.Flow {
	unionVal := (EpSelectedRegMark.GetValue() << EndpointPortField.GetRange().Length()) + uint32(endpointPort)
	flowBuilder := EndpointDNATTable.ofTable.BuildFlow(priorityNormal).
		MatchProtocol(protocol).
		Cookie(f.cookieAllocator.Request(f.category).Raw()).
		MatchRegFieldWithValue(EpUnionField, unionVal)
	ipProtocol := getIPProtocol(endpointIP)

	if ipProtocol == binding.ProtocolIP {
		ipVal := binary.BigEndian.Uint32(endpointIP.To4())
		flowBuilder = flowBuilder.MatchRegFieldWithValue(EndpointIPField, ipVal)
	} else {
		ipVal := []byte(endpointIP)
		flowBuilder = flowBuilder.MatchXXReg(EndpointIP6Field.GetRegID(), ipVal)
	}

	return flowBuilder.Action().
		CT(true, EndpointDNATTable.GetNext(), f.dnatCtZones[ipProtocol], f.ctZoneSrcField).
		DNAT(
			&binding.IPRange{StartIP: endpointIP, EndIP: endpointIP},
			&binding.PortRange{StartPort: endpointPort, EndPort: endpointPort},
		).
		LoadToCtMark(ServiceCTMark).
		MoveToCtMarkField(PktSourceField, ConnSourceCTMarkField).
		CTDone().
		Done()
}

// dsrServiceNoDNATFlows generates the flows which prevent traffic in DSR mode from being DNATed on the ingress Node.
func (f *featureService) dsrServiceNoDNATFlows() []binding.Flow {
	var flows []binding.Flow
	for _, ipProtocol := range f.ipProtocols {
		flows = append(flows, EndpointDNATTable.ofTable.BuildFlow(priorityHigh).
			MatchProtocol(ipProtocol).
			Cookie(f.cookieAllocator.Request(f.category).Raw()).
			MatchRegMark(DSRServiceRegMark).
			Action().
			CT(true, EndpointDNATTable.GetNext(), f.dnatCtZones[ipProtocol], f.ctZoneSrcField).
			// Note that the ct mark cannot be read from conntrack by ct action because the connection is in invalid state.
			// We load it more for consistency.
			MoveToCtMarkField(PktSourceField, ConnSourceCTMarkField).
			CTDone().
			Done())
	}
	return flows
}

// serviceEndpointGroup creates/modifies the group/buckets of Endpoints. If the withSessionAffinity is true, then buckets
// will resubmit packets back to ServiceLBTable to trigger the learn flow, the learn flow will then send packets to
// EndpointDNATTable. Otherwise, buckets will resubmit packets to EndpointDNATTable directly.
// IMPORTANT: Ensure any changes to this function are tested in TestServiceEndpointGroupMaxBuckets.
func (f *featureService) serviceEndpointGroup(groupID binding.GroupIDType, withSessionAffinity bool, endpoints ...proxy.Endpoint) binding.Group {
	group := f.bridge.NewGroup(groupID)

	if len(endpoints) == 0 {
		return group.Bucket().Weight(100).
			LoadRegMark(SvcNoEpRegMark).
			ResubmitToTable(EndpointDNATTable.GetID()).
			Done()
	}

	var resubmitTableID uint8
	if withSessionAffinity {
		resubmitTableID = ServiceLBTable.GetID()
	} else {
		resubmitTableID = ServiceLBTable.GetNext() // It will be EndpointDNATTable if DSR is not enabled, otherwise DSRServiceMarkTable.
	}
	for _, endpoint := range endpoints {
		endpointPort, _ := endpoint.Port()
		endpointIP := net.ParseIP(endpoint.IP())
		portVal := util.PortToUint16(endpointPort)
		ipProtocol := getIPProtocol(endpointIP)
		bucketBuilder := group.Bucket().Weight(100)
		// Load RemoteEndpointRegMark for remote non-hostNetwork Endpoints.
		if !endpoint.GetIsLocal() && endpoint.GetNodeName() != "" && !f.nodeIPChecker.IsNodeIP(endpoint.IP()) {
			bucketBuilder = bucketBuilder.LoadRegMark(RemoteEndpointRegMark)
		}
		if ipProtocol == binding.ProtocolIP {
			ipVal := binary.BigEndian.Uint32(endpointIP.To4())
			bucketBuilder = bucketBuilder.LoadToRegField(EndpointIPField, ipVal)
		} else if ipProtocol == binding.ProtocolIPv6 {
			ipVal := []byte(endpointIP)
			bucketBuilder = bucketBuilder.LoadXXReg(EndpointIP6Field.GetRegID(), ipVal)
		}
		group = bucketBuilder.
			LoadToRegField(EndpointPortField, uint32(portVal)).
			ResubmitToTable(resubmitTableID).
			Done()
	}
	return group
}

// preRoutingClassifierFlows generates the flow to classify packets in stagePreRouting.
func (f *featureService) preRoutingClassifierFlows() []binding.Flow {
	cookieID := f.cookieAllocator.Request(f.category).Raw()
	var flows []binding.Flow

	targetTables := []uint8{SessionAffinityTable.GetID(), ServiceLBTable.GetID()}
	if f.proxyAll {
		targetTables = append([]uint8{NodePortMarkTable.GetID()}, targetTables...)
	}
	for _, ipProtocol := range f.ipProtocols {
		flows = append(flows,
			// This generates the default flow to match the first packet of a connection.
			PreRoutingClassifierTable.ofTable.BuildFlow(priorityNormal).
				Cookie(cookieID).
				MatchProtocol(ipProtocol).
				Action().ResubmitToTables(targetTables...).
				Done(),
		)
	}

	return flows
}

// l3FwdFlowToExternalEndpoint generates the flow to forward the packets of Service connections sourced from local Antrea
// gateway and destined for external network. Note that, the destination MAC address of the packets should be rewritten to
// local Antrea gateway's so that the packets can be forwarded to external network via local Antrea gateway.
func (f *featureService) l3FwdFlowToExternalEndpoint() binding.Flow {
	return L3ForwardingTable.ofTable.BuildFlow(priorityLow).
		Cookie(f.cookieAllocator.Request(f.category).Raw()).
		MatchRegMark(RewriteMACRegMark, FromGatewayRegMark).
		MatchCTMark(ServiceCTMark).
		Action().SetDstMAC(f.gatewayMAC).
		Action().LoadRegMark(ToGatewayRegMark).
		Action().GotoTable(L3DecTTLTable.GetID()).
		Done()
}

// podHairpinSNATFlow generates the flow to match the first packet of hairpin connection initiated through a local Pod.
// ConnSNATCTMark and HairpinCTMark will be loaded in DNAT CT zone.
func (f *featureService) podHairpinSNATFlow(endpoint net.IP) binding.Flow {
	ipProtocol := getIPProtocol(endpoint)
	return SNATMarkTable.ofTable.BuildFlow(priorityLow).
		Cookie(f.cookieAllocator.Request(f.category).Raw()).
		MatchProtocol(ipProtocol).
		MatchCTStateNew(true).
		MatchCTStateTrk(true).
		MatchSrcIP(endpoint).
		MatchDstIP(endpoint).
		Action().CT(true, SNATMarkTable.GetNext(), f.dnatCtZones[ipProtocol], f.ctZoneSrcField).
		LoadToCtMark(ConnSNATCTMark, HairpinCTMark).
		CTDone().
		Done()
}

// gatewaySNATFlows generate the flows to match the first packet of Service connection initiated through the Antrea gateway,
// and the connection requires SNAT.
func (f *featureService) gatewaySNATFlows() []binding.Flow {
	cookieID := f.cookieAllocator.Request(f.category).Raw()
	var flows []binding.Flow
	for _, ipProtocol := range f.ipProtocols {
		// This generates the flow to match the first packet of hairpin connection initiated through the Antrea gateway.
		// ConnSNATCTMark and HairpinCTMark will be loaded in DNAT CT zone.
		flows = append(flows, SNATMarkTable.ofTable.BuildFlow(priorityNormal).
			Cookie(cookieID).
			MatchProtocol(ipProtocol).
			MatchCTStateNew(true).
			MatchCTStateTrk(true).
			MatchRegMark(FromGatewayRegMark, ToGatewayRegMark).
			Action().CT(true, SNATMarkTable.GetNext(), f.dnatCtZones[ipProtocol], f.ctZoneSrcField).
			LoadToCtMark(ConnSNATCTMark, HairpinCTMark).
			CTDone().
			Done())

		var pktDstRegMarks []*binding.RegMark
		if f.networkConfig.TrafficEncapMode.SupportsEncap() {
			pktDstRegMarks = append(pktDstRegMarks, ToTunnelRegMark)
		}
		if f.networkConfig.TrafficEncapMode.SupportsNoEncap() && runtime.IsWindowsPlatform() {
			pktDstRegMarks = append(pktDstRegMarks, ToUplinkRegMark)
		}
		for _, pktDstRegMark := range pktDstRegMarks {
			// This generates the flow to match the first packets of externally-originated connections towards external
			// addresses of the Service initiated through the Antrea gateway, and the selected Endpoint is on a remote Node,
			// then ConnSNATCTMark will be loaded in DNAT CT zone, indicating that SNAT is required for the connection.
			flows = append(flows, SNATMarkTable.ofTable.BuildFlow(priorityNormal).
				Cookie(cookieID).
				MatchProtocol(ipProtocol).
				MatchCTStateNew(true).
				MatchCTStateTrk(true).
				MatchRegMark(FromGatewayRegMark, pktDstRegMark, ToExternalAddressRegMark, NotDSRServiceRegMark). // Do not SNAT DSR traffic.
				Action().CT(true, SNATMarkTable.GetNext(), f.dnatCtZones[ipProtocol], f.ctZoneSrcField).
				LoadToCtMark(ConnSNATCTMark).
				CTDone().
				Done())
		}
	}

	return flows
}

func (f *featureService) replayFlows() []*openflow15.FlowMod {
	return getCachedFlowMessages(f.cachedFlows)
}

func (f *featureService) replayGroups() []binding.OFEntry {
	var groups []binding.OFEntry
	f.groupCache.Range(func(id, value interface{}) bool {
		group := value.(binding.Group)
		group.Reset()
		groups = append(groups, group)
		return true
	})
	return groups
}

func (f *featureService) initGroups() []binding.OFEntry {
	return nil
}

func (f *featureService) replayMeters() []binding.OFEntry {
	return nil
}

func (f *featureService) getRequiredTables() []*Table {
	if !f.enableProxy {
		return []*Table{DNATTable}
	}
	tables := []*Table{
		UnSNATTable,
		PreRoutingClassifierTable,
		SessionAffinityTable,
		ServiceLBTable,
		EndpointDNATTable,
		L3ForwardingTable,
		SNATMarkTable,
		SNATTable,
		ConntrackCommitTable,
		OutputTable,
	}
	if f.proxyAll {
		tables = append(tables, NodePortMarkTable)
	}
	if f.enableDSR {
		tables = append(tables, DSRServiceMarkTable)
	}
	return tables
}
