package netlink

import (
	"testing"
	"time"
)

func TestConnect(t *testing.T) {
	conn := new(UEventConn)
	if err := conn.Connect(UdevEvent); err != nil {
		t.Fatal("unable to subscribe to netlink uevent, err:", err)
	}
	defer conn.Close()

	conn2 := new(UEventConn)
	if err := conn2.Connect(UdevEvent); err == nil {
		// see issue: https://github.com/pilebones/go-udev/issues/3 by @stolowski
		t.Fatal("can't subscribing a second time to netlink socket with PID", conn2.Addr.Pid)
	}
	defer conn2.Close()
}

func TestMonitorStop(t *testing.T) {
	conn := new(UEventConn)
	if err := conn.Connect(UdevEvent); err != nil {
		t.Fatal("unable to subscribe to netlink uevent, err:", err)
	}
	defer conn.Close()

	stop := conn.Monitor(nil, nil, nil)
	ok := stop(200 * time.Millisecond)
	if !ok {
		t.Fatal("stop timed out instead of working")
	}
}
