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

package agent

import (
	oftest "antrea.io/antrea/pkg/agent/openflow/testing"
	routetest "antrea.io/antrea/pkg/agent/route/testing"
	fakecrd "antrea.io/antrea/pkg/client/clientset/versioned/fake"
	"antrea.io/antrea/pkg/features"
	"antrea.io/antrea/pkg/ovs/ovsctl"
	ovsctltest "antrea.io/antrea/pkg/ovs/ovsctl/testing"
	"context"
	"fmt"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	mock "github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes/fake"

	"antrea.io/antrea/pkg/agent/config"
	"antrea.io/antrea/pkg/agent/interfacestore"
	"antrea.io/antrea/pkg/agent/types"
	"antrea.io/antrea/pkg/ovs/ovsconfig"
	ovsconfigtest "antrea.io/antrea/pkg/ovs/ovsconfig/testing"
	"antrea.io/antrea/pkg/util/env"
	"antrea.io/antrea/pkg/util/ip"
	"antrea.io/antrea/pkg/util/runtime"
)

func newAgentInitializer(ovsBridgeClient ovsconfig.OVSBridgeClient, ifaceStore interfacestore.InterfaceStore) *Initializer {
	nodeConfig := &config.NodeConfig{UplinkNetConfig: &config.AdapterNetConfig{Name: "eth0"}}
	return &Initializer{ovsBridgeClient: ovsBridgeClient, ifaceStore: ifaceStore, hostGateway: "antrea-gw0", ovsBridge: "br-int", nodeConfig: nodeConfig}
}

type fakeInitializer struct {
	*Initializer
	mockOVSBridgeClient *ovsconfigtest.MockOVSBridgeClient
	mockOFClient        *oftest.MockClient
	mockOVSCtlClient    *ovsctltest.MockOVSCtlClient
	mockRouteClient     *routetest.MockInterface
	node                *corev1.Node
}

func newFakeInitializer(ctrl *mock.Controller, stopCh chan struct{}, node *corev1.Node) *fakeInitializer {
	mockOVSBridgeClient := ovsconfigtest.NewMockOVSBridgeClient(ctrl)
	mockOFClient := oftest.NewMockClient(ctrl)
	mockOVSCtlClient := ovsctltest.NewMockOVSCtlClient(ctrl)
	mockRouteClient := routetest.NewMockInterface(ctrl)
	ifaceStore := interfacestore.NewInterfaceStore()
	k8sObjects := []k8sruntime.Object{}
	if node != nil {
		k8sObjects = append(k8sObjects, node)
	}
	k8sClient := fake.NewSimpleClientset(k8sObjects...)
	crdClient := fakecrd.NewSimpleClientset()
	networkReadyCh := make(chan struct{}, 1)
	initializer := NewInitializer(k8sClient,
		crdClient,
		mockOVSBridgeClient,
		mockOFClient,
		mockRouteClient,
		mockOVSCtlClient,
		ifaceStore,
		"br-int",
		"antrea-gw0",
		0,
		&config.NetworkConfig{TunnelType: ovsconfig.GeneveTunnel},
		&config.WireGuardConfig{},
		&config.EgressConfig{},
		&config.ServiceConfig{},
		networkReadyCh,
		stopCh,
		config.K8sNode,
		"",
		true,
		true,
		false)
	return &fakeInitializer{Initializer: initializer,
		mockOFClient:        mockOFClient,
		mockOVSBridgeClient: mockOVSBridgeClient,
		mockRouteClient:     mockRouteClient,
		mockOVSCtlClient:    mockOVSCtlClient,
		node:                node,
	}
}

func TestInitInterfaceStore(t *testing.T) {
	tests := []struct {
		name                    string
		expectedGetPortListCall func(*ovsconfigtest.MockOVSBridgeClientMockRecorder)
		expectedInterfaces      []*interfacestore.InterfaceConfig
		expectedErr             error
	}{
		{
			name: "transaction error",
			expectedGetPortListCall: func(recorder *ovsconfigtest.MockOVSBridgeClientMockRecorder) {
				recorder.GetPortList().Return(nil, ovsconfig.NewTransactionError(fmt.Errorf("Failed to list OVS ports"), true))
			},
			expectedErr: ovsconfig.NewTransactionError(fmt.Errorf("Failed to list OVS ports"), true),
		},
		{
			name: "normal",
			expectedGetPortListCall: func(recorder *ovsconfigtest.MockOVSBridgeClientMockRecorder) {
				recorder.GetPortList().Return([]ovsconfig.OVSPortData{
					{
						UUID:   "uuid1",
						Name:   "pod1-abc",
						IFName: "pod1-abc",
						OFPort: 11,
						ExternalIDs: map[string]string{
							"antrea-type":   "container",
							"attached-mac":  "56:ba:da:72:6a:3a",
							"container-id":  "container1",
							"ip-address":    "192.168.0.9",
							"pod-name":      "pod1",
							"pod-namespace": "default",
						},
					},
					{
						UUID:        "uuid2",
						Name:        "antrea-gw0",
						IFName:      "antrea-gw0",
						OFPort:      1,
						ExternalIDs: map[string]string{"antrea-type": "gateway"},
					},
					{
						UUID:        "uuid3",
						Name:        "antrea-tun0",
						IFName:      "antrea-tun0",
						IFType:      "geneve",
						OFPort:      2,
						ExternalIDs: map[string]string{"antrea-type": "tunnel"},
						Options:     map[string]string{"csum": "true"},
					},
					{
						UUID:        "uuid4",
						Name:        "antrea-tc0",
						IFName:      "antrea-tc0",
						OFPort:      3,
						ExternalIDs: map[string]string{"antrea-type": "traffic-control"},
					},
					{
						UUID:        "uuid5",
						Name:        "unknown",
						IFName:      "unknown",
						OFPort:      100,
						ExternalIDs: map[string]string{"antrea-type": "unknown"},
					},
					{
						UUID:        "uuid6",
						Name:        "br-int",
						IFName:      "br-int",
						OFPort:      101,
						ExternalIDs: map[string]string{"antrea-type": "host"},
					},
				}, nil)
			},
			expectedInterfaces: []*interfacestore.InterfaceConfig{
				{
					Type:          interfacestore.ContainerInterface,
					InterfaceName: "pod1-abc",
					IPs:           []net.IP{net.ParseIP("192.168.0.9")},
					MAC:           ip.MustParseMAC("56:ba:da:72:6a:3a"),
					OVSPortConfig: &interfacestore.OVSPortConfig{
						PortUUID: "uuid1",
						OFPort:   11,
					},
					ContainerInterfaceConfig: &interfacestore.ContainerInterfaceConfig{
						ContainerID:  "container1",
						PodName:      "pod1",
						PodNamespace: "default",
					},
				},
				{
					Type:          interfacestore.GatewayInterface,
					InterfaceName: "antrea-gw0",
					OVSPortConfig: &interfacestore.OVSPortConfig{
						PortUUID: "uuid2",
						OFPort:   1,
					},
				},
				{
					Type:          interfacestore.TunnelInterface,
					InterfaceName: "antrea-tun0",
					OVSPortConfig: &interfacestore.OVSPortConfig{
						PortUUID: "uuid3",
						OFPort:   2,
					},
					TunnelInterfaceConfig: &interfacestore.TunnelInterfaceConfig{Csum: true, Type: ovsconfig.GeneveTunnel},
				},
				{
					Type:          interfacestore.TrafficControlInterface,
					InterfaceName: "antrea-tc0",
					OVSPortConfig: &interfacestore.OVSPortConfig{
						PortUUID: "uuid4",
						OFPort:   3,
					},
				},
			},
		},
		{
			name: "upgrade from unset interface type",
			expectedGetPortListCall: func(recorder *ovsconfigtest.MockOVSBridgeClientMockRecorder) {
				recorder.GetPortList().Return([]ovsconfig.OVSPortData{
					{
						UUID:   "uuid1",
						Name:   "pod1-abc",
						IFName: "pod1-abc",
						OFPort: 11,
						ExternalIDs: map[string]string{
							"attached-mac":  "56:ba:da:72:6a:3a",
							"container-id":  "container1",
							"ip-address":    "192.168.0.9",
							"pod-name":      "pod1",
							"pod-namespace": "default",
						},
					},
					{
						UUID:   "uuid2",
						Name:   "antrea-gw0",
						IFName: "antrea-gw0",
						OFPort: 2,
					},
					{
						UUID:    "uuid3",
						Name:    "antrea-tun0",
						IFName:  "antrea-tun0",
						IFType:  "geneve",
						OFPort:  1,
						Options: map[string]string{"csum": "false"},
					},
				}, nil)
				recorder.SetPortExternalIDs("pod1-abc", map[string]interface{}{
					"antrea-type":   "container",
					"attached-mac":  "56:ba:da:72:6a:3a",
					"container-id":  "container1",
					"ip-address":    "192.168.0.9",
					"pod-name":      "pod1",
					"pod-namespace": "default",
				})
				recorder.SetPortExternalIDs("antrea-gw0", map[string]interface{}{
					"antrea-type": "gateway",
				})
				recorder.SetPortExternalIDs("antrea-tun0", map[string]interface{}{
					"antrea-type": "tunnel",
				})
			},
			expectedInterfaces: []*interfacestore.InterfaceConfig{
				{
					Type:          interfacestore.ContainerInterface,
					InterfaceName: "pod1-abc",
					IPs:           []net.IP{net.ParseIP("192.168.0.9")},
					MAC:           ip.MustParseMAC("56:ba:da:72:6a:3a"),
					OVSPortConfig: &interfacestore.OVSPortConfig{
						PortUUID: "uuid1",
						OFPort:   11,
					},
					ContainerInterfaceConfig: &interfacestore.ContainerInterfaceConfig{
						ContainerID:  "container1",
						PodName:      "pod1",
						PodNamespace: "default",
					},
				},
				{
					Type:          interfacestore.GatewayInterface,
					InterfaceName: "antrea-gw0",
					OVSPortConfig: &interfacestore.OVSPortConfig{
						PortUUID: "uuid2",
						OFPort:   2,
					},
				},
				{
					Type:          interfacestore.TunnelInterface,
					InterfaceName: "antrea-tun0",
					OVSPortConfig: &interfacestore.OVSPortConfig{
						PortUUID: "uuid3",
						OFPort:   1,
					},
					TunnelInterfaceConfig: &interfacestore.TunnelInterfaceConfig{Csum: false, Type: ovsconfig.GeneveTunnel},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			controller := mock.NewController(t)
			defer controller.Finish()
			mockOVSBridgeClient := ovsconfigtest.NewMockOVSBridgeClient(controller)

			tt.expectedGetPortListCall(mockOVSBridgeClient.EXPECT())
			store := interfacestore.NewInterfaceStore()
			initializer := newAgentInitializer(mockOVSBridgeClient, store)
			err := initializer.initInterfaceStore()
			assert.Equal(t, tt.expectedErr, err)
			assert.Equal(t, len(tt.expectedInterfaces), store.Len())
			for _, expectedInterface := range tt.expectedInterfaces {
				actualInterface, exists := store.GetInterfaceByName(expectedInterface.InterfaceName)
				assert.True(t, exists)
				assert.Equal(t, expectedInterface, actualInterface)
			}
		})
	}
}

func TestInitialize(t *testing.T) {
	stopCh := make(chan struct{})
	defer close(stopCh)
	controller := mock.NewController(t)
	defer controller.Finish()
	defer mockDeleteStaleFlowsDelay(0)()
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "k8snode1",
		},
		Spec: corev1.NodeSpec{
			PodCIDR: "192.168.1.0/24",
		},
		Status: corev1.NodeStatus{
			Addresses: []corev1.NodeAddress{
				{
					Type:    corev1.NodeInternalIP,
					Address: "10.0.0.1",
				},
			},
		},
	}
	defer mockNodeNameEnv(node.Name)()
	defer mockGetIPNetDeviceFromIP(ip.MustParseCIDR("10.0.0.1/32"), &net.Interface{
		Index:        11,
		MTU:          1500,
		Name:         "ens192",
		HardwareAddr: ip.MustParseMAC("aa:bb:cc:dd:ee:ff"),
	})()
	defer mockSetLinkUp(ip.MustParseMAC("11:22:33:44:55:66"), 12, nil)()
	defer mockConfigureLinkAddresses(nil)()

	initializer := newFakeInitializer(controller, stopCh, node)
	initializer.mockOVSBridgeClient.EXPECT().Create()
	initializer.mockOVSCtlClient.EXPECT().GetDPFeatures().Return(map[ovsctl.DPFeature]bool{
		ovsctl.CTStateFeature:    true,
		ovsctl.CTZoneFeature:     true,
		ovsctl.CTMarkFeature:     true,
		ovsctl.CTLabelFeature:    true,
		ovsctl.CTStateNATFeature: true,
	}, nil)
	initializer.mockOVSBridgeClient.EXPECT().GetPortList().Return(nil, nil)
	initializer.mockOVSBridgeClient.EXPECT().CreateTunnelPortExt(defaultTunInterfaceName,
		ovsconfig.TunnelType(ovsconfig.GeneveTunnel),
		int32(config.DefaultTunOFPort),
		false,
		"",
		"",
		"",
		"",
		map[string]interface{}{},
		map[string]interface{}{interfacestore.AntreaInterfaceTypeKey: interfacestore.AntreaTunnel})
	initializer.mockOVSBridgeClient.EXPECT().GetOFPort(defaultTunInterfaceName, false).Return(int32(config.DefaultTunOFPort), nil)
	initializer.mockOVSBridgeClient.EXPECT().CreateInternalPort("antrea-gw0", int32(config.HostGatewayOFPort), "", map[string]interface{}{interfacestore.AntreaInterfaceTypeKey: interfacestore.AntreaGateway})
	initializer.mockOVSBridgeClient.EXPECT().GetOFPort("antrea-gw0", false).Return(int32(config.HostGatewayOFPort), nil)
	initializer.mockOVSBridgeClient.EXPECT().SetInterfaceMTU("antrea-gw0", 1450)
	initializer.mockOVSBridgeClient.EXPECT().GetOVSOtherConfig().Return(nil, nil)
	initializer.mockOVSBridgeClient.EXPECT().DeleteOVSOtherConfig(map[string]interface{}{
		"certificate": "", "private_key": "", "ca_cert": "", "remote_cert": "", "remote_name": "",
	})
	initializer.mockRouteClient.EXPECT().Initialize(mock.Any(), mock.Any())
	initializer.mockOVSBridgeClient.EXPECT().GetExternalIDs().Return(nil, nil).Times(2)

	initializer.mockOFClient.EXPECT().Initialize(types.RoundInfo{RoundNum: initialRoundNum}, mock.Any(), mock.Any(), mock.Any(), mock.Any())
	initializer.mockOFClient.EXPECT().DeleteStaleFlows()
	initializer.mockOVSBridgeClient.EXPECT().SetExternalIDs(map[string]interface{}{"roundNum": "1"})

	assert.NoError(t, initializer.Initialize())
	assert.Eventually(t, func() bool {
		return atomic.LoadUint32(&initializer.staleFlowsDeleted) == 1
	}, 2*time.Second, 100*time.Millisecond)
}

func TestInitOpenFlowPipeline(t *testing.T) {
	stopCh := make(chan struct{})
	defer close(stopCh)
	controller := mock.NewController(t)
	defer controller.Finish()
	defer mockDeleteStaleFlowsDelay(0)()

	initializer := newFakeInitializer(controller, stopCh, nil)
	prevRoundNum := uint64(2)
	initializer.mockOVSBridgeClient.EXPECT().GetExternalIDs().Return(map[string]string{"roundNum": "2"}, nil).Times(2)

	ofConnCh := make(chan struct{}, 1)
	initializer.mockOFClient.EXPECT().Initialize(types.RoundInfo{PrevRoundNum: &prevRoundNum, RoundNum: 3}, mock.Any(), mock.Any(), mock.Any(), mock.Any()).Return(ofConnCh, nil)
	initializer.mockOFClient.EXPECT().DeleteStaleFlows()
	initializer.mockOVSBridgeClient.EXPECT().SetExternalIDs(map[string]interface{}{"roundNum": "3"})
	assert.NoError(t, initializer.initOpenFlowPipeline())
	assert.Eventually(t, func() bool {
		return atomic.LoadUint32(&initializer.staleFlowsDeleted) == 1
	}, 2*time.Second, 100*time.Millisecond)

	// Trigger replaying flows
	ofConnCh <- struct{}{}
	initializer.mockOFClient.EXPECT().ReplayFlows()
	initializer.mockOVSBridgeClient.EXPECT().GetOVSOtherConfig().Return(map[string]string{"flow-restore-wait": "true"}, nil)
	initializer.mockOVSBridgeClient.EXPECT().DeleteOVSOtherConfig(map[string]interface{}{"flow-restore-wait": "true"})
	initializer.mockOVSBridgeClient.EXPECT().GetOVSOtherConfig().Return(map[string]string{}, nil)
	assert.Eventually(t, func() bool {
		return atomic.LoadUint32(&initializer.flowsReplayed) > 0
	}, 2*time.Second, 100*time.Millisecond)
}

func TestPersistRoundNum(t *testing.T) {
	const maxRetries = 3
	const roundNum uint64 = 5555

	controller := mock.NewController(t)
	defer controller.Finish()
	mockOVSBridgeClient := ovsconfigtest.NewMockOVSBridgeClient(controller)

	transactionError := ovsconfig.NewTransactionError(fmt.Errorf("Failed to get external IDs"), true)
	firstCall := mockOVSBridgeClient.EXPECT().GetExternalIDs().Return(nil, transactionError)
	externalIDs := make(map[string]string)
	mockOVSBridgeClient.EXPECT().GetExternalIDs().Return(externalIDs, nil).After(firstCall)
	newExternalIDs := make(map[string]interface{})
	newExternalIDs[roundNumKey] = fmt.Sprint(roundNum)
	mockOVSBridgeClient.EXPECT().SetExternalIDs(mock.Eq(newExternalIDs)).Times(1)

	// The first call to saveRoundNum will fail. Because we set the retry interval to 0,
	// persistRoundNum should retry immediately and the second call will succeed (as per the
	// expectations above).
	persistRoundNum(roundNum, mockOVSBridgeClient, 0, maxRetries)
}

func TestGetRoundInfo(t *testing.T) {
	controller := mock.NewController(t)
	defer controller.Finish()
	mockOVSBridgeClient := ovsconfigtest.NewMockOVSBridgeClient(controller)

	mockOVSBridgeClient.EXPECT().GetExternalIDs().Return(nil, ovsconfig.NewTransactionError(fmt.Errorf("Failed to get external IDs"), true))
	roundInfo := getRoundInfo(mockOVSBridgeClient)
	assert.Equal(t, uint64(initialRoundNum), roundInfo.RoundNum, "Unexpected round number")
	externalIDs := make(map[string]string)
	mockOVSBridgeClient.EXPECT().GetExternalIDs().Return(externalIDs, nil)
	roundInfo = getRoundInfo(mockOVSBridgeClient)
	assert.Equal(t, uint64(initialRoundNum), roundInfo.RoundNum, "Unexpected round number")
}

func TestInitNodeLocalConfig(t *testing.T) {
	nodeName := "node1"
	ovsBridge := "br-int"
	nodeIPStr := "192.168.10.10"
	_, nodeIPNet, _ := net.ParseCIDR("192.168.10.10/24")
	macAddr, _ := net.ParseMAC("00:00:5e:00:53:01")
	ipDevice := &net.Interface{
		Index:        10,
		MTU:          1500,
		Name:         "ens160",
		HardwareAddr: macAddr,
	}
	podCIDRStr := "172.16.10.0/24"
	transportCIDRs := []string{"172.16.100.7/24", "2002:1a23:fb46::11:3/32"}
	_, podCIDR, _ := net.ParseCIDR(podCIDRStr)
	transportIfaceMAC, _ := net.ParseMAC("00:0c:29:f5:e2:ce")
	type testTransInterface struct {
		iface   *net.Interface
		ipV4Net *net.IPNet
		ipV6Net *net.IPNet
	}
	testTransportIface := &testTransInterface{
		iface: &net.Interface{
			Index:        11,
			MTU:          1500,
			Name:         "ens192",
			HardwareAddr: transportIfaceMAC,
		},
	}
	for _, cidr := range transportCIDRs {
		parsedIP, parsedIPNet, _ := net.ParseCIDR(cidr)
		parsedIPNet.IP = parsedIP
		if parsedIP.To4() != nil {
			testTransportIface.ipV4Net = parsedIPNet
		} else {
			testTransportIface.ipV6Net = parsedIPNet
		}
	}
	transportAddresses := strings.Join([]string{testTransportIface.ipV4Net.IP.String(), testTransportIface.ipV6Net.IP.String()}, ",")
	tests := []struct {
		name                      string
		trafficEncapMode          config.TrafficEncapModeType
		transportIfName           string
		transportIfCIDRs          []string
		transportInterface        *testTransInterface
		tunnelType                ovsconfig.TunnelType
		mtu                       int
		expectedMTU               int
		expectedNodeLocalIfaceMTU int
		expectedNodeAnnotation    map[string]string
	}{
		{
			name:                      "noencap mode",
			trafficEncapMode:          config.TrafficEncapModeNoEncap,
			mtu:                       0,
			expectedNodeLocalIfaceMTU: 1500,
			expectedMTU:               1500,
			expectedNodeAnnotation:    map[string]string{types.NodeMACAddressAnnotationKey: macAddr.String()},
		},
		{
			name:                      "hybrid mode",
			trafficEncapMode:          config.TrafficEncapModeHybrid,
			mtu:                       0,
			expectedNodeLocalIfaceMTU: 1500,
			expectedMTU:               1500,
			expectedNodeAnnotation:    map[string]string{types.NodeMACAddressAnnotationKey: macAddr.String()},
		},
		{
			name:                      "encap mode, geneve tunnel",
			trafficEncapMode:          config.TrafficEncapModeEncap,
			tunnelType:                ovsconfig.GeneveTunnel,
			mtu:                       0,
			expectedNodeLocalIfaceMTU: 1500,
			expectedMTU:               1450,
			expectedNodeAnnotation:    nil,
		},
		{
			name:                      "encap mode, mtu specified",
			trafficEncapMode:          config.TrafficEncapModeEncap,
			tunnelType:                ovsconfig.GeneveTunnel,
			mtu:                       1400,
			expectedNodeLocalIfaceMTU: 1500,
			expectedMTU:               1400,
			expectedNodeAnnotation:    nil,
		},
		{
			name:                      "noencap mode with transportInterface",
			trafficEncapMode:          config.TrafficEncapModeNoEncap,
			transportIfName:           testTransportIface.iface.Name,
			transportInterface:        testTransportIface,
			mtu:                       0,
			expectedNodeLocalIfaceMTU: 1500,
			expectedMTU:               1500,
			expectedNodeAnnotation: map[string]string{
				types.NodeMACAddressAnnotationKey:       transportIfaceMAC.String(),
				types.NodeTransportAddressAnnotationKey: transportAddresses,
			},
		},
		{
			name:                      "hybrid mode with transportInterface",
			trafficEncapMode:          config.TrafficEncapModeHybrid,
			transportIfName:           testTransportIface.iface.Name,
			transportInterface:        testTransportIface,
			mtu:                       0,
			expectedNodeLocalIfaceMTU: 1500,
			expectedMTU:               1500,
			expectedNodeAnnotation: map[string]string{
				types.NodeMACAddressAnnotationKey:       transportIfaceMAC.String(),
				types.NodeTransportAddressAnnotationKey: transportAddresses,
			},
		},
		{
			name:                      "encap mode with transportInterface, geneve tunnel",
			trafficEncapMode:          config.TrafficEncapModeEncap,
			transportIfName:           testTransportIface.iface.Name,
			transportInterface:        testTransportIface,
			tunnelType:                ovsconfig.GeneveTunnel,
			mtu:                       0,
			expectedNodeLocalIfaceMTU: 1500,
			expectedMTU:               1450,
			expectedNodeAnnotation: map[string]string{
				types.NodeTransportAddressAnnotationKey: transportAddresses,
			},
		},
		{
			name:                      "encap mode with transportInterface, mtu specified",
			trafficEncapMode:          config.TrafficEncapModeEncap,
			transportIfName:           testTransportIface.iface.Name,
			transportInterface:        testTransportIface,
			tunnelType:                ovsconfig.GeneveTunnel,
			mtu:                       1400,
			expectedNodeLocalIfaceMTU: 1500,
			expectedMTU:               1400,
			expectedNodeAnnotation: map[string]string{
				types.NodeTransportAddressAnnotationKey: transportAddresses,
			},
		},
		{
			name:                      "noencap mode with transportInterfaceCIDRs",
			trafficEncapMode:          config.TrafficEncapModeNoEncap,
			transportIfCIDRs:          transportCIDRs,
			transportInterface:        testTransportIface,
			mtu:                       0,
			expectedNodeLocalIfaceMTU: 1500,
			expectedMTU:               1500,
			expectedNodeAnnotation: map[string]string{
				types.NodeMACAddressAnnotationKey:       transportIfaceMAC.String(),
				types.NodeTransportAddressAnnotationKey: transportAddresses,
			},
		},
		{
			name:                      "hybrid mode with transportInterfaceCIDRs",
			trafficEncapMode:          config.TrafficEncapModeHybrid,
			transportIfCIDRs:          transportCIDRs,
			transportInterface:        testTransportIface,
			mtu:                       0,
			expectedNodeLocalIfaceMTU: 1500,
			expectedMTU:               1500,
			expectedNodeAnnotation: map[string]string{
				types.NodeMACAddressAnnotationKey:       transportIfaceMAC.String(),
				types.NodeTransportAddressAnnotationKey: transportAddresses,
			},
		},
		{
			name:                      "encap mode with transportInterfaceCIDRs, geneve tunnel",
			trafficEncapMode:          config.TrafficEncapModeEncap,
			transportIfCIDRs:          transportCIDRs,
			transportInterface:        testTransportIface,
			tunnelType:                ovsconfig.GeneveTunnel,
			mtu:                       0,
			expectedNodeLocalIfaceMTU: 1500,
			expectedMTU:               1450,
			expectedNodeAnnotation: map[string]string{
				types.NodeTransportAddressAnnotationKey: transportAddresses,
			},
		},
		{
			name:                      "encap mode with transportInterfaceCIDRs, mtu specified",
			trafficEncapMode:          config.TrafficEncapModeEncap,
			transportIfCIDRs:          transportCIDRs,
			transportInterface:        testTransportIface,
			tunnelType:                ovsconfig.GeneveTunnel,
			mtu:                       1400,
			expectedNodeLocalIfaceMTU: 1500,
			expectedMTU:               1400,
			expectedNodeAnnotation: map[string]string{
				types.NodeTransportAddressAnnotationKey: transportAddresses,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: nodeName,
				},
				Spec: corev1.NodeSpec{
					PodCIDR:  podCIDRStr,
					PodCIDRs: []string{podCIDRStr},
				},
				Status: corev1.NodeStatus{
					Addresses: []corev1.NodeAddress{
						{
							Type:    corev1.NodeInternalIP,
							Address: nodeIPStr,
						},
					},
				},
			}
			client := fake.NewSimpleClientset(node)
			ifaceStore := interfacestore.NewInterfaceStore()
			expectedNodeConfig := config.NodeConfig{
				Name:                       nodeName,
				Type:                       config.K8sNode,
				OVSBridge:                  ovsBridge,
				DefaultTunName:             defaultTunInterfaceName,
				PodIPv4CIDR:                podCIDR,
				NodeIPv4Addr:               nodeIPNet,
				NodeTransportInterfaceName: ipDevice.Name,
				NodeTransportIPv4Addr:      nodeIPNet,
				NodeTransportInterfaceMTU:  tt.expectedNodeLocalIfaceMTU,
				NodeMTU:                    tt.expectedMTU,
				UplinkNetConfig:            new(config.AdapterNetConfig),
			}

			initializer := &Initializer{
				client:     client,
				ifaceStore: ifaceStore,
				mtu:        tt.mtu,
				ovsBridge:  ovsBridge,
				networkConfig: &config.NetworkConfig{
					TrafficEncapMode: tt.trafficEncapMode,
					TunnelType:       tt.tunnelType,
				},
			}
			if tt.transportIfName != "" {
				initializer.networkConfig.TransportIface = tt.transportInterface.iface.Name
				expectedNodeConfig.NodeTransportInterfaceName = tt.transportInterface.iface.Name
				expectedNodeConfig.NodeTransportIPv4Addr = tt.transportInterface.ipV4Net
				expectedNodeConfig.NodeTransportIPv6Addr = tt.transportInterface.ipV6Net
				defer mockGetTransportIPNetDeviceByName(tt.transportInterface.ipV4Net, tt.transportInterface.ipV6Net, tt.transportInterface.iface)()
			} else if len(tt.transportIfCIDRs) > 0 {
				initializer.networkConfig.TransportIfaceCIDRs = tt.transportIfCIDRs
				expectedNodeConfig.NodeTransportInterfaceName = tt.transportInterface.iface.Name
				expectedNodeConfig.NodeTransportIPv4Addr = tt.transportInterface.ipV4Net
				expectedNodeConfig.NodeTransportIPv6Addr = tt.transportInterface.ipV6Net
				defer mockGetIPNetDeviceByCIDRs(tt.transportInterface.ipV4Net, tt.transportInterface.ipV6Net, tt.transportInterface.iface)()
			}
			defer mockGetIPNetDeviceFromIP(nodeIPNet, ipDevice)()
			defer mockNodeNameEnv(nodeName)()

			require.NoError(t, initializer.initK8sNodeLocalConfig(nodeName))
			assert.Equal(t, expectedNodeConfig, *initializer.nodeConfig)
			node, err := client.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
			require.NoError(t, err)
			assert.Equal(t, tt.expectedNodeAnnotation, node.Annotations)
		})
	}
}

func mockGetIPNetDeviceFromIP(ipNet *net.IPNet, ipDevice *net.Interface) func() {
	prevGetIPNetDeviceFromIP := getIPNetDeviceFromIP
	getIPNetDeviceFromIP = func(localIP *ip.DualStackIPs, ignoredHostInterfaces sets.String) (*net.IPNet, *net.IPNet, *net.Interface, error) {
		return ipNet, nil, ipDevice, nil
	}
	return func() { getIPNetDeviceFromIP = prevGetIPNetDeviceFromIP }
}

func mockNodeNameEnv(name string) func() {
	_ = os.Setenv(env.NodeNameEnvKey, name)
	return func() { os.Unsetenv(env.NodeNameEnvKey) }
}

func mockDeleteStaleFlowsDelay(duration time.Duration) func() {
	preDeleteStaleFlowsDelay := deleteStaleFlowsDelay
	deleteStaleFlowsDelay = duration
	return func() { deleteStaleFlowsDelay = preDeleteStaleFlowsDelay }
}

func mockSetLinkUp(mac net.HardwareAddr, index int, err error) func() {
	prevSetLinkUp := setLinkUp
	setLinkUp = func(name string) (net.HardwareAddr, int, error) {
		return mac, index, err
	}
	return func() { setLinkUp = prevSetLinkUp }
}

func mockConfigureLinkAddresses(err error) func() {
	preConfigureLinkAddresses := configureLinkAddresses
	configureLinkAddresses = func(idx int, ipNets []*net.IPNet) error {
		return err
	}
	return func() { configureLinkAddresses = preConfigureLinkAddresses }
}

func mockGetTransportIPNetDeviceByName(ipV4Net, ipV6Net *net.IPNet, ipDevice *net.Interface) func() {
	prevGetIPNetDeviceByName := getTransportIPNetDeviceByName
	getTransportIPNetDeviceByName = func(ifName, brName string) (*net.IPNet, *net.IPNet, *net.Interface, error) {
		return ipV4Net, ipV6Net, ipDevice, nil
	}
	return func() { getTransportIPNetDeviceByName = prevGetIPNetDeviceByName }
}

func mockGetIPNetDeviceByCIDRs(ipV4Net, ipV6Net *net.IPNet, ipDevice *net.Interface) func() {
	prevGetIPNetDeviceByCIDRs := getIPNetDeviceByCIDRs
	getIPNetDeviceByCIDRs = func(cidr []string) (*net.IPNet, *net.IPNet, *net.Interface, error) {
		return ipV4Net, ipV6Net, ipDevice, nil
	}
	return func() { getIPNetDeviceByCIDRs = prevGetIPNetDeviceByCIDRs }
}

func TestSetupDefaultTunnelInterface(t *testing.T) {
	_, nodeIPNet, _ := net.ParseCIDR("192.168.10.10/24")
	var tunnelPortLocalIP net.IP
	var tunnelPortLocalIPStr string
	if runtime.IsWindowsPlatform() {
		tunnelPortLocalIP = nodeIPNet.IP
		tunnelPortLocalIPStr = tunnelPortLocalIP.String()
	}
	tests := []struct {
		name                    string
		nodeConfig              *config.NodeConfig
		networkConfig           *config.NetworkConfig
		existingTunnelInterface *interfacestore.InterfaceConfig
		expectedOVSCalls        func(client *ovsconfigtest.MockOVSBridgeClientMockRecorder)
		expectedErr             error
	}{
		{
			name: "create default Geneve tunnel",
			nodeConfig: &config.NodeConfig{
				DefaultTunName:        defaultTunInterfaceName,
				NodeTransportIPv4Addr: nodeIPNet,
			},
			networkConfig: &config.NetworkConfig{
				TrafficEncapMode: config.TrafficEncapModeEncap,
				TunnelType:       ovsconfig.GeneveTunnel,
			},
			expectedOVSCalls: func(client *ovsconfigtest.MockOVSBridgeClientMockRecorder) {
				client.CreateTunnelPortExt(defaultTunInterfaceName,
					ovsconfig.TunnelType(ovsconfig.GeneveTunnel),
					int32(config.DefaultTunOFPort),
					false,
					tunnelPortLocalIPStr,
					"",
					"",
					"",
					map[string]interface{}{},
					map[string]interface{}{interfacestore.AntreaInterfaceTypeKey: interfacestore.AntreaTunnel})
				client.GetOFPort(defaultTunInterfaceName, false)
			},
		},
		{
			name: "update Geneve tunnel csum",
			nodeConfig: &config.NodeConfig{
				DefaultTunName:        defaultTunInterfaceName,
				NodeTransportIPv4Addr: nodeIPNet,
			},
			networkConfig: &config.NetworkConfig{
				TrafficEncapMode: config.TrafficEncapModeEncap,
				TunnelType:       ovsconfig.GeneveTunnel,
				TunnelCsum:       false,
			},
			existingTunnelInterface: interfacestore.NewTunnelInterface(defaultTunInterfaceName, ovsconfig.GeneveTunnel, 0, tunnelPortLocalIP, true, &interfacestore.OVSPortConfig{OFPort: 1}),
			expectedOVSCalls: func(client *ovsconfigtest.MockOVSBridgeClientMockRecorder) {
				client.GetInterfaceOptions(defaultTunInterfaceName).Return(map[string]string{"csum": "true"}, nil)
				client.SetInterfaceOptions(defaultTunInterfaceName, map[string]interface{}{"csum": "false"})
			},
		},
		{
			name: "update tunnel type and port",
			nodeConfig: &config.NodeConfig{
				DefaultTunName:        defaultTunInterfaceName,
				NodeTransportIPv4Addr: nodeIPNet,
			},
			networkConfig: &config.NetworkConfig{
				TrafficEncapMode: config.TrafficEncapModeEncap,
				TunnelType:       ovsconfig.VXLANTunnel,
				TunnelPort:       9999,
			},
			existingTunnelInterface: interfacestore.NewTunnelInterface(defaultTunInterfaceName, ovsconfig.GeneveTunnel, 0, tunnelPortLocalIP, true, &interfacestore.OVSPortConfig{
				PortUUID: "foo",
				OFPort:   1,
			}),
			expectedOVSCalls: func(client *ovsconfigtest.MockOVSBridgeClientMockRecorder) {
				client.DeletePort("foo")
				client.CreateTunnelPortExt(defaultTunInterfaceName,
					ovsconfig.TunnelType(ovsconfig.VXLANTunnel),
					int32(config.DefaultTunOFPort),
					false,
					tunnelPortLocalIPStr,
					"",
					"",
					"",
					map[string]interface{}{"dst_port": "9999"},
					map[string]interface{}{interfacestore.AntreaInterfaceTypeKey: interfacestore.AntreaTunnel})
				client.GetOFPort(defaultTunInterfaceName, false)
			},
		},
		{
			name: "no change",
			nodeConfig: &config.NodeConfig{
				DefaultTunName:        defaultTunInterfaceName,
				NodeTransportIPv4Addr: nodeIPNet,
			},
			networkConfig: &config.NetworkConfig{
				TrafficEncapMode: config.TrafficEncapModeEncap,
				TunnelType:       ovsconfig.GeneveTunnel,
				TunnelCsum:       false,
			},
			existingTunnelInterface: interfacestore.NewTunnelInterface(defaultTunInterfaceName, ovsconfig.GeneveTunnel, 0, tunnelPortLocalIP, false, &interfacestore.OVSPortConfig{OFPort: 1}),
			expectedOVSCalls:        func(client *ovsconfigtest.MockOVSBridgeClientMockRecorder) {},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			controller := mock.NewController(t)
			defer controller.Finish()
			mockOVSBridgeClient := ovsconfigtest.NewMockOVSBridgeClient(controller)
			client := fake.NewSimpleClientset()
			ifaceStore := interfacestore.NewInterfaceStore()
			if tt.existingTunnelInterface != nil {
				ifaceStore.AddInterface(tt.existingTunnelInterface)
			}
			initializer := &Initializer{
				client:          client,
				ifaceStore:      ifaceStore,
				ovsBridgeClient: mockOVSBridgeClient,
				ovsBridge:       "br-int",
				networkConfig:   tt.networkConfig,
				nodeConfig:      tt.nodeConfig,
			}
			tt.expectedOVSCalls(mockOVSBridgeClient.EXPECT())
			err := initializer.setupDefaultTunnelInterface()
			assert.Equal(t, err, tt.expectedErr)
		})
	}
}

func TestValidateSupportedDPFeatures(t *testing.T) {
	tests := []struct {
		name               string
		antreaProxyEnabled bool
		gotDPFeatures      map[ovsctl.DPFeature]bool
		gotError           error
		expectedErr        error
	}{
		{
			name:               "all features supported",
			antreaProxyEnabled: true,
			gotDPFeatures: map[ovsctl.DPFeature]bool{
				ovsctl.CTStateFeature:    true,
				ovsctl.CTZoneFeature:     true,
				ovsctl.CTMarkFeature:     true,
				ovsctl.CTLabelFeature:    true,
				ovsctl.CTStateNATFeature: true,
			},
			expectedErr: nil,
		},
		{
			name:               "CTStateNATFeature not supported",
			antreaProxyEnabled: true,
			gotDPFeatures: map[ovsctl.DPFeature]bool{
				ovsctl.CTStateFeature:    true,
				ovsctl.CTZoneFeature:     true,
				ovsctl.CTMarkFeature:     true,
				ovsctl.CTLabelFeature:    true,
				ovsctl.CTStateNATFeature: false,
			},
			expectedErr: fmt.Errorf("the required OVS DP feature '%s' is not supported", ovsctl.CTStateNATFeature),
		},
		{
			name: "CTMarkFeature not found",
			gotDPFeatures: map[ovsctl.DPFeature]bool{
				ovsctl.CTStateFeature: true,
				ovsctl.CTZoneFeature:  true,
				ovsctl.CTLabelFeature: true,
			},
			expectedErr: fmt.Errorf("the required OVS DP feature '%s' support is unknown", ovsctl.CTMarkFeature),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer featuregatetesting.SetFeatureGateDuringTest(t, features.DefaultFeatureGate, features.AntreaProxy, tt.antreaProxyEnabled)()

			controller := mock.NewController(t)
			defer controller.Finish()
			ovsctlClient := ovsctltest.NewMockOVSCtlClient(controller)
			initializer := &Initializer{
				ovsctlClient: ovsctlClient,
			}
			ovsctlClient.EXPECT().GetDPFeatures().Return(tt.gotDPFeatures, tt.gotError)
			err := initializer.validateSupportedDPFeatures()
			assert.Equal(t, tt.expectedErr, err)
		})
	}
}

func TestSetOVSDatapath(t *testing.T) {
	errOVSNotConnected := ovsconfig.NewTransactionError(fmt.Errorf("OVS not connected"), true)
	tests := []struct {
		name                         string
		expectedOVSBridgeClientCalls func(ovsBridgeClient *ovsconfigtest.MockOVSBridgeClientMockRecorder)
		expectedErr                  error
	}{
		{
			name: "GetOVSOtherConfig failure",
			expectedOVSBridgeClientCalls: func(ovsBridgeClient *ovsconfigtest.MockOVSBridgeClientMockRecorder) {
				ovsBridgeClient.GetOVSOtherConfig().Return(nil, errOVSNotConnected)
			},
			expectedErr: errOVSNotConnected,
		},
		{
			name: "datapath set",
			expectedOVSBridgeClientCalls: func(ovsBridgeClient *ovsconfigtest.MockOVSBridgeClientMockRecorder) {
				ovsBridgeClient.GetOVSOtherConfig().Return(map[string]string{ovsconfig.OVSOtherConfigDatapathIDKey: "foo"}, nil)
			},
			expectedErr: nil,
		},
		{
			name: "datapath unset",
			expectedOVSBridgeClientCalls: func(ovsBridgeClient *ovsconfigtest.MockOVSBridgeClientMockRecorder) {
				ovsBridgeClient.GetOVSOtherConfig().Return(map[string]string{}, nil)
				ovsBridgeClient.SetDatapathID(mock.Any())
			},
			expectedErr: nil,
		},
		{
			name: "SetDatapathID failure",
			expectedOVSBridgeClientCalls: func(ovsBridgeClient *ovsconfigtest.MockOVSBridgeClientMockRecorder) {
				ovsBridgeClient.GetOVSOtherConfig().Return(map[string]string{}, nil)
				ovsBridgeClient.SetDatapathID(mock.Any()).Return(errOVSNotConnected)
			},
			expectedErr: errOVSNotConnected,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stopCh := make(chan struct{})
			defer close(stopCh)
			controller := mock.NewController(t)
			defer controller.Finish()

			initializer := newFakeInitializer(controller, stopCh, nil)
			tt.expectedOVSBridgeClientCalls(initializer.mockOVSBridgeClient.EXPECT())
			assert.Equal(t, tt.expectedErr, initializer.setOVSDatapath())
		})
	}

}
