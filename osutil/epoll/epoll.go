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
	fd        int
	sysEvents []unix.EpollEvent
}

// Open opens an event monitoring descriptor.
func Open() (*Epoll, error) {
	fd, err := unix.EpollCreate1(unix.EPOLL_CLOEXEC)
	if err != nil {
		return nil, fmt.Errorf("cannot open epoll file descriptor: %w", err)
	}
	e := &Epoll{
		fd:        fd,
		sysEvents: make([]unix.EpollEvent, 0, 10),
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
	return unix.Close(fd)
}

// Fd returns the integer unix file descriptor referencing the open file.
func (e *Epoll) Fd() int {
	return e.fd
}

// Register registers a file descriptor and allows observing speicifc I/O readiness events.
//
// Please refer to epoll_ctl(2) and EPOLL_CTL_ADD for details.
func (e *Epoll) Register(fd int, mask Readiness) error {
	err := unix.EpollCtl(e.fd, unix.EPOLL_CTL_ADD, fd, &unix.EpollEvent{
		Events: uint32(mask),
		Fd:     int32(fd),
	})
	if err == nil {
		e.sysEvents = append(e.sysEvents, unix.EpollEvent{})
	}
	runtime.KeepAlive(e)
	return err
}

// Deregister removes the given file descriptor from the epoll instance.
//
// Please refer to epoll_ctl(2) and EPOLL_CTL_DEL for details.
func (e *Epoll) Deregister(fd int) error {
	err := unix.EpollCtl(e.fd, unix.EPOLL_CTL_DEL, fd, &unix.EpollEvent{})
	runtime.KeepAlive(e)
	if err == nil {
		e.sysEvents = e.sysEvents[:len(e.sysEvents)-1]
	}
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

// WaitTimeout blocks and waits with the given timeout for arrival of events on any of the added file descriptors.
//
// A msec value of -1 disables timeout.
//
// Please refer to epoll_wait(2) and EPOLL_WAIT for details.
//
// Warning, using epoll from Golang explicitly is tricky.
func (e *Epoll) WaitTimeout(msec int) ([]Event, error) {
	n, err := unix.EpollWait(e.fd, e.sysEvents, msec)
	runtime.KeepAlive(e)
	if err != nil {
		return nil, err
	}
	events := make([]Event, 0, n)
	for i := 0; i < n; i++ {
		event := Event{
			Fd:        int(e.sysEvents[i].Fd),
			Readiness: Readiness(e.sysEvents[i].Events),
		}
		events = append(events, event)
	}
	return events, nil
}

// Wait blocks and waits for arrival of events on any of the added file descriptors.
func (e *Epoll) Wait() ([]Event, error) {
	return e.WaitTimeout(-1)
}
