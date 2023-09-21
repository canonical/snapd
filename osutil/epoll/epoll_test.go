package epoll_test

import (
	"errors"
	"os"
	"sync"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"

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
	e, err := epoll.Open()
	c.Assert(err, IsNil)
	c.Assert(e.RegisteredFdCount(), Equals, 0)
	c.Assert(e.IsClosed(), Equals, false)

	err = e.Close()
	c.Assert(err, IsNil)
	c.Assert(e.IsClosed(), Equals, true)
}

func concurrentlyRegister(e *epoll.Epoll, fd int, errCh chan error) {
	err := e.Register(fd, epoll.Readable)
	errCh <- err
}

func concurrentlyDeregister(e *epoll.Epoll, fd int, errCh chan error) {
	err := e.Deregister(fd)
	errCh <- err
}

func waitThenWriteToFd(duration time.Duration, fd int, msg []byte) error {
	time.Sleep(duration)
	_, err := unix.Write(fd, msg)
	return err
}

func waitSomewhereElse(e *epoll.Epoll, eventCh chan []epoll.Event, errCh chan error) {
	events, err := e.Wait()
	eventCh <- events
	errCh <- err
}

func waitTimeoutSomewhereElse(e *epoll.Epoll, timeout time.Duration, eventCh chan []epoll.Event, errCh chan error) {
	events, err := e.WaitTimeout(timeout)
	eventCh <- events
	errCh <- err
}

func closeAfter(c *C, e *epoll.Epoll, duration time.Duration) {
	_ = time.AfterFunc(duration, func() {
		err := e.Close()
		c.Assert(err, Equals, nil)
	})
}

func (*epollSuite) TestRegisterWaitModifyDeregister(c *C) {
	e, err := epoll.Open()
	c.Assert(err, IsNil)

	socketFds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	c.Assert(err, IsNil)
	defer unix.Close(socketFds[0])
	defer unix.Close(socketFds[1])

	listenerFd := socketFds[0]
	senderFd := socketFds[1]

	err = unix.SetNonblock(listenerFd, true)
	c.Assert(err, IsNil)

	err = e.Register(listenerFd, epoll.Readable)
	c.Assert(err, IsNil)

	msg := []byte("foo")

	go waitThenWriteToFd(defaultDuration, senderFd, msg)

	events, err := e.Wait()
	c.Assert(err, IsNil)
	c.Assert(events, HasLen, 1)
	c.Assert(events[0].Fd, Equals, listenerFd)

	buf := make([]byte, len(msg))
	_, err = unix.Read(events[0].Fd, buf)
	c.Assert(err, IsNil)
	c.Assert(buf, DeepEquals, msg)

	err = e.Modify(listenerFd, epoll.Readable|epoll.Writable)
	c.Assert(err, IsNil)

	err = e.Deregister(listenerFd)
	c.Assert(err, IsNil)

	err = e.Close()
	c.Assert(err, IsNil)
}

// need a large enough FD that it will not match any FD opened during these tests
const arbitraryNonexistentLargeFd int = 98765

func (*epollSuite) TestRegisterUnhappy(c *C) {
	e, err := epoll.Open()
	c.Assert(err, IsNil)

	// attempt to register a standard file
	newFile, err := os.CreateTemp("/tmp", "snapd-TestRegisterUnhappy-")
	c.Assert(err, IsNil)
	defer newFile.Close()
	defer os.Remove(newFile.Name())
	err = e.Register(int(newFile.Fd()), epoll.Readable)
	c.Check(err, Equals, syscall.Errno(unix.EPERM)) // "operation not permitted"

	// attempt to register nonexistent FD
	err = e.Register(arbitraryNonexistentLargeFd, epoll.Readable)
	c.Check(err, Equals, syscall.Errno(unix.EBADF)) // "bad file descriptor"

	err = e.Close()
	c.Assert(err, IsNil)
}

func (*epollSuite) TestDeregisterUnhappy(c *C) {
	e, err := epoll.Open()
	c.Assert(err, IsNil)

	// attempt to deregister an unregistered FD
	err = e.Deregister(1)
	c.Check(err, Equals, syscall.Errno(unix.ENOENT)) // "no such file or directory"

	// attempt to deregister nonexistent FD
	err = e.Deregister(arbitraryNonexistentLargeFd)
	c.Check(err, Equals, syscall.Errno(unix.EBADF)) // "bad file descriptor"

	err = e.Close()
	c.Assert(err, IsNil)
}

func (*epollSuite) TestWaitTimeout(c *C) {
	e, err := epoll.Open()
	c.Assert(err, IsNil)

	socketFds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	c.Assert(err, IsNil)
	defer unix.Close(socketFds[0])
	defer unix.Close(socketFds[1])

	listenerFd := socketFds[0]
	senderFd := socketFds[1]

	err = unix.SetNonblock(listenerFd, true)
	c.Assert(err, IsNil)

	err = e.Register(listenerFd, epoll.Readable)
	c.Assert(err, IsNil)

	msg := []byte("foo")

	longerDuration := defaultDuration * 2
	go waitThenWriteToFd(longerDuration, senderFd, msg)

	// timeout shorter than wait time before writing
	events, err := e.WaitTimeout(defaultDuration)
	c.Assert(err, IsNil)
	c.Assert(events, HasLen, 0)

	evenLongerDuration := defaultDuration * 10
	events, err = e.WaitTimeout(evenLongerDuration)
	c.Assert(err, IsNil)
	c.Assert(events, HasLen, 1)
	c.Assert(events[0].Fd, Equals, listenerFd)

	buf := make([]byte, len(msg))
	_, err = unix.Read(events[0].Fd, buf)
	c.Assert(err, IsNil)
	c.Assert(buf, DeepEquals, msg)

	err = e.Deregister(listenerFd)
	c.Assert(err, IsNil)

	err = e.Close()
	c.Assert(err, IsNil)
}

func (*epollSuite) TestEpollWaitEintrHandling(c *C) {
	e, err := epoll.Open()
	c.Assert(err, IsNil)

	socketFds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	c.Assert(err, IsNil)
	defer unix.Close(socketFds[0])
	defer unix.Close(socketFds[1])

	listenerFd := socketFds[0]
	senderFd := socketFds[1]

	err = unix.SetNonblock(listenerFd, true)
	c.Assert(err, IsNil)

	err = e.Register(listenerFd, epoll.Readable)
	c.Assert(err, IsNil)
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

	events, err := e.WaitTimeout(defaultDuration)
	c.Assert(err, IsNil)
	c.Assert(events, HasLen, 0)

	go waitSomewhereElse(e, eventCh, errCh)

	msg := []byte("foo")
	_, err = unix.Write(senderFd, msg)
	c.Check(err, IsNil)

	startTime := time.Now()

	time.AfterFunc(defaultDuration, stopEintr)

	events = <-eventCh
	err = <-errCh

	// Check that WaitTimeout kept retrying until unixEpollWait was restored
	c.Assert(time.Now().After(startTime.Add(defaultDuration)), Equals, true)
	c.Assert(err, IsNil)
	c.Assert(events, HasLen, 1)
	c.Assert(events[0].Fd, Equals, listenerFd)

	buf := make([]byte, len(msg))
	_, err = unix.Read(events[0].Fd, buf)
	c.Assert(err, IsNil)
	c.Assert(buf, DeepEquals, msg)

	err = e.Close()
	c.Assert(err, IsNil)
}

func (*epollSuite) TestWriteBeforeWait(c *C) {
	e, err := epoll.Open()
	c.Assert(err, IsNil)

	socketFds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	c.Assert(err, IsNil)
	defer unix.Close(socketFds[0])
	defer unix.Close(socketFds[1])

	listenerFd := socketFds[0]
	senderFd := socketFds[1]

	err = unix.SetNonblock(listenerFd, true)
	c.Assert(err, IsNil)

	err = e.Register(listenerFd, epoll.Readable)
	c.Assert(err, IsNil)

	msgs := [][]byte{
		[]byte("foo"),
		[]byte("bar"),
		[]byte("baz"),
	}

	for _, msg := range msgs {
		_, err = unix.Write(senderFd, msg)
		c.Assert(err, IsNil)
	}

	for _, msg := range msgs {
		events, err := e.Wait()
		c.Assert(err, IsNil)
		c.Assert(events, HasLen, 1) // multiple writes to same fd appear as one event per Wait

		c.Assert(events[0].Fd, Equals, listenerFd)
		buf := make([]byte, len(msg))
		_, err = unix.Read(events[0].Fd, buf)
		c.Assert(err, IsNil)
		c.Assert(buf, DeepEquals, msg)
	}

	err = e.Deregister(listenerFd)
	c.Assert(err, IsNil)

	err = e.Close()
	c.Assert(err, IsNil)
}

func (*epollSuite) TestRegisterMultiple(c *C) {
	e, err := epoll.Open()
	c.Assert(err, IsNil)

	numSockets := 20

	socketRxFds := make([]int, 0, numSockets)
	socketTxFds := make([]int, 0, numSockets)

	msg1 := []byte("foo")
	msg2 := []byte("bar")

	for i := 0; i < numSockets; i++ {
		socketFds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
		c.Assert(err, IsNil)
		defer unix.Close(socketFds[0])
		defer unix.Close(socketFds[1])

		listenerFd := socketFds[0]
		senderFd := socketFds[1]

		err = unix.SetNonblock(listenerFd, true)
		c.Assert(err, IsNil)

		err = e.Register(listenerFd, epoll.Readable)
		c.Assert(err, IsNil)

		_, err = unix.Write(senderFd, msg1)
		c.Assert(err, IsNil)

		socketRxFds = append(socketRxFds, listenerFd)
		socketTxFds = append(socketTxFds, senderFd)
	}

	for _, senderFd := range socketTxFds {
		_, err = unix.Write(senderFd, msg2)
		c.Assert(err, IsNil)
	}

	events, err := e.Wait()
	c.Assert(err, IsNil)
	c.Assert(len(events), Equals, len(socketRxFds))

	for i, listenerFd := range socketRxFds {
		buf := make([]byte, len(msg1))
		c.Assert(events[i].Fd, Equals, listenerFd)
		_, err = unix.Read(events[i].Fd, buf)
		c.Assert(err, IsNil)
		c.Assert(buf, DeepEquals, msg1)
	}

	for i, listenerFd := range socketRxFds {
		buf := make([]byte, len(msg2))
		c.Assert(events[i].Fd, Equals, listenerFd)
		_, err = unix.Read(events[i].Fd, buf)
		c.Assert(err, IsNil)
		c.Assert(buf, DeepEquals, msg2)
	}

	for i := 0; i < len(socketRxFds)/2; i++ {
		err = e.Deregister(socketRxFds[i])
		c.Assert(err, IsNil)
	}

	msg3 := []byte("baz")

	for _, senderFd := range socketTxFds {
		_, err = unix.Write(senderFd, msg3)
		c.Assert(err, IsNil)
	}

	events, err = e.Wait()
	c.Assert(err, IsNil)
	c.Assert(len(events), Equals, len(socketRxFds)/2)

	for i, listenerFd := range socketRxFds[len(socketRxFds)/2:] {
		buf := make([]byte, len(msg3))
		c.Assert(events[i].Fd, Equals, listenerFd)
		_, err = unix.Read(events[i].Fd, buf)
		c.Assert(err, IsNil)
		c.Assert(buf, DeepEquals, msg3)
	}

	err = e.Close()
	c.Assert(err, IsNil)
}

func (epollSuite) TestRegisterDeregisterConcurrency(c *C) {
	e, err := epoll.Open()
	c.Assert(err, IsNil)
	c.Assert(e.RegisteredFdCount(), Equals, 0)

	concurrencyCount := 20

	listenerFds := make([]int, 0, concurrencyCount)

	for i := 0; i < concurrencyCount; i++ {
		socketFds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
		c.Assert(err, IsNil)
		defer unix.Close(socketFds[0])
		defer unix.Close(socketFds[1])

		listenerFd := socketFds[0]

		err = unix.SetNonblock(listenerFd, true)
		c.Assert(err, IsNil)

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

	err = e.Close()
	c.Assert(err, IsNil)
}

func (*epollSuite) TestWaitWithoutRegistering(c *C) {
	e, err := epoll.Open()
	c.Assert(err, IsNil)

	events, err := e.WaitTimeout(defaultDuration)
	c.Assert(err, IsNil)
	c.Assert(events, HasLen, 0)

	err = e.Close()
	c.Assert(err, IsNil)
}

func (*epollSuite) TestWaitThenDeregister(c *C) {
	e, err := epoll.Open()
	c.Assert(err, IsNil)

	socketFds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	c.Assert(err, IsNil)
	defer unix.Close(socketFds[0])
	defer unix.Close(socketFds[1])

	listenerFd := socketFds[0]
	senderFd := socketFds[1]

	err = unix.SetNonblock(listenerFd, true)
	c.Assert(err, IsNil)

	err = e.Register(listenerFd, epoll.Readable)
	c.Assert(err, IsNil)
	c.Assert(e.RegisteredFdCount(), Equals, 1)

	eventCh := make(chan []epoll.Event)
	errCh := make(chan error)

	go waitTimeoutSomewhereElse(e, defaultDuration, eventCh, errCh)

	err = e.Deregister(listenerFd)
	c.Check(err, IsNil)
	c.Check(e.RegisteredFdCount(), Equals, 0)

	// check that deregistered FD does not trigger epoll event
	msg := []byte("foo")
	_, err = unix.Write(senderFd, msg)
	c.Check(err, IsNil)

	events := <-eventCh
	err = <-errCh
	c.Assert(events, HasLen, 0)
	c.Assert(err, IsNil)

	err = e.Close()
	c.Assert(err, IsNil)
}

func (*epollSuite) TestWaitThenRegister(c *C) {
	e, err := epoll.Open()
	c.Assert(err, IsNil)

	eventCh := make(chan []epoll.Event)
	errCh := make(chan error)

	go waitSomewhereElse(e, eventCh, errCh)

	time.Sleep(defaultDuration)

	socketFds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	c.Check(err, IsNil)
	defer unix.Close(socketFds[0])
	defer unix.Close(socketFds[1])

	listenerFd := socketFds[0]
	senderFd := socketFds[1]

	err = unix.SetNonblock(listenerFd, true)
	c.Check(err, IsNil)

	err = e.Register(listenerFd, epoll.Readable)
	c.Check(err, IsNil)
	c.Check(e.RegisteredFdCount(), Equals, 1)

	// check that fd registered after Wait() began still triggers epoll event
	msg := []byte("foo")
	_, err = unix.Write(senderFd, msg)
	c.Check(err, IsNil)

	events := <-eventCh
	err = <-errCh
	c.Assert(err, IsNil)
	c.Assert(events, HasLen, 1)

	buf := make([]byte, len(msg))
	c.Assert(events[0].Fd, Equals, listenerFd)
	_, err = unix.Read(events[0].Fd, buf)
	c.Assert(err, IsNil)
	c.Assert(buf, DeepEquals, msg)

	err = e.Close()
	c.Assert(err, IsNil)
}

func (*epollSuite) TestErrorsOnClosedEpoll(c *C) {
	e, err := epoll.Open()
	c.Assert(err, IsNil)

	err = e.Close()
	c.Assert(err, IsNil)

	err = e.Close()
	c.Assert(err, Equals, epoll.ErrEpollClosed)

	err = e.Register(0, epoll.Readable)
	c.Assert(err, Equals, epoll.ErrEpollClosed)

	err = e.Deregister(0)
	c.Assert(err, Equals, epoll.ErrEpollClosed)

	err = e.Modify(0, epoll.Readable)
	c.Assert(err, Equals, epoll.ErrEpollClosed)

	err = e.Modify(0, epoll.Readable)
	c.Assert(err, Equals, epoll.ErrEpollClosed)

	events, err := e.WaitTimeout(defaultDuration)
	c.Assert(err, Equals, epoll.ErrEpollClosed)
	c.Assert(events, HasLen, 0)

	events, err = e.Wait()
	c.Assert(err, Equals, epoll.ErrEpollClosed)
	c.Assert(events, HasLen, 0)
}

func (*epollSuite) TestWaitErrors(c *C) {
	fakeError := errors.New("injected fake error")

	restore := epoll.MockUnixEpollWait(func(epfd int, events []unix.EpollEvent, msec int) (n int, err error) {
		return 0, fakeError
	})
	defer restore()

	e, err := epoll.Open()
	c.Assert(err, IsNil)

	events, err := e.Wait()
	c.Assert(err, Equals, fakeError)
	c.Assert(events, HasLen, 0)

	err = e.Close()
	c.Assert(err, IsNil)

	restore = epoll.MockUnixEpollWait(func(epfd int, events []unix.EpollEvent, msec int) (n int, err error) {
		return unix.EpollWait(-1, events, msec)
	})
	defer restore()

	e, err = epoll.Open()
	c.Assert(err, IsNil)

	events, err = e.Wait()
	c.Assert(err, Equals, unix.EBADF)
	c.Assert(events, HasLen, 0)

	err = e.Close()
	c.Assert(err, IsNil)

	restore = epoll.MockUnixEpollWait(func(epfd int, events []unix.EpollEvent, msec int) (n int, err error) {
		// Make syscall on bad fd, as if it had been closed.
		n, err = unix.EpollWait(-1, events, msec)
		// Close the epoll, as if it had been the reason the fd in the syscall
		// had been closed.
		e.Close()
		return n, err
	})
	defer restore()

	e, err = epoll.Open()
	c.Assert(err, IsNil)

	events, err = e.Wait()
	c.Assert(err, Equals, epoll.ErrEpollClosed)
	c.Assert(events, HasLen, 0)

	err = e.Close()
	c.Assert(err, Equals, epoll.ErrEpollClosed)
}

func (*epollSuite) TestWaitThenClose(c *C) {
	e, err := epoll.Open()
	c.Assert(err, IsNil)

	closeAfter(c, e, defaultDuration)

	startTime := time.Now()

	events, err := e.Wait()

	// Check that Wait() returned "immediately"
	c.Assert(time.Now().Before(startTime.Add(2*defaultDuration)), Equals, true)
	c.Assert(err, Equals, epoll.ErrEpollClosed)
	c.Assert(events, HasLen, 0)

	e, err = epoll.Open()
	c.Assert(err, IsNil)

	closeAfter(c, e, defaultDuration)

	startTime = time.Now()

	events, err = e.WaitTimeout(defaultDuration * 2)

	// Check that WaitTimeout() returned "immediately"
	c.Assert(time.Now().Before(startTime.Add(2*defaultDuration)), Equals, true)
	c.Assert(err, Equals, epoll.ErrEpollClosed)
	c.Assert(events, HasLen, 0)
}
