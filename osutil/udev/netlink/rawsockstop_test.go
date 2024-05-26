package netlink

import (
	"os"
	"syscall"
	"testing"

	"github.com/ddkwork/golibrary/mylog"
)

func TestRawSockStopperReadable(t *testing.T) {
	r, w := mylog.Check3(os.Pipe())

	oldStopperSelectTimeout := stopperSelectTimeout
	stopperSelectTimeout = func() *syscall.Timeval {
		return &syscall.Timeval{
			Usec: 50 * 1000, // 50ms
		}
	}
	defer func() {
		stopperSelectTimeout = oldStopperSelectTimeout
	}()

	readableOrStop, _ := mylog.Check3(RawSockStopper(int(r.Fd())))

	readable := mylog.Check2(readableOrStop())

	if readable {
		t.Fatal("readableOrStop: expected nothing to read yet")
	}

	w.Write([]byte{1})
	readable = mylog.Check2(readableOrStop())

	if !readable {
		t.Fatal("readableOrStop: expected something to read")
	}
}

func TestRawSockStopperStop(t *testing.T) {
	r, _ := mylog.Check3(os.Pipe())

	readableOrStop, stop := mylog.Check3(RawSockStopper(int(r.Fd())))

	stop()
	readable := mylog.Check2(readableOrStop())

	if readable {
		t.Fatal("readableOrStop: expected nothing to read, just stopped")
	}
}
