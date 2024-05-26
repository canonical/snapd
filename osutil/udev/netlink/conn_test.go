package netlink

import (
	"syscall"
	"testing"
	"time"

	"github.com/ddkwork/golibrary/mylog"
	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

func TestConnect(t *testing.T) {
	conn := new(UEventConn)
	mylog.Check(conn.Connect(UdevEvent))

	defer conn.Close()

	conn2 := new(UEventConn)
	mylog.Check(conn2.Connect(UdevEvent))

	defer conn2.Close()
}

func TestMonitorStop(t *testing.T) {
	conn := new(UEventConn)
	mylog.Check(conn.Connect(UdevEvent))

	defer conn.Close()

	stop := conn.Monitor(nil, nil, nil)
	ok := stop(200 * time.Millisecond)
	if !ok {
		t.Fatal("stop timed out instead of working")
	}
}

func TestMonitorSelectTimeoutIsHarmless(t *testing.T) {
	conn := new(UEventConn)
	mylog.Check(conn.Connect(UdevEvent))

	defer conn.Close()

	selectCalled := 0
	oldStopperSelectTimeout := stopperSelectTimeout
	stopperSelectTimeout = func() *syscall.Timeval {
		selectCalled += 1
		return &syscall.Timeval{
			Usec: 10 * 1000, // 10ms
		}
	}
	defer func() {
		stopperSelectTimeout = oldStopperSelectTimeout
	}()

	stop := conn.Monitor(nil, nil, nil)
	time.Sleep(100 * time.Millisecond)
	ok := stop(200 * time.Millisecond)
	if !ok {
		t.Fatal("stop timed out instead of working")
	}
	if selectCalled <= 1 {
		t.Fatal("select->read->select should have been exercised at least once")
	}
}
