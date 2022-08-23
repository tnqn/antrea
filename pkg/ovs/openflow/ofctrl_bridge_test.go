package openflow

import (
	"antrea.io/antrea/pkg/ovs/ovsconfig"
	"antrea.io/libOpenflow/util"
	"antrea.io/ofnet/ofctrl"
	"io"
	"math/rand"
	"net"
	"sync"
	"testing"
	"time"
)

type fakeConn struct {
	count int
	max   int
}

func (f *fakeConn) Close() error {
	return nil
}

func (f *fakeConn) Read(b []byte) (int, error) {
	if f.count == f.max {
		return 0, io.EOF
	}
	f.count++
	return len(b), nil
}

func (f *fakeConn) Write(b []byte) (int, error) {
	return len(b), nil
}

func (f *fakeConn) LocalAddr() net.Addr {
	return nil
}

func (f *fakeConn) RemoteAddr() net.Addr {
	return nil
}

func (f *fakeConn) SetDeadline(t time.Time) error {
	return nil
}

func (f *fakeConn) SetReadDeadline(t time.Time) error {
	return nil
}

func (f *fakeConn) SetWriteDeadline(t time.Time) error {
	return nil
}

func TestOFBridgeSwitchConnected(t *testing.T) {

	b := NewOFBridge("test-br", GetMgmtAddress(ovsconfig.DefaultOVSRunDir, "test-br"))
	stream := util.NewMessageStream(&fakeConn{count: 10}, nil)
	dpid, _ := net.ParseMAC("01:02:03:04:05:06:07:08")
	connCh := make(chan int)
	sw := ofctrl.NewSwitch(stream, dpid, b, connCh, uint16(rand.Uint32()))
	b.SwitchConnected(sw)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		b.SwitchConnected(sw)
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		b.IsConnected()
	}()

	wg.Wait()
}
