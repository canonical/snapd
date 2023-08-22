package listener_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"gopkg.in/tomb.v2"

	"golang.org/x/sys/unix"

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/sandbox/apparmor/notify"
	"github.com/snapcore/snapd/sandbox/apparmor/notify/listener"
	"github.com/snapcore/snapd/testutil"
)

func Test(t *testing.T) { TestingT(t) }

type listenerSuite struct {
	testutil.BaseTest
}

var _ = Suite(&listenerSuite{})

func (s *listenerSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })
}

func (*listenerSuite) TestRegisterClose(c *C) {
	sockFdChan, restoreOpen := listener.MockOsOpen()
	defer restoreOpen()

	restoreIoctl := listener.MockNotifyIoctl()
	defer restoreIoctl()

	l, err := listener.Register()
	c.Assert(err, IsNil)

	<-sockFdChan

	err = l.Close()
	c.Assert(err, IsNil)
}

type msgNotificationFile struct {
	// MsgHeader
	Length  uint16
	Version uint16
	// MsgNotification
	NotificationType notify.NotificationType
	Signalled        uint8
	NoCache          uint8
	ID               uint64
	Error            int32
	// msgNotificationOpKernel
	Allow uint32
	Deny  uint32
	Pid   uint32
	Label uint32
	Class uint32
	Op    uint32
	// msgNotificationFileKernel
	SUID uint32
	OUID uint32
	Name uint32
}

func (*listenerSuite) TestRun(c *C) {
	sockFdChan, restoreOpen := listener.MockOsOpen()
	defer restoreOpen()

	restoreIoctl := listener.MockNotifyIoctl()
	defer restoreIoctl()

	l, err := listener.Register()
	c.Assert(err, IsNil)
	notifySocket := <-sockFdChan

	var lt tomb.Tomb
	lt.Go(func() error {
		l.Run(&lt)
		return nil
	})
	defer lt.Kill(errors.New("exiting"))

	label := "snap.foo.bar"
	path := "/home/Documents/foo"

	ids := []uint64{0xdead, 0xbeef}
	requests := make([]*listener.Request, 0, len(ids))

	for _, id := range ids {
		msg := notify.MsgNotificationFile{}
		msg.Version = 3
		msg.NotificationType = notify.APPARMOR_NOTIF_OP
		msg.NoCache = 1
		msg.ID = id
		msg.Allow = 0b1010
		msg.Deny = 0b0101
		msg.Pid = 1234
		msg.Label = label
		msg.Class = notify.AA_CLASS_FILE
		msg.SUID = 1000
		msg.Name = path
		buf, err := msg.MarshalBinary()
		c.Assert(err, IsNil)
		unix.Write(notifySocket, buf)

		select {
		case req := <-l.R:
			c.Assert(req.Pid, Equals, msg.Pid)
			c.Assert(req.Label, Equals, label)
			c.Assert(req.Path, Equals, path)
			c.Assert(req.SubjectUid, Equals, msg.SUID)
			requests = append(requests, req)
		case err := <-l.E:
			c.Assert(err, IsNil)
		case reason := <-lt.Dying():
			c.Assert(reason, Equals, nil)
		}
	}

	for i, id := range ids {
		switch i % 2 {
		case 0:
			requests[i].YesNo <- false
		case 1:
			requests[i].YesNo <- true
		}

		time.Sleep(25 * time.Millisecond)

		msgNotification := notify.MsgNotification{
			NotificationType: notify.APPARMOR_NOTIF_RESP,
			NoCache:          1,
			ID:               id,
			Error:            0,
		}
		resp := notify.MsgNotificationResponse{
			MsgNotification: msgNotification,
			Error:           0,
			Allow:           uint32(0b1111 * i),
			Deny:            uint32(0b0101 * (1 - i)),
		}
		respBuf, err := resp.MarshalBinary()
		c.Assert(err, IsNil)

		receivedBuf := make([]byte, 0xFFFF)
		size, err := unix.Read(notifySocket, receivedBuf)
		c.Assert(err, IsNil)
		c.Assert(size, Equals, len(respBuf))
		c.Assert(receivedBuf[:size], DeepEquals, respBuf)
	}

	err = l.Close()
	c.Assert(err, IsNil)
}

func (*listenerSuite) TestRunErrors(c *C) {
	sockFdChan, restoreOpen := listener.MockOsOpen()
	defer restoreOpen()

	restoreIoctl := listener.MockNotifyIoctl()
	defer restoreIoctl()

	order := arch.Endian()

	for _, testCase := range []struct {
		msg msgNotificationFile
		err string
	}{
		{
			msgNotificationFile{},
			`cannot unmarshal apparmor message header: unsupported version: 0`,
		},
		{
			msgNotificationFile{
				Version: 3,
			},
			`cannot unmarshal apparmor message header: length mismatch 0 != 56`,
		},
		{
			msgNotificationFile{
				Length:           56,
				Version:          3,
				NotificationType: notify.APPARMOR_NOTIF_CANCEL,
			},
			`unsupported notification type: APPARMOR_NOTIF_CANCEL`,
		},
		{
			msgNotificationFile{
				Length:           56,
				Version:          3,
				NotificationType: notify.APPARMOR_NOTIF_OP,
				Class:            uint32(notify.AA_CLASS_DBUS),
			},
			`unsupported mediation class: AA_CLASS_DBUS`,
		},
	} {
		l, err := listener.Register()
		c.Assert(err, IsNil)
		notifySocket := <-sockFdChan

		var lt tomb.Tomb
		lt.Go(func() error {
			l.Run(&lt)
			return nil
		})

		buf := bytes.NewBuffer(make([]byte, 0, testCase.msg.Length))
		err = binary.Write(buf, order, testCase.msg)
		c.Check(err, IsNil)
		unix.Write(notifySocket, buf.Bytes())

		eChanResult := <-l.E
		c.Assert(eChanResult, ErrorMatches, testCase.err)

		err = l.Close()
		c.Assert(err, IsNil)
		unix.Close(notifySocket)
	}
}
