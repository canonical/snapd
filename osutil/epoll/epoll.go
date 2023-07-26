// Package epoll contains a thin wrapper around the epoll(7) facility.
//
// Using epoll from Go is unusual as the language provides facilities that
// normally make using it directly pointless. Epoll is strictly required for
// unusual kernel interfaces that use event notification but don't implement
// file descriptors that provide usual read/write semantics.

package epoll

import (
	"fmt"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/sys/unix"
)

// Readiness is the bit mask of aspects of readiness to monitor with epoll.
type Readiness int

const (
	// Readable indicates readiness for reading (EPOLLIN).
	Readable Readiness = unix.EPOLLIN
	// Writable indicates readiness for writing (EPOLLOUT).
	Writable Readiness = unix.EPOLLOUT
)

// String returns readable representation of the readiness flags.
func (r Readiness) String() string {
	frags := make([]string, 0, 2)
	if r&Readable != 0 {
		frags = append(frags, "Readable")
	}
	if r&Writable != 0 {
		frags = append(frags, "Writable")
	}
	return strings.Join(frags, "|")
}

// Epoll wraps a file descriptor which can be used for I/O readiness notification.
type Epoll struct {
	fd                int
	registeredFdCount int32 // read/modify using helper functions
}

// Open opens an event monitoring descriptor.
func Open() (*Epoll, error) {
	fd, err := unix.EpollCreate1(unix.EPOLL_CLOEXEC)
	if err != nil {
		return nil, fmt.Errorf("cannot open epoll file descriptor: %w", err)
	}
	e := &Epoll{
		fd:                fd,
		registeredFdCount: 0,
	}
	runtime.SetFinalizer(e, func(e *Epoll) {
		if e.fd != -1 {
			e.Close()
		}
	})
	return e, nil
}

// Close closes the event monitoring descriptor.
func (e *Epoll) Close() error {
	runtime.SetFinalizer(e, nil)
	fd := e.fd
	e.fd = -1
	e.zeroRegisteredFdCount()
	return unix.Close(fd)
}

// Fd returns the integer unix file descriptor referencing the open file.
func (e *Epoll) Fd() int {
	return e.fd
}

// RegisteredFdCount returns the number of file descriptors which are currently
// registered to the epoll instance.
func (e *Epoll) RegisteredFdCount() int {
	return int(atomic.LoadInt32(&e.registeredFdCount))
}

func (e *Epoll) incrementRegisteredFdCount() {
	atomic.AddInt32(&e.registeredFdCount, 1)
}

func (e *Epoll) decrementRegisteredFdCount() {
	atomic.AddInt32(&e.registeredFdCount, -1)
}

func (e *Epoll) zeroRegisteredFdCount() {
	atomic.StoreInt32(&e.registeredFdCount, 0)
}

// Register registers a file descriptor and allows observing speicifc I/O readiness events.
//
// Please refer to epoll_ctl(2) and EPOLL_CTL_ADD for details.
func (e *Epoll) Register(fd int, mask Readiness) error {
	e.incrementRegisteredFdCount()
	err := unix.EpollCtl(e.fd, unix.EPOLL_CTL_ADD, fd, &unix.EpollEvent{
		Events: uint32(mask),
		Fd:     int32(fd),
	})
	if err != nil {
		e.decrementRegisteredFdCount()
		return err
	}
	runtime.KeepAlive(e)
	return err
}

// Deregister removes the given file descriptor from the epoll instance.
//
// Please refer to epoll_ctl(2) and EPOLL_CTL_DEL for details.
func (e *Epoll) Deregister(fd int) error {
	err := unix.EpollCtl(e.fd, unix.EPOLL_CTL_DEL, fd, &unix.EpollEvent{})
	if err != nil {
		return err
	}
	e.decrementRegisteredFdCount()
	return err
}

// Modify changes the set of monitored I/O readiness events of a previously registered file descriptor.
//
// Please refer to epoll_ctl(2) and EPOLL_CTL_MOD for details.
func (e *Epoll) Modify(fd int, mask Readiness) error {
	err := unix.EpollCtl(e.fd, unix.EPOLL_CTL_MOD, fd, &unix.EpollEvent{
		Events: uint32(mask),
		Fd:     int32(fd),
	})
	runtime.KeepAlive(e)
	return err
}

// Event describes an IO readiness event on a specific file descriptor.
type Event struct {
	Fd        int
	Readiness Readiness
}

var unixEpollWait = unix.EpollWait

// WaitTimeout blocks and waits with the given timeout for arrival of events on any of the added file descriptors.
//
// A msec value of -1 disables timeout.
//
// Please refer to epoll_wait(2) and EPOLL_WAIT for details.
//
// Warning, using epoll from Golang explicitly is tricky.
func (e *Epoll) WaitTimeout(duration time.Duration) ([]Event, error) {
	msec := int(duration.Milliseconds())
	if duration < 0 {
		msec = -1
	}
	n := 0
	var err error
	var sysEvents []unix.EpollEvent
	for {
		bufLen := e.RegisteredFdCount()
		if bufLen < 1 {
			// Even if RegisteredFdCount is zero, it could increase after a
			// call in a multi-threaded environment.  This ensures that there
			// is at least one entry available in the event buffer.  The size
			// of the buffer does not need to match the number of events, and
			// the syscall will populate as many buffer entries as are
			// available, up to the number of epoll events which have yet to
			// be handled.
			bufLen = 1
		}
		sysEvents = make([]unix.EpollEvent, bufLen)
		startTs := time.Now()
		n, err = unixEpollWait(e.fd, sysEvents, msec)
		runtime.KeepAlive(e)
		// unix.EpollWait can return unix.EINTR, which we want to handle by
		// adjusting the timeout (if necessary) and restarting the syscall
		if err == nil {
			break
		} else if err != unix.EINTR {
			return nil, err
		}
		if msec == -1 {
			continue
		}
		elapsed := time.Since(startTs)
		msec -= int(elapsed.Milliseconds())
		if msec <= 0 {
			n = 0
			break
		}
	}
	if n == 0 {
		return nil, nil
	}
	events := make([]Event, 0, n)
	for i := 0; i < n; i++ {
		event := Event{
			Fd:        int(sysEvents[i].Fd),
			Readiness: Readiness(sysEvents[i].Events),
		}
		events = append(events, event)
	}
	return events, nil
}

// Wait blocks and waits for arrival of events on any of the added file descriptors.
func (e *Epoll) Wait() ([]Event, error) {
	duration := time.Duration(-1)
	return e.WaitTimeout(duration)
}
