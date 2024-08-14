// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023-2024 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

// Package listener implements a high-level interface to the apparmor
// notification mechanism. It can be used to build userspace applications
// which respond to apparmor prompting profiles.
package listener

import (
	"errors"
	"fmt"
	"os"
	"sync/atomic"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil/epoll"
	"github.com/snapcore/snapd/sandbox/apparmor/notify"
)

var (
	// ErrClosed indicates that the listener has been closed.
	ErrClosed = errors.New("listener has been closed")

	// ErrAlreadyRun indicates that the Run() method has already been run.
	// Each listener must only be run once, so that spawned goroutines can
	// be safely tracked and terminated.
	ErrAlreadyRun = errors.New("listener has already been run")

	// ErrAlreadyClosed indicates that the listener has previously been closed.
	ErrAlreadyClosed = errors.New("listener has already been closed")

	// ErrAlreadyReplied indicates that the request has already received a reply.
	ErrAlreadyReplied = errors.New("request has already received a reply")

	// ErrNotSupported indicates that the kernel does not support apparmor prompting.
	ErrNotSupported = errors.New("kernel does not support apparmor notifications")

	osOpen      = os.Open
	notifyIoctl = notify.Ioctl
)

// Response includes whether the request is allowed or denied, along with the
// permissions for which the decision should apply.
type Response struct {
	Allow      bool
	Permission any
}

// Request is a high-level representation of an apparmor prompting message.
//
// Each request must be replied to by writing a boolean to the YesNo channel.
type Request struct {
	// PID is the identifier of the process which triggered the request.
	PID uint32
	// Label is the apparmor label on the process which triggered the request.
	Label string
	// SubjectUID is the UID of the subject which triggered the request.
	SubjectUID uint32

	// Path is the path of the file, as seen by the process triggering the request.
	Path string
	// Class is the mediation class corresponding to this request.
	Class notify.MediationClass
	// Permission is the opaque permission that is being requested.
	Permission any

	// replyChan is a channel for writing the response.
	replyChan chan *Response
	// replied indicates whether a reply has already been sent for this request.
	replied uint32
}

func newRequest(msg *notify.MsgNotificationFile) (*Request, error) {
	var perm any
	switch msg.Class {
	case notify.AA_CLASS_FILE:
		_, missingPerms, err := msg.DecodeFilePermissions()
		if err != nil {
			return nil, err
		}
		perm = missingPerms
	default:
		return nil, fmt.Errorf("unsupported mediation class: %v", msg.Class)
	}
	return &Request{
		PID:        msg.Pid,
		Label:      msg.Label,
		SubjectUID: msg.SUID,

		Path:       msg.Name,
		Class:      msg.Class,
		Permission: perm,

		replyChan: make(chan *Response, 1),
	}, nil
}

// Reply sends the given response back to the kernel.
func (r *Request) Reply(response *Response) error {
	if !atomic.CompareAndSwapUint32(&r.replied, 0, 1) {
		return ErrAlreadyReplied
	}
	var ok bool
	switch r.Class {
	case notify.AA_CLASS_FILE:
		_, ok = response.Permission.(notify.FilePermission)
	default:
		// should not occur, since the request was created in this package
		return fmt.Errorf("internal error: unsupported mediation class: %v", r.Class)
	}
	if !ok {
		expectedType := expectedResponseTypeForClass(r.Class)
		return fmt.Errorf("invalid reply: response permission must be of type %s", expectedType)
	}
	r.replyChan <- response
	return nil
}

func expectedResponseTypeForClass(class notify.MediationClass) string {
	switch class {
	case notify.AA_CLASS_FILE:
		return "notify.FilePermission"
	default:
		// This should never occur, as caller should return an error before
		// calling this if the class is unsupported.
		return "???"
	}
}

// Listener encapsulates a loop for receiving apparmor notification requests
// and responding with notification responses, hiding the low-level details.
type Listener struct {
	// reqs is a channel with incoming requests. Each request is asynchronous
	// and needs to be replied to.
	reqs chan *Request

	notifyFile *os.File
	poll       *epoll.Epoll

	tomb tomb.Tomb

	// status keeps track of whether the listener has been run and/or closed,
	// both to ensure that Run() and Close() are executed at most once each,
	// and to ensure that the listener cannot be run after it has been closed.
	// Must be read/modified atomically, and can only be changed in one of the
	// following ways:
	// - statusReady -> statusRunning
	// - statusReady -> statusClosed
	// - statusRunning -> statusClosed
	status uint32
}

const (
	statusReady uint32 = iota
	statusRunning
	statusClosed
)

// Register opens and configures the apparmor notification interface.
//
// If the kernel does not support the notification mechanism the error is ErrNotSupported.
func Register() (listener *Listener, err error) {
	path := notify.SysPath
	if override := os.Getenv("PROMPT_NOTIFY_PATH"); override != "" {
		path = override
	}

	notifyFile, err := osOpen(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotSupported
		}
		return nil, fmt.Errorf("cannot open %q: %v", path, err)
	}
	defer func() {
		if err != nil {
			notifyFile.Close()
		}
	}()

	msg := notify.MsgNotificationFilter{ModeSet: notify.APPARMOR_MODESET_USER}
	data, err := msg.MarshalBinary()
	if err != nil {
		return nil, err
	}
	ioctlBuf := notify.IoctlRequestBuffer(data)
	_, err = notifyIoctl(notifyFile.Fd(), notify.APPARMOR_NOTIF_SET_FILTER, ioctlBuf)
	if err != nil {
		return nil, fmt.Errorf("cannot notify ioctl to modeset user on %q: %v", path, err)
	}

	poll, err := epoll.Open()
	if err != nil {
		return nil, fmt.Errorf("cannot open epoll file descriptor: %v", err)
	}
	defer func() {
		if err != nil {
			poll.Close()
		}
	}()
	if err = poll.Register(int(notifyFile.Fd()), epoll.Readable); err != nil {
		return nil, fmt.Errorf("cannot register epoll on %q: %v", path, err)
	}

	listener = &Listener{
		reqs: make(chan *Request, 1),

		notifyFile: notifyFile,
		poll:       poll,
	}
	return listener, nil
}

// Close stops the listener and closes the kernel communication file.
// Returns once all waiting goroutines terminate and the communication file
// is closed.
func (l *Listener) Close() error {
	origStatus := atomic.SwapUint32(&l.status, statusClosed)
	switch origStatus {
	case statusReady:
		// A goroutine was never spawned with the listener tomb.
		// Spawn one, so the tomb can die.
		l.tomb.Go(func() error {
			<-l.tomb.Dying()
			return nil
		})
	case statusRunning:
	case statusClosed:
		l.tomb.Wait()
		return ErrAlreadyClosed
	}
	l.tomb.Kill(ErrClosed)
	// Close epoll instance to stop the run loop waiting on epoll events
	err1 := l.poll.Close()
	// Wait for main run loop and waiters to terminate
	l.tomb.Wait()
	// l.reqs is only written to by run loop, so it's now safe to close
	close(l.reqs)
	// Closing the notify file signals to the kernel that the listener is
	// disconnecting, so the kernel will send back denials or pass requests
	// on to other listeners which connect.
	err2 := l.notifyFile.Close()
	if err1 != nil {
		return err1
	}
	return err2
}

// Reqs returns a read-only channel through which requests may be received.
// The channel is closed when the Close() method is called or an error occurs.
func (l *Listener) Reqs() <-chan *Request {
	return l.reqs
}

// Allow tests to kill the listener instead of logging errors so that tests
// don't have race condition failures.
var exitOnError = false

// Run reads and dispatches kernel requests until the listener is closed.
//
// Run should only be called once per listener object. If called more than once,
// Run returns an error. Otherwise, waits until the listener stops, and returns
// the cause as an error. If the listener was intentionally stopped via the
// Close() method, returns nil.
func (l *Listener) Run() error {
	if !atomic.CompareAndSwapUint32(&l.status, statusReady, statusRunning) {
		currStatus := atomic.LoadUint32(&l.status)
		switch currStatus {
		case statusRunning:
			return ErrAlreadyRun
		case statusClosed:
			return ErrAlreadyClosed
		default:
			return fmt.Errorf("listener has unexpected status: %d", currStatus)
		}
	}
	// This is the first and only time calling Run().
	// Even if Close() kills the tomb before l.tomb.Go() occurs, a panic will
	// not occur, since this new goroutine will (immediately after l.tomb.Err()
	// is called) return and close the tomb's dead channel, as it is the last
	// and only tracked goroutine.
	l.tomb.Go(func() error {
		for {
			if err := l.tomb.Err(); err != tomb.ErrStillAlive {
				// Do not log error here, as the only error from outside of
				// runOnce should be when listener was deliberately closed,
				// and we don't want a log message for that.
				break
			}
			err := l.runOnce()
			if err != nil {
				if exitOnError {
					return err
				} else {
					logger.Noticef("error in prompting listener run loop: %v", err)
				}
			}
		}
		return nil
	})
	// Wait for an error to occur or the listener to be explicitly closed.
	<-l.tomb.Dying()
	// Close the listener, in case an internal error occurred and Close()
	// was not explicitly called.
	l.Close()
	return l.tomb.Err()
}

var listenerEpollWait = func(l *Listener) ([]epoll.Event, error) {
	return l.poll.Wait()
}

func (l *Listener) runOnce() error {
	events, err := listenerEpollWait(l)
	if err != nil {
		// If epoll instance is closed, then tomb error status has already
		// been set. Otherwise, this is a true error. Either way, return it.
		return err
	}
	for _, event := range events {
		if event.Fd != int(l.notifyFile.Fd()) {
			logger.Debugf("unexpected event from fd %v (%v)", event.Fd, event.Readiness)
			continue
		}
		if event.Readiness&epoll.Readable == 0 {
			continue
		}
		// Prepare a receive buffer for incoming request. The buffer is of the
		// maximum allowed size and will contain one or more kernel requests
		// upon return.
		ioctlBuf := notify.NewIoctlRequestBuffer()
		buf, err := notifyIoctl(l.notifyFile.Fd(), notify.APPARMOR_NOTIF_RECV, ioctlBuf)
		if err != nil {
			// If epoll instance is closed, then tomb error status has already
			// been set. Otherwise, this is a true error. Either way, return it.
			return err
		}
		if err := l.decodeAndDispatchRequest(buf); err != nil {
			return err
		}
	}
	return nil
}

func (l *Listener) decodeAndDispatchRequest(buf []byte) error {
	for {
		first, rest, err := notify.ExtractFirstMsg(buf)
		if err != nil {
			return err
		}
		var nmsg notify.MsgNotification
		if err := nmsg.UnmarshalBinary(first); err != nil {
			return err
		}
		// What kind of notification message did we get?
		if nmsg.NotificationType != notify.APPARMOR_NOTIF_OP {
			return fmt.Errorf("unsupported notification type: %v", nmsg.NotificationType)
		}
		var omsg notify.MsgNotificationOp
		if err := omsg.UnmarshalBinary(first); err != nil {
			return err
		}
		// What kind of operation notification did we get?
		switch omsg.Class {
		case notify.AA_CLASS_FILE:
			if err := l.handleRequestAaClassFile(first); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported mediation class: %v", omsg.Class)
		}
		if len(rest) == 0 {
			return nil
		}
		buf = rest
	}
}

func (l *Listener) handleRequestAaClassFile(buf []byte) error {
	var fmsg notify.MsgNotificationFile
	if err := fmsg.UnmarshalBinary(buf); err != nil {
		return err
	}
	logger.Debugf("received prompt request from the kernel: %+v", fmsg)
	req, err := newRequest(&fmsg)
	if err != nil {
		return err
	}
	select {
	case l.reqs <- req:
		// request received
	case <-l.tomb.Dying():
		return l.tomb.Err()
	}
	l.tomb.Go(func() error {
		err := l.waitAndRespondAaClassFile(req, &fmsg)
		if err != nil {
			logger.Noticef("error while responding to kernel: %v", err)
		}
		return nil
	})
	return nil
}

func (l *Listener) waitAndRespondAaClassFile(req *Request, msg *notify.MsgNotificationFile) error {
	resp := notify.ResponseForRequest(&msg.MsgNotification)
	resp.MsgNotification.Error = 0 // ignored in responses
	resp.MsgNotification.NoCache = 1
	var response *Response
	select {
	case response = <-req.replyChan:
		break
	case <-l.tomb.Dying():
		// don't bother sending deny response, kernel will auto-deny if needed
		return nil
	}
	allow := response.Allow
	perms, ok := response.Permission.(notify.FilePermission)
	if !ok {
		// should not occur, Reply() checks that type is correct
		logger.Debugf("invalid reply from client: %+v; denying request", response)
		allow = false
	}
	if allow {
		// allow permissions which kernel initially allowed, along with those
		// which the were initially denied but the user explicitly allowed.
		resp.Allow = msg.Allow | (uint32(perms) & msg.Deny)
		resp.Deny = 0
		resp.Error = 0
	} else {
		resp.Allow = msg.Allow
		resp.Deny = msg.Deny
		resp.Error = msg.Error
		// msg.Error is field from MsgNotificationResponse, and is unused.
		// msg.MsgNotification.Error is also ignored in responses.
	}
	logger.Debugf("sending request response back to the kernel: %+v", resp)
	return l.encodeAndSendResponse(&resp)
}

func (l *Listener) encodeAndSendResponse(resp *notify.MsgNotificationResponse) error {
	buf, err := resp.MarshalBinary()
	if err != nil {
		return err
	}
	ioctlBuf := notify.IoctlRequestBuffer(buf)
	_, err = notifyIoctl(l.notifyFile.Fd(), notify.APPARMOR_NOTIF_SEND, ioctlBuf)
	return err
}
