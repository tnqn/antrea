package ipallocator

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net"
	"testing"
)

func TestAllocateNext(t *testing.T) {
	tests := []struct {
		name      string
		ipSetStr  string
		wantNum   int
		wantFirst net.IP
		wantLast  net.IP
	}{
		{
			name:      "IPv4-24",
			ipSetStr:  "10.10.10.0/24",
			wantNum:   254,
			wantFirst: net.ParseIP("10.10.10.1"),
			wantLast:  net.ParseIP("10.10.10.254"),
		},
		{
			name:      "IPv4-30",
			ipSetStr:  "10.10.10.128/30",
			wantNum:   2,
			wantFirst: net.ParseIP("10.10.10.129"),
			wantLast:  net.ParseIP("10.10.10.130"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, err := NewCIDRAllocator(tt.ipSetStr)
			require.NoError(t, err)
			gotFirst, err := a.AllocateNext()
			require.NoError(t, err)
			assert.Equal(t, tt.wantFirst, gotFirst)
			for i := 0; i < tt.wantNum-2; i++ {
				_, err := a.AllocateNext()
				require.NoError(t, err)
			}
			gotLast, err := a.AllocateNext()
			require.NoError(t, err)
			assert.Equal(t, tt.wantLast, gotLast)

			_, err = a.AllocateNext()
			require.Error(t, err)
		})
	}
}

func TestAllocateIP(t *testing.T) {
	tests := []struct {
		name         string
		ipSetStr     string
		allocatedIP1 net.IP
		allocatedIP2 net.IP
		wantErr      bool
	}{
		{
			name:         "IPv4-duplicate",
			ipSetStr:     "10.10.10.0/24",
			allocatedIP1: net.ParseIP("10.10.10.1"),
			allocatedIP2: net.ParseIP("10.10.10.1"),
			wantErr:      true,
		},
		{
			name:         "IPv4-no-duplicate",
			ipSetStr:     "10.10.10.0/24",
			allocatedIP1: net.ParseIP("10.10.10.1"),
			allocatedIP2: net.ParseIP("10.10.10.2"),
			wantErr:      false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, err := NewCIDRAllocator(tt.ipSetStr)
			require.NoError(t, err)
			err = a.AllocateIP(tt.allocatedIP1)
			require.NoError(t, err)
			err = a.AllocateIP(tt.allocatedIP2)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestAllocateRelease(t *testing.T) {
	a, err := NewCIDRAllocator("10.10.10.0/24")
	require.NoError(t, err)
	got1, err := a.AllocateNext()
	require.NoError(t, err)
	assert.Equal(t, 1, a.Used())

	err = a.Release(got1)
	require.NoError(t, err)
	assert.Equal(t, 0, a.Used())

	got2, err := a.AllocateNext()
	require.NoError(t, err)
	assert.Equal(t, got1, got2)
}
