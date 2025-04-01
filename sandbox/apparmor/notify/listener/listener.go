// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023-2025 Canonical Ltd
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
	"sync"
	"time"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil/epoll"
	"github.com/snapcore/snapd/sandbox/apparmor"
	"github.com/snapcore/snapd/sandbox/apparmor/notify"
)

var (
	// ErrClosed indicates that the listener has been closed.
	ErrClosed = errors.New("listener has been closed")

	// ErrAlreadyClosed indicates that the listener has previously been closed.
	ErrAlreadyClosed = errors.New("listener has already been closed")

	// ErrNotSupported indicates that the kernel does not support apparmor prompting.
	ErrNotSupported = errors.New("kernel does not support apparmor notifications")

	// readyTimeout is the time to wait to re-receive and process previously-
	// pending requests from the kernel before we tell the manager we're ready.
	// We should be able to re-receive messages very quickly, but there must be
	// someone listening over l.reqs to quickly receive and process them too.
	readyTimeout = time.Duration(5 * time.Second)

	osOpen                       = os.Open
	notifyRegisterFileDescriptor = notify.RegisterFileDescriptor
	notifyIoctl                  = notify.Ioctl
)

// Request is a high-level representation of an apparmor prompting message.
//
// A request must be replied to via its Reply method.
type Request struct {
	// ID is the unique ID of the message notification associated with the request.
	ID uint64
	// PID is the identifier of the process which triggered the request.
	PID int32
	// Label is the apparmor label on the process which triggered the request.
	Label string
	// SubjectUID is the UID of the subject which triggered the request.
	SubjectUID uint32

	// Path is the path of the file, as seen by the process triggering the request.
	Path string
	// Class is the mediation class corresponding to this request.
	Class notify.MediationClass
	// Permission is the opaque permission that is being requested.
	Permission notify.AppArmorPermission
	// AaAllowed is the opaque permission mask which was already allowed by
	// AppArmor rules.
	AaAllowed notify.AppArmorPermission
	// Tagsets is the metadata tagsets associated with the permissions in the
	// request. The tagsets map from permission mask to the list of tags
	// associated with those permissions.
	Tagsets notify.TagsetMap

	// listener is a pointer to the Listener which will handle the reply.
	listener *Listener
}

// Reply validates that the given permission is of the appropriate type for
// the mediation class associated with the request, and then constructs a
// response which allows those permissions and sends it to the kernel.
func (r *Request) Reply(allowedPermission notify.AppArmorPermission) error {
	var ok bool
	switch r.Class {
	case notify.AA_CLASS_FILE:
		_, ok = allowedPermission.(notify.FilePermission)
	default:
		// should not occur, since the request was created in this package
		return fmt.Errorf("internal error: unsupported mediation class: %v", r.Class)
	}
	// Treat nil allowedPermission as allowing no permissions, which is valid
	if !ok && allowedPermission != nil {
		expectedType := expectedResponseTypeForClass(r.Class)
		return fmt.Errorf("invalid reply: response permission must be of type %s", expectedType)
	}

	resp := notify.BuildResponse(r.listener.protocolVersion, r.ID, r.AaAllowed, r.Permission, allowedPermission)

	return encodeAndSendResponse(r.listener, resp)
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
	// once ensures that the Run method is only run once, to avoid closing
	// a channel multiple times
	once sync.Once

	// reqs is a channel over which to send requests to the manager.
	// Only the main run loop may close this channel.
	reqs chan *Request

	// pendingCount is the number of "pending" (NOTIF_RESENT) messages still
	// expected to be re-received from the kernel.
	//
	// XXX: In order for this count to be accurate, it is important that the
	// pending messages at time of registration are only re-sent by the kernel
	// once, and that no messages which were first sent this listener was
	// registered are resent. If, however, we are able to reset this count
	// based on the result of the resend command, and the ready channel has not
	// yet been closed, this is okay.
	pendingCount int
	// ready is a channel which is closed once all requests which were pending
	// at time of registration have been re-received from the kernel and sent
	// over the reqs channel. This occurs once pendingCount reaches 0.
	//
	// Until this occurs, other new (not NOTF_RESENT) requests will be queued
	// up by the listener, and only sent once all NOTIF_RESENT requests have
	// been sent over the reqs channel.
	ready chan struct{}
	// readyQueue is the queue of "ready" (not NOTIF_RESENT) requests from the
	// kernel which were received before any remaining pending requests were
	// re-received. Once the final pending (NOTIF_RESENT) message is received
	// from the kernel and sent over the reqs channel, then send all of these
	// requests (in the order they were received) and discard the queue.
	//
	// The motivation for queueing non-resent requests until the listener is
	// ready is so that consumers of the listener requests can re-create their
	// state (i.e. prompts) fully before any new requests are received. In
	// particular, if a request were received with the same content as one
	// which was previously sent, before the latter was re-sent, a prompt may
	// be created for the new request with a new ID. Then, when the original
	// request is re-received, it must either be merged into the new prompt,
	// and its original prompt ID marked as cancelled, or a prompt with the
	// original ID can be recreated, resulting in two different prompts with
	// the same contents. By ensuring all pending requests are re-received
	// before any new ones are sent, situations like these can be avoided.
	readyQueue []*Request
	// readyTimer is a timer which will close the ready channel if not enough
	// pending requests are re-received before a timeout expires.
	readyTimer *time.Timer

	// protocolVersion is the notification protocol version associated with the
	// listener's notify socket. Once registered with a particular version,
	// that version will be used for all messages sent or received over that
	// socket.
	protocolVersion notify.ProtocolVersion

	// socketMu guards the notify file, the epoll instance, and the close chan.
	socketMu   sync.Mutex
	notifyFile *os.File
	poll       *epoll.Epoll
	// closeChan will be closed by Close to indicate to the run loop that the
	// listener should be closed.
	closeChan chan struct{}
}

// Register opens and configures the apparmor notification interface.
//
// If the kernel does not support the notification mechanism the error is ErrNotSupported.
func Register() (listener *Listener, err error) {
	path := apparmor.NotifySocketPath
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

	protoVersion, pendingCount, err := notifyRegisterFileDescriptor(notifyFile.Fd())
	if err != nil {
		return nil, err
	}

	listener = &Listener{
		reqs: make(chan *Request),

		pendingCount: pendingCount,
		ready:        make(chan struct{}),
		readyTimer:   time.NewTimer(0), // initialize placeholder non-nil timer

		protocolVersion: protoVersion,

		notifyFile: notifyFile,
		poll:       poll,
		closeChan:  make(chan struct{}),
	}
	if pendingCount == 0 {
		close(listener.ready)
	} else {
		// Set a real timer
		listener.readyTimer = time.AfterFunc(readyTimeout, listener.signalReadyAndFlushQueue)
	}
	return listener, nil
}

// isClosed returns true if the listener has been closed.
//
// Any caller which must ensure that a Close is not in progress should hold the
// lock while checking isClosed, and continue to hold the lock until it is safe
// for Close to be run.
func (l *Listener) isClosed() bool {
	select {
	case <-l.closeChan:
		return true
	default:
		return false
	}
}

// Close stops the listener and closes the kernel communication file.
func (l *Listener) Close() error {
	l.socketMu.Lock()
	defer l.socketMu.Unlock()
	if l.isClosed() {
		return ErrAlreadyClosed
	}

	// Closing the notify file signals to the kernel that the listener is
	// disconnecting, so the kernel will send back denials or pass requests
	// on to other listeners which connect. Do this before closing the epoll
	// instance so that the kernel does not try to send any further messages
	// which won't be received.
	err1 := l.notifyFile.Close()

	// Close the close channel so that the the run loop knows to stop trying
	// to send requests over the request channel, and so that once the epoll
	// FD is closed (causing the syscall to error), it can check the closeChan
	// to see whether Close was called or whether a real error occurred.
	close(l.closeChan)

	// Close the epoll so that if the run loop is waiting on an event, it will
	// return an error.
	err2 := l.poll.Close()
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

// Ready returns a read-only channel which will be closed once all requests
// which were pending when the listener was registered have been re-received
// from the kernel and sent over the reqs channel. No non-resent requests will
// be sent until all originally-pending requests have been resent.
//
// The channel will close automatically after a timeout even if not all pending
// requests have been re-received.
//
// The caller may wish to block new prompt replies or rules until after the
// ready channel has been closed.
func (l *Listener) Ready() <-chan struct{} {
	return l.ready
}

// Allow tests to kill the listener instead of logging errors so that tests
// don't have race condition failures.
var exitOnError = false

// Run reads and dispatches kernel requests until the listener is closed.
//
// Run should only be called once per listener object, and it runs until the
// listener is closed or errors (if exitOnError is true), and returns the cause.
// If the listener was intentionally stopped via the Close() method, returns nil.
func (l *Listener) Run() error {
	var err error
	l.once.Do(func() {
		// Run should only be called once, so this once.Do is really only an
		// extra precaution to ensure that l.reqs is only closed once.
		defer func() {
			if !l.readyTimer.Stop() {
				// Already fired or stopped (which in both cases should result
				// in signalReadyAndFlushQueue() being called), so ensure it
				// won't try to write to l.reqs by waiting for l.readyQueue to
				// be nil. This also handles closing the ready channel.
				for l.readyQueue != nil {
					time.Sleep(time.Millisecond)
				}
			}
			// Don't want to close l.ready as an else here, since that could
			// tell the manager to let some method calls begin, whereas what we
			// really want is to indicate that we're stopping by closing l.reqs.

			// When listener run loop ends, close the requests channel.
			close(l.reqs)

			// Don't bother closing the ready channel, since it may already be
			// closed, and the manager closes its own ready channel once it has
			// been stopped and doesn't check the listener ready channel again.
		}()
		for {
			err = l.handleRequests()
			if err != nil {
				if errors.Is(err, ErrClosed) {
					// Don't treat the listener closing as a real error
					err = nil
					return
				} else if exitOnError {
					l.Close() // make sure Close is called at least once
					return
				}
				logger.Noticef("error in prompting listener run loop: %v", err)
			}
		}
	})
	return err
}

var listenerEpollWait = func(l *Listener) ([]epoll.Event, error) {
	return l.poll.Wait()
}

func (l *Listener) handleRequests() error {
	// Get the socket FD with the lock held, so we don't break our contract.
	// Do this before any check whether the epoll instance is closed, so we
	// are sure to have the pre-close FD in case the instance is closed after
	// the check.
	l.socketMu.Lock()
	socketFd := int(l.notifyFile.Fd())
	l.socketMu.Unlock() // unlock immediately, we'll lock again later if needed

	events, err := listenerEpollWait(l)
	if err != nil {
		// The epoll syscall returned an error, so let's see whether it was
		// because we closed the epoll FD.
		if l.isClosed() {
			return ErrClosed
		}
		return err
	}

	for _, event := range events {
		if event.Fd != socketFd {
			logger.Debugf("unexpected event from fd %v (%v)", event.Fd, event.Readiness)
			continue
		}
		if event.Readiness&epoll.Readable == 0 {
			continue
		}
		// Prepare a receive buffer for incoming request. The buffer is of the
		// maximum allowed size and will contain one or more kernel requests
		// upon return.
		ioctlBuf := notify.NewIoctlRequestBuffer(l.protocolVersion)
		buf, err := l.doIoctl(notify.APPARMOR_NOTIF_RECV, ioctlBuf)
		if err != nil {
			return err
		}
		if err := l.decodeAndDispatchRequest(buf); err != nil {
			return err
		}
	}
	return nil
}

// doIoctl locks the mutex guarding the notify socket, checks whether the
// listener is being closed, and if not, sends an ioctl request with the given
// request type and buffer.
func (l *Listener) doIoctl(sendOrRecv notify.IoctlRequest, buf notify.IoctlRequestBuffer) ([]byte, error) {
	l.socketMu.Lock()
	defer l.socketMu.Unlock()
	if l.isClosed() {
		return nil, ErrClosed
	}
	return notifyIoctl(l.notifyFile.Fd(), sendOrRecv, buf)
}

// decodeAndDispatchRequest reads all messages from the given buffer, decodes
// each one into a message notification for a particular mediation class,
// creates a Request from that message, and attempts to send it to the manager
// via the request channel.
func (l *Listener) decodeAndDispatchRequest(buf []byte) error {
	for len(buf) > 0 {
		first, rest, err := notify.ExtractFirstMsg(buf)
		if err != nil {
			return err
		}
		buf = rest

		var nmsg notify.MsgNotification
		if err := nmsg.UnmarshalBinary(first); err != nil {
			return err
		}
		if nmsg.Version != l.protocolVersion {
			return fmt.Errorf("unexpected protocol version: listener registered with %d, but received %d", l.protocolVersion, nmsg.Version)
		}
		// What kind of notification message did we get? (I hope it's an Op)
		if nmsg.NotificationType != notify.APPARMOR_NOTIF_OP {
			return fmt.Errorf("unsupported notification type: %v", nmsg.NotificationType)
		}
		var omsg notify.MsgNotificationOp
		if err := omsg.UnmarshalBinary(first); err != nil {
			return err
		}

		var msg notify.MsgNotificationGeneric
		// What kind of operation notification did we get?
		switch omsg.Class {
		case notify.AA_CLASS_FILE:
			msg, err = parseMsgNotificationFile(first)
		default:
			return fmt.Errorf("unsupported mediation class: %v", omsg.Class)
		}
		if err != nil {
			return err
		}

		// Build request
		req, err := l.newRequest(msg)
		if err != nil {
			return err
		}

		// Before sending the request, check if it should instead be queued.
		if l.pendingCount > 0 && !msg.Resent() {
			// Not a previously-sent pending message, so it's from the
			// kernel's ready queue, not the pending queue. Queue it up to
			// be sent once all pending messages have been re-received.
			l.readyQueue = append(l.readyQueue, req)
			continue
		}

		// Try to send request to manager, or wait for listener to be closed
		select {
		case l.reqs <- req:
			// request received
		case <-l.closeChan:
			// The listener is being closed, so stop trying to deliver the
			// message up to the manager. It will appear to the kernel that we
			// have received and processed the request, when in reality, we
			// have not. The higher-level restart handling logic will ensure
			// that the request will be re-sent to snapd when it restarts and
			// registers a new listener.
			return ErrClosed
		}

		if l.pendingCount > 0 && msg.Resent() {
			// Message was previously-sent, so decrease the pendingCount
			l.pendingCount--
			if l.pendingCount > 0 {
				continue
			}
			// This was the final pending request we were waiting for.
			if l.readyTimer.Stop() {
				// We stopped the timer before it fired, so we can signal
				// ready. Otherwise, the timer already signalled for us.
				l.signalReadyAndFlushQueue()
			}
		}
	}
	return nil
}

func parseMsgNotificationFile(buf []byte) (*notify.MsgNotificationFile, error) {
	var fmsg notify.MsgNotificationFile
	if err := fmsg.UnmarshalBinary(buf); err != nil {
		return nil, err
	}
	logger.Debugf("received file request from the kernel: %+v", fmsg)
	return &fmsg, nil
}

func (l *Listener) newRequest(msg notify.MsgNotificationGeneric) (*Request, error) {
	aaAllowed, aaDenied, err := msg.AllowedDeniedPermissions()
	if err != nil {
		return nil, err
	}
	return &Request{
		ID:         msg.ID(),
		PID:        msg.PID(),
		Label:      msg.ProcessLabel(),
		SubjectUID: msg.SubjectUID(),

		Path:       msg.Name(),
		Class:      msg.MediationClass(),
		Permission: aaDenied, // Request permissions which were initially denied
		AaAllowed:  aaAllowed,
		Tagsets:    msg.DeniedMetadataTagsets(),

		listener: l,
	}, nil
}

// The caller(s) must ensure that this method is only called once per listener,
// and that l.Close() is not called between when this function is called and
// when it returns.
func (l *Listener) signalReadyAndFlushQueue() {
	close(l.ready)
	for _, req := range l.readyQueue {
		// Try to send request to manager, or wait for listener to be closed
		select {
		case l.reqs <- req:
			// request received
		case <-l.closeChan:
			// listener closing, so don't try to send over l.reqs again.
			// Break rather than return so we're sure to set readyQueue to nil.
			break
		}
	}
	// Clear the ready queue. Since every pending request has been re-received,
	// we'll never need the queue again and all future requests can be sent
	// directly. Setting readyQueue to nil also indicates that this function is
	// done attempting to write to l.reqs.
	l.readyQueue = nil
}

var encodeAndSendResponse = func(l *Listener, resp *notify.MsgNotificationResponse) error {
	return l.encodeAndSendResponse(resp)
}

func (l *Listener) encodeAndSendResponse(resp *notify.MsgNotificationResponse) error {
	buf, err := resp.MarshalBinary()
	if err != nil {
		return err
	}
	ioctlBuf := notify.IoctlRequestBuffer(buf)
	_, err = l.doIoctl(notify.APPARMOR_NOTIF_SEND, ioctlBuf)
	return err
}
