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

package ipallocator

import (
	"fmt"
	"math/big"
	"net"

	utilnet "k8s.io/utils/net"
)

type Allocator interface {
	AllocateIP(ip net.IP) error

	AllocateNext() (net.IP, error)

	Release(ip net.IP) error

	Contains(ip net.IP) bool

	Used() int
}

var _ Allocator = &IPSetAllocator{}

type IPSetAllocator struct {
	ipSetStr string
	// base is a cached version of the start IP in the CIDR range as a *big.Int
	base *big.Int
	// max is the maximum size of the usable addresses in the range
	max int
	// allocated is a bit array of the allocated items in the range
	allocated *big.Int
	// count is the number of currently allocated elements in the range
	count int
}

func NewCIDRAllocator(cidr string) (*IPSetAllocator, error) {
	ip, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, err
	}
	base := utilnet.BigForIP(ip)
	// Start from "x.x.x.1".
	base.Add(base, big.NewInt(1))
	max := utilnet.RangeSize(ipNet) - 1
	if ip.To4() != nil {
		// Don't use the IPv4 network's broadcast address.
		max--
	}
	if max < 0 {
		return nil, fmt.Errorf("no available IP in %s", cidr)
	}
	// In case a big range occupies too much memory, allow at most 65536 IP for each IP set.
	if max > 65536 {
		max = 65536
	}

	allocator := &IPSetAllocator{
		ipSetStr:  cidr,
		base:      base,
		max:       int(max),
		allocated: big.NewInt(0),
		count:     0,
	}
	return allocator, nil
}

func NewIPRangeAllocator(startIP, endIP string) (*IPSetAllocator, error) {
	ipSetStr := fmt.Sprintf("%s-%s", startIP, endIP)
	ip := net.ParseIP(startIP)
	if ip == nil {
		return nil, fmt.Errorf("invalid start IP %s", startIP)
	}
	base := utilnet.BigForIP(ip)
	ip = net.ParseIP(endIP)
	if ip == nil {
		return nil, fmt.Errorf("invalid end IP %s", endIP)
	}
	offset := big.NewInt(0).Sub(utilnet.BigForIP(ip), base).Int64()
	if offset < 0 {
		return nil, fmt.Errorf("invalid IP range %s", ipSetStr)
	}
	max := offset + 1
	// In case a big range occupies too much memory, allow at most 65536 IP for each IP set.
	if max > 65536 {
		max = 65536
	}

	allocator := &IPSetAllocator{
		ipSetStr:  ipSetStr,
		base:      base,
		max:       int(max),
		allocated: big.NewInt(0),
		count:     0,
	}
	return allocator, nil
}

func (a *IPSetAllocator) AllocateIP(ip net.IP) error {
	offset := int(big.NewInt(0).Sub(utilnet.BigForIP(ip), a.base).Int64())
	if offset < 0 || offset >= a.max {
		return fmt.Errorf("IP %v is not in the range of ipset %s", ip, a.ipSetStr)
	}
	if a.allocated.Bit(offset) == 1 {
		return fmt.Errorf("IP %v is already allocated", ip)
	}
	a.allocated.SetBit(a.allocated, offset, 1)
	a.count++
	return nil
}

func (a *IPSetAllocator) AllocateNext() (net.IP, error) {
	if a.count >= a.max {
		return nil, fmt.Errorf("no available IP")
	}
	for i := 0; i < a.max; i++ {
		if a.allocated.Bit(i) == 0 {
			a.allocated.SetBit(a.allocated, i, 1)
			a.count++
			ip := utilnet.AddIPOffset(a.base, i)
			return ip, nil
		}
	}
	return nil, fmt.Errorf("no available IP")
}

func (a *IPSetAllocator) Release(ip net.IP) error {
	offset := int(big.NewInt(0).Sub(utilnet.BigForIP(ip), a.base).Int64())
	if offset < 0 || offset >= a.max {
		return fmt.Errorf("IP %v is not in the range of ipset %s", ip, a.ipSetStr)
	}
	if a.allocated.Bit(offset) == 0 {
		return nil
	}
	a.allocated.SetBit(a.allocated, offset, 0)
	a.count--
	return nil
}

func (a *IPSetAllocator) Contains(ip net.IP) bool {
	offset := int(big.NewInt(0).Sub(utilnet.BigForIP(ip), a.base).Int64())
	return offset >= 0 && offset < a.max
}

func (a *IPSetAllocator) Used() int {
	return a.count
}

var _ Allocator = MultiIPSetAllocator{}

type MultiIPSetAllocator []*IPSetAllocator

func (ma MultiIPSetAllocator) AllocateIP(ip net.IP) error {
	for _, a := range ma {
		if err := a.AllocateIP(ip); err == nil {
			return nil
		}
	}
	return fmt.Errorf("cannot allocate IP %v in any ipset", ip)
}

func (ma MultiIPSetAllocator) AllocateNext() (net.IP, error) {
	for _, a := range ma {
		if ip, err := a.AllocateNext(); err == nil {
			return ip, nil
		}
	}
	return nil, fmt.Errorf("cannot allocate IP in any ipset")
}

func (ma MultiIPSetAllocator) Release(ip net.IP) error {
	for _, a := range ma {
		if err := a.Release(ip); err == nil {
			return nil
		}
	}
	return fmt.Errorf("cannot release IP in any ipset")
}

func (ma MultiIPSetAllocator) Contains(ip net.IP) bool {
	for _, a := range ma {
		if a.Contains(ip) {
			return true
		}
	}
	return false
}

func (ma MultiIPSetAllocator) Used() int {
	used := 0
	for _, a := range ma {
		used += a.Used()
	}
	return used
}
