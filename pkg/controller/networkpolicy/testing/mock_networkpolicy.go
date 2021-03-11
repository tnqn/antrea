// Copyright 2022 Antrea Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//

// Code generated by MockGen. DO NOT EDIT.
// Source: antrea.io/antrea/pkg/controller/networkpolicy (interfaces: EndpointQuerier,NetworkPolicyUsageReporter)

// Package testing is a generated GoMock package.
package testing

import (
	networkpolicy "antrea.io/antrea/pkg/controller/networkpolicy"
	gomock "github.com/golang/mock/gomock"
	reflect "reflect"
)

// MockEndpointQuerier is a mock of EndpointQuerier interface
type MockEndpointQuerier struct {
	ctrl     *gomock.Controller
	recorder *MockEndpointQuerierMockRecorder
}

// MockEndpointQuerierMockRecorder is the mock recorder for MockEndpointQuerier
type MockEndpointQuerierMockRecorder struct {
	mock *MockEndpointQuerier
}

// NewMockEndpointQuerier creates a new mock instance
func NewMockEndpointQuerier(ctrl *gomock.Controller) *MockEndpointQuerier {
	mock := &MockEndpointQuerier{ctrl: ctrl}
	mock.recorder = &MockEndpointQuerierMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use
func (m *MockEndpointQuerier) EXPECT() *MockEndpointQuerierMockRecorder {
	return m.recorder
}

// QueryNetworkPolicies mocks base method
func (m *MockEndpointQuerier) QueryNetworkPolicies(arg0, arg1 string) (*networkpolicy.EndpointQueryResponse, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "QueryNetworkPolicies", arg0, arg1)
	ret0, _ := ret[0].(*networkpolicy.EndpointQueryResponse)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// QueryNetworkPolicies indicates an expected call of QueryNetworkPolicies
func (mr *MockEndpointQuerierMockRecorder) QueryNetworkPolicies(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "QueryNetworkPolicies", reflect.TypeOf((*MockEndpointQuerier)(nil).QueryNetworkPolicies), arg0, arg1)
}

// MockNetworkPolicyUsageReporter is a mock of NetworkPolicyUsageReporter interface
type MockNetworkPolicyUsageReporter struct {
	ctrl     *gomock.Controller
	recorder *MockNetworkPolicyUsageReporterMockRecorder
}

// MockNetworkPolicyUsageReporterMockRecorder is the mock recorder for MockNetworkPolicyUsageReporter
type MockNetworkPolicyUsageReporterMockRecorder struct {
	mock *MockNetworkPolicyUsageReporter
}

// NewMockNetworkPolicyUsageReporter creates a new mock instance
func NewMockNetworkPolicyUsageReporter(ctrl *gomock.Controller) *MockNetworkPolicyUsageReporter {
	mock := &MockNetworkPolicyUsageReporter{ctrl: ctrl}
	mock.recorder = &MockNetworkPolicyUsageReporterMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use
func (m *MockNetworkPolicyUsageReporter) EXPECT() *MockNetworkPolicyUsageReporterMockRecorder {
	return m.recorder
}

// GetNumAntreaClusterNetworkPolicies mocks base method
func (m *MockNetworkPolicyUsageReporter) GetNumAntreaClusterNetworkPolicies() (int, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetNumAntreaClusterNetworkPolicies")
	ret0, _ := ret[0].(int)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetNumAntreaClusterNetworkPolicies indicates an expected call of GetNumAntreaClusterNetworkPolicies
func (mr *MockNetworkPolicyUsageReporterMockRecorder) GetNumAntreaClusterNetworkPolicies() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetNumAntreaClusterNetworkPolicies", reflect.TypeOf((*MockNetworkPolicyUsageReporter)(nil).GetNumAntreaClusterNetworkPolicies))
}

// GetNumAntreaNetworkPolicies mocks base method
func (m *MockNetworkPolicyUsageReporter) GetNumAntreaNetworkPolicies() (int, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetNumAntreaNetworkPolicies")
	ret0, _ := ret[0].(int)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetNumAntreaNetworkPolicies indicates an expected call of GetNumAntreaNetworkPolicies
func (mr *MockNetworkPolicyUsageReporterMockRecorder) GetNumAntreaNetworkPolicies() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetNumAntreaNetworkPolicies", reflect.TypeOf((*MockNetworkPolicyUsageReporter)(nil).GetNumAntreaNetworkPolicies))
}

// GetNumNetworkPolicies mocks base method
func (m *MockNetworkPolicyUsageReporter) GetNumNetworkPolicies() (int, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetNumNetworkPolicies")
	ret0, _ := ret[0].(int)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetNumNetworkPolicies indicates an expected call of GetNumNetworkPolicies
func (mr *MockNetworkPolicyUsageReporterMockRecorder) GetNumNetworkPolicies() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetNumNetworkPolicies", reflect.TypeOf((*MockNetworkPolicyUsageReporter)(nil).GetNumNetworkPolicies))
}

// GetNumTiers mocks base method
func (m *MockNetworkPolicyUsageReporter) GetNumTiers() (int, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetNumTiers")
	ret0, _ := ret[0].(int)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetNumTiers indicates an expected call of GetNumTiers
func (mr *MockNetworkPolicyUsageReporterMockRecorder) GetNumTiers() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetNumTiers", reflect.TypeOf((*MockNetworkPolicyUsageReporter)(nil).GetNumTiers))
}
