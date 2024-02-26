// Copyright 2019 Antrea Authors
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
	binding "antrea.io/antrea/pkg/ovs/openflow"
	"fmt"
	"net"
)

var (
	//      _   _   _             _   _               _
	//     / \ | |_| |_ ___ _ __ | |_(_) ___  _ __   | |
	//    / _ \| __| __/ _ \ '_ \| __| |/ _ \| '_ \  | |
	//   / ___ \ |_| ||  __/ | | | |_| | (_) | | | | |_|
	//  /_/   \_\__|\__\___|_| |_|\__|_|\___/|_| |_| (_)
	//
	// Before adding a new table in FlexiblePipeline, please read the following instructions carefully.
	//
	// - Double confirm the necessity of adding a new table, and consider reusing an existing table to implement the
	//   functionality alternatively.
	// - Choose a name that can help users to understand the function of the table.
	// - Choose a stage. Existing stageIDs are defined in file pkg/agent/openflow/framework.go. If you want to add a new
	//   stage, please discuss with maintainers or OVS pipeline developers of Antrea.
	// - Choose a pipeline. Existing pipelineIDs are defined in file pkg/agent/openflow/framework.go. If you want to add
	//   a new pipeline, please discuss with maintainers or OVS pipeline developers of Antrea.
	// - Decide where to add the new table in the pipeline. The order table declaration decides the order of tables in the
	//   stage. For example:
	//     * If you want to add a table called `FooTable` between `SpoofGuardTable` and `IPv6Table` in pipelineIP, then
	//       the table should be declared after `SpoofGuardTable` and before `IPv6Table`:
	//       ```go
	//          SpoofGuardTable  = newTable("SpoofGuard", stageValidation, pipelineIP)
	//          FooTable         = newTable("Foo", stageValidation, pipelineIP)
	//          IPv6Table        = newTable("IPv6", stageValidation, pipelineIP)
	//       ```
	//      * If you want to add a table called `FooTable` just before `ARPResponderTable` in pipelineARP, then the table
	//        should be declared before `ARPResponderTable`:
	//       ```go
	//          FooTable          = newTable("Foo", stageOutput, binding.PipelineARP)
	//          ARPResponderTable = newTable("ARPResponder", stageOutput, binding.PipelineARP)
	//       ```
	//       * If you want to add a table called `FooTable` just after `ConntrackStateTable` in pipelineARP, then the
	//         table should be declared after `ConntrackStateTable`:
	//       ```go
	//          UnSNATTable         = newTable("UnSNAT", stageConntrackState, pipelineIP)
	//          ConntrackTable      = newTable("ConntrackZone", stageConntrackState, pipelineIP)
	//          ConntrackStateTable = newTable("ConntrackState", stageConntrackState, pipelineIP)
	//          FooTable            = newTable("Foo", stageConntrackState, pipelineIP)
	//       ```
	//  - Reference the new table in a feature in file pkg/agent/openflow/framework.go. The table can be referenced by multiple
	//    features if multiple features need to install flows in the table. Note that, if the newly added table is not
	//    referenced by any feature or the features referencing the table are all inactivated, then the table will not
	//    be realized in OVS; if at least one feature referencing the table is activated, then the table will be realized
	//    at the desired position in OVS pipeline.
	//  - By default, the miss action of the new table is to forward packets to next table. If the miss action needs to
	//    drop packets, add argument defaultDrop when creating the new table.
	//
	// How to forward packet between tables with a proper action in FlexiblePipeline?
	//
	// |   table A   | |   table B   | |   table C   | |   table D   | |   table E   | |   table F   | |   table G   |
	// |   stage S1  | |                           stage S2                          | |          stage S4           |
	//
	//  - NextTable is used to forward packets to the next table. E.g. A -> B, B -> C, C -> D, etc.
	//  - GotoTable is used to forward packets to a specific table, and the target table ID should be greater than the
	//    current table ID. Within a stage, GotoTable should be used to forward packets to a specific table, e.g. B -> D,
	//    C -> E. Today we do not have the case, but if in future there is a case that a packet needs to be forwarded to
	//    a table in another stage directly, e.g. A -> C, B -> G, GotoTable can also be used.
	//  - GotoStage is used to forward packets to a specific stage. Note that, packets are forwarded to the first table of
	//    the target stage, and the first table ID of the target stage should be greater than the current table ID. E.g.
	//    A -> S4 (F), D -> S4 (F) are fine, but D -> S1 (A), F -> S2 (B) are not allowed. It is recommended to use
	//    GotoStage to forward packets across stages.
	//  - ResubmitToTables is used to forward packets to one or multiple tables. It should be used only when the target
	//    table ID is smaller than the current table ID, like E -> B; or when forwarding packets to multiple tables,
	//    like B - > D E; otherwise, in all other cases GotoTable should be used.

	// Tables of PipelineRoot are declared below.

	// PipelineRootClassifierTable is the only table of pipelineRoot at this moment and its table ID should be 0. Packets
	// are forwarded to pipelineIP or pipelineARP in this table.
	PipelineRootClassifierTable = newTable("PipelineRootClassifier", stageStart, pipelineRoot, defaultDrop)

	// Tables of pipelineARP are declared below.

	// Tables in stageValidation:
	ARPSpoofGuardTable = newTable("ARPSpoofGuard", stageValidation, pipelineARP, defaultDrop)

	// Tables in stageOutput:
	ARPResponderTable = newTable("ARPResponder", stageOutput, pipelineARP)

	// Tables of pipelineIP are declared below.

	// Tables in stageClassifier:
	ClassifierTable = newTable("Classifier", stageClassifier, pipelineIP, defaultDrop)

	// Tables in stageValidation:
	SpoofGuardTable           = newTable("SpoofGuard", stageValidation, pipelineIP, defaultDrop)
	IPv6Table                 = newTable("IPv6", stageValidation, pipelineIP)
	PipelineIPClassifierTable = newTable("PipelineIPClassifier", stageValidation, pipelineIP)

	// Tables in stageConntrackState:
	UnSNATTable         = newTable("UnSNAT", stageConntrackState, pipelineIP)
	ConntrackTable      = newTable("ConntrackZone", stageConntrackState, pipelineIP)
	ConntrackStateTable = newTable("ConntrackState", stageConntrackState, pipelineIP)

	// Tables in stagePreRouting:
	// When proxy is enabled.
	PreRoutingClassifierTable = newTable("PreRoutingClassifier", stagePreRouting, pipelineIP)
	NodePortMarkTable         = newTable("NodePortMark", stagePreRouting, pipelineIP)
	SessionAffinityTable      = newTable("SessionAffinity", stagePreRouting, pipelineIP)
	ServiceLBTable            = newTable("ServiceLB", stagePreRouting, pipelineIP)
	DSRServiceMarkTable       = newTable("DSRServiceMark", stagePreRouting, pipelineIP)
	EndpointDNATTable         = newTable("EndpointDNAT", stagePreRouting, pipelineIP)
	// When proxy is disabled.
	DNATTable = newTable("DNAT", stagePreRouting, pipelineIP)

	// Tables in stageEgressSecurity:
	EgressSecurityClassifierTable = newTable("EgressSecurityClassifier", stageEgressSecurity, pipelineIP)
	AntreaPolicyEgressRuleTable   = newTable("AntreaPolicyEgressRule", stageEgressSecurity, pipelineIP)
	EgressRuleTable               = newTable("EgressRule", stageEgressSecurity, pipelineIP)
	EgressDefaultTable            = newTable("EgressDefaultRule", stageEgressSecurity, pipelineIP)
	EgressMetricTable             = newTable("EgressMetric", stageEgressSecurity, pipelineIP)

	// Tables in stageRouting:
	L3ForwardingTable = newTable("L3Forwarding", stageRouting, pipelineIP)
	EgressMarkTable   = newTable("EgressMark", stageRouting, pipelineIP)
	EgressQoSTable    = newTable("EgressQoS", stageRouting, pipelineIP)
	L3DecTTLTable     = newTable("L3DecTTL", stageRouting, pipelineIP)

	// Tables in stagePostRouting:
	SNATMarkTable = newTable("SNATMark", stagePostRouting, pipelineIP)
	SNATTable     = newTable("SNAT", stagePostRouting, pipelineIP)

	// Tables in stageSwitching:
	L2ForwardingCalcTable = newTable("L2ForwardingCalc", stageSwitching, pipelineIP)
	TrafficControlTable   = newTable("TrafficControl", stageSwitching, pipelineIP)

	// Tables in stageIngressSecurity:
	IngressSecurityClassifierTable = newTable("IngressSecurityClassifier", stageIngressSecurity, pipelineIP)
	AntreaPolicyIngressRuleTable   = newTable("AntreaPolicyIngressRule", stageIngressSecurity, pipelineIP)
	IngressRuleTable               = newTable("IngressRule", stageIngressSecurity, pipelineIP)
	IngressDefaultTable            = newTable("IngressDefaultRule", stageIngressSecurity, pipelineIP)
	IngressMetricTable             = newTable("IngressMetric", stageIngressSecurity, pipelineIP)

	// Tables in stageConntrack:
	ConntrackCommitTable = newTable("ConntrackCommit", stageConntrack, pipelineIP)

	// Tables in stageOutput:
	VLANTable   = newTable("VLAN", stageOutput, pipelineIP)
	OutputTable = newTable("Output", stageOutput, pipelineIP)

	// Tables of pipelineMulticast are declared below. Do don't declare any tables of other pipelines here!
	// Tables in stageEgressSecurity:
	// Since IGMP Egress rules only support IGMP report which is handled by packetIn, it is not necessary to add
	// MulticastIGMPEgressMetricTable here.
	MulticastEgressRuleTable   = newTable("MulticastEgressRule", stageEgressSecurity, pipelineMulticast)
	MulticastEgressMetricTable = newTable("MulticastEgressMetric", stageEgressSecurity, pipelineMulticast)

	MulticastEgressPodMetricTable = newTable("MulticastEgressPodMetric", stageEgressSecurity, pipelineMulticast)

	// Tables in stageRouting:
	MulticastRoutingTable = newTable("MulticastRouting", stageRouting, pipelineMulticast)
	// Tables in stageIngressSecurity
	MulticastIngressRuleTable      = newTable("MulticastIngressRule", stageIngressSecurity, pipelineMulticast)
	MulticastIngressMetricTable    = newTable("MulticastIngressMetric", stageIngressSecurity, pipelineMulticast)
	MulticastIngressPodMetricTable = newTable("MulticastIngressPodMetric", stageIngressSecurity, pipelineMulticast)
	// Tables in stageOutput
	MulticastOutputTable = newTable("MulticastOutput", stageOutput, pipelineMulticast)

	// NonIPTable is used when Antrea Agent is running on an external Node. It forwards the non-IP packet
	// between the uplink and its pair port directly.
	NonIPTable = newTable("NonIP", stageClassifier, pipelineNonIP, defaultDrop)

	// Flow priority level
	priorityHigh            = uint16(210)
	priorityNormal          = uint16(200)
	priorityLow             = uint16(190)
	priorityMiss            = uint16(0)
	priorityTopAntreaPolicy = uint16(64990)
	priorityDNSIntercept    = uint16(64991)

	// Index for priority cache
	priorityIndex = "priority"

	// IPv6 multicast prefix
	ipv6MulticastAddr = "FF00::/8"
	// IPv6 link-local prefix
	ipv6LinkLocalAddr = "FE80::/10"

	// Operation field values in ARP packets
	arpOpRequest = uint16(1)
	arpOpReply   = uint16(2)

	tableNameIndex = "tableNameIndex"
)

// tableCache caches the OpenFlow tables used in pipelines, and it supports using the table ID and name as the index to query the OpenFlow table.
//var tableCache = cache.NewIndexer(tableIDKeyFunc, cache.Indexers{tableNameIndex: tableNameIndexFunc})

func tableNameIndexFunc(obj interface{}) ([]string, error) {
	table := obj.(*Table)
	return []string{table.GetName()}, nil
}

func tableIDKeyFunc(obj interface{}) (string, error) {
	table := obj.(*Table)
	return fmt.Sprintf("%d", table.GetID()), nil
}

func GetAntreaPolicyEgressTables() []*Table {
	return []*Table{
		AntreaPolicyEgressRuleTable,
		EgressDefaultTable,
	}
}

func GetAntreaIGMPIngressTables() []*Table {
	return []*Table{
		MulticastIngressRuleTable,
	}
}

func GetAntreaMulticastEgressTables() []*Table {
	return []*Table{
		MulticastEgressRuleTable,
	}
}

func GetAntreaPolicyIngressTables() []*Table {
	return []*Table{
		AntreaPolicyIngressRuleTable,
		IngressDefaultTable,
	}
}

func GetAntreaPolicyBaselineTierTables() []*Table {
	return []*Table{
		EgressDefaultTable,
		IngressDefaultTable,
	}
}

func GetAntreaPolicyMultiTierTables() []*Table {
	return []*Table{
		AntreaPolicyEgressRuleTable,
		AntreaPolicyIngressRuleTable,
	}
}

const (
	CtZone       = 0xfff0
	CtZoneV6     = 0xffe6
	SNATCtZone   = 0xfff1
	SNATCtZoneV6 = 0xffe7

	// disposition values used in AP
	DispositionAllow = 0b00
	DispositionDrop  = 0b01
	DispositionRej   = 0b10
	DispositionPass  = 0b11
	// DispositionL7NPRedirect is used when sending packet-in to controller for
	// logging layer 7 NetworkPolicy indicating that this packet is redirected to
	// l7 engine to determine the disposition.
	DispositionL7NPRedirect = 0b1

	// EtherTypeDot1q is used when adding 802.1Q VLAN header in OVS action
	EtherTypeDot1q = 0x8100

	// dsrServiceConnectionIdleTimeout represents the idle timeout of the flows learned for DSR Service.
	// 160 means the learned flows will be deleted if the flow is not used in 160s.
	// It tolerates 1 keep-alive drop (net.ipv4.tcp_keepalive_intvl defaults to 75) and a deviation of 10s for long connections.
	dsrServiceConnectionIdleTimeout = 160
	// dsrServiceConnectionFinIdleTimeout represents the idle timeout of the flows learned for DSR Service after a TCP
	// packet with the FIN or RST flag is received.
	dsrServiceConnectionFinIdleTimeout = 5
)

var DispositionToString = map[uint32]string{
	DispositionAllow: "Allow",
	DispositionDrop:  "Drop",
	DispositionRej:   "Reject",
	DispositionPass:  "Pass",
}

var (
	// snatPktMarkRange takes an 8-bit range of pkt_mark to store the ID of
	// a SNAT IP. The bit range must match SNATIPMarkMask.
	snatPktMarkRange = &binding.Range{0, 7}

	GlobalVirtualMAC, _ = net.ParseMAC("aa:bb:cc:dd:ee:ff")
)
