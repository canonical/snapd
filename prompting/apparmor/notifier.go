package apparmor

import (
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/snapcore/cerberus/epoll"
)

type Notifier struct {
	R chan *Request
	E chan error

	notify *os.File
	poll   *epoll.Epoll
}

type Request struct {
	n *Notifier

	Pid   uint32
	Label string
	Path  string

	YesNo chan bool
}

func NewRequest(n *Notifier, msg *MsgNotificationFile) *Request {
	return &Request{
		n: n,

		Pid:   msg.Pid,
		Label: msg.Label,
		Path:  msg.Name,

		YesNo: make(chan bool, 1),
	}
}

var (
	// ErrNotifierNotSupported indicates that the kernel does not support apparmor prompting
	ErrNotifierNotSupported = errors.New("kernel does not support apparmor notifications")
)

// RegisterNotifier returns a new listener talking to the kernel apparmor notification interface.
//
// If the kernel does not support the notification mechanism the error is ErrNotSupported.
func RegisterNotifier() (*Notifier, error) {
	notify, err := os.Open("/sys/kernel/security/apparmor/.notify")
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotifierNotSupported
		}
		return nil, err
	}
	msg := MsgNotificationFilter{ModeSet: ModeSetUser}
	data, err := msg.MarshalBinary()
	if err != nil {
		notify.Close()
		return nil, err
	}
	_, err = NotifyIoctl(notify.Fd(), IoctlSetFilter, data)
	// TODO: check ioctl return size
	if err != nil {
		notify.Close()
		return nil, err
	}

	poll, err := epoll.Open()
	if err != nil {
		notify.Close()
		return nil, err
	}
	if err := poll.Register(int(notify.Fd()), epoll.Readable|epoll.Writable); err != nil {
		notify.Close()
		poll.Close()
		return nil, err
	}

	notifier := &Notifier{
		R: make(chan *Request),
		E: make(chan error),

		notify: notify,
		poll:   poll,
	}
	return notifier, nil
}

func (n *Notifier) decodeAndDispatchRequest(buf []byte) error {
	var nmsg MsgNotification
	if err := nmsg.UnmarshalBinary(buf); err != nil {
		return err
	}
	// What kind of notification message did we get?
	switch nmsg.NotificationType {
	case Operation:
		var omsg MsgNotificationOp
		if err := omsg.UnmarshalBinary(buf); err != nil {
			return err
		}
		// What kind of operation notification did we get?
		switch omsg.Class {
		case MediationClassFile:
			var fmsg MsgNotificationFile
			if err := fmsg.UnmarshalBinary(buf); err != nil {
				return err
			}
			log.Printf("notification request: %#v\n", fmsg)
			req := NewRequest(n, &fmsg)
			n.R <- req
			// XXX: The request interface is synchronous. Attempting to wait for
			// another request before this one is replied to causes ioctl to
			// return ENOENT.
			n.waitAndRespond(req, &fmsg)
		default:
			return fmt.Errorf("unsupported mediation class : %v", omsg.Class)
		}
	default:
		return fmt.Errorf("unsupported notification type: %v", nmsg.NotificationType)
	}
	return nil
}

func (n *Notifier) waitAndRespond(req *Request, msg *MsgNotificationFile) {
	resp := ResponseForRequest(&msg.MsgNotification)
	// XXX: should both error fields be zeroed?
	resp.MsgNotification.Error = 0
	if allow := <-req.YesNo; allow {
		resp.Allow = msg.Allow | msg.Deny
		resp.Deny = 0
		resp.Error = 0
	} else {
		resp.Allow = 0
		resp.Deny = msg.Deny
		resp.Error = msg.Error
	}
	log.Printf("notification response: %#v\n", resp)
	if err := n.encodeAndSendResponse(&resp); err != nil {
		n.fail(err)
	}
}

func (n *Notifier) encodeAndSendResponse(resp *MsgNotificationResponse) error {
	buf, err := resp.MarshalBinary()
	if err != nil {
		return err
	}
	_, err = NotifyIoctl(n.notify.Fd(), IoctlSend, buf)
	return err
}

func (n *Notifier) runOnce() error {
	// XXX: Wait must return immediately once epoll is closed.
	events, err := n.poll.Wait()
	if err != nil {
		return err
	}
	for _, event := range events {
		switch event.Fd {
		case int(n.notify.Fd()):
			// Prepare a receive buffer for incoming request. The buffer is of the
			// maximum allowed size and will contain one kernel request upon return.
			// Note that the actually occupied buffer is indicated by the Length field
			// in the header.
			buf := RequestBuffer()
			size, err := NotifyIoctl(n.notify.Fd(), IoctlReceive, buf)
			if err != nil {
				return err
			}
			if err := n.decodeAndDispatchRequest(buf[:size]); err != nil {
				return err
			}
		}
	}
	return nil
}

// Run reads and dispatches kernel requests until stopped.
func (n *Notifier) Run() {
	// TODO: allow the run to stop
	for {
		if err := n.runOnce(); err != nil {
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
