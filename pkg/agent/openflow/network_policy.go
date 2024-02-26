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
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"

	"antrea.io/libOpenflow/openflow15"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	"antrea.io/antrea/pkg/agent/config"
	"antrea.io/antrea/pkg/agent/openflow/cookie"
	"antrea.io/antrea/pkg/agent/types"
	"antrea.io/antrea/pkg/apis/controlplane/v1beta2"
	crdv1beta1 "antrea.io/antrea/pkg/apis/crd/v1beta1"
	binding "antrea.io/antrea/pkg/ovs/openflow"
	thirdpartynp "antrea.io/antrea/third_party/networkpolicy"
)

var (
	MatchDstIP          = types.NewMatchKey(binding.ProtocolIP, types.IPAddr, "nw_dst")
	MatchSrcIP          = types.NewMatchKey(binding.ProtocolIP, types.IPAddr, "nw_src")
	MatchDstIPNet       = types.NewMatchKey(binding.ProtocolIP, types.IPNetAddr, "nw_dst")
	MatchSrcIPNet       = types.NewMatchKey(binding.ProtocolIP, types.IPNetAddr, "nw_src")
	MatchCTDstIP        = types.NewMatchKey(binding.ProtocolIP, types.IPAddr, "ct_nw_dst")
	MatchCTSrcIP        = types.NewMatchKey(binding.ProtocolIP, types.IPAddr, "ct_nw_src")
	MatchCTDstIPNet     = types.NewMatchKey(binding.ProtocolIP, types.IPNetAddr, "ct_nw_dst")
	MatchCTSrcIPNet     = types.NewMatchKey(binding.ProtocolIP, types.IPNetAddr, "ct_nw_src")
	MatchDstIPv6        = types.NewMatchKey(binding.ProtocolIPv6, types.IPAddr, "ipv6_dst")
	MatchSrcIPv6        = types.NewMatchKey(binding.ProtocolIPv6, types.IPAddr, "ipv6_src")
	MatchDstIPNetv6     = types.NewMatchKey(binding.ProtocolIPv6, types.IPNetAddr, "ipv6_dst")
	MatchSrcIPNetv6     = types.NewMatchKey(binding.ProtocolIPv6, types.IPNetAddr, "ipv6_src")
	MatchCTDstIPv6      = types.NewMatchKey(binding.ProtocolIPv6, types.IPAddr, "ct_ipv6_dst")
	MatchCTSrcIPv6      = types.NewMatchKey(binding.ProtocolIPv6, types.IPAddr, "ct_ipv6_src")
	MatchCTDstIPNetv6   = types.NewMatchKey(binding.ProtocolIPv6, types.IPNetAddr, "ct_ipv6_dst")
	MatchCTSrcIPNetv6   = types.NewMatchKey(binding.ProtocolIPv6, types.IPNetAddr, "ct_ipv6_src")
	MatchDstOFPort      = types.NewMatchKey(binding.ProtocolIP, types.OFPortAddr, "reg1[0..31]")
	MatchSrcOFPort      = types.NewMatchKey(binding.ProtocolIP, types.OFPortAddr, "in_port")
	MatchTCPDstPort     = types.NewMatchKey(binding.ProtocolTCP, types.L4PortAddr, "tp_dst")
	MatchTCPv6DstPort   = types.NewMatchKey(binding.ProtocolTCPv6, types.L4PortAddr, "tp_dst")
	MatchUDPDstPort     = types.NewMatchKey(binding.ProtocolUDP, types.L4PortAddr, "tp_dst")
	MatchUDPv6DstPort   = types.NewMatchKey(binding.ProtocolUDPv6, types.L4PortAddr, "tp_dst")
	MatchSCTPDstPort    = types.NewMatchKey(binding.ProtocolSCTP, types.L4PortAddr, "tp_dst")
	MatchSCTPv6DstPort  = types.NewMatchKey(binding.ProtocolSCTPv6, types.L4PortAddr, "tp_dst")
	MatchSCTPSrcPort    = types.NewMatchKey(binding.ProtocolSCTP, types.L4PortAddr, "tp_src")
	MatchSCTPv6SrcPort  = types.NewMatchKey(binding.ProtocolSCTPv6, types.L4PortAddr, "tp_src")
	MatchTCPSrcPort     = types.NewMatchKey(binding.ProtocolTCP, types.L4PortAddr, "tp_src")
	MatchTCPv6SrcPort   = types.NewMatchKey(binding.ProtocolTCPv6, types.L4PortAddr, "tp_src")
	MatchUDPSrcPort     = types.NewMatchKey(binding.ProtocolUDP, types.L4PortAddr, "tp_src")
	MatchUDPv6SrcPort   = types.NewMatchKey(binding.ProtocolUDPv6, types.L4PortAddr, "tp_src")
	MatchICMPType       = types.NewMatchKey(binding.ProtocolICMP, types.ICMPAddr, "icmp_type")
	MatchICMPCode       = types.NewMatchKey(binding.ProtocolICMP, types.ICMPAddr, "icmp_code")
	MatchICMPv6Type     = types.NewMatchKey(binding.ProtocolICMPv6, types.ICMPAddr, "icmpv6_type")
	MatchICMPv6Code     = types.NewMatchKey(binding.ProtocolICMPv6, types.ICMPAddr, "icmpv6_code")
	MatchServiceGroupID = types.NewMatchKey(binding.ProtocolIP, types.ServiceGroupIDAddr, "reg7[0..31]")
	MatchIGMPProtocol   = types.NewMatchKey(binding.ProtocolIGMP, types.IGMPAddr, "igmp")
	MatchLabelID        = types.NewMatchKey(binding.ProtocolIP, types.LabelIDAddr, "tun_id")
	MatchTCPFlags       = types.NewMatchKey(binding.ProtocolTCP, types.TCPFlagsAddr, "tcp_flags")
	MatchTCPv6Flags     = types.NewMatchKey(binding.ProtocolTCPv6, types.TCPFlagsAddr, "tcp_flags")
	// MatchCTState should be used with ct_state condition as matchValue.
	// MatchValue example: `+rpl+trk`.
	MatchCTState = types.NewMatchKey(binding.ProtocolIP, types.CTStateAddr, "ct_state")
	Unsupported  = types.NewMatchKey(binding.ProtocolIP, types.UnSupported, "unknown")

	// metricFlowIdentifier is used to identify metric flows in metric table.
	// There could be other flows like default flow and Traceflow flows in the table. Only metric flows are supposed to
	// have normal priority.
	metricFlowIdentifier = fmt.Sprintf("priority=%d,", priorityNormal)

	protocolTCP = v1beta2.ProtocolTCP
	dnsPort     = int32(53)
)

type TCPFlags struct {
	Flag uint16
	Mask uint16
}

// policyConjKeyFunc knows how to get key of a *policyRuleConjunction.
func policyConjKeyFunc(obj interface{}) (string, error) {
	conj := obj.(*policyRuleConjunction)
	return fmt.Sprint(conj.id), nil
}

// priorityIndexFunc knows how to get priority of actionFlows in a *policyRuleConjunction.
// It's provided to cache.Indexer to build an index of policyRuleConjunction.
func priorityIndexFunc(obj interface{}) ([]string, error) {
	conj := obj.(*policyRuleConjunction)
	return conj.ActionFlowPriorities(), nil
}

// IP address calculated from Pod's address.
type IPAddress net.IP

func (a *IPAddress) GetMatchKey(addrType types.AddressType) *types.MatchKey {
	ipArr := net.IP(*a)
	switch addrType {
	case types.SrcAddress:
		if ipArr.To4() != nil {
			return MatchSrcIP
		}
		return MatchSrcIPv6
	case types.DstAddress:
		if ipArr.To4() != nil {
			return MatchDstIP
		}
		return MatchDstIPv6
	default:
		klog.Errorf("Unknown AddressType %d in IPAddress", addrType)
		return Unsupported
	}
}

func (a *IPAddress) GetMatchValue() string {
	addr := net.IP(*a)
	return addr.String()
}

func (a *IPAddress) GetValue() interface{} {
	return net.IP(*a)
}

func NewIPAddress(addr net.IP) *IPAddress {
	ia := IPAddress(addr)
	return &ia
}

// IP block calculated from Pod's address.
type IPNetAddress net.IPNet

func (a *IPNetAddress) GetMatchKey(addrType types.AddressType) *types.MatchKey {
	ipAddr := net.IPNet(*a)
	switch addrType {
	case types.SrcAddress:
		if ipAddr.IP.To4() != nil {
			return MatchSrcIPNet
		}
		return MatchSrcIPNetv6
	case types.DstAddress:
		if ipAddr.IP.To4() != nil {
			return MatchDstIPNet
		}
		return MatchDstIPNetv6
	default:
		klog.Errorf("Unknown AddressType %d in IPNetAddress", addrType)
		return Unsupported
	}
}

func (a *IPNetAddress) GetMatchValue() string {
	addr := net.IPNet(*a)
	return addr.String()
}

func (a *IPNetAddress) GetValue() interface{} {
	return net.IPNet(*a)
}

func NewIPNetAddress(addr net.IPNet) *IPNetAddress {
	ia := IPNetAddress(addr)
	return &ia
}

// OFPortAddress is the Openflow port of an interface.
type OFPortAddress int32

func (a *OFPortAddress) GetMatchKey(addrType types.AddressType) *types.MatchKey {
	switch addrType {
	case types.SrcAddress:
		// in_port is used in egress rule to match packets sent from local Pod. Service traffic is not covered by this
		// match, and source IP will be matched instead.
		return MatchSrcOFPort
	case types.DstAddress:
		return MatchDstOFPort
	default:
		klog.Errorf("Unknown AddressType %d in OFPortAddress", addrType)
		return Unsupported
	}
}

func (a *OFPortAddress) GetMatchValue() string {
	return fmt.Sprintf("%d", int32(*a))
}

func (a *OFPortAddress) GetValue() interface{} {
	return int32(*a)
}

func NewOFPortAddress(addr int32) *OFPortAddress {
	a := OFPortAddress(addr)
	return &a
}

type ServiceGroupIDAddress binding.GroupIDType

func (a *ServiceGroupIDAddress) GetMatchKey(addrType types.AddressType) *types.MatchKey {
	return MatchServiceGroupID
}

func (a *ServiceGroupIDAddress) GetMatchValue() string {
	return fmt.Sprintf("%d", uint32(*a))
}

func (a *ServiceGroupIDAddress) GetValue() interface{} {
	return uint32(*a)
}

func NewServiceGroupIDAddress(groupID binding.GroupIDType) *ServiceGroupIDAddress {
	a := ServiceGroupIDAddress(groupID)
	return &a
}

// CT IP address calculated from Pod's address.
type CTIPAddress net.IP

func (a *CTIPAddress) GetMatchKey(addrType types.AddressType) *types.MatchKey {
	ipArr := net.IP(*a)
	switch addrType {
	case types.SrcAddress:
		if ipArr.To4() != nil {
			return MatchCTSrcIP
		}
		return MatchCTSrcIPv6
	case types.DstAddress:
		if ipArr.To4() != nil {
			return MatchCTDstIP
		}
		return MatchCTDstIPv6
	default:
		klog.Errorf("Unknown AddressType %d in CTIPAddress", addrType)
		return Unsupported
	}
}

func (a *CTIPAddress) GetMatchValue() string {
	addr := net.IP(*a)
	return addr.String()
}

func (a *CTIPAddress) GetValue() interface{} {
	return net.IP(*a)
}

func NewCTIPAddress(addr net.IP) *CTIPAddress {
	cia := CTIPAddress(addr)
	return &cia
}

// CT IP block calculated from Pod's address.
type CTIPNetAddress net.IPNet

func (a *CTIPNetAddress) GetMatchKey(addrType types.AddressType) *types.MatchKey {
	ipAddr := net.IPNet(*a)
	switch addrType {
	case types.SrcAddress:
		if ipAddr.IP.To4() != nil {
			return MatchCTSrcIPNet
		}
		return MatchCTSrcIPNetv6
	case types.DstAddress:
		if ipAddr.IP.To4() != nil {
			return MatchCTDstIPNet
		}
		return MatchCTDstIPNetv6
	default:
		klog.Errorf("Unknown AddressType %d in CTIPNetAddress", addrType)
		return Unsupported
	}
}

func (a *CTIPNetAddress) GetMatchValue() string {
	addr := net.IPNet(*a)
	return addr.String()
}

func (a *CTIPNetAddress) GetValue() interface{} {
	return net.IPNet(*a)
}

func NewCTIPNetAddress(addr net.IPNet) *CTIPNetAddress {
	ia := CTIPNetAddress(addr)
	return &ia
}

type LabelIDAddress uint32

func (a *LabelIDAddress) GetMatchKey(addrType types.AddressType) *types.MatchKey {
	return MatchLabelID
}

func (a *LabelIDAddress) GetMatchValue() string {
	return fmt.Sprintf("%d", uint32(*a))
}

func (a *LabelIDAddress) GetValue() interface{} {
	return uint32(*a)
}

func NewLabelIDAddress(labelID uint32) *LabelIDAddress {
	a := LabelIDAddress(labelID)
	return &a
}

// ConjunctionNotFound is an error response when the specified policyRuleConjunction is not found from the local cache.
type ConjunctionNotFound uint32

func (e *ConjunctionNotFound) Error() string {
	return fmt.Sprintf("policyRuleConjunction with ID %d not found", uint32(*e))
}

func newConjunctionNotFound(conjunctionID uint32) *ConjunctionNotFound {
	err := ConjunctionNotFound(conjunctionID)
	return &err
}

// conjunctiveMatch generates match conditions for conjunctive match flow entry, including source or destination
// IP address, ofport number of OVS interface, or Service port. When conjunctiveMatch is used to match IP
// address or ofport number, matchProtocol is "ip". When conjunctiveMatch is used to match Service
// port, matchProtocol is Service protocol. If Service protocol is not set, "tcp" is used by default.
type conjunctiveMatch struct {
	tableID    uint8
	priority   *uint16
	matchPairs []matchPair
}

type matchPair struct {
	matchKey   *types.MatchKey
	matchValue interface{}
}

func (m *matchPair) KeyString() string {
	matchType := m.matchKey
	var valueStr string
	switch v := m.matchValue.(type) {
	case net.IP:
		// Use the unique format "x.x.x.x/xx" for IP address and IP net, to avoid generating two different global map
		// keys for IP and IP/mask. Use MatchDstIPNet/MatchSrcIPNet as match type to generate global cache key for both IP
		// and IPNet. This is because OVS treats IP and IP/$maskLen as the same condition (maskLen=32 for an IPv4 address,
		// and maskLen=128 for an IPv6 address). If Antrea has two different conjunctive match flow contexts, only one
		// flow entry is installed on OVS, and the conjunctive actions in the first context wil be overwritten by those
		// in the second one.
		var maskLen int
		if v.To4() != nil {
			maskLen = net.IPv4len * 8
		} else {
			maskLen = net.IPv6len * 8
		}
		valueStr = fmt.Sprintf("%s/%d", v.String(), maskLen)
		switch m.matchKey {
		case MatchDstIP:
			matchType = MatchDstIPNet
		case MatchDstIPv6:
			matchType = MatchDstIPNetv6
		case MatchSrcIP:
			matchType = MatchSrcIPNet
		case MatchSrcIPv6:
			matchType = MatchSrcIPNetv6
		}
	case net.IPNet:
		valueStr = v.String()
	case types.BitRange:
		bitRange := m.matchValue.(types.BitRange)
		if bitRange.Mask != nil {
			valueStr = fmt.Sprintf("%d/%d", bitRange.Value, *bitRange.Mask)
		} else {
			// To normalize the key, set full mask while a single port is provided.
			valueStr = fmt.Sprintf("%d/65535", bitRange.Value)
		}
	case *int32:
		// This case includes the matchValue is ICMPType or ICMPCode.
		if v != nil {
			valueStr = fmt.Sprintf("%d", *v)
		} else {
			valueStr = fmt.Sprintf("%v", m.matchValue)
		}
	default:
		// The default cases include the matchValue is an ofport Number.
		valueStr = fmt.Sprintf("%s", m.matchValue)
	}
	return fmt.Sprintf("%v=%s", matchType, valueStr)
}

func (m *conjunctiveMatch) generateGlobalMapKey() string {
	var priorityStr string
	var matchPairStrList []string
	for _, eachMatchPair := range m.matchPairs {
		matchPairStrList = append(matchPairStrList, eachMatchPair.KeyString())
	}
	if m.priority == nil {
		priorityStr = strconv.Itoa(int(priorityNormal))
	} else {
		priorityStr = strconv.Itoa(int(*m.priority))
	}
	return fmt.Sprintf("table:%d,priority:%s,matchPair:%s", m.tableID, priorityStr, strings.Join(matchPairStrList, ","))
}

// changeType is generally used to describe the change type of a conjMatchFlowContext. It is also used in "flowChange"
// to describe the expected OpenFlow operation which needs to be applied on the OVS bridge, and used in "actionChange"
// to describe the policyRuleConjunction is expected to be added to or removed from conjMatchFlowContext's actions.
// The value of changeType could be creation, modification, and deletion.
type changeType int

const (
	insertion changeType = iota
	modification
	deletion
)

// flowChange stores the expected OpenFlow entry and flow operation type which need to be applied on the OVS bridge.
// The "flow" in flowChange should be nil if there is no change on the OpenFlow entry. A possible case is that a
// DENY-ALL rule is required by a policyRuleConjunction, the flowChange will update the in-memory cache, but will not
// change on OVS.
type flowChange struct {
	flow       *openflow15.FlowMod
	changeType changeType
}

// actionChange stores the changed action of the conjunctive match flow, and the change type.
// The "action" in actionChange is not nil.
type actionChange struct {
	action     *conjunctiveAction
	changeType changeType
}

// conjunctiveAction generates the policyRuleConjunction action in Openflow entry. The flow action is like
// policyRuleConjunction(conjID,clauseID/nClause) when it has been realized on the switch.
type conjunctiveAction struct {
	conjID   uint32
	clauseID uint8
	nClause  uint8
}

// conjMatchFlowContext generates conjunctive match flow entries for conjunctions share the same match conditions.
// One conjMatchFlowContext is responsible for one specific conjunctive match flow entry. As the match condition
// of the flow entry can be shared by different conjunctions, the realized Openflow entry might have multiple
// conjunctive actions. If the dropTable is not nil, conjMatchFlowContext also installs a drop flow in the dropTable.
type conjMatchFlowContext struct {
	// conjunctiveMatch describes the match condition of conjunctive match flow entry.
	*conjunctiveMatch
	// actions is a map from policyRuleConjunction ID to conjunctiveAction. It records all the conjunctive actions in
	// the conjunctive match flow. When the number of actions is reduced to 0, the conjMatchFlowContext.flow is
	// uninstalled from the switch.
	actions map[uint32]*conjunctiveAction
	// denyAllRules is a set to cache the "DENY-ALL" rules that is applied to the matching address in this context.
	denyAllRules         map[uint32]bool
	featureNetworkPolicy *featureNetworkPolicy
	// flow is the conjunctive match flow built from this context. flow needs to be updated if actions are changed.
	flow *openflow15.FlowMod
	// dropflow is the default drop flow built from this context to drop packets in the AppliedToGroup but not pass the
	// NetworkPolicy rule. dropFlow is installed on the switch as long as either actions or denyAllRules is not
	// empty, and uninstalled when both two are empty. When the dropFlow is uninstalled from the switch, the
	// conjMatchFlowContext is removed from the cache.
	dropFlow *openflow15.FlowMod
	// dropFlowEnableLogging describes the logging requirement of the dropFlow.
	dropFlowEnableLogging bool
}

// createOrUpdateConjunctiveMatchFlow creates or updates the conjunctive match flow with the latest actions. It returns
// the flowChange including the changed OpenFlow entry and the expected operation which need to be applied on the OVS bridge.
func (ctx *conjMatchFlowContext) createOrUpdateConjunctiveMatchFlow(actions []*conjunctiveAction) *flowChange {
	// Check if flow is already installed. If not, create a new flow.
	if ctx.flow == nil {
		// Check the number of valid conjunctiveActions, and return nil immediately if it is 0. It happens when the match
		// condition is used only for matching AppliedToGroup, but no From or To is defined in the NetworkPolicy rule.
		if len(actions) == 0 {
			return nil
		}

		// Create the conjunctive match flow entry. The actions here should not be empty for either add or update case.
		// The expected operation for a new Openflow entry should be "insertion".
		flow := ctx.featureNetworkPolicy.conjunctiveMatchFlow(ctx.tableID, ctx.matchPairs, ctx.priority, actions)
		msg := getFlowModMessage(flow, binding.AddMessage)
		return &flowChange{
			flow:       msg,
			changeType: insertion,
		}
	}

	// Modify the existing Openflow entry and reset the actions.
	flow := ctx.featureNetworkPolicy.conjunctiveMatchFlow(ctx.tableID, ctx.matchPairs, ctx.priority, actions)
	msg := getFlowModMessage(flow, binding.AddMessage)
	// The expected operation for an existing Openflow entry should be "modification".
	return &flowChange{
		flow:       msg,
		changeType: modification,
	}
}

// deleteAction deletes the specified policyRuleConjunction from conjunctiveMatchFlow's actions, and then returns the
// flowChange.
func (ctx *conjMatchFlowContext) deleteAction(conjID uint32) *flowChange {
	// If the specified conjunctive action is the last one, delete the conjunctive match flow entry from the OVS bridge.
	// No need to check if the conjunction ID of the only conjunctive action is the specified ID or not, as it
	// has been checked in the caller.
	if len(ctx.actions) == 1 && ctx.flow != nil {
		return &flowChange{
			flow:       ctx.flow,
			changeType: deletion,
		}
	}
	// Modify the Openflow entry and reset the other conjunctive actions.
	var actions []*conjunctiveAction
	for _, act := range ctx.actions {
		if act.conjID != conjID {
			actions = append(actions, act)
		}
	}
	return ctx.createOrUpdateConjunctiveMatchFlow(actions)
}

// addAction adds the specified policyRuleConjunction into conjunctiveMatchFlow's actions, and then returns the flowChange.
func (ctx *conjMatchFlowContext) addAction(action *conjunctiveAction) *flowChange {
	// Check if the conjunction exists in conjMatchFlowContext actions or not. If yes, return nil immediately.
	_, found := ctx.actions[action.conjID]
	if found {
		return nil
	}

	// Append current conjunctive action to the existing actions, and then calculate the conjunctive match flow changes.
	actions := []*conjunctiveAction{action}
	for _, act := range ctx.actions {
		actions = append(actions, act)
	}
	return ctx.createOrUpdateConjunctiveMatchFlow(actions)
}

func (ctx *conjMatchFlowContext) addDenyAllRule(ruleID uint32) {
	if ctx.denyAllRules == nil {
		ctx.denyAllRules = make(map[uint32]bool)
	}
	ctx.denyAllRules[ruleID] = true
}

func (ctx *conjMatchFlowContext) delDenyAllRule(ruleID uint32) {
	// Delete the DENY-ALL rule if it is in context denyAllRules.
	_, found := ctx.denyAllRules[ruleID]
	if found {
		delete(ctx.denyAllRules, ruleID)
	}
}

// conjMatchFlowContextChange describes the changes of a conjMatchFlowContext. It is generated when a policyRuleConjunction
// is added, deleted, or the addresses in an existing policyRuleConjunction are changed. The changes are calculated first,
// and then applied on the OVS bridge using a single Bundle, and lastly the local cache is updated. The local cahce
// is updated only if conjMatchFlowContextChange is applied on the OVS bridge successfully.
type conjMatchFlowContextChange struct {
	// context is the changed conjMatchFlowContext, which needs to be updated after the OpenFlow entries are applied to
	// the OVS bridge. context is not nil.
	context *conjMatchFlowContext
	// ctxChangeType is the changed type of the conjMatchFlowContext. The possible values are "creation", "modification"
	// and "deletion". Add the context into the globalConjMatchFlowCache if the ctxChangeType is "insertion", and remove
	// from the globalConjMatchFlowCache if it is "deletion".
	ctxChangeType changeType
	// matchFlow is the changed conjunctive match flow which needs to be realized on the OVS bridge. It is used to update
	// conjMatchFlowContext.flow. matchFlow is set if the conjunctive match flow needs to be updated on the OVS bridge, or
	// a DENY-ALL rule change is required by the policyRuleConjunction. matchFlow is nil if the policyRuleConjunction
	// is already added/removed in the conjMatchFlowContext's actions or denyAllRules.
	matchFlow *flowChange
	// dropFlow is the changed drop flow which needs to be realized on the OVS bridge. It is used to update
	// conjMatchFlowContext.dropFlow. dropFlow is set when the default drop flow needs to be added or removed on the OVS
	// bridge, and it is nil in other cases.
	dropFlow *flowChange
	// clause is the policyRuleConjunction's clause having current conjMatchFlowContextChange. It is used to update the
	// mapping relations between the policyRuleConjunction and the conjMatchFlowContext. Update the clause.matches after
	// the conjMatchFlowContextChange is realized on the OVS bridge. clause is not nil.
	clause *clause
	// actChange is the changed conjunctive action. It is used to update the conjMatchFlowContext's actions. actChange
	// is not nil.
	actChange *actionChange
}

// updateContextStatus changes conjMatchFlowContext's status, including,
//  1. reset flow and dropFlow after the flow changes have been applied to the OVS bridge,
//  2. modify the actions with the changed action,
//  3. update the mapping of denyAllRules and corresponding policyRuleConjunction,
//  4. add the new conjMatchFlowContext into the globalConjMatchFlowCache, or remove the deleted conjMatchFlowContext
//     from the globalConjMatchFlowCache.
func (c *conjMatchFlowContextChange) updateContextStatus() {
	matcherKey := c.context.generateGlobalMapKey()
	// Update clause.matches with the conjMatchFlowContext, and update conjMatchFlowContext.actions with the changed
	// conjunctive action.
	changedAction := c.actChange.action
	switch c.actChange.changeType {
	case insertion:
		c.clause.matches[matcherKey] = c.context
		if changedAction != nil {
			c.context.actions[changedAction.conjID] = changedAction
		}
	case deletion:
		delete(c.clause.matches, matcherKey)
		if changedAction != nil {
			delete(c.context.actions, changedAction.conjID)
		}
	}
	// Update the match flow in the conjMatchFlowContext. There are two kinds of possible changes on the match flow:
	// 1) A conjunctive match flow change required by the policyRuleConjunction.
	// 2) A DENY-ALL rule required by the policyRuleConjunction.
	// For 1), conjMatchFlowContext.Flow should be updated with the conjMatchFlowContextChange.matchFlow.flow.
	// For 2), append or delete the conjunction ID from the conjMatchFlowContext's denyAllRules.
	if c.matchFlow != nil {
		switch c.matchFlow.changeType {
		case insertion:
			fallthrough
		case modification:
			if c.matchFlow.flow != nil {
				c.context.flow = c.matchFlow.flow
			} else {
				switch c.actChange.changeType {
				case insertion:
					c.context.addDenyAllRule(c.clause.action.conjID)
				case deletion:
					c.context.delDenyAllRule(c.clause.action.conjID)
				}
			}
		case deletion:
			if c.matchFlow.flow != nil {
				c.context.flow = nil
			} else {
				c.context.delDenyAllRule(c.clause.action.conjID)
			}
		}
	}
	// Update conjMatchFlowContext.dropFlow.
	if c.dropFlow != nil {
		switch c.dropFlow.changeType {
		case insertion:
			c.context.dropFlow = c.dropFlow.flow
		case deletion:
			c.context.dropFlow = nil
		}
	}

	// Update globalConjMatchFlowCache. Add the conjMatchFlowContext into the globalConjMatchFlowCache if the ctxChangeType
	// is "insertion", or delete from the globalConjMatchFlowCache if the ctxChangeType is "deletion".
	switch c.ctxChangeType {
	case insertion:
		c.context.featureNetworkPolicy.globalConjMatchFlowCache[matcherKey] = c.context
	case deletion:
		delete(c.context.featureNetworkPolicy.globalConjMatchFlowCache, matcherKey)
	}
}

// policyRuleConjunction is responsible to build Openflow entries for Pods that are in a NetworkPolicy rule's AppliedToGroup.
// The Openflow entries include conjunction action flows, conjunctive match flows, and default drop flows in the dropTable.
// NetworkPolicyController will make sure only one goroutine operates on a policyRuleConjunction.
//  1. Conjunction action flows use policyRuleConjunction ID as match condition. policyRuleConjunction ID is the single
//     match condition for conjunction action flows to allow packets. If the NetworkPolicy rule has also configured excepts
//     in From or To, Openflow entries are installed only for diff IPBlocks between From/To and Excepts. These are added as
//     conjunctive match flows as described below.
//  2. Conjunctive match flows adds conjunctive actions in Openflow entry, and they are grouped by clauses. The match
//     condition in one clause is one of these three types: from address(for fromClause), or to address(for toClause), or
//     service ports(for serviceClause) configured in the NetworkPolicy rule. Each conjunctive match flow entry is
//     maintained by one specific conjMatchFlowContext which is stored in globalConjMatchFlowCache, and shared by clauses
//     if they have the same match conditions. clause adds or deletes conjunctive action to conjMatchFlowContext actions.
//     A clause is hit if the packet matches any conjunctive match flow that are grouped by this clause. Conjunction
//     action flow is hit only if all clauses in the policyRuleConjunction are hit.
//  3. Default drop flows are also maintained by conjMatchFlowContext. It is used to drop packets sent from or to the
//     AppliedToGroup but not pass the Network Policy rule.
type policyRuleConjunction struct {
	id            uint32
	fromClause    *clause
	toClause      *clause
	serviceClause *clause
	actionFlows   []*openflow15.FlowMod
	metricFlows   []*openflow15.FlowMod
	// NetworkPolicy reference information for debugging usage, its value can be nil
	// for conjunctions that are not built for a specific NetworkPolicy, e.g. DNS packetin Conjunction.
	npRef        *v1beta2.NetworkPolicyReference
	ruleName     string
	ruleTableID  uint8
	ruleLogLabel string
}

// clause groups conjunctive match flows. Matches in a clause represent source addresses(for fromClause), or destination
// addresses(for toClause) or service ports(for serviceClause) in a NetworkPolicy rule. When the new address or service
// port is added into the clause, it adds a new conjMatchFlowContext into globalConjMatchFlowCache (or finds the
// existing one from globalConjMatchFlowCache), and then update the key of the conjunctiveMatch into its own matches.
// When address is deleted from the clause, it deletes the conjunctive action from the conjMatchFlowContext,
// and then deletes the key of conjunctiveMatch from its own matches.
type clause struct {
	action *conjunctiveAction
	// matches is a map from the unique string generated from the conjunctiveMatch to conjMatchFlowContext. It is used
	// to cache conjunctive match conditions in the same clause.
	matches map[string]*conjMatchFlowContext
	// ruleTable is where to install conjunctive match flows.
	ruleTable binding.Table
	// dropTable is where to install Openflow entries to drop the packet sent to or from the AppliedToGroup but does not
	// satisfy any conjunctive match conditions. It should be nil, if the clause is used for matching service port.
	dropTable binding.Table
}

func (c *clause) addConjunctiveMatchFlow(featureNetworkPolicy *featureNetworkPolicy, match *conjunctiveMatch, enableLogging, isMCNPRule bool) *conjMatchFlowContextChange {
	matcherKey := match.generateGlobalMapKey()
	_, found := c.matches[matcherKey]
	if found {
		klog.V(2).Infof("Conjunctive match flow with matcher %s is already added in rule: %d", matcherKey, c.action.conjID)
		return nil
	}

	var context *conjMatchFlowContext
	ctxType := modification
	var dropFlow *flowChange
	// Get conjMatchFlowContext from globalConjMatchFlowCache. If it doesn't exist, create a new one and add into the cache.
	context, found = featureNetworkPolicy.globalConjMatchFlowCache[matcherKey]
	if !found {
		context = &conjMatchFlowContext{
			conjunctiveMatch:      match,
			actions:               make(map[uint32]*conjunctiveAction),
			featureNetworkPolicy:  featureNetworkPolicy,
			dropFlowEnableLogging: enableLogging,
		}
		ctxType = insertion

		// Generate the default drop flow if dropTable is not nil and the default drop flow is not set yet.
		if c.dropTable != nil && context.dropFlow == nil {
			if isMCNPRule {
				dropFlow = &flowChange{
					flow:       getFlowModMessage(context.featureNetworkPolicy.multiClusterNetworkPolicySecurityDropFlow(c.dropTable, match.matchPairs), binding.AddMessage),
					changeType: insertion,
				}
			} else {
				dropFlow = &flowChange{
					flow:       getFlowModMessage(context.featureNetworkPolicy.defaultDropFlow(c.dropTable, match.matchPairs, enableLogging), binding.AddMessage),
					changeType: insertion,
				}
			}
		}
	} else if context.dropFlowEnableLogging != enableLogging {
		// Logging requirement of the rule has changed, modify default drop flow accordingly.
		context.dropFlowEnableLogging = enableLogging
		if c.dropTable != nil && context.dropFlow != nil {
			dropFlow = &flowChange{
				flow:       getFlowModMessage(context.featureNetworkPolicy.defaultDropFlow(c.dropTable, match.matchPairs, enableLogging), binding.AddMessage),
				changeType: modification,
			}
		}
	}

	// Calculate the change on the conjMatchFlowContext.
	ctxChanges := &conjMatchFlowContextChange{
		context:       context,
		ctxChangeType: ctxType,
		clause:        c,
		actChange: &actionChange{
			changeType: insertion,
		},
		dropFlow: dropFlow,
	}
	if c.action.nClause > 1 {
		// Append the conjunction to conjunctiveFlowContext's actions, and add the changed flow into the conjMatchFlowContextChange.
		flowChange := context.addAction(c.action)
		if flowChange != nil {
			ctxChanges.matchFlow = flowChange
			ctxChanges.actChange.action = c.action
		}
	} else {
		// Set the flowChange type as "insertion" but do not set flowChange.Flow. In this case, the policyRuleConjunction should
		// be added into conjunctiveFlowContext's denyAllRules.
		ctxChanges.matchFlow = &flowChange{
			changeType: insertion,
		}
	}

	return ctxChanges
}

func generateAddressConjMatch(ruleTableID uint8, addr types.Address, addrType types.AddressType, priority *uint16) *conjunctiveMatch {
	matchKey := addr.GetMatchKey(addrType)
	matchValue := addr.GetValue()
	match := &conjunctiveMatch{
		tableID:    ruleTableID,
		matchPairs: []matchPair{{matchKey: matchKey, matchValue: matchValue}},
		priority:   priority,
	}
	return match
}

func generateServiceConjMatches(ruleTableID uint8, service v1beta2.Service, priority *uint16, ipProtocols []binding.Protocol) []*conjunctiveMatch {
	var matches []*conjunctiveMatch
	conjMatchesMatchPairs := getServiceMatchPairs(service, ipProtocols)
	for _, conjMatchMatchPairs := range conjMatchesMatchPairs {
		matches = append(matches,
			&conjunctiveMatch{
				tableID:    ruleTableID,
				matchPairs: conjMatchMatchPairs,
				priority:   priority,
			})
	}
	return matches
}

func getServiceMatchPairs(service v1beta2.Service, ipProtocols []binding.Protocol) [][]matchPair {
	var conjMatchesMatchPairs [][]matchPair
	ovsBitRanges := portsToBitRanges(service.Port, service.EndPort)
	var srcOVSBitRanges []types.BitRange
	if service.SrcPort != nil {
		srcPortTyped := intstr.FromInt(int(*service.SrcPort))
		srcOVSBitRanges = portsToBitRanges(&srcPortTyped, service.SrcEndPort)
	}
	addL4MatchPairs := func(matchKey, srcMatchKey *types.MatchKey) {
		for _, ovsBitRange := range ovsBitRanges {
			matchPairs := []matchPair{{matchKey: matchKey, matchValue: ovsBitRange}}
			if srcOVSBitRanges != nil {
				for _, srcRange := range srcOVSBitRanges {
					matchPairs = append(matchPairs, matchPair{matchKey: srcMatchKey, matchValue: srcRange})
					conjMatchesMatchPairs = append(conjMatchesMatchPairs, matchPairs)
				}
			} else {
				conjMatchesMatchPairs = append(conjMatchesMatchPairs, matchPairs)
			}
		}
	}
	switch *service.Protocol {
	case v1beta2.ProtocolTCP:
		for _, ipProtocol := range ipProtocols {
			if ipProtocol == binding.ProtocolIP {
				addL4MatchPairs(MatchTCPDstPort, MatchTCPSrcPort)
			} else {
				addL4MatchPairs(MatchTCPv6DstPort, MatchTCPv6SrcPort)
			}
		}
	case v1beta2.ProtocolUDP:
		for _, ipProtocol := range ipProtocols {
			if ipProtocol == binding.ProtocolIP {
				addL4MatchPairs(MatchUDPDstPort, MatchUDPSrcPort)
			} else {
				addL4MatchPairs(MatchUDPv6DstPort, MatchUDPv6SrcPort)
			}
		}
	case v1beta2.ProtocolSCTP:
		for _, ipProtocol := range ipProtocols {
			if ipProtocol == binding.ProtocolIP {
				addL4MatchPairs(MatchSCTPDstPort, MatchSCTPSrcPort)
			} else {
				addL4MatchPairs(MatchSCTPv6DstPort, MatchSCTPv6SrcPort)
			}
		}
	case v1beta2.ProtocolICMP:
		for _, ipProtocol := range ipProtocols {
			if ipProtocol == binding.ProtocolIP {
				var matchPairs []matchPair
				if service.ICMPType != nil {
					matchPairs = append(matchPairs, matchPair{matchKey: MatchICMPType, matchValue: service.ICMPType})
				}
				if service.ICMPCode != nil {
					matchPairs = append(matchPairs, matchPair{matchKey: MatchICMPCode, matchValue: service.ICMPCode})
				}
				if len(matchPairs) == 0 {
					matchPairs = append(matchPairs, matchPair{matchKey: MatchICMPType, matchValue: nil})
				}
				conjMatchesMatchPairs = append(conjMatchesMatchPairs, matchPairs)
			} else {
				var matchPairs []matchPair
				if service.ICMPType != nil {
					matchPairs = append(matchPairs, matchPair{matchKey: MatchICMPv6Type, matchValue: service.ICMPType})
				}
				if service.ICMPCode != nil {
					matchPairs = append(matchPairs, matchPair{matchKey: MatchICMPv6Code, matchValue: service.ICMPCode})
				}
				if len(matchPairs) == 0 {
					matchPairs = append(matchPairs, matchPair{matchKey: MatchICMPv6Type, matchValue: nil})
				}
				conjMatchesMatchPairs = append(conjMatchesMatchPairs, matchPairs)
			}
		}
	case v1beta2.ProtocolIGMP:
		var matchPairs []matchPair
		if service.IGMPType != nil && *service.IGMPType == crdv1beta1.IGMPQuery {
			// Since OVS only matches layer 3 IP address on the IGMP query packet, and doesn't
			// identify the multicast group address set in the IGMP protocol, the flow entry
			// processes all IGMP query packets by matching the destination IP address ( 224.0.0.1 )
			if service.GroupAddress != "" {
				matchPairs = append(matchPairs, matchPair{matchKey: MatchDstIP, matchValue: net.ParseIP(service.GroupAddress)})
			} else {
				matchPairs = append(matchPairs, matchPair{matchKey: MatchDstIP, matchValue: types.McastAllHosts})
			}
			matchPairs = append(matchPairs, matchPair{matchKey: MatchIGMPProtocol, matchValue: nil})
			conjMatchesMatchPairs = append(conjMatchesMatchPairs, matchPairs)
		}
	default:
		addL4MatchPairs(MatchTCPDstPort, MatchTCPSrcPort)
	}
	return conjMatchesMatchPairs
}

// portsToBitRanges converts ports in Service to a list of BitRange.
func portsToBitRanges(port *intstr.IntOrString, endPort *int32) []types.BitRange {
	var ovsBitRanges []types.BitRange
	// If `EndPort` is equal to `Port`, then treat it as single port case.
	if endPort != nil && *endPort > port.IntVal {
		// Add several antrea range services based on a port range.
		portRange := thirdpartynp.PortRange{Start: uint16(port.IntVal), End: uint16(*endPort)}
		bitRanges, err := portRange.BitwiseMatch()
		if err != nil {
			klog.Errorf("Error when getting BitRanges from %v: %v", portRange, err)
			return ovsBitRanges
		}
		for _, bitRange := range bitRanges {
			curBitRange := bitRange
			ovsBitRanges = append(ovsBitRanges, types.BitRange{
				Value: curBitRange.Value,
				Mask:  &curBitRange.Mask,
			})
		}
	} else if port != nil {
		// Add single antrea service based on a single port.
		ovsBitRanges = append(ovsBitRanges, types.BitRange{
			Value: uint16(port.IntVal),
		})
	} else {
		// Match all ports with the given protocol type if `Port` and `EndPort` are not
		// specified (value is 0).
		ovsBitRanges = append(ovsBitRanges, types.BitRange{
			Value: uint16(0),
		})
	}
	return ovsBitRanges
}

// addAddrFlows translates the specified addresses to conjunctiveMatchFlows, and returns the corresponding changes on the
// conjunctiveMatchFlows.
func (c *clause) addAddrFlows(featureNetworkPolicy *featureNetworkPolicy, addrType types.AddressType, addresses []types.Address, priority *uint16, enableLogging, isMCNPRule bool) []*conjMatchFlowContextChange {
	var conjMatchFlowContextChanges []*conjMatchFlowContextChange
	// Calculate Openflow changes for the added addresses.
	for _, addr := range addresses {
		match := generateAddressConjMatch(c.ruleTable.GetID(), addr, addrType, priority)
		ctxChange := c.addConjunctiveMatchFlow(featureNetworkPolicy, match, enableLogging, isMCNPRule)
		if ctxChange != nil {
			conjMatchFlowContextChanges = append(conjMatchFlowContextChanges, ctxChange)
		}
	}
	return conjMatchFlowContextChanges
}

// addServiceFlows translates the specified Antrea Service to conjunctiveMatchFlow,
// and returns corresponding conjMatchFlowContextChange.
func (c *clause) addServiceFlows(featureNetworkPolicy *featureNetworkPolicy, services []v1beta2.Service, priority *uint16, enableLogging bool) []*conjMatchFlowContextChange {
	var conjMatchFlowContextChanges []*conjMatchFlowContextChange
	for _, service := range services {
		matches := generateServiceConjMatches(c.ruleTable.GetID(), service, priority, featureNetworkPolicy.ipProtocols)
		for _, match := range matches {
			ctxChange := c.addConjunctiveMatchFlow(featureNetworkPolicy, match, enableLogging, false)
			conjMatchFlowContextChanges = append(conjMatchFlowContextChanges, ctxChange)
		}
	}
	return conjMatchFlowContextChanges
}

// deleteConjunctiveMatchFlow deletes the specific conjunctiveAction from existing flow.
func (c *clause) deleteConjunctiveMatchFlow(flowContextKey string) *conjMatchFlowContextChange {
	context, found := c.matches[flowContextKey]
	// Match is not located in clause cache. It happens if the conjMatchFlowContext is already deleted from clause local cache.
	if !found {
		return nil
	}

	ctxChange := &conjMatchFlowContextChange{
		context:       context,
		clause:        c,
		ctxChangeType: modification,
		actChange: &actionChange{
			changeType: deletion,
		},
	}
	conjID := c.action.conjID
	expectedConjunctiveActions := len(context.actions)
	expectedDenyAllRules := len(context.denyAllRules)
	if c.action.nClause > 1 {
		// Delete the conjunctive action if it is in context actions.
		action, found := context.actions[conjID]
		if found {
			ctxChange.matchFlow = context.deleteAction(conjID)
			ctxChange.actChange.action = action
			expectedConjunctiveActions--
		}
	} else {
		// Delete the DENY-ALL rule if it is in context denyAllRules.
		ctxChange.matchFlow = &flowChange{
			changeType: deletion,
		}
		expectedDenyAllRules--
	}

	// Uninstall default drop flow if the deleted conjunctiveAction is the last action or the rule is the last one in
	// the denyAllRules.
	if expectedConjunctiveActions == 0 && expectedDenyAllRules == 0 {
		if context.dropFlow != nil {
			ctxChange.dropFlow = &flowChange{
				flow:       context.dropFlow,
				changeType: deletion,
			}
		}
		// Remove the context from global cache if the match condition is not used by either DENEY-ALL or the conjunctive
		// match flow.
		ctxChange.ctxChangeType = deletion
	}

	return ctxChange
}

// deleteAddrFlows deletes conjunctiveMatchFlow relevant to the specified addresses from local cache,
// and uninstalls Openflow entry.
func (c *clause) deleteAddrFlows(addrType types.AddressType, addresses []types.Address, priority *uint16) []*conjMatchFlowContextChange {
	var ctxChanges []*conjMatchFlowContextChange
	for _, addr := range addresses {
		match := generateAddressConjMatch(c.ruleTable.GetID(), addr, addrType, priority)
		contextKey := match.generateGlobalMapKey()
		ctxChange := c.deleteConjunctiveMatchFlow(contextKey)
		if ctxChange != nil {
			ctxChanges = append(ctxChanges, ctxChange)
		}
	}
	return ctxChanges
}

// deleteAllMatches deletes all conjunctiveMatchFlow in the clause, and removes Openflow entry. deleteAllMatches
// is always invoked when NetworkPolicy rule is deleted.
func (c *clause) deleteAllMatches() []*conjMatchFlowContextChange {
	var ctxChanges []*conjMatchFlowContextChange
	for key := range c.matches {
		ctxChange := c.deleteConjunctiveMatchFlow(key)
		if ctxChange != nil {
			ctxChanges = append(ctxChanges, ctxChange)
		}
	}
	return ctxChanges
}

func (c *policyRuleConjunction) getAddressClause(addrType types.AddressType) *clause {
	switch addrType {
	case types.SrcAddress:
		return c.fromClause
	case types.DstAddress:
		return c.toClause
	default:
		klog.Errorf("no address clause use AddressType %d", addrType)
		return nil
	}
}

// calculateActionFlowChangesForRule calculates and updates the actionFlows for the conjunction corresponded to the ofPolicyRule.
func (f *featureNetworkPolicy) calculateActionFlowChangesForRule(rule *types.PolicyRule) *policyRuleConjunction {
	ruleOfID := rule.FlowID
	// Check if the policyRuleConjunction is added into cache or not. If yes, return nil.
	conj := f.getPolicyRuleConjunction(ruleOfID)
	if conj != nil {
		klog.V(2).Infof("PolicyRuleConjunction %d is already added in cache", ruleOfID)
		return nil
	}
	conj = &policyRuleConjunction{
		id:           ruleOfID,
		npRef:        rule.PolicyRef,
		ruleName:     rule.Name,
		ruleLogLabel: rule.LogLabel,
	}
	nClause, ruleTable, dropTable := conj.calculateClauses(rule)
	conj.ruleTableID = rule.TableID
	_, isEgress := f.egressTables[rule.TableID]
	isIngress := !isEgress

	// Conjunction action flows are installed only if the number of clauses in the conjunction is > 1. It should be a rule
	// to drop all packets.  If the number is 1, no conjunctive match flows or conjunction action flows are installed,
	// but the default drop flow is installed.
	if nClause > 1 {
		// Install action flows.
		var actionFlows []binding.Flow
		var metricFlows []binding.Flow
		if rule.IsAntreaNetworkPolicyRule() && *rule.Action == crdv1beta1.RuleActionDrop {
			metricFlows = append(metricFlows, f.denyRuleMetricFlow(ruleOfID, isIngress, rule.TableID))
			actionFlows = append(actionFlows, f.conjunctionActionDenyFlow(ruleOfID, ruleTable, rule.Priority, DispositionDrop, rule.EnableLogging))
		} else if rule.IsAntreaNetworkPolicyRule() && *rule.Action == crdv1beta1.RuleActionReject {
			metricFlows = append(metricFlows, f.denyRuleMetricFlow(ruleOfID, isIngress, rule.TableID))
			actionFlows = append(actionFlows, f.conjunctionActionDenyFlow(ruleOfID, ruleTable, rule.Priority, DispositionRej, rule.EnableLogging))
		} else if rule.IsAntreaNetworkPolicyRule() && *rule.Action == crdv1beta1.RuleActionPass {
			actionFlows = append(actionFlows, f.conjunctionActionPassFlow(ruleOfID, ruleTable, rule.Priority, rule.EnableLogging))
		} else {
			metricFlows = append(metricFlows, f.allowRulesMetricFlows(ruleOfID, isIngress, rule.TableID)...)
			actionFlows = append(actionFlows, f.conjunctionActionFlow(ruleOfID, ruleTable, dropTable.GetNext(), rule.Priority, rule.EnableLogging, rule.L7RuleVlanID)...)
		}
		conj.actionFlows = GetFlowModMessages(actionFlows, binding.AddMessage)
		conj.metricFlows = GetFlowModMessages(metricFlows, binding.AddMessage)
	}
	return conj
}

// calculateMatchFlowChangesForRule calculates the contextChanges for the policyRule, and updates the context status in case of batch install.
func (f *featureNetworkPolicy) calculateMatchFlowChangesForRule(conj *policyRuleConjunction, rule *types.PolicyRule) []*conjMatchFlowContextChange {
	// Calculate the conjMatchFlowContext changes. The changed Openflow entries are included in the conjMatchFlowContext change.
	ctxChanges := conj.calculateChangesForRuleCreation(f, rule)
	return ctxChanges
}

// addRuleToConjunctiveMatch adds a rule's clauses to corresponding conjunctive match contexts.
// Unlike calculateMatchFlowChangesForRule, it updates the context status directly and doesn't calculate flow changes.
// It's used in initial batch install where we first add all rules then calculates flows change based on final state.
func (f *featureNetworkPolicy) addRuleToConjunctiveMatch(conj *policyRuleConjunction, rule *types.PolicyRule) {
	isMCNPRule := containsLabelIdentityAddress(rule.From)
	if conj.fromClause != nil {
		for _, addr := range rule.From {
			match := generateAddressConjMatch(conj.fromClause.ruleTable.GetID(), addr, types.SrcAddress, rule.Priority)
			f.addActionToConjunctiveMatch(conj.fromClause, match, rule.EnableLogging, isMCNPRule)
		}
	}
	if conj.toClause != nil {
		for _, addr := range rule.To {
			match := generateAddressConjMatch(conj.toClause.ruleTable.GetID(), addr, types.DstAddress, rule.Priority)
			f.addActionToConjunctiveMatch(conj.toClause, match, rule.EnableLogging, isMCNPRule)
		}
	}
	if conj.serviceClause != nil {
		for _, eachService := range rule.Service {
			matches := generateServiceConjMatches(conj.serviceClause.ruleTable.GetID(), eachService, rule.Priority, f.ipProtocols)
			for _, match := range matches {
				f.addActionToConjunctiveMatch(conj.serviceClause, match, rule.EnableLogging, isMCNPRule)
			}
		}
	}
}

// addActionToConjunctiveMatch adds a clause to corresponding conjunctive match context.
// It updates the context status directly and doesn't calculate the match flow, which is supposed to be calculated after
// all actions are added. It's used in initial batch install only.
func (f *featureNetworkPolicy) addActionToConjunctiveMatch(clause *clause, match *conjunctiveMatch, enableLogging, isMCNPRule bool) {
	matcherKey := match.generateGlobalMapKey()
	_, found := clause.matches[matcherKey]
	if found {
		klog.V(2).InfoS("Conjunctive match flow is already added for rule", "matcherKey", matcherKey, "ruleID", clause.action.conjID)
		return
	}

	var context *conjMatchFlowContext
	// Get conjMatchFlowContext from globalConjMatchFlowCache. If it doesn't exist, create a new one and add into the cache.
	context, found = f.globalConjMatchFlowCache[matcherKey]
	if !found {
		context = &conjMatchFlowContext{
			conjunctiveMatch:      match,
			actions:               make(map[uint32]*conjunctiveAction),
			featureNetworkPolicy:  f,
			dropFlowEnableLogging: enableLogging,
		}
		// Generate the default drop flow if dropTable is not nil.
		if clause.dropTable != nil {
			if isMCNPRule {
				context.dropFlow = getFlowModMessage(context.featureNetworkPolicy.multiClusterNetworkPolicySecurityDropFlow(clause.dropTable, match.matchPairs), binding.AddMessage)
			} else {
				context.dropFlow = getFlowModMessage(context.featureNetworkPolicy.defaultDropFlow(clause.dropTable, match.matchPairs, enableLogging), binding.AddMessage)
			}
		}
		f.globalConjMatchFlowCache[matcherKey] = context
	}
	clause.matches[matcherKey] = context

	if clause.action.nClause > 1 {
		// Add the conjunction to the conjunctiveFlowContext's actions.
		context.actions[clause.action.conjID] = clause.action
	} else {
		// Add the conjunction ID to the conjunctiveFlowContext's denyAllRules.
		context.addDenyAllRule(clause.action.conjID)
	}
}

// applyConjunctiveMatchFlows installs OpenFlow entries on the OVS bridge, and then updates the conjMatchFlowContext.
func (f *featureNetworkPolicy) applyConjunctiveMatchFlows(flowChanges []*conjMatchFlowContextChange) error {
	// Send the OpenFlow entries to the OVS bridge.
	if err := f.sendConjunctiveFlows(flowChanges); err != nil {
		return err
	}
	// Update conjunctiveMatchContext.
	for _, ctxChange := range flowChanges {
		ctxChange.updateContextStatus()
	}
	return nil
}

// sendConjunctiveFlows sends all the changed OpenFlow entries to the OVS bridge in a single Bundle.
func (f *featureNetworkPolicy) sendConjunctiveFlows(changes []*conjMatchFlowContextChange) error {
	var addFlows, modifyFlows, deleteFlows []*openflow15.FlowMod
	var flowChanges []*flowChange
	for _, change := range changes {
		if change.matchFlow != nil && change.matchFlow.flow != nil {
			flowChanges = append(flowChanges, change.matchFlow)
		}
		if change.dropFlow != nil {
			flowChanges = append(flowChanges, change.dropFlow)
		}
	}
	// Retrieve the OpenFlow entries from the flowChanges.
	for _, fc := range flowChanges {
		flowInfo := fc.flow
		switch fc.changeType {
		case insertion:
			addFlows = append(addFlows, flowInfo)
		case modification:
			modifyFlows = append(modifyFlows, flowInfo)
		case deletion:
			deleteFlows = append(deleteFlows, flowInfo)
		}
	}
	return f.bridge.AddFlowsInBundle(addFlows, modifyFlows, deleteFlows)
}

// ActionFlowPriorities returns the OF priorities of the actionFlows in the policyRuleConjunction
func (c *policyRuleConjunction) ActionFlowPriorities() []string {
	priorities := make([]string, 0, len(c.actionFlows))
	for _, flow := range c.actionFlows {
		priorityStr := strconv.Itoa(int(flow.Priority))
		priorities = append(priorities, priorityStr)
	}
	return priorities
}

func (c *policyRuleConjunction) newClause(clauseID uint8, nClause uint8, ruleTable, dropTable binding.Table) *clause {
	return &clause{
		ruleTable: ruleTable,
		dropTable: dropTable,
		matches:   make(map[string]*conjMatchFlowContext, 0),
		action: &conjunctiveAction{
			conjID:   c.id,
			clauseID: clauseID,
			nClause:  nClause,
		},
	}
}

// calculateClauses configures the policyRuleConjunction's clauses according to the PolicyRule. The Openflow entries are
// not installed on the OVS bridge when calculating the clauses.
func (c *policyRuleConjunction) calculateClauses(rule *types.PolicyRule) (uint8, binding.Table, binding.Table) {
	var dropTable binding.Table
	var isEgressRule = false
	switch rule.Direction {
	case v1beta2.DirectionOut:
		dropTable = EgressDefaultTable.ofTable
		isEgressRule = true
	default:
		dropTable = IngressDefaultTable.ofTable
	}
	ruleTable := getTableByID(rule.TableID)

	var fromID, toID, serviceID, nClause uint8
	// Calculate clause IDs and the total number of clauses.
	if rule.From != nil {
		nClause++
		fromID = nClause
	}
	if rule.To != nil {
		nClause++
		toID = nClause
	}
	if rule.Service != nil {
		nClause++
		serviceID = nClause
	}

	var defaultTable binding.Table
	if rule.From != nil {
		// deny rule does not need to be created for ClusterNetworkPolicies
		if !isEgressRule || rule.IsAntreaNetworkPolicyRule() {
			defaultTable = nil
		} else {
			defaultTable = dropTable
		}
		c.fromClause = c.newClause(fromID, nClause, ruleTable, defaultTable)
	}
	if rule.To != nil {
		if isEgressRule || (rule.IsAntreaNetworkPolicyRule() && !containsLabelIdentityAddress(rule.From)) {
			defaultTable = nil
		} else {
			defaultTable = dropTable
		}
		c.toClause = c.newClause(toID, nClause, ruleTable, defaultTable)
	}
	if rule.Service != nil {
		c.serviceClause = c.newClause(serviceID, nClause, ruleTable, nil)
	}
	return nClause, ruleTable, dropTable
}

// calculateChangesForRuleCreation returns the conjMatchFlowContextChanges of the new policyRuleConjunction. It
// will calculate the expected conjMatchFlowContext status, and the changed Openflow entries.
func (c *policyRuleConjunction) calculateChangesForRuleCreation(featureNetworkPolicy *featureNetworkPolicy, rule *types.PolicyRule) []*conjMatchFlowContextChange {
	isMCNPRule := containsLabelIdentityAddress(rule.From)
	var ctxChanges []*conjMatchFlowContextChange
	if c.fromClause != nil {
		ctxChanges = append(ctxChanges, c.fromClause.addAddrFlows(featureNetworkPolicy, types.SrcAddress, rule.From, rule.Priority, rule.EnableLogging, isMCNPRule)...)
	}
	if c.toClause != nil {
		ctxChanges = append(ctxChanges, c.toClause.addAddrFlows(featureNetworkPolicy, types.DstAddress, rule.To, rule.Priority, rule.EnableLogging, isMCNPRule)...)
	}
	if c.serviceClause != nil {
		ctxChanges = append(ctxChanges, c.serviceClause.addServiceFlows(featureNetworkPolicy, rule.Service, rule.Priority, rule.EnableLogging)...)
	}
	return ctxChanges
}

func containsLabelIdentityAddress(addresses []types.Address) bool {
	contains := false
	for _, addr := range addresses {
		if _, ok := addr.(*LabelIDAddress); ok {
			contains = true
			break
		}
	}
	return contains
}

// calculateChangesForRuleDeletion returns the conjMatchFlowContextChanges of the deleted policyRuleConjunction. It
// will calculate the expected conjMatchFlowContext status, and the changed Openflow entries.
func (c *policyRuleConjunction) calculateChangesForRuleDeletion() []*conjMatchFlowContextChange {
	var ctxChanges []*conjMatchFlowContextChange
	if c.fromClause != nil {
		ctxChanges = append(ctxChanges, c.fromClause.deleteAllMatches()...)
	}
	if c.toClause != nil {
		ctxChanges = append(ctxChanges, c.toClause.deleteAllMatches()...)
	}
	if c.serviceClause != nil {
		ctxChanges = append(ctxChanges, c.serviceClause.deleteAllMatches()...)
	}
	return ctxChanges
}

// getAllFlowKeys returns the matching strings of actions flows of
// policyRuleConjunction, as well as matching flows of all its clauses.
func (c *policyRuleConjunction) getAllFlowKeys() []string {
	flowKeys := []string{}
	dropFlowKeys := []string{}
	for _, flow := range c.actionFlows {
		flowKeys = append(flowKeys, getFlowKey(flow))
	}

	addClauseFlowKeys := func(clause *clause) {
		if clause == nil {
			return
		}
		for _, ctx := range clause.matches {
			if ctx.flow != nil {
				flowKeys = append(flowKeys, getFlowKey(ctx.flow))
			}
			if ctx.dropFlow != nil {
				dropFlowKeys = append(dropFlowKeys, getFlowKey(ctx.dropFlow))
			}
		}
	}
	addClauseFlowKeys(c.fromClause)
	addClauseFlowKeys(c.toClause)
	addClauseFlowKeys(c.serviceClause)

	// Add flows in the order of action flows, conjunctive match flows, drop flows.
	return append(flowKeys, dropFlowKeys...)
}

func (f *featureNetworkPolicy) getPolicyRuleConjunction(ruleID uint32) *policyRuleConjunction {
	conj, found, _ := f.policyCache.GetByKey(fmt.Sprint(ruleID))
	if !found {
		return nil
	}
	return conj.(*policyRuleConjunction)
}

// getStalePriorities returns the ofPriorities that will be stale on the rule table where the
// policyRuleConjunction is installed, after the deletion of that policyRuleConjunction.
func (f *featureNetworkPolicy) getStalePriorities(conj *policyRuleConjunction) (staleOFPriorities []string) {
	var ofPrioritiesPotentiallyStale []string
	if conj.ruleTableID != IngressRuleTable.ofTable.GetID() && conj.ruleTableID != EgressRuleTable.ofTable.GetID() {
		ofPrioritiesPotentiallyStale = conj.ActionFlowPriorities()
	}
	klog.V(4).Infof("Potential stale ofpriority %v found", ofPrioritiesPotentiallyStale)
	for _, p := range ofPrioritiesPotentiallyStale {
		// Filter out all the policyRuleConjuctions created at the ofPriority across all CNP tables.
		conjs, _ := f.policyCache.ByIndex(priorityIndex, p)
		priorityStale := true
		for i := 0; i < len(conjs); i++ {
			conjFiltered := conjs[i].(*policyRuleConjunction)
			if conj.id != conjFiltered.id && conj.ruleTableID == conjFiltered.ruleTableID {
				// There are other policyRuleConjuctions in the same table created with this
				// ofPriority. The ofPriority is thus not stale and cannot be released.
				priorityStale = false
				break
			}
		}
		if priorityStale {
			klog.V(2).Infof("ofPriority %v is now stale", p)
			staleOFPriorities = append(staleOFPriorities, p)
		}
	}
	return staleOFPriorities
}

func (f *featureNetworkPolicy) replayFlows() []*openflow15.FlowMod {
	var flows []*openflow15.FlowMod
	addActionFlows := func(conj *policyRuleConjunction) {
		for _, flow := range conj.actionFlows {
			flows = append(flows, flow)
		}
	}
	addMetricFlows := func(conj *policyRuleConjunction) {
		for _, flow := range conj.metricFlows {
			flows = append(flows, flow)
		}
	}

	for _, conj := range f.policyCache.List() {
		addActionFlows(conj.(*policyRuleConjunction))
		addMetricFlows(conj.(*policyRuleConjunction))
	}

	addMatchFlows := func(ctx *conjMatchFlowContext) {
		if ctx.dropFlow != nil {
			flows = append(flows, ctx.dropFlow)
		}
		if ctx.flow != nil {
			flows = append(flows, ctx.flow)
		}
	}

	for _, ctx := range f.globalConjMatchFlowCache {
		addMatchFlows(ctx)
	}
	return flows
}

// flowUpdates stores updates to the actionFlows and matchFlows in a policyRuleConjunction.
type flowUpdates struct {
	newActionFlows []*openflow15.FlowMod
	newPriority    uint16
}

// getMatchFlowUpdates calculates the update for conjuctiveMatchFlows in a policyRuleConjunction to be
// installed on a new priority.
func getMatchFlowUpdates(conj *policyRuleConjunction, newPriority uint16) (add, del []*openflow15.FlowMod) {
	allClause := []*clause{conj.fromClause, conj.toClause, conj.serviceClause}
	for _, c := range allClause {
		if c == nil {
			continue
		}
		for _, ctx := range c.matches {
			f := ctx.flow
			updatedFlow := copyFlowWithNewPriority(f, newPriority)
			add = append(add, updatedFlow)
			del = append(del, f)
		}
	}
	return add, del
}

// processFlowUpdates identifies the update cases in flow adds and deletes.
// For conjunctiveMatchFlow updates, the following scenario is possible:
//
// A flow {priority=100,ip,reg1=0x1f action=conjunction(1,1/3)} need to be re-assigned priority=99.
// In this case, an addFlow of <priority=99,ip,reg1=0x1f> and delFlow <priority=100,ip,reg1=0x1f> will be issued.
// At the same time, another flow {priority=99,ip,reg1=0x1f action=conjunction(2,1/3)} exists and now needs to
// be re-assigned priority 98. This operation will issue a delFlow <priority=99,ip,reg1=0x1f>, which
// would essentially void the add flow for conj=1.
//
// In this case, we remove the conflicting delFlow and set addFlow as a modifyFlow.
func (f *featureNetworkPolicy) processFlowUpdates(addFlows, delFlows []*openflow15.FlowMod) (add, update, del []*openflow15.FlowMod) {
	for _, a := range addFlows {
		matched := false
		for i := 0; i < len(delFlows); i++ {
			if flowMessageMatched(a, delFlows[i]) {
				matched = true
				// treat the addFlow as update
				update = append(update, a)
				// remove the delFlow from the list
				delFlows = append(delFlows[:i], delFlows[i+1:]...)
				// reset list index as delFlows[i] is removed
				i--
			}
		}
		if !matched {
			add = append(add, a)
		}
	}
	del = delFlows
	return add, update, del
}

// updateConjunctionActionFlows constructs a new policyRuleConjunction with actionFlows updated to be
// stored in the policyCache.
func (f *featureNetworkPolicy) updateConjunctionActionFlows(conj *policyRuleConjunction, updates flowUpdates) *policyRuleConjunction {
	newActionFlows := make([]*openflow15.FlowMod, len(conj.actionFlows))
	copy(newActionFlows, updates.newActionFlows)
	newConj := &policyRuleConjunction{
		id:            conj.id,
		fromClause:    conj.fromClause,
		toClause:      conj.toClause,
		serviceClause: conj.serviceClause,
		actionFlows:   newActionFlows,
		npRef:         conj.npRef,
		ruleName:      conj.ruleName,
		ruleTableID:   conj.ruleTableID,
		ruleLogLabel:  conj.ruleLogLabel,
	}
	return newConj
}

// updateConjunctionMatchFlows updates the conjuctiveMatchFlows in a policyRuleConjunction.
func (f *featureNetworkPolicy) updateConjunctionMatchFlows(conj *policyRuleConjunction, newPriority uint16) {
	allClause := []*clause{conj.fromClause, conj.toClause, conj.serviceClause}
	for _, cl := range allClause {
		if cl == nil {
			continue
		}
		for i, ctx := range cl.matches {
			delete(f.globalConjMatchFlowCache, ctx.generateGlobalMapKey())
			updatedFlow := copyFlowWithNewPriority(ctx.flow, newPriority)
			cl.matches[i].flow = updatedFlow
			cl.matches[i].priority = &newPriority
		}
		// update the globalConjMatchFlowCache so that the keys are updated
		for _, ctx := range cl.matches {
			f.globalConjMatchFlowCache[ctx.generateGlobalMapKey()] = ctx
		}
	}
}

// calculateFlowUpdates calculates the flow updates required for the priority re-assignments specified in the input map.
func (f *featureNetworkPolicy) calculateFlowUpdates(updates map[uint16]uint16, table uint8) (addFlows, delFlows []*openflow15.FlowMod,
	conjFlowUpdates map[uint32]flowUpdates) {
	conjFlowUpdates = map[uint32]flowUpdates{}
	for original, newPriority := range updates {
		originalPriorityStr := strconv.Itoa(int(original))
		conjs, _ := f.policyCache.ByIndex(priorityIndex, originalPriorityStr)
		for _, conjObj := range conjs {
			conj := conjObj.(*policyRuleConjunction)
			// Only re-assign flow priorities for flows in the table specified.
			if conj.ruleTableID != table {
				klog.V(4).Infof("Conjunction %v with the same actionFlow priority is from a different table %v", conj.id, conj.ruleTableID)
				continue
			}
			for _, actionFlow := range conj.actionFlows {
				flowPriority := actionFlow.Priority
				if flowPriority == original {
					// The OF flow was created at the priority which need to be re-installed
					// at the NewPriority now
					updatedFlow := copyFlowWithNewPriority(actionFlow, newPriority)
					addFlows = append(addFlows, updatedFlow)
					delFlows = append(delFlows, actionFlow)
					// Store the actionFlow update to the policyRuleConjunction and update all
					// policyRuleConjunctions if flow installation is successful.
					conjFlowUpdates[conj.id] = flowUpdates{
						append(conjFlowUpdates[conj.id].newActionFlows, updatedFlow),
						newPriority,
					}
				}
			}
			matchFlowAdd, matchFlowDel := getMatchFlowUpdates(conj, newPriority)
			addFlows = append(addFlows, matchFlowAdd...)
			delFlows = append(delFlows, matchFlowDel...)
		}
	}
	return addFlows, delFlows, conjFlowUpdates
}

func parseMulticastIngressPodFlow(flowMap map[string]string) (uint32, types.RuleMetric) {
	m := parseFlowMetric(flowMap)
	reg1 := flowMap["reg1"]
	id, _ := strconv.ParseUint(reg1, 0, 32)
	return uint32(id), m
}

func parseMulticastEgressPodFlow(flowMap map[string]string) (string, types.RuleMetric) {
	m := parseFlowMetric(flowMap)
	nwSrc := flowMap["nw_src"]
	return nwSrc, m
}

func parseMulticastMetricFlow(flowMap map[string]string) (uint32, types.RuleMetric) {
	// example MulticastEgressMetric allow flow format:
	// table=MulticastEgressMetric, n_packets=11, n_bytes=1562, priority=200,reg0=0x400/0x400,reg3=0x4 actions=goto_table:MulticastEgressPodMetric
	// example MulticastEgressMetric drop flow format:
	// table=MulticastEgressMetric, n_packets=230, n_bytes=32660, priority=200,reg0=0x400/0x400,reg3=0x3 actions=drop
	// example MulticastIngressMetric allow flow format:
	// table=MulticastIngressMetric, n_packets=18, n_bytes=780, priority=200,reg0=0x400/0x400,reg3=0x3 actions=resubmit(,MulticastOutput)
	m := parseFlowMetric(flowMap)
	conjID := flowMap["reg3"]
	id, _ := strconv.ParseUint(conjID, 0, 32)
	return uint32(id), m
}

func parseFlowMetric(flowMap map[string]string) types.RuleMetric {
	m := types.RuleMetric{}
	pkts, _ := strconv.ParseUint(flowMap["n_packets"], 10, 64)
	m.Packets = pkts
	bytes, _ := strconv.ParseUint(flowMap["n_bytes"], 10, 64)
	m.Bytes = bytes
	return m
}

func parseDropFlow(flowMap map[string]string) (uint32, types.RuleMetric) {
	m := parseFlowMetric(flowMap)
	m.Sessions = m.Packets
	reg3 := flowMap["reg3"]
	id, _ := strconv.ParseUint(reg3, 0, 32)
	return uint32(id), m
}

func parseAllowFlow(flowMap map[string]string) (uint32, types.RuleMetric) {
	m := parseFlowMetric(flowMap)
	if strings.Contains(flowMap["ct_state"], "+") { // ct_state=+new
		m.Sessions = m.Packets
	}
	ct_label := flowMap["ct_label"]
	idRaw := ct_label[strings.Index(ct_label, "0x")+2 : strings.Index(ct_label, "/")]
	if len(idRaw) > 8 { // only 32 bits are valid.
		idRaw = idRaw[:len(idRaw)-8]
	}
	id, _ := strconv.ParseUint(idRaw, 16, 32)
	return uint32(id), m
}

func parseFlowToMap(flow string) map[string]string {
	split := strings.Split(flow, ",")
	flowMap := make(map[string]string)
	for _, seg := range split {
		equalIndex := strings.Index(seg, "=")
		// Some substrings spilt by "," may have no "=", for instance, if "resubmit(,70)" is present.
		if equalIndex == -1 {
			continue
		}
		key := strings.TrimSpace(seg[:equalIndex])
		value := strings.TrimSpace(seg[equalIndex+1:])
		// There is space not comma before actions, which causes troubles when parsing value, example:
		// table=MulticastEgressRule, ...,conj_id=2 actions=load:0x2->NXM_NX_REG5[],load...
		actionIndex := strings.Index(value, "actions")
		if actionIndex != -1 {
			value = value[:actionIndex-1]
		}
		flowMap[key] = value
	}
	return flowMap
}

func parseMetricFlow(flowMap map[string]string) (uint32, types.RuleMetric) {
	dropIdentifier := "reg0"
	// example allow flow format:
	// table=101, n_packets=0, n_bytes=0, priority=200,ct_state=-new,ct_label=0x1/0xffffffff,ip actions=goto_table:105
	// example drop flow format:
	// table=101, n_packets=9, n_bytes=666, priority=200,reg0=0x100000/0x100000,reg3=0x5 actions=drop
	if _, ok := flowMap[dropIdentifier]; ok {
		return parseDropFlow(flowMap)
	}
	return parseAllowFlow(flowMap)
}

type featureNetworkPolicy struct {
	cookieAllocator       cookie.Allocator
	ipProtocols           []binding.Protocol
	bridge                binding.Bridge
	nodeType              config.NodeType
	l7NetworkPolicyConfig *config.L7NetworkPolicyConfig

	// globalConjMatchFlowCache is a global map for conjMatchFlowContext. The key is a string generated from the
	// conjMatchFlowContext.
	globalConjMatchFlowCache map[string]*conjMatchFlowContext
	conjMatchFlowLock        sync.Mutex // Lock for access globalConjMatchFlowCache
	// policyCache is a storage that supports listing policyRuleConjunction with different indexers.
	// It's guaranteed that one policyRuleConjunction is processed by at most one goroutine at any given time.
	policyCache cache.Indexer
	// egressTables map records all IDs of tables related to egress rules.
	egressTables map[uint8]struct{}

	// loggingGroupCache is a storage for the logging groups, each one includes 2 buckets: one is to send the packet
	// to antrea-agent using the packetIn mechanism, the other is to force the packet to continue forwardingin the
	// OVS pipeline. The key is the next table used in the second bucket, and the value is the Openflow group.
	loggingGroupCache sync.Map
	groupAllocator    GroupAllocator

	ovsMetersAreSupported bool
	enableDenyTracking    bool
	enableAntreaPolicy    bool
	enableL7NetworkPolicy bool
	enableMulticast       bool
	proxyAll              bool
	ctZoneSrcField        *binding.RegField
	// deterministic represents whether to generate flows deterministically.
	// For example, if a flow has multiple actions, setting it to true can get consistent flow.
	// Enabling it may carry a performance impact. It's disabled by default and should only be used in testing.
	deterministic bool

	category cookie.Category
}

func (f *featureNetworkPolicy) getFeatureName() string {
	return "NetworkPolicy"
}

func newFeatureNetworkPolicy(
	cookieAllocator cookie.Allocator,
	ipProtocols []binding.Protocol,
	bridge binding.Bridge,
	l7NetworkPolicyConfig *config.L7NetworkPolicyConfig,
	ovsMetersAreSupported,
	enableDenyTracking,
	enableAntreaPolicy bool,
	enableL7NetworkPolicy bool,
	enableMulticast bool,
	proxyAll bool,
	connectUplinkToBridge bool,
	nodeType config.NodeType,
	grpAllocator GroupAllocator) *featureNetworkPolicy {
	return &featureNetworkPolicy{
		cookieAllocator:          cookieAllocator,
		ipProtocols:              ipProtocols,
		bridge:                   bridge,
		nodeType:                 nodeType,
		enableL7NetworkPolicy:    enableL7NetworkPolicy,
		l7NetworkPolicyConfig:    l7NetworkPolicyConfig,
		globalConjMatchFlowCache: make(map[string]*conjMatchFlowContext),
		policyCache:              cache.NewIndexer(policyConjKeyFunc, cache.Indexers{priorityIndex: priorityIndexFunc}),
		enableMulticast:          enableMulticast,
		ovsMetersAreSupported:    ovsMetersAreSupported,
		enableDenyTracking:       enableDenyTracking,
		enableAntreaPolicy:       enableAntreaPolicy,
		proxyAll:                 proxyAll,
		category:                 cookie.NetworkPolicy,
		ctZoneSrcField:           getZoneSrcField(connectUplinkToBridge),
		loggingGroupCache:        sync.Map{},
		groupAllocator:           grpAllocator,
	}
}

func (f *featureNetworkPolicy) initFlows() []*openflow15.FlowMod {
	f.egressTables = map[uint8]struct{}{EgressRuleTable.GetID(): {}, EgressDefaultTable.GetID(): {}}
	if f.enableAntreaPolicy {
		f.egressTables[AntreaPolicyEgressRuleTable.GetID()] = struct{}{}
		if f.enableMulticast {
			f.egressTables[MulticastEgressRuleTable.GetID()] = struct{}{}
		}
	}
	var flows []binding.Flow
	if f.nodeType == config.K8sNode {
		flows = append(flows, f.ingressClassifierFlows()...)
		if f.enableL7NetworkPolicy {
			flows = append(flows, f.l7NPTrafficControlFlows()...)
		}
	}
	flows = append(flows, f.skipPolicyRuleCheckFlows()...)
	flows = append(flows, f.initLoggingFlows()...)
	return GetFlowModMessages(flows, binding.AddMessage)
}

// skipPolicyRuleCheckFlows generates the flows to forward the packets in an established or related
// connections to the metric table in the same stage directly, so that these packets would skip the flows
// for NetworkPolicy rules.
func (f *featureNetworkPolicy) skipPolicyRuleCheckFlows() []binding.Flow {
	var flows []binding.Flow
	cookieID := f.cookieAllocator.Request(f.category).Raw()
	egressCTStateFlowTable := EgressRuleTable
	ingressCTStateFlowTable := IngressRuleTable
	priority := priorityHigh
	if f.enableAntreaPolicy {
		egressCTStateFlowTable = AntreaPolicyEgressRuleTable
		ingressCTStateFlowTable = AntreaPolicyIngressRuleTable
		priority = priorityTopAntreaPolicy
	}
	for _, ipProtocol := range f.ipProtocols {
		flows = append(flows,
			egressCTStateFlowTable.ofTable.BuildFlow(priority).
				Cookie(cookieID).
				MatchProtocol(ipProtocol).
				MatchCTStateNew(false).
				MatchCTStateEst(true).
				Action().GotoTable(EgressMetricTable.GetID()).
				Done(),
			egressCTStateFlowTable.ofTable.BuildFlow(priority).
				Cookie(cookieID).
				MatchProtocol(ipProtocol).
				MatchCTStateNew(false).
				MatchCTStateRel(true).
				Action().GotoTable(EgressMetricTable.GetID()).
				Done(),
			ingressCTStateFlowTable.ofTable.BuildFlow(priority).
				Cookie(cookieID).
				MatchProtocol(ipProtocol).
				MatchCTStateNew(false).
				MatchCTStateEst(true).
				Action().GotoTable(IngressMetricTable.GetID()).
				Done(),
			ingressCTStateFlowTable.ofTable.BuildFlow(priority).
				Cookie(cookieID).
				MatchProtocol(ipProtocol).
				MatchCTStateNew(false).
				MatchCTStateRel(true).
				Action().GotoTable(IngressMetricTable.GetID()).
				Done(),
		)
	}
	return flows
}

func (f *featureNetworkPolicy) l7NPTrafficControlFlows() []binding.Flow {
	cookieID := f.cookieAllocator.Request(f.category).Raw()
	vlanMask := uint16(openflow15.OFPVID_PRESENT)
	return []binding.Flow{
		// This generates the flow to output the packets marked with L7NPRedirectCTMark to an application-aware engine
		// via the target ofPort. Note that, before outputting the packets, VLAN ID stored on field L7NPRuleVlanIDCTMarkField
		// will be copied to VLAN ID register (OXM_OF_VLAN_VID) to set VLAN ID of the packets.
		OutputTable.ofTable.BuildFlow(priorityHigh+2).
			Cookie(cookieID).
			MatchRegMark(OutputToOFPortRegMark).
			MatchCTMark(L7NPRedirectCTMark).
			Action().PushVLAN(EtherTypeDot1q).
			Action().MoveRange(binding.NxmFieldCtLabel, binding.OxmFieldVLANVID, *L7NPRuleVlanIDCTLabel.GetRange(), *binding.VLANVIDRange).
			Action().Output(f.l7NetworkPolicyConfig.TargetOFPort).
			Done(),
		// This generates the flow to mark the packets from an application-aware engine via the return ofPort and forward
		// the packets to stageRouting directly. Note that, for the packets which are originally to be output to a tunnel
		// port, value of NXM_NX_TUN_IPV4_DST needs to be loaded in L3ForwardingTable of stageRouting.
		ClassifierTable.ofTable.BuildFlow(priorityNormal).
			Cookie(f.cookieAllocator.Request(f.category).Raw()).
			MatchInPort(f.l7NetworkPolicyConfig.ReturnOFPort).
			MatchVLAN(false, 0, &vlanMask).
			Action().PopVLAN().
			Action().LoadRegMark(FromTCReturnRegMark).
			Action().GotoStage(stageRouting).
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

func (f *featureNetworkPolicy) loggingNPPacketFlowWithOperations(cookieID uint64, operations uint8) binding.Flow {
	loggingOperations := binding.NewRegMark(PacketInOperationField, uint32(operations))
	fb := OutputTable.ofTable.BuildFlow(priorityNormal).
		MatchRegMark(OutputToControllerRegMark, loggingOperations)
	if f.ovsMetersAreSupported {
		fb = fb.Action().Meter(PacketInMeterIDNP)
	}
	return fb.Action().SendToController([]byte{uint8(PacketInCategoryNP), operations}, false).
		Cookie(cookieID).
		Done()
}

func (f *featureNetworkPolicy) initLoggingFlows() []binding.Flow {
	maxOperationValue := PacketInNPLoggingOperation + PacketInNPStoreDenyOperation + PacketInNPRejectOperation
	flows := make([]binding.Flow, 0, maxOperationValue)
	cookieID := f.cookieAllocator.Request(f.category).Raw()
	for operation := 1; operation <= maxOperationValue; operation++ {
		flows = append(flows, f.loggingNPPacketFlowWithOperations(cookieID, uint8(operation)))
	}
	return flows
}

func (f *featureNetworkPolicy) allowRulesMetricFlows(conjunctionID uint32, ingress bool, tableID uint8) []binding.Flow {
	cookieID := f.cookieAllocator.Request(f.category).Raw()
	metricTable := IngressMetricTable
	offset := 0
	// We use the 0..31 bits of the ct_label to store the ingress rule ID and use the 32..63 bits to store the
	// egress rule ID.
	field := IngressRuleCTLabel
	if !ingress {
		metricTable = EgressMetricTable
		offset = 32
		field = EgressRuleCTLabel
	}
	if f.enableMulticast && tableID == MulticastEgressRuleTable.GetID() {
		metricTable = MulticastEgressMetricTable
	}
	if f.enableMulticast && tableID == MulticastIngressRuleTable.GetID() {
		metricTable = MulticastIngressMetricTable
	}
	metricFlow := func(isCTNew bool, protocol binding.Protocol) binding.Flow {
		return metricTable.ofTable.BuildFlow(priorityNormal).
			Cookie(cookieID).
			MatchProtocol(protocol).
			MatchCTStateNew(isCTNew).
			MatchCTLabelField(0, uint64(conjunctionID)<<offset, field).
			Action().NextTable().
			Done()
	}
	var flows []binding.Flow
	// Unlike rules for unicast traffic, each IGMP and multicast rule uses single metric flow to track stats
	// in multicast metric tables.
	if metricTable == MulticastEgressMetricTable || metricTable == MulticastIngressMetricTable {
		flow := metricTable.ofTable.BuildFlow(priorityNormal).
			Cookie(f.cookieAllocator.Request(f.category).Raw()).
			MatchRegFieldWithValue(APConjIDField, conjunctionID).
			Action().GotoTable(metricTable.GetNext()).
			Done()
		flows = append(flows, flow)
		return flows
	}
	// These two flows track the number of sessions in addition to the packet and byte counts.
	// The flow matching 'ct_state=+new' tracks the number of sessions and byte count of the first packet for each
	// session.
	// The flow matching 'ct_state=-new' tracks the byte/packet count of an established connection (both directions).
	for _, ipProtocol := range f.ipProtocols {
		flows = append(flows, metricFlow(true, ipProtocol), metricFlow(false, ipProtocol))
	}
	return flows
}

func (f *featureNetworkPolicy) denyRuleMetricFlow(conjunctionID uint32, ingress bool, tableID uint8) binding.Flow {
	metricTable := IngressMetricTable
	if !ingress {
		metricTable = EgressMetricTable
	}
	if f.enableMulticast && tableID == MulticastEgressRuleTable.GetID() {
		metricTable = MulticastEgressMetricTable
	}
	if f.enableMulticast && tableID == MulticastIngressRuleTable.GetID() {
		metricTable = MulticastIngressMetricTable
	}
	return metricTable.ofTable.BuildFlow(priorityNormal).
		Cookie(f.cookieAllocator.Request(f.category).Raw()).
		MatchRegMark(APDenyRegMark).
		MatchRegFieldWithValue(APConjIDField, conjunctionID).
		Action().Drop().
		Done()
}

// For normal traffic, conjunctionActionFlow generates the flow to jump to a specific table if policyRuleConjunction ID is matched. Priority of
// conjunctionActionFlow is created at priorityLow for k8s network policies, and *priority assigned by PriorityAssigner for AntreaPolicy.
func (f *featureNetworkPolicy) conjunctionActionFlow(conjunctionID uint32, table binding.Table, nextTable uint8, priority *uint16, enableLogging bool, l7RuleVlanID *uint32) []binding.Flow {
	tableID := table.GetID()
	cookieID := f.cookieAllocator.Request(f.category).Raw()
	var ofPriority uint16
	if priority == nil {
		ofPriority = priorityLow
	} else {
		ofPriority = *priority
	}
	conjReg := TFIngressConjIDField
	labelField := IngressRuleCTLabel

	if _, ok := f.egressTables[tableID]; ok {
		conjReg = TFEgressConjIDField
		labelField = EgressRuleCTLabel
	}
	conjActionFlow := func(proto binding.Protocol) binding.Flow {
		ctZone := CtZone
		if proto == binding.ProtocolIPv6 {
			ctZone = CtZoneV6
		}
		if enableLogging {
			fb := table.BuildFlow(ofPriority).MatchProtocol(proto).
				MatchConjID(conjunctionID)
			if l7RuleVlanID != nil {
				return fb.
					Action().LoadToRegField(conjReg, conjunctionID).        // Traceflow.
					Action().CT(true, nextTable, ctZone, f.ctZoneSrcField). // CT action requires commit flag if actions other than NAT without arguments are specified.
					LoadToLabelField(uint64(conjunctionID), labelField).
					LoadToCtMark(L7NPRedirectCTMark).                               // Mark the packets of the connection should be redirected to an application-aware engine.
					LoadToLabelField(uint64(*l7RuleVlanID), L7NPRuleVlanIDCTLabel). // Load the VLAN ID allocated for L7 NetworkPolicy rule to CT mark field L7NPRuleVlanIDCTMarkField.
					CTDone().
					Action().LoadRegMark(DispositionAllowRegMark, L7NPRedirectRegMark, OutputToControllerRegMark). // AntreaPolicy.
					Action().LoadToRegField(PacketInOperationField, PacketInNPLoggingOperation).
					Action().LoadToRegField(PacketInTableField, uint32(tableID)).
					Action().GotoTable(OutputTable.GetID()).
					Cookie(cookieID).
					Done()
			}
			return fb.
				Action().LoadToRegField(conjReg, conjunctionID).        // Traceflow.
				Action().CT(true, nextTable, ctZone, f.ctZoneSrcField). // CT action requires commit flag if actions other than NAT without arguments are specified.
				LoadToLabelField(uint64(conjunctionID), labelField).
				CTDone().
				Action().LoadRegMark(DispositionAllowRegMark, OutputToControllerRegMark). // AntreaPolicy.
				Action().LoadToRegField(PacketInOperationField, PacketInNPLoggingOperation).
				Action().LoadToRegField(PacketInTableField, uint32(tableID)).
				Action().GotoTable(OutputTable.GetID()).
				Cookie(cookieID).
				Done()
		}
		if l7RuleVlanID != nil {
			return table.BuildFlow(ofPriority).MatchProtocol(proto).
				MatchConjID(conjunctionID).
				Action().LoadToRegField(conjReg, conjunctionID).        // Traceflow.
				Action().CT(true, nextTable, ctZone, f.ctZoneSrcField). // CT action requires commit flag if actions other than NAT without arguments are specified.
				LoadToLabelField(uint64(conjunctionID), labelField).
				LoadToCtMark(L7NPRedirectCTMark).                               // Mark the packets of the connection should be redirected to an application-aware engine.
				LoadToLabelField(uint64(*l7RuleVlanID), L7NPRuleVlanIDCTLabel). // Load the VLAN ID allocated for L7 NetworkPolicy rule to CT mark field L7NPRuleVlanIDCTMarkField.
				CTDone().
				Cookie(cookieID).
				Done()
		}
		return table.BuildFlow(ofPriority).MatchProtocol(proto).
			MatchConjID(conjunctionID).
			Action().LoadToRegField(conjReg, conjunctionID).        // Traceflow.
			Action().CT(true, nextTable, ctZone, f.ctZoneSrcField). // CT action requires commit flag if actions other than NAT without arguments are specified.
			LoadToLabelField(uint64(conjunctionID), labelField).
			CTDone().
			Cookie(cookieID).
			Done()
	}
	var flows []binding.Flow
	// As IGMP and multicast use a different pipeline 'Multicast', if the rule is
	// IGMP ingress or multicast egress，conjunctionActionFlow generates the flow
	// to mark the packet to be allowed if policyRuleConjunction ID is matched.
	// Any matched flow will be resubmitted to next table in corresponding metric tables.
	if f.enableMulticast && (tableID == MulticastEgressRuleTable.GetID() || tableID == MulticastIngressRuleTable.GetID()) {
		flow := table.BuildFlow(ofPriority).MatchConjID(conjunctionID).
			Action().LoadToRegField(APConjIDField, conjunctionID).
			Action().NextTable().
			Cookie(f.cookieAllocator.Request(f.category).Raw()).
			Done()
		flows = append(flows, flow)
		return flows
	}
	for _, proto := range f.ipProtocols {
		flows = append(flows, conjActionFlow(proto))
	}
	return flows
}

// conjunctionActionDenyFlow generates the flow to mark the packet to be denied (dropped or rejected) if policyRuleConjunction
// ID is matched. Any matched flow will be dropped in corresponding metric tables.
func (f *featureNetworkPolicy) conjunctionActionDenyFlow(conjunctionID uint32, table binding.Table, priority *uint16,
	disposition uint32, enableLogging bool) binding.Flow {
	ofPriority := *priority
	metricTable := IngressMetricTable
	tableID := table.GetID()
	if _, ok := f.egressTables[tableID]; ok {
		metricTable = EgressMetricTable
	}

	if f.enableMulticast && tableID == MulticastEgressRuleTable.GetID() {
		metricTable = MulticastEgressMetricTable
	}
	if f.enableMulticast && tableID == MulticastIngressRuleTable.GetID() {
		metricTable = MulticastIngressMetricTable
	}
	flowBuilder := table.BuildFlow(ofPriority).
		Cookie(f.cookieAllocator.Request(f.category).Raw()).
		MatchConjID(conjunctionID).
		Action().LoadToRegField(APConjIDField, conjunctionID).
		Action().LoadRegMark(APDenyRegMark)

	var packetInOperations uint8
	if f.enableDenyTracking {
		packetInOperations += PacketInNPStoreDenyOperation
		flowBuilder = flowBuilder.
			Action().LoadToRegField(APDispositionField, disposition)
	}
	if enableLogging {
		packetInOperations += PacketInNPLoggingOperation
		flowBuilder = flowBuilder.
			Action().LoadToRegField(APDispositionField, disposition)
	}
	if disposition == DispositionRej {
		packetInOperations += PacketInNPRejectOperation
	}

	if enableLogging || f.enableDenyTracking || disposition == DispositionRej {
		groupID := f.getLoggingAndResubmitGroupID(metricTable.GetID())
		return flowBuilder.Action().LoadToRegField(PacketInOperationField, uint32(packetInOperations)).
			Action().LoadToRegField(PacketInTableField, uint32(tableID)).
			Action().Group(groupID).
			Done()
	}

	// We do not drop the packet immediately but send the packet to the metric table to update the rule metrics.
	return flowBuilder.Action().GotoTable(metricTable.GetID()).
		Done()
}

func (f *featureNetworkPolicy) conjunctionActionPassFlow(conjunctionID uint32, table binding.Table, priority *uint16, enableLogging bool) binding.Flow {
	ofPriority := *priority
	conjReg := TFIngressConjIDField
	nextTable := IngressRuleTable
	tableID := table.GetID()
	if _, ok := f.egressTables[tableID]; ok {
		conjReg = TFEgressConjIDField
		nextTable = EgressRuleTable
	}
	flowBuilder := table.BuildFlow(ofPriority).
		Cookie(f.cookieAllocator.Request(f.category).Raw()).
		MatchConjID(conjunctionID).
		Action().LoadToRegField(conjReg, conjunctionID)

	if enableLogging {
		groupID := f.getLoggingAndResubmitGroupID(nextTable.GetID())
		return flowBuilder.
			Action().LoadRegMark(DispositionPassRegMark).
			Action().LoadToRegField(PacketInOperationField, PacketInNPLoggingOperation).
			Action().LoadToRegField(PacketInTableField, uint32(tableID)).
			Action().Group(groupID).
			Done()
	}
	return flowBuilder.Action().GotoTable(nextTable.GetID()).
		Done()
}

func (f *featureNetworkPolicy) addFlowMatch(fb binding.FlowBuilder, matchKey *types.MatchKey, matchValue interface{}) binding.FlowBuilder {
	switch matchKey {
	case MatchDstOFPort:
		// ofport number in NXM_NX_REG1 is used in ingress rule to match packets sent to local Pod.
		fb = fb.MatchRegFieldWithValue(TargetOFPortField, uint32(matchValue.(int32)))
	case MatchSrcOFPort:
		fb = fb.MatchInPort(uint32(matchValue.(int32)))
	case MatchDstIP:
		fallthrough
	case MatchDstIPv6:
		fb = fb.MatchProtocol(matchKey.GetOFProtocol()).MatchDstIP(matchValue.(net.IP))
	case MatchDstIPNet:
		fallthrough
	case MatchDstIPNetv6:
		fb = fb.MatchProtocol(matchKey.GetOFProtocol()).MatchDstIPNet(matchValue.(net.IPNet))
	case MatchCTDstIP:
		fallthrough
	case MatchCTDstIPv6:
		fb = fb.MatchCTStateNew(true).MatchProtocol(matchKey.GetOFProtocol()).MatchCTDstIP(matchValue.(net.IP))
	case MatchCTDstIPNet:
		fallthrough
	case MatchCTDstIPNetv6:
		fb = fb.MatchCTStateNew(true).MatchProtocol(matchKey.GetOFProtocol()).MatchCTDstIPNet(matchValue.(net.IPNet))
	case MatchSrcIP:
		fallthrough
	case MatchSrcIPv6:
		fb = fb.MatchProtocol(matchKey.GetOFProtocol()).MatchSrcIP(matchValue.(net.IP))
	case MatchSrcIPNet:
		fb = fb.MatchProtocol(matchKey.GetOFProtocol()).MatchSrcIPNet(matchValue.(net.IPNet))
	case MatchSrcIPNetv6:
		fb = fb.MatchProtocol(matchKey.GetOFProtocol()).MatchSrcIPNet(matchValue.(net.IPNet))
	case MatchCTSrcIP:
		fallthrough
	case MatchCTSrcIPv6:
		fb = fb.MatchCTStateNew(true).MatchProtocol(matchKey.GetOFProtocol()).MatchCTSrcIP(matchValue.(net.IP))
	case MatchCTSrcIPNet:
		fallthrough
	case MatchCTSrcIPNetv6:
		fb = fb.MatchCTStateNew(true).MatchProtocol(matchKey.GetOFProtocol()).MatchCTSrcIPNet(matchValue.(net.IPNet))
	case MatchTCPDstPort:
		fallthrough
	case MatchTCPv6DstPort:
		fallthrough
	case MatchUDPDstPort:
		fallthrough
	case MatchUDPv6DstPort:
		fallthrough
	case MatchSCTPDstPort:
		fallthrough
	case MatchSCTPv6DstPort:
		fb = fb.MatchProtocol(matchKey.GetOFProtocol())
		portValue := matchValue.(types.BitRange)
		if portValue.Value > 0 {
			fb = fb.MatchDstPort(portValue.Value, portValue.Mask)
		}
	case MatchTCPSrcPort:
		fallthrough
	case MatchTCPv6SrcPort:
		fallthrough
	case MatchUDPSrcPort:
		fallthrough
	case MatchUDPv6SrcPort:
		fb = fb.MatchProtocol(matchKey.GetOFProtocol())
		portValue := matchValue.(types.BitRange)
		if portValue.Value > 0 {
			fb = fb.MatchSrcPort(portValue.Value, portValue.Mask)
		}
	case MatchICMPType:
		fb = fb.MatchProtocol(matchKey.GetOFProtocol())
		if matchValue != nil {
			fb = fb.MatchICMPType(uint8(*matchValue.(*int32)))
		}
	case MatchICMPCode:
		fb = fb.MatchProtocol(matchKey.GetOFProtocol())
		if matchValue != nil {
			fb = fb.MatchICMPCode(uint8(*matchValue.(*int32)))
		}
	case MatchICMPv6Type:
		fb = fb.MatchProtocol(matchKey.GetOFProtocol())
		if matchValue != nil {
			fb = fb.MatchICMPv6Type(uint8(*matchValue.(*int32)))
		}
	case MatchICMPv6Code:
		fb = fb.MatchProtocol(matchKey.GetOFProtocol())
		if matchValue != nil {
			fb = fb.MatchICMPv6Code(uint8(*matchValue.(*int32)))
		}
	case MatchServiceGroupID:
		fb = fb.MatchRegFieldWithValue(ServiceGroupIDField, matchValue.(uint32))
	case MatchIGMPProtocol:
		fb = fb.MatchProtocol(matchKey.GetOFProtocol())
	case MatchLabelID:
		fb = fb.MatchTunnelID(uint64(matchValue.(uint32)))
	case MatchTCPFlags:
		fallthrough
	case MatchTCPv6Flags:
		fb = fb.MatchProtocol(matchKey.GetOFProtocol())
		tcpFlag := matchValue.(TCPFlags)
		fb = fb.MatchTCPFlags(tcpFlag.Flag, tcpFlag.Mask)
	case MatchCTState:
		ctState := matchValue.(*openflow15.CTStates)
		fb = fb.MatchCTState(ctState)
	}
	return fb
}

// conjunctionExceptionFlow generates the flow to jump to a specific table if both policyRuleConjunction ID and except address are matched.
// Keeping this for reference to generic exception flow.
// nolint: unused
func (f *featureNetworkPolicy) conjunctionExceptionFlow(conjunctionID uint32, tableID uint8, nextTable uint8, matchKey *types.MatchKey, matchValue interface{}) binding.Flow {
	conjReg := TFIngressConjIDField
	if tableID == EgressRuleTable.GetID() {
		conjReg = TFEgressConjIDField
	}
	fb := getTableByID(tableID).BuildFlow(priorityNormal).MatchConjID(conjunctionID)
	return f.addFlowMatch(fb, matchKey, matchValue).
		Action().LoadToRegField(conjReg, conjunctionID). // Traceflow.
		Action().GotoTable(nextTable).
		Cookie(f.cookieAllocator.Request(f.category).Raw()).
		Done()
}

type conjunctiveActionsInOrder []*conjunctiveAction

func (sl conjunctiveActionsInOrder) Len() int      { return len(sl) }
func (sl conjunctiveActionsInOrder) Swap(i, j int) { sl[i], sl[j] = sl[j], sl[i] }
func (sl conjunctiveActionsInOrder) Less(i, j int) bool {
	if sl[i].conjID != sl[j].conjID {
		return sl[i].conjID < sl[j].conjID
	}
	if sl[i].clauseID != sl[j].clauseID {
		return sl[i].clauseID < sl[j].clauseID
	}
	return sl[i].nClause < sl[j].nClause
}

// conjunctiveMatchFlow generates the flow to set conjunctive actions if the match condition is matched.
func (f *featureNetworkPolicy) conjunctiveMatchFlow(tableID uint8, matchPairs []matchPair, priority *uint16, actions []*conjunctiveAction) binding.Flow {
	var ofPriority uint16
	if priority != nil {
		ofPriority = *priority
	} else {
		ofPriority = priorityNormal
	}
	fb := getTableByID(tableID).BuildFlow(ofPriority)
	for _, eachMatchPair := range matchPairs {
		fb = f.addFlowMatch(fb, eachMatchPair.matchKey, eachMatchPair.matchValue)
	}
	if f.deterministic {
		sort.Sort(conjunctiveActionsInOrder(actions))
	}
	for _, act := range actions {
		fb.Action().Conjunction(act.conjID, act.clauseID, act.nClause)
	}
	return fb.Cookie(f.cookieAllocator.Request(f.category).Raw()).Done()
}

// defaultDropFlow generates the flow to drop packets if the match condition is matched.
func (f *featureNetworkPolicy) defaultDropFlow(table binding.Table, matchPairs []matchPair, enableLogging bool) binding.Flow {
	cookieID := f.cookieAllocator.Request(f.category).Raw()
	fb := table.BuildFlow(priorityNormal).Cookie(cookieID)
	for _, eachMatchPair := range matchPairs {
		fb = f.addFlowMatch(fb, eachMatchPair.matchKey, eachMatchPair.matchValue)
	}

	var packetInOperations uint8
	if f.enableDenyTracking {
		packetInOperations += PacketInNPStoreDenyOperation
	}
	if enableLogging {
		packetInOperations += PacketInNPLoggingOperation
	}

	if enableLogging || f.enableDenyTracking {
		return fb.Action().LoadRegMark(DispositionDropRegMark).
			Action().LoadToRegField(PacketInOperationField, uint32(packetInOperations)).
			Action().LoadRegMark(OutputToControllerRegMark).
			Action().LoadToRegField(PacketInTableField, uint32(table.GetID())).
			Action().GotoTable(OutputTable.GetID()).
			Done()
	}
	return fb.Action().Drop().
		Done()
}

// multiClusterNetworkPolicySecurityDropFlow generates the security drop flows for MultiClusterNetworkPolicy.
func (f *featureNetworkPolicy) multiClusterNetworkPolicySecurityDropFlow(table binding.Table, matchPairs []matchPair) binding.Flow {
	cookieID := f.cookieAllocator.Request(f.category).Raw()
	fb := table.BuildFlow(priorityNormal)
	fb = f.addFlowMatch(fb, MatchLabelID, UnknownLabelIdentity)
	for _, eachMatchPair := range matchPairs {
		fb = f.addFlowMatch(fb, eachMatchPair.matchKey, eachMatchPair.matchValue)
	}
	return fb.Cookie(cookieID).Action().Drop().Done()
}

// dnsPacketInFlow generates the flow to send dns response packets of fqdn policy selected Pods to the fqdnController for
// processing.
func (f *featureNetworkPolicy) dnsPacketInFlow(conjunctionID uint32) binding.Flow {
	fb := AntreaPolicyIngressRuleTable.ofTable.BuildFlow(priorityDNSIntercept).
		Cookie(f.cookieAllocator.Request(f.category).Raw()).
		MatchConjID(conjunctionID)
	if f.ovsMetersAreSupported {
		fb = fb.Action().Meter(PacketInMeterIDDNS)
	}
	// FQDN should pause DNS response packets and send them to the controller. After
	// the controller processes DNS response packets, like creating related flows in
	// the OVS or no operations are needed, the controller will resume those packets.
	return fb.Action().SendToController([]byte{uint8(PacketInCategoryDNS)}, true).
		Action().GotoTable(IngressMetricTable.GetID()).
		Done()
}

// ingressClassifierFlows generates the flows to classify the packets from local Pods or the Antrea gateway to different
// tables within stageIngressSecurity.
func (f *featureNetworkPolicy) ingressClassifierFlows() []binding.Flow {
	cookieID := f.cookieAllocator.Request(f.category).Raw()
	flows := []binding.Flow{
		// This generates the flow to match the packets to the Antrea gateway and forward them to IngressMetricTable.
		IngressSecurityClassifierTable.ofTable.BuildFlow(priorityNormal).
			Cookie(cookieID).
			MatchRegMark(ToGatewayRegMark).
			Action().GotoTable(IngressMetricTable.GetID()).
			Done(),
		// This generates the flow to match the packets to tunnel and forward them to IngressMetricTable.
		IngressSecurityClassifierTable.ofTable.BuildFlow(priorityNormal).
			Cookie(cookieID).
			MatchRegMark(ToTunnelRegMark).
			Action().GotoTable(IngressMetricTable.GetID()).
			Done(),
		// This generates the flow to match the packets to uplink and forward them to IngressMetricTable.
		IngressSecurityClassifierTable.ofTable.BuildFlow(priorityNormal).
			Cookie(cookieID).
			MatchRegMark(ToUplinkRegMark).
			Action().GotoTable(IngressMetricTable.GetID()).
			Done(),
		// This generates the flow to match the hairpin service packets and forward them to stageConntrack.
		IngressSecurityClassifierTable.ofTable.BuildFlow(priorityNormal).
			Cookie(cookieID).
			MatchCTMark(HairpinCTMark).
			Action().GotoStage(stageConntrack).
			Done(),
	}
	if f.enableAntreaPolicy && f.proxyAll {
		// This generates the flow to match the NodePort Service packets and forward them to AntreaPolicyIngressRuleTable.
		// Policies applied on NodePort Service will be enforced in AntreaPolicyIngressRuleTable.
		flows = append(flows, IngressSecurityClassifierTable.ofTable.BuildFlow(priorityNormal+1).
			Cookie(cookieID).
			MatchRegMark(ToNodePortAddressRegMark).
			Action().GotoTable(AntreaPolicyIngressRuleTable.GetID()).
			Done())
	}
	return flows
}

// flowsToTrace is used to generate flows for Traceflow from globalConjMatchFlowCache and policyCache.
func (f *featureNetworkPolicy) flowsToTrace(dataplaneTag uint8,
	ovsMetersAreSupported,
	liveTraffic,
	droppedOnly,
	receiverOnly bool,
	packet *binding.Packet,
	ofPort uint32,
	timeout uint16) []binding.Flow {
	cookieID := f.cookieAllocator.Request(cookie.Traceflow).Raw()
	var flows []binding.Flow
	f.conjMatchFlowLock.Lock()
	defer f.conjMatchFlowLock.Unlock()
	for _, ctx := range f.globalConjMatchFlowCache {
		if ctx.dropFlow != nil {
			table, err := f.bridge.GetTableByID(ctx.dropFlow.TableId)
			if err != nil {
				klog.ErrorS(err, "Failed to get OpenFlow table by tableID", "id", ctx.dropFlow.TableId)
				continue
			}
			dropFlow := f.defaultDropFlow(table, ctx.matchPairs, false)
			copyFlowBuilder := dropFlow.CopyToBuilder(priorityNormal+2, false)
			if dropFlow.FlowProtocol() == "" {
				copyFlowBuilderIPv6 := dropFlow.CopyToBuilder(priorityNormal+2, false)
				copyFlowBuilderIPv6 = copyFlowBuilderIPv6.MatchProtocol(binding.ProtocolIPv6)
				if f.ovsMetersAreSupported {
					copyFlowBuilderIPv6 = copyFlowBuilderIPv6.Action().Meter(PacketInMeterIDTF)
				}
				flows = append(flows, copyFlowBuilderIPv6.MatchIPDSCP(dataplaneTag).
					Cookie(cookieID).
					SetHardTimeout(timeout).
					Action().SendToController([]byte{uint8(PacketInCategoryTF)}, false).
					Done())
				copyFlowBuilder = copyFlowBuilder.MatchProtocol(binding.ProtocolIP)
			}
			if f.ovsMetersAreSupported {
				copyFlowBuilder = copyFlowBuilder.Action().Meter(PacketInMeterIDTF)
			}
			flows = append(flows, copyFlowBuilder.MatchIPDSCP(dataplaneTag).
				Cookie(cookieID).
				SetHardTimeout(timeout).
				Action().SendToController([]byte{uint8(PacketInCategoryTF)}, false).
				Done())
		}
	}
	// Copy Antrea NetworkPolicy drop rules.
	for _, obj := range f.policyCache.List() {
		conj := obj.(*policyRuleConjunction)
		for _, flow := range conj.metricFlows {
			table, err := f.bridge.GetTableByID(flow.TableId)
			if err != nil {
				klog.ErrorS(err, "Failed to get OpenFlow table by tableID", "id", flow.TableId)
				continue
			}
			if isDropFlow(flow) {
				conjID := conj.id

				// Generate both IPv4 and IPv6 flows if the original drop flow doesn't match IP/IPv6.
				// DSCP field is in IP/IPv6 headers so IP/IPv6 match is required in a flow.
				copyFlowBuilderIPv6 := table.BuildFlow(priorityNormal+2).
					MatchRegMark(APDenyRegMark).
					MatchRegFieldWithValue(APConjIDField, conjID).
					MatchProtocol(binding.ProtocolIPv6)
				if f.ovsMetersAreSupported {
					copyFlowBuilderIPv6 = copyFlowBuilderIPv6.Action().Meter(PacketInMeterIDTF)
				}
				flows = append(flows, copyFlowBuilderIPv6.MatchIPDSCP(dataplaneTag).
					SetHardTimeout(timeout).
					Cookie(cookieID).
					Action().SendToController([]byte{uint8(PacketInCategoryTF)}, false).
					Done())
				copyFlowBuilder := table.BuildFlow(priorityNormal+2).
					MatchRegMark(APDenyRegMark).
					MatchRegFieldWithValue(APConjIDField, conjID).MatchProtocol(binding.ProtocolIP)
				if f.ovsMetersAreSupported {
					copyFlowBuilder = copyFlowBuilder.Action().Meter(PacketInMeterIDTF)
				}
				flows = append(flows, copyFlowBuilder.MatchIPDSCP(dataplaneTag).
					SetHardTimeout(timeout).
					Cookie(cookieID).
					Action().SendToController([]byte{uint8(PacketInCategoryTF)}, false).
					Done())
			}
		}
	}
	return flows
}

func (f *featureNetworkPolicy) initGroups() []binding.OFEntry {
	var groups []binding.OFEntry
	candidateTables := []*Table{EgressRuleTable, EgressMetricTable, IngressRuleTable, IngressMetricTable}
	if f.enableMulticast {
		candidateTables = append(candidateTables, MulticastEgressMetricTable, MulticastIngressMetricTable)
	}
	for _, nextTable := range candidateTables {
		groupKey := fmt.Sprintf("%d", nextTable.GetID())
		obj, ok := f.loggingGroupCache.Load(groupKey)
		if ok {
			groups = append(groups, obj.(binding.Group))
			continue
		}
		// Create OpenFlow group to both log Antrea-native policy events and resubmit the packet to nextTable.
		groupID := f.groupAllocator.Allocate()
		// There are two buckets in this type All group which have generated two copies of the packet: 1) resubmit
		// the first one back to the next table of which the original packet consumes the group, then it is able to
		// continue with the previous forwarding logic; 2) set packetIn marks in the second one and resubmit it to
		// OutputTable.
		group := f.bridge.NewGroupTypeAll(groupID).Bucket().
			ResubmitToTable(nextTable.GetID()).
			Done().
			Bucket().
			LoadRegMark(OutputToControllerRegMark).
			ResubmitToTable(OutputTable.GetID()).
			Done()
		f.loggingGroupCache.Store(groupKey, group)
		groups = append(groups, group)
	}
	return groups
}

func (f *featureNetworkPolicy) replayMeters() []binding.OFEntry {
	return nil
}

func (f *featureNetworkPolicy) getLoggingAndResubmitGroupID(nextTable uint8) binding.GroupIDType {
	groupKey := fmt.Sprintf("%d", nextTable)
	group, _ := f.loggingGroupCache.Load(groupKey)
	return group.(binding.Group).GetID()
}

func (f *featureNetworkPolicy) replayGroups() []binding.OFEntry {
	return nil
}

func (f *featureNetworkPolicy) getRequiredTables() []*Table {
	tables := []*Table{
		EgressRuleTable,
		EgressDefaultTable,
		EgressMetricTable,
		IngressSecurityClassifierTable,
		IngressRuleTable,
		IngressDefaultTable,
		IngressMetricTable,
	}
	if f.enableAntreaPolicy {
		tables = append(tables,
			AntreaPolicyEgressRuleTable,
			AntreaPolicyIngressRuleTable,
		)
		if f.enableL7NetworkPolicy {
			tables = append(tables, TrafficControlTable) // For L7 NetworkPolicy.
		}
		if f.enableMulticast {
			tables = append(tables,
				MulticastEgressRuleTable,
				MulticastEgressPodMetricTable,
				MulticastEgressMetricTable,
				MulticastIngressRuleTable,
				MulticastIngressPodMetricTable,
				MulticastIngressMetricTable,
			)
		}
	}
	if f.nodeType == config.ExternalNode {
		tables = append(tables,
			EgressSecurityClassifierTable,
		)
	}
	return tables
}

// isDropFlow returns true if no instructions are defined in the OpenFlow modification message.
// According to the OpenFlow spec, there is no explicit action to represent drops. Instead, the action of dropping
// packets could come from empty instruction sets.
func isDropFlow(f *openflow15.FlowMod) bool {
	return len(f.Instructions) == 0
}

func copyFlowWithNewPriority(flowMod *openflow15.FlowMod, priority uint16) *openflow15.FlowMod {
	newFlow := *flowMod
	newFlow.Priority = priority
	return &newFlow
}

func flowMessageMatched(oldFlow, newFlow *openflow15.FlowMod) bool {
	return oldFlow.Priority == newFlow.Priority && getFlowKey(oldFlow) == getFlowKey(newFlow)
}
