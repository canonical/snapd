// Package notifier implements a high-level interface to the apparmor
// notification mechanism. It can be used to build userspace applications
// which respond to apparmor prompting profiles.
package notifier

import (
	"errors"
	"fmt"
	"os"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/osutil/epoll"
	"github.com/snapcore/snapd/prompting/apparmor"
)

// Notifier contains low-level components for receiving notification requests
// and responding with notification responses.
type Notifier struct {
	// R is a channel with incoming requests. Each request is asynchronous
	// and needs to be replied to.
	R chan *Request
	// E is a channel for receiving asynchronous error messages from
	// concurrently running parts of the notifier system.
	E chan error

	notify *os.File
	poll   *epoll.Epoll
}

// Request is a high-level representation of an apparmor prompting message.
//
// Each request must be replied to by writing a boolean to the YesNo channel.
type Request struct {
	n *Notifier

	// Pid is the identifier of the process triggering the request.
	Pid uint32
	// Label is the apparmor label on the process triggering the request.
	Label string
	// SubjectUID is the UID of the subject that triggered the prompt
	SubjectUid uint32

	// Path is the path of the file, as seen by the process triggering the request.
	Path string
	// Permission is the opaque permission that is being requested.
	Permission interface{}
	// YesNo is a channel for writing the response.
	YesNo chan bool
}

func newRequest(n *Notifier, msg *apparmor.MsgNotificationFile) *Request {
	var perm interface{}
	if msg.Class == apparmor.MediationClassFile {
		_, deny, _ := msg.DecodeFilePermissions()
		perm = deny
	}
	return &Request{
		n: n, // why is this needed?

		Pid:        msg.Pid,
		Label:      msg.Label,
		Path:       msg.Name,
		SubjectUid: msg.SUID,

		Permission: perm,

		YesNo: make(chan bool, 1),
	}
}

var (
	// ErrNotifierNotSupported indicates that the kernel does not support apparmor prompting
	ErrNotifierNotSupported = errors.New("kernel does not support apparmor notifications")
)

// Register opens and configures the apparmor notification interface.
//
// If the kernel does not support the notification mechanism the error is ErrNotSupported.
func Register() (*Notifier, error) {
	path := apparmor.NotifyPath()
	if override := os.Getenv("PROMPT_NOTIFY_PATH"); override != "" {
		path = override
	}

	notify, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotifierNotSupported
		}
		return nil, err
	}

	msg := apparmor.MsgNotificationFilter{ModeSet: apparmor.ModeSetUser}
	data, err := msg.MarshalBinary()
	if err != nil {
		notify.Close()
		return nil, err
	}
	_, err = apparmor.NotifyIoctl(notify.Fd(), apparmor.IoctlSetFilter, data)
	// TODO: check ioctl return size
	if err != nil {
		notify.Close()
		return nil, fmt.Errorf("cannot notify ioctl %q: %v", path, err)
	}

	poll, err := epoll.Open()
	if err != nil {
		notify.Close()
		return nil, fmt.Errorf("cannot open %q: %v", path, err)
	}
	// XXX: Do we need a notification for Writable, to send responses back?
	if err := poll.Register(int(notify.Fd()), epoll.Readable); err != nil {
		notify.Close()
		poll.Close()
		return nil, fmt.Errorf("cannot register poll on %q: %v", path, err)
	}

	notifier := &Notifier{
		R: make(chan *Request),
		E: make(chan error),

		notify: notify,
		poll:   poll,
	}
	return notifier, nil
}

func (n *Notifier) decodeAndDispatchRequest(buf []byte, tomb *tomb.Tomb) error {
	var nmsg apparmor.MsgNotification
	if err := nmsg.UnmarshalBinary(buf); err != nil {
		return err
	}
	// What kind of notification message did we get?
	switch nmsg.NotificationType {
	case apparmor.Operation:
		var omsg apparmor.MsgNotificationOp
		if err := omsg.UnmarshalBinary(buf); err != nil {
			return err
		}
		// What kind of operation notification did we get?
		switch omsg.Class {
		case apparmor.MediationClassFile:
			var fmsg apparmor.MsgNotificationFile
			if err := fmsg.UnmarshalBinary(buf); err != nil {
				return err
			}
			// log.Printf("notification request: %#v\n", fmsg)
			req := newRequest(n, &fmsg)
			n.R <- req
			tomb.Go(func() error {
				n.waitAndRespond(req, &fmsg)
				return nil
			})
		default:
			return fmt.Errorf("unsupported mediation class : %v", omsg.Class)
		}
	default:
		return fmt.Errorf("unsupported notification type: %v", nmsg.NotificationType)
	}
	return nil
}

func (n *Notifier) waitAndRespond(req *Request, msg *apparmor.MsgNotificationFile) {
	resp := apparmor.ResponseForRequest(&msg.MsgNotification)
	// XXX: should both error fields be zeroed?
	resp.MsgNotification.Error = 0
	// XXX: flags 1 means not-cache the reply, make this a proper named flag
	resp.MsgNotification.Flags = 1
	if allow := <-req.YesNo; allow {
		resp.Allow = msg.Allow | msg.Deny
		resp.Deny = 0
		resp.Error = 0
	} else {
		resp.Allow = 0
		resp.Deny = msg.Deny
		resp.Error = msg.Error
	}
	//log.Printf("notification response: %#v\n", resp)
	if err := n.encodeAndSendResponse(&resp); err != nil {
		n.fail(err)
	}
}

func (n *Notifier) encodeAndSendResponse(resp *apparmor.MsgNotificationResponse) error {
	buf, err := resp.MarshalBinary()
	if err != nil {
		return err
	}
	_, err = apparmor.NotifyIoctl(n.notify.Fd(), apparmor.IoctlSend, buf)
	return err
}

func (n *Notifier) runOnce(tomb *tomb.Tomb) error {
	// XXX: Wait must return immediately once epoll is closed.
	events, err := n.poll.Wait()
	if err != nil {
		return err
	}
	for _, event := range events {
		switch event.Fd {
		case int(n.notify.Fd()):
			if event.Readiness&epoll.Readable != 0 {
				// Prepare a receive buffer for incoming request. The buffer is of the
				// maximum allowed size and will contain one kernel request upon return.
				// Note that the actually occupied buffer is indicated by the Length field
				// in the header.
				buf := apparmor.RequestBuffer()
				size, err := apparmor.NotifyIoctl(n.notify.Fd(), apparmor.IoctlReceive, buf)
				if err != nil {
					return err
				}
				if err := n.decodeAndDispatchRequest(buf[:size], tomb); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// Run reads and dispatches kernel requests until stopped.
func (n *Notifier) Run(tomb *tomb.Tomb) {
	// TODO: allow the run to stop
	for {
		if err := n.runOnce(tomb); err != nil {
			n.fail(err)
			break
		}
	}
}

func (n *Notifier) fail(err error) {
	n.E <- err
	close(n.E)
	close(n.R)
}

// Close closes the kernel communication file.
func (n *Notifier) Close() error {
	err1 := n.notify.Close()
	err2 := n.poll.Close()
	if err1 != nil {
		return err1
	}
	return err2
}
