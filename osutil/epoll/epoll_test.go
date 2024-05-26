package epoll_test

import (
	"errors"
	"os"
	"sync"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/osutil/epoll"
	"github.com/snapcore/snapd/testutil"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type epollSuite struct{}

var _ = Suite(&epollSuite{})

var defaultDuration time.Duration = testutil.HostScaledTimeout(100 * time.Millisecond)

func (*epollSuite) TestString(c *C) {
	c.Check(epoll.Readable.String(), Equals, "Readable")
	c.Check(epoll.Writable.String(), Equals, "Writable")
	c.Check(epoll.Readiness(epoll.Readable|epoll.Writable).String(), Equals, "Readable|Writable")
}

func (*epollSuite) TestOpenClose(c *C) {
	e := mylog.Check2(epoll.Open())

	c.Assert(e.RegisteredFdCount(), Equals, 0)
	c.Assert(e.IsClosed(), Equals, false)
	mylog.Check(e.Close())

	c.Assert(e.IsClosed(), Equals, true)
}

func concurrentlyRegister(e *epoll.Epoll, fd int, errCh chan error) {
	mylog.Check(e.Register(fd, epoll.Readable))
	errCh <- err
}

func concurrentlyDeregister(e *epoll.Epoll, fd int, errCh chan error) {
	mylog.Check(e.Deregister(fd))
	errCh <- err
}

func waitThenWriteToFd(duration time.Duration, fd int, msg []byte) error {
	time.Sleep(duration)
	_ := mylog.Check2(unix.Write(fd, msg))
	return err
}

func waitSomewhereElse(e *epoll.Epoll, eventCh chan []epoll.Event, errCh chan error) {
	events := mylog.Check2(e.Wait())
	eventCh <- events
	errCh <- err
}

func waitTimeoutSomewhereElse(e *epoll.Epoll, timeout time.Duration, eventCh chan []epoll.Event, errCh chan error) {
	events := mylog.Check2(e.WaitTimeout(timeout))
	eventCh <- events
	errCh <- err
}

func closeAfter(c *C, e *epoll.Epoll, duration time.Duration) {
	_ = time.AfterFunc(duration, func() {
		mylog.Check(e.Close())
		c.Assert(err, Equals, nil)
	})
}

func (*epollSuite) TestRegisterWaitModifyDeregister(c *C) {
	e := mylog.Check2(epoll.Open())


	socketFds := mylog.Check2(unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0))

	defer unix.Close(socketFds[0])
	defer unix.Close(socketFds[1])

	listenerFd := socketFds[0]
	senderFd := socketFds[1]
	mylog.Check(unix.SetNonblock(listenerFd, true))

	mylog.Check(e.Register(listenerFd, epoll.Readable))


	msg := []byte("foo")

	go waitThenWriteToFd(defaultDuration, senderFd, msg)

	events := mylog.Check2(e.Wait())

	c.Assert(events, HasLen, 1)
	c.Assert(events[0].Fd, Equals, listenerFd)

	buf := make([]byte, len(msg))
	_ = mylog.Check2(unix.Read(events[0].Fd, buf))

	c.Assert(buf, DeepEquals, msg)
	mylog.Check(e.Modify(listenerFd, epoll.Readable|epoll.Writable))

	mylog.Check(e.Deregister(listenerFd))

	mylog.Check(e.Close())

}

// need a large enough FD that it will not match any FD opened during these tests
const arbitraryNonexistentLargeFd int = 98765

func (*epollSuite) TestRegisterUnhappy(c *C) {
	e := mylog.Check2(epoll.Open())


	// attempt to register a standard file
	newFile := mylog.Check2(os.CreateTemp("/tmp", "snapd-TestRegisterUnhappy-"))

	defer newFile.Close()
	defer os.Remove(newFile.Name())
	mylog.Check(e.Register(int(newFile.Fd()), epoll.Readable))
	c.Check(err, Equals, syscall.Errno(unix.EPERM))
	mylog. // "operation not permitted"
		Check(

			// attempt to register nonexistent FD
			e.Register(arbitraryNonexistentLargeFd, epoll.Readable))
	c.Check(err, Equals, syscall.Errno(unix.EBADF))
	mylog. // "bad file descriptor"
		Check(e.Close())

}

func (*epollSuite) TestDeregisterUnhappy(c *C) {
	e := mylog.Check2(epoll.Open())

	mylog.

		// attempt to deregister an unregistered FD
		Check(e.Deregister(1))
	c.Check(err, Equals, syscall.Errno(unix.ENOENT))
	mylog. // "no such file or directory"
		Check(

			// attempt to deregister nonexistent FD
			e.Deregister(arbitraryNonexistentLargeFd))
	c.Check(err, Equals, syscall.Errno(unix.EBADF))
	mylog. // "bad file descriptor"
		Check(e.Close())

}

func (*epollSuite) TestWaitTimeout(c *C) {
	e := mylog.Check2(epoll.Open())


	socketFds := mylog.Check2(unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0))

	defer unix.Close(socketFds[0])
	defer unix.Close(socketFds[1])

	listenerFd := socketFds[0]
	senderFd := socketFds[1]
	mylog.Check(unix.SetNonblock(listenerFd, true))

	mylog.Check(e.Register(listenerFd, epoll.Readable))


	msg := []byte("foo")

	longerDuration := defaultDuration * 2
	go waitThenWriteToFd(longerDuration, senderFd, msg)

	// timeout shorter than wait time before writing
	events := mylog.Check2(e.WaitTimeout(defaultDuration))

	c.Assert(events, HasLen, 0)

	evenLongerDuration := defaultDuration * 10
	events = mylog.Check2(e.WaitTimeout(evenLongerDuration))

	c.Assert(events, HasLen, 1)
	c.Assert(events[0].Fd, Equals, listenerFd)

	buf := make([]byte, len(msg))
	_ = mylog.Check2(unix.Read(events[0].Fd, buf))

	c.Assert(buf, DeepEquals, msg)
	mylog.Check(e.Deregister(listenerFd))

	mylog.Check(e.Close())

}

func (*epollSuite) TestEpollWaitEintrHandling(c *C) {
	e := mylog.Check2(epoll.Open())


	socketFds := mylog.Check2(unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0))

	defer unix.Close(socketFds[0])
	defer unix.Close(socketFds[1])

	listenerFd := socketFds[0]
	senderFd := socketFds[1]
	mylog.Check(unix.SetNonblock(listenerFd, true))

	mylog.Check(e.Register(listenerFd, epoll.Readable))

	c.Assert(e.RegisteredFdCount(), Equals, 1)

	var mu sync.Mutex
	eintr := true
	shouldReturnEintr := func() bool {
		mu.Lock()
		defer mu.Unlock()
		return eintr
	}
	stopEintr := func() {
		mu.Lock()
		defer mu.Unlock()
		eintr = false
	}
	restore := epoll.MockUnixEpollWait(func(epfd int, events []unix.EpollEvent, msec int) (n int, err error) {
		if shouldReturnEintr() {
			time.Sleep(time.Millisecond * 10) // rate limit a bit
			return 0, unix.EINTR
		}
		return unix.EpollWait(epfd, events, msec)
	})
	defer restore()

	eventCh := make(chan []epoll.Event)
	errCh := make(chan error)

	events := mylog.Check2(e.WaitTimeout(defaultDuration))

	c.Assert(events, HasLen, 0)

	go waitSomewhereElse(e, eventCh, errCh)

	msg := []byte("foo")
	_ = mylog.Check2(unix.Write(senderFd, msg))
	c.Check(err, IsNil)

	startTime := time.Now()

	time.AfterFunc(defaultDuration, stopEintr)

	events = <-eventCh
	err = <-errCh

	// Check that WaitTimeout kept retrying until unixEpollWait was restored
	c.Assert(time.Now().After(startTime.Add(defaultDuration)), Equals, true)

	c.Assert(events, HasLen, 1)
	c.Assert(events[0].Fd, Equals, listenerFd)

	buf := make([]byte, len(msg))
	_ = mylog.Check2(unix.Read(events[0].Fd, buf))

	c.Assert(buf, DeepEquals, msg)
	mylog.Check(e.Close())

}

func (*epollSuite) TestWriteBeforeWait(c *C) {
	e := mylog.Check2(epoll.Open())


	socketFds := mylog.Check2(unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0))

	defer unix.Close(socketFds[0])
	defer unix.Close(socketFds[1])

	listenerFd := socketFds[0]
	senderFd := socketFds[1]
	mylog.Check(unix.SetNonblock(listenerFd, true))

	mylog.Check(e.Register(listenerFd, epoll.Readable))


	msgs := [][]byte{
		[]byte("foo"),
		[]byte("bar"),
		[]byte("baz"),
	}

	for _, msg := range msgs {
		_ = mylog.Check2(unix.Write(senderFd, msg))

	}

	for _, msg := range msgs {
		events := mylog.Check2(e.Wait())

		c.Assert(events, HasLen, 1) // multiple writes to same fd appear as one event per Wait

		c.Assert(events[0].Fd, Equals, listenerFd)
		buf := make([]byte, len(msg))
		_ = mylog.Check2(unix.Read(events[0].Fd, buf))

		c.Assert(buf, DeepEquals, msg)
	}
	mylog.Check(e.Deregister(listenerFd))

	mylog.Check(e.Close())

}

func (*epollSuite) TestRegisterMultiple(c *C) {
	e := mylog.Check2(epoll.Open())


	numSockets := 20

	socketRxFds := make([]int, 0, numSockets)
	socketTxFds := make([]int, 0, numSockets)

	msg1 := []byte("foo")
	msg2 := []byte("bar")

	for i := 0; i < numSockets; i++ {
		socketFds := mylog.Check2(unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0))

		defer unix.Close(socketFds[0])
		defer unix.Close(socketFds[1])

		listenerFd := socketFds[0]
		senderFd := socketFds[1]
		mylog.Check(unix.SetNonblock(listenerFd, true))

		mylog.Check(e.Register(listenerFd, epoll.Readable))


		_ = mylog.Check2(unix.Write(senderFd, msg1))


		socketRxFds = append(socketRxFds, listenerFd)
		socketTxFds = append(socketTxFds, senderFd)
	}

	for _, senderFd := range socketTxFds {
		_ = mylog.Check2(unix.Write(senderFd, msg2))

	}

	events := mylog.Check2(e.Wait())

	c.Assert(len(events), Equals, len(socketRxFds))

	for i, listenerFd := range socketRxFds {
		buf := make([]byte, len(msg1))
		c.Assert(events[i].Fd, Equals, listenerFd)
		_ = mylog.Check2(unix.Read(events[i].Fd, buf))

		c.Assert(buf, DeepEquals, msg1)
	}

	for i, listenerFd := range socketRxFds {
		buf := make([]byte, len(msg2))
		c.Assert(events[i].Fd, Equals, listenerFd)
		_ = mylog.Check2(unix.Read(events[i].Fd, buf))

		c.Assert(buf, DeepEquals, msg2)
	}

	for i := 0; i < len(socketRxFds)/2; i++ {
		mylog.Check(e.Deregister(socketRxFds[i]))

	}

	msg3 := []byte("baz")

	for _, senderFd := range socketTxFds {
		_ = mylog.Check2(unix.Write(senderFd, msg3))

	}

	events = mylog.Check2(e.Wait())

	c.Assert(len(events), Equals, len(socketRxFds)/2)

	for i, listenerFd := range socketRxFds[len(socketRxFds)/2:] {
		buf := make([]byte, len(msg3))
		c.Assert(events[i].Fd, Equals, listenerFd)
		_ = mylog.Check2(unix.Read(events[i].Fd, buf))

		c.Assert(buf, DeepEquals, msg3)
	}
	mylog.Check(e.Close())

}

func (epollSuite) TestRegisterDeregisterConcurrency(c *C) {
	e := mylog.Check2(epoll.Open())

	c.Assert(e.RegisteredFdCount(), Equals, 0)

	concurrencyCount := 20

	listenerFds := make([]int, 0, concurrencyCount)

	for i := 0; i < concurrencyCount; i++ {
		socketFds := mylog.Check2(unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0))

		defer unix.Close(socketFds[0])
		defer unix.Close(socketFds[1])

		listenerFd := socketFds[0]
		mylog.Check(unix.SetNonblock(listenerFd, true))


		listenerFds = append(listenerFds, listenerFd)
	}

	errCh := make(chan error)

	for _, fd := range listenerFds {
		go concurrentlyRegister(e, fd, errCh)
	}

	for range listenerFds {
		err := <-errCh
		c.Check(err, IsNil)
	}

	c.Assert(e.RegisteredFdCount(), Equals, len(listenerFds))

	for _, fd := range listenerFds {
		go concurrentlyDeregister(e, fd, errCh)
	}

	for range listenerFds {
		err := <-errCh
		c.Check(err, IsNil)
	}

	c.Assert(e.RegisteredFdCount(), Equals, 0)
	mylog.Check(e.Close())

}

func (*epollSuite) TestWaitWithoutRegistering(c *C) {
	e := mylog.Check2(epoll.Open())


	events := mylog.Check2(e.WaitTimeout(defaultDuration))

	c.Assert(events, HasLen, 0)
	mylog.Check(e.Close())

}

func (*epollSuite) TestWaitThenDeregister(c *C) {
	e := mylog.Check2(epoll.Open())


	socketFds := mylog.Check2(unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0))

	defer unix.Close(socketFds[0])
	defer unix.Close(socketFds[1])

	listenerFd := socketFds[0]
	senderFd := socketFds[1]
	mylog.Check(unix.SetNonblock(listenerFd, true))

	mylog.Check(e.Register(listenerFd, epoll.Readable))

	c.Assert(e.RegisteredFdCount(), Equals, 1)

	eventCh := make(chan []epoll.Event)
	errCh := make(chan error)

	go waitTimeoutSomewhereElse(e, defaultDuration, eventCh, errCh)
	mylog.Check(e.Deregister(listenerFd))
	c.Check(err, IsNil)
	c.Check(e.RegisteredFdCount(), Equals, 0)

	// check that deregistered FD does not trigger epoll event
	msg := []byte("foo")
	_ = mylog.Check2(unix.Write(senderFd, msg))
	c.Check(err, IsNil)

	events := <-eventCh
	err = <-errCh
	c.Assert(events, HasLen, 0)

	mylog.Check(e.Close())

}

func (*epollSuite) TestWaitThenRegister(c *C) {
	e := mylog.Check2(epoll.Open())


	eventCh := make(chan []epoll.Event)
	errCh := make(chan error)

	go waitSomewhereElse(e, eventCh, errCh)

	time.Sleep(defaultDuration)

	socketFds := mylog.Check2(unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0))
	c.Check(err, IsNil)
	defer unix.Close(socketFds[0])
	defer unix.Close(socketFds[1])

	listenerFd := socketFds[0]
	senderFd := socketFds[1]
	mylog.Check(unix.SetNonblock(listenerFd, true))
	c.Check(err, IsNil)
	mylog.Check(e.Register(listenerFd, epoll.Readable))
	c.Check(err, IsNil)
	c.Check(e.RegisteredFdCount(), Equals, 1)

	// check that fd registered after Wait() began still triggers epoll event
	msg := []byte("foo")
	_ = mylog.Check2(unix.Write(senderFd, msg))
	c.Check(err, IsNil)

	events := <-eventCh
	err = <-errCh

	c.Assert(events, HasLen, 1)

	buf := make([]byte, len(msg))
	c.Assert(events[0].Fd, Equals, listenerFd)
	_ = mylog.Check2(unix.Read(events[0].Fd, buf))

	c.Assert(buf, DeepEquals, msg)
	mylog.Check(e.Close())

}

func (*epollSuite) TestErrorsOnClosedEpoll(c *C) {
	e := mylog.Check2(epoll.Open())

	mylog.Check(e.Close())

	mylog.Check(e.Close())
	c.Assert(err, Equals, epoll.ErrEpollClosed)
	mylog.Check(e.Register(0, epoll.Readable))
	c.Assert(err, Equals, epoll.ErrEpollClosed)
	mylog.Check(e.Deregister(0))
	c.Assert(err, Equals, epoll.ErrEpollClosed)
	mylog.Check(e.Modify(0, epoll.Readable))
	c.Assert(err, Equals, epoll.ErrEpollClosed)
	mylog.Check(e.Modify(0, epoll.Readable))
	c.Assert(err, Equals, epoll.ErrEpollClosed)

	events := mylog.Check2(e.WaitTimeout(defaultDuration))
	c.Assert(err, Equals, epoll.ErrEpollClosed)
	c.Assert(events, HasLen, 0)

	events = mylog.Check2(e.Wait())
	c.Assert(err, Equals, epoll.ErrEpollClosed)
	c.Assert(events, HasLen, 0)
}

func (*epollSuite) TestWaitErrors(c *C) {
	fakeError := errors.New("injected fake error")

	restore := epoll.MockUnixEpollWait(func(epfd int, events []unix.EpollEvent, msec int) (n int, err error) {
		return 0, fakeError
	})
	defer restore()

	e := mylog.Check2(epoll.Open())


	events := mylog.Check2(e.Wait())
	c.Assert(err, Equals, fakeError)
	c.Assert(events, HasLen, 0)
	mylog.Check(e.Close())


	restore = epoll.MockUnixEpollWait(func(epfd int, events []unix.EpollEvent, msec int) (n int, err error) {
		return unix.EpollWait(-1, events, msec)
	})
	defer restore()

	e = mylog.Check2(epoll.Open())


	events = mylog.Check2(e.Wait())
	c.Assert(err, Equals, unix.EBADF)
	c.Assert(events, HasLen, 0)
	mylog.Check(e.Close())


	restore = epoll.MockUnixEpollWait(func(epfd int, events []unix.EpollEvent, msec int) (n int, err error) {
		// Make syscall on bad fd, as if it had been closed.
		n = mylog.Check2(unix.EpollWait(-1, events, msec))
		// Close the epoll, as if it had been the reason the fd in the syscall
		// had been closed.
		e.Close()
		return n, err
	})
	defer restore()

	e = mylog.Check2(epoll.Open())


	events = mylog.Check2(e.Wait())
	c.Assert(err, Equals, epoll.ErrEpollClosed)
	c.Assert(events, HasLen, 0)
	mylog.Check(e.Close())
	c.Assert(err, Equals, epoll.ErrEpollClosed)
}

func (*epollSuite) TestWaitThenClose(c *C) {
	e := mylog.Check2(epoll.Open())


	closeAfter(c, e, defaultDuration)

	startTime := time.Now()

	events := mylog.Check2(e.Wait())

	// Check that Wait() returned "immediately"
	c.Assert(time.Now().Before(startTime.Add(2*defaultDuration)), Equals, true)
	c.Assert(err, Equals, epoll.ErrEpollClosed)
	c.Assert(events, HasLen, 0)

	e = mylog.Check2(epoll.Open())


	closeAfter(c, e, defaultDuration)

	startTime = time.Now()

	events = mylog.Check2(e.WaitTimeout(defaultDuration * 2))

	// Check that WaitTimeout() returned "immediately"
	c.Assert(time.Now().Before(startTime.Add(2*defaultDuration)), Equals, true)
	c.Assert(err, Equals, epoll.ErrEpollClosed)
	c.Assert(events, HasLen, 0)
}
