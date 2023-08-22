// Package listener implements a high-level interface to the apparmor
// notification mechanism. It can be used to build userspace applications
// which respond to apparmor prompting profiles.
package listener

import (
	"errors"
	"fmt"
	"os"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/osutil/epoll"
	"github.com/snapcore/snapd/sandbox/apparmor/notify"
)

// Listener contains low-level components for receiving notification requests
// and responding with notification responses.
type Listener struct {
	// R is a channel with incoming requests. Each request is asynchronous
	// and needs to be replied to.
	R chan *Request
	// E is a channel for receiving asynchronous error messages from
	// concurrently running parts of the listener system.
	E chan error

	notifyFile *os.File
	poll       *epoll.Epoll
}

// Request is a high-level representation of an apparmor prompting message.
//
// Each request must be replied to by writing a boolean to the YesNo channel.
type Request struct {
	l *Listener

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

func newRequest(l *Listener, msg *notify.MsgNotificationFile) *Request {
	var perm interface{}
	if msg.Class == notify.AA_CLASS_FILE {
		_, deny, _ := msg.DecodeFilePermissions()
		perm = deny
	}
	return &Request{
		l: l, // why is this needed?

		Pid:        msg.Pid,
		Label:      msg.Label,
		Path:       msg.Name,
		SubjectUid: msg.SUID,

		Permission: perm,

		YesNo: make(chan bool, 1),
	}
}

var (
	// ErrListenerNotSupported indicates that the kernel does not support apparmor prompting
	ErrListenerNotSupported = errors.New("kernel does not support apparmor notifications")
)

// Register opens and configures the apparmor notification interface.
//
// If the kernel does not support the notification mechanism the error is ErrNotSupported.
func Register() (*Listener, error) {
	path := notify.SysPath
	if override := os.Getenv("PROMPT_NOTIFY_PATH"); override != "" {
		path = override
	}

	notifyFile, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrListenerNotSupported
		}
		return nil, err
	}

	msg := notify.MsgNotificationFilter{ModeSet: notify.APPARMOR_MODESET_USER}
	data, err := msg.MarshalBinary()
	if err != nil {
		notifyFile.Close()
		return nil, err
	}
	ioctlBuf := notify.BytesToIoctlRequestBuffer(data)
	_, err = notify.Ioctl(notifyFile.Fd(), notify.APPARMOR_NOTIF_SET_FILTER, ioctlBuf)
	// TODO: check ioctl return size
	if err != nil {
		notifyFile.Close()
		return nil, fmt.Errorf("cannot notify ioctl %q: %v", path, err)
	}

	poll, err := epoll.Open()
	if err != nil {
		notifyFile.Close()
		return nil, fmt.Errorf("cannot open %q: %v", path, err)
	}
	// XXX: Do we need a notification for Writable, to send responses back?
	if err := poll.Register(int(notifyFile.Fd()), epoll.Readable); err != nil {
		notifyFile.Close()
		poll.Close()
		return nil, fmt.Errorf("cannot register poll on %q: %v", path, err)
	}

	listener := &Listener{
		R: make(chan *Request),
		E: make(chan error),

		notifyFile: notifyFile,
		poll:       poll,
	}
	return listener, nil
}

func (l *Listener) decodeAndDispatchRequest(buf []byte, tomb *tomb.Tomb) error {
	var nmsg notify.MsgNotification
	if err := nmsg.UnmarshalBinary(buf); err != nil {
		return err
	}
	// What kind of notification message did we get?
	switch nmsg.NotificationType {
	case notify.APPARMOR_NOTIF_OP:
		var omsg notify.MsgNotificationOp
		if err := omsg.UnmarshalBinary(buf); err != nil {
			return err
		}
		// What kind of operation notification did we get?
		switch omsg.Class {
		case notify.AA_CLASS_FILE:
			var fmsg notify.MsgNotificationFile
			if err := fmsg.UnmarshalBinary(buf); err != nil {
				return err
			}
			// log.Printf("notification request: %#v\n", fmsg)
			req := newRequest(l, &fmsg)
			l.R <- req
			tomb.Go(func() error {
				l.waitAndRespond(req, &fmsg)
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

func (l *Listener) waitAndRespond(req *Request, msg *notify.MsgNotificationFile) {
	resp := notify.ResponseForRequest(&msg.MsgNotification)
	// XXX: should both error fields be zeroed?
	resp.MsgNotification.Error = 0
	// XXX: flags 1 means not-cache the reply, make this a proper named flag
	resp.MsgNotification.NoCache = 1
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
	if err := l.encodeAndSendResponse(&resp); err != nil {
		l.fail(err)
	}
}

func (l *Listener) encodeAndSendResponse(resp *notify.MsgNotificationResponse) error {
	buf, err := resp.MarshalBinary()
	if err != nil {
		return err
	}
	ioctlBuf := notify.BytesToIoctlRequestBuffer(buf)
	_, err = notify.Ioctl(l.notifyFile.Fd(), notify.APPARMOR_NOTIF_SEND, ioctlBuf)
	return err
}

func (l *Listener) runOnce(tomb *tomb.Tomb) error {
	// XXX: Wait must return immediately once epoll is closed.
	events, err := l.poll.Wait()
	if err != nil {
		return err
	}
	for _, event := range events {
		switch event.Fd {
		case int(l.notifyFile.Fd()):
			if event.Readiness&epoll.Readable != 0 {
				// Prepare a receive buffer for incoming request. The buffer is of the
				// maximum allowed size and will contain one kernel request upon return.
				// Note that the actually occupied buffer is indicated by the Length field
				// in the header.
				ioctlBuf := notify.NewIoctlRequestBuffer()
				buf, err := notify.Ioctl(l.notifyFile.Fd(), notify.APPARMOR_NOTIF_RECV, ioctlBuf)
				if err != nil {
					return err
				}
				if err := l.decodeAndDispatchRequest(buf, tomb); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// Run reads and dispatches kernel requests until stopped.
func (l *Listener) Run(tomb *tomb.Tomb) {
	// TODO: allow the run to stop
	for {
		if err := l.runOnce(tomb); err != nil {
			l.fail(err)
			break
		}
	}
}

func (l *Listener) fail(err error) {
	l.E <- err
	close(l.E)
	close(l.R)
}

// Close closes the kernel communication file.
func (l *Listener) Close() error {
	err1 := l.notifyFile.Close()
	err2 := l.poll.Close()
	if err1 != nil {
		return err1
	}
	return err2
}
