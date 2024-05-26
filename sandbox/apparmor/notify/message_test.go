package notify_test

import (
	"encoding/binary"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/sandbox/apparmor/notify"

	. "gopkg.in/check.v1"
)

type messageSuite struct{}

var _ = Suite(&messageSuite{})

func (*messageSuite) TestMsgLength(c *C) {
	if arch.Endian() == binary.BigEndian {
		c.Skip("test only written for little-endian architectures")
	}
	for _, t := range []struct {
		bytes  []byte
		length int
	}{
		{
			bytes: []byte{
				0x4, 0x0, // Length
				0x3, 0x0, // Protocol
			},
			length: 4,
		},
		{
			bytes: []byte{
				0xff, 0x0, // Incorrect length, but no validation done here
				0x3, 0x0, // Protocol
			},
			length: 255,
		},
		{
			bytes: []byte{
				0x10, 0x0, // Length
				0xff, 0xff, // Protocol (invalid, should still work)
				0x80, 0x0, 0x0, 0x0, // Mode Set
				0x0, 0x0, 0x0, 0x0, // Namespace
				0x0, 0x0, 0x0, 0x0, // Filter
			},
			length: 16,
		},
		{
			bytes: []byte{
				0x4, 0x0, // Length
				0x3, 0x0, // Protocol
				// Next 4 bytes should be next header, but no validation done here
				0x80, 0x0, 0x0, 0x0, // Mode Set
				0x0, 0x0, 0x0, 0x0, // Namespace
				0x0, 0x0, 0x0, 0x0, // Filter
			},
			length: 4,
		},
	} {
		length := mylog.Check2(notify.MsgLength(t.bytes))
		c.Check(err, IsNil)
		c.Check(length, Equals, t.length)
	}
}

func (*messageSuite) TestMsgLengthErrors(c *C) {
	if arch.Endian() == binary.BigEndian {
		c.Skip("test only written for little-endian architectures")
	}
	for _, t := range []struct {
		bytes []byte
		err   string
	}{
		{
			bytes: []byte{},
			err:   "cannot parse message header: EOF",
		},
		{
			bytes: []byte{
				0x4, 0x0, // Length
				0x3, // Incomplete Protocol
			},
			err: "cannot parse message header: unexpected EOF",
		},
		{
			bytes: []byte{
				0x3, 0x0, // Incorrect length
				0x3, 0x0, // Protocol
			},
			err: `cannot parse message header: invalid length \(must be >= 4\): 3`,
		},
	} {
		length := mylog.Check2(notify.MsgLength(t.bytes))
		c.Check(err, ErrorMatches, t.err, Commentf("bytes: %v", t.bytes))
		c.Check(length, Equals, -1)
	}
}

func (*messageSuite) TestExtractFirstMsg(c *C) {
	if arch.Endian() == binary.BigEndian {
		c.Skip("test only written for little-endian architectures")
	}

	simple := []byte{
		0x4, 0x0, // Length
		0x3, 0x0, // Protocol
	}
	first, rest := mylog.Check3(notify.ExtractFirstMsg(simple))

	c.Assert(first, DeepEquals, simple)
	c.Assert(rest, HasLen, 0)

	origBytes := []byte{
		// first
		0x4, 0x0, // Length
		0x3, 0x0, // Protocol
		// second
		0x10, 0x0, // Length
		0xff, 0xff, // Protocol (invalid, should still work)
		0x80, 0x0, 0x0, 0x0, // Mode Set
		0x0, 0x0, 0x0, 0x0, // Namespace
		0x0, 0x0, 0x0, 0x0, // Filter
		// third
		0x4, 0x0, // Length
		0x3, 0x0, // Protocol
		// Next 4 bytes should be next header, but no validation done here
		0x80, 0x0, 0x0, 0x0, // Mode Set
		0x0, 0x0, 0x0, 0x0, // Namespace
		0x0, 0x0, 0x0, 0x0, // Filter
	}
	for _, t := range []struct {
		first []byte
		rest  []byte
	}{
		{
			first: []byte{
				0x4, 0x0, // Length
				0x3, 0x0, // Protocol
			},
			rest: []byte{
				// second
				0x10, 0x0, // Length
				0xff, 0xff, // Protocol (invalid, should still work)
				0x80, 0x0, 0x0, 0x0, // Mode Set
				0x0, 0x0, 0x0, 0x0, // Namespace
				0x0, 0x0, 0x0, 0x0, // Filter
				// third
				0x4, 0x0, // Length
				0x3, 0x0, // Protocol
				// Next 4 bytes should be next header, but no validation done here
				0x80, 0x0, 0x0, 0x0, // Mode Set
				0x0, 0x0, 0x0, 0x0, // Namespace
				0x0, 0x0, 0x0, 0x0, // Filter
			},
		},
		{
			first: []byte{
				0x10, 0x0, // Length
				0xff, 0xff, // Protocol (invalid, should still work)
				0x80, 0x0, 0x0, 0x0, // Mode Set
				0x0, 0x0, 0x0, 0x0, // Namespace
				0x0, 0x0, 0x0, 0x0, // Filter
			},
			rest: []byte{
				// third
				0x4, 0x0, // Length
				0x3, 0x0, // Protocol
				// Next 4 bytes should be next header, but no validation done here
				0x80, 0x0, 0x0, 0x0, // Mode Set
				0x0, 0x0, 0x0, 0x0, // Namespace
				0x0, 0x0, 0x0, 0x0, // Filter
			},
		},
		{
			first: []byte{
				0x4, 0x0, // Length
				0x3, 0x0, // Protocol
			},
			rest: []byte{
				// Next 4 bytes should be next header, but no validation done here
				0x80, 0x0, 0x0, 0x0, // Mode Set
				0x0, 0x0, 0x0, 0x0, // Namespace
				0x0, 0x0, 0x0, 0x0, // Filter
			},
		},
	} {
		first, rest := mylog.Check3(notify.ExtractFirstMsg(origBytes))
		c.Check(err, IsNil)
		c.Check(first, DeepEquals, t.first)
		c.Check(rest, DeepEquals, t.rest)
		origBytes = rest
	}
}

func (*messageSuite) TestExtractFirstMsgErrors(c *C) {
	if arch.Endian() == binary.BigEndian {
		c.Skip("test only written for little-endian architectures")
	}

	for _, t := range []struct {
		bytes []byte
		err   string
	}{
		{
			bytes: []byte{},
			err:   "cannot extract first message: cannot parse message header: EOF",
		},
		{
			bytes: []byte{
				0x4, 0x0, // Length
				0x3, // Incomplete Protocol
			},
			err: "cannot extract first message: cannot parse message header: unexpected EOF",
		},
		{
			bytes: []byte{
				0x3, 0x0, // Incorrect length less than header
				0x3, 0x0, // Protocol
			},
			err: `cannot extract first message: cannot parse message header: invalid length \(must be >= 4\): 3`,
		},
		{
			bytes: []byte{
				0x5, 0x0, // Incorrect length greater than data
				0x3, 0x0, // Protocol
			},
			err: `cannot extract first message: length in header exceeds data length: 5 > 4`,
		},
	} {
		first, rest := mylog.Check3(notify.ExtractFirstMsg(t.bytes))
		c.Check(err, ErrorMatches, t.err, Commentf("bytes: %v", t.bytes))
		c.Check(first, IsNil)
		c.Check(rest, IsNil)
	}
}

func (*messageSuite) TestMsgNotificationFilterMarshalUnmarshal(c *C) {
	if arch.Endian() == binary.BigEndian {
		c.Skip("test only written for little-endian architectures")
	}
	for _, t := range []struct {
		bytes []byte
		msg   notify.MsgNotificationFilter
	}{
		{
			bytes: []byte{
				0x10, 0x0, // Length
				0x3, 0x0, // Protocol
				0x80, 0x0, 0x0, 0x0, // Mode Set
				0x0, 0x0, 0x0, 0x0, // Namespace
				0x0, 0x0, 0x0, 0x0, // Filter
			},
			msg: notify.MsgNotificationFilter{
				MsgHeader: notify.MsgHeader{
					Length:  0x10,
					Version: 0x03,
				},
				ModeSet: notify.APPARMOR_MODESET_USER,
			},
		},
		{
			bytes: []byte{
				0x17, 0x0, // Length
				0x3, 0x0, // Protocol
				0x80, 0x0, 0x0, 0x0, // Mode Set
				0x10, 0x0, 0x0, 0x0, // Namespace (offset)
				0x14, 0x0, 0x0, 0x0, // Filter
				'f', 'o', 'o', 0x0, // Packed namespace string.
				'b', 'a', 'r', // Packed filter []byte.
			},
			msg: notify.MsgNotificationFilter{
				MsgHeader: notify.MsgHeader{
					Length:  0x17,
					Version: 0x03,
				},
				ModeSet:   notify.APPARMOR_MODESET_USER,
				NameSpace: "foo",
				Filter:    []byte("bar"),
			},
		},
	} {
		bytes := mylog.Check2(t.msg.MarshalBinary())

		c.Assert(bytes, DeepEquals, t.bytes)

		var msg notify.MsgNotificationFilter
		mylog.Check(msg.UnmarshalBinary(t.bytes))

		c.Assert(msg, DeepEquals, t.msg)
	}
}

func (*messageSuite) TestMsgNotificationFilterUnmarshalErrors(c *C) {
	if arch.Endian() == binary.BigEndian {
		c.Skip("test only written for little-endian architectures")
	}
	for _, t := range []struct {
		comment string
		bytes   []byte
		errMsg  string
	}{
		{
			comment: "header short of one byte",
			bytes:   []byte{0x10, 0x00, 0x02},
			errMsg:  `cannot unmarshal apparmor message header: unexpected EOF`,
		},
		{
			comment: "header without the remaining data",
			bytes:   []byte{0x10, 0x00, 0x03, 0x00},
			errMsg:  `cannot unmarshal apparmor message header: length mismatch 16 != 4`,
		},
		{
			comment: "unsupported protocol version",
			bytes:   []byte{0x04, 0x0, 0x2, 0x0},
			errMsg:  `cannot unmarshal apparmor message header: unsupported version: 2`,
		},
		{
			comment: "message with truncated mode set",
			bytes: []byte{
				0x10, 0x0, // Length
				0x3, 0x0, // Protocol
				0x80, 0x0, 0x0, // Mode Set, short of one byte
			},
			errMsg: `cannot unmarshal apparmor message header: length mismatch 16 != 7`,
		},
		{
			comment: "message with truncated namespace",
			bytes: []byte{
				0x10, 0x0, // Length
				0x3, 0x0, // Protocol
				0x80, 0x0, 0x0, 0x0, // Mode Set
				0x0, 0x0, 0x0, // Namespace, short of one byte
			},
			errMsg: `cannot unmarshal apparmor message header: length mismatch 16 != 11`,
		},
		{
			comment: "message with truncated filter",
			bytes: []byte{
				0x10, 0x0, // Length
				0x3, 0x0, // Protocol
				0x80, 0x0, 0x0, 0x0, // Mode Set
				0x0, 0x0, 0x0, 0x0, // Namespace
				0x0, 0x0, 0x0, // Filter, short of one byte
			},
			errMsg: `cannot unmarshal apparmor message header: length mismatch 16 != 15`,
		},
		{
			comment: "message with namespace address pointing beyond message body",
			bytes: []byte{
				0x10, 0x0, // Length
				0x3, 0x0, // Protocol
				0x80, 0x0, 0x0, 0x0, // Mode Set
				0xFF, 0x0, 0x0, 0x0, // Namespace, pointing to invalid address
				0x0, 0x0, 0x0, 0x0, // Filter
			},
			errMsg: `cannot unmarshal apparmor notification filter message: cannot unpack namespace: address 255 points outside of message body`,
		},
		{
			comment: "message with with namespace without proper termination",
			bytes: []byte{
				0x13, 0x0, // Length
				0x3, 0x0, // Protocol
				0x80, 0x0, 0x0, 0x0, // Mode Set
				0x10, 0x0, 0x0, 0x0, // Namespace, pointing to invalid address
				0x0, 0x0, 0x0, 0x0, // Filter
				'f', 'o', 'o',
			},
			errMsg: `cannot unmarshal apparmor notification filter message: cannot unpack namespace: unterminated string at address 16`,
		},
	} {
		var msg notify.MsgNotificationFilter
		mylog.Check(msg.UnmarshalBinary(t.bytes))
		c.Assert(err, ErrorMatches, t.errMsg, Commentf("%s", t.comment))
	}
}

func (*messageSuite) TestMsgNotificationFilterValidate(c *C) {
	msg := notify.MsgNotificationFilter{}
	c.Check(msg.Validate(), IsNil)
	msg = notify.MsgNotificationFilter{ModeSet: 10000}
	c.Check(msg.Validate(), ErrorMatches, "unsupported modeset: 10000")
}

func (*messageSuite) TestMsgNotificationMarshalBinary(c *C) {
	if arch.Endian() == binary.BigEndian {
		c.Skip("test only written for little-endian architectures")
	}
	msg := notify.MsgNotification{
		NotificationType: notify.APPARMOR_NOTIF_RESP,
		Signalled:        1,
		NoCache:          0,
		ID:               0x1234,
		Error:            0xFF,
	}
	data := mylog.Check2(msg.MarshalBinary())

	c.Check(data, HasLen, 20)
	c.Check(data, DeepEquals, []byte{
		0x14, 0x0, // Length
		0x3, 0x0, // Protocol
		0x0, 0x0, // Notification Type
		0x1,                                            // Signalled
		0x0,                                            // Reserved
		0x34, 0x12, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // ID
		0xFF, 0x00, 0x00, 0x00, // Error
	})
}

func (s *messageSuite) TestMsgNotificationFileMarshalUnmarshalBinary(c *C) {
	if arch.Endian() == binary.BigEndian {
		c.Skip("test only written for little-endian architectures")
	}
	// Notification for accessing a the /root/.ssh/ file.
	bytes := []byte{
		0x4c, 0x0, // Length == 76 bytes
		0x3, 0x0, // Protocol
		0x4, 0x0, // Notification type == notify.APPARMOR_NOTIF_OP
		0x0,                                    // Signalled
		0x0,                                    // Reserved
		0x2, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, // ID (request #2, just a number)
		0xf3, 0xff, 0xff, 0xff, // Error -13 EACCESS
		0x4, 0x0, 0x0, 0x0, // Allow - ???
		0x4, 0x0, 0x0, 0x0, // Deny - ???
		0x19, 0x8, 0x0, 0x0, // PID
		0x34, 0x0, 0x0, 0x0, // Label at +52 bytes into buffer
		0x2, 0x0, // Class - AA_CLASS_FILE
		0x0, 0x0, // Op - ???
		0x0, 0x0, 0x0, 0x0, // SUID
		0x0, 0x0, 0x0, 0x0, // OUID
		0x40, 0x0, 0x0, 0x0, // Name at +64 bytes into buffer
		0x74, 0x65, 0x73, 0x74, 0x2d, 0x70, 0x72, 0x6f, 0x6d, 0x70, 0x74, 0x0, // "test-prompt\0"
		0x2f, 0x72, 0x6f, 0x6f, 0x74, 0x2f, 0x2e, 0x73, 0x73, 0x68, 0x2f, 0x0, // "/root/.ssh/\0"
	}
	c.Assert(bytes, HasLen, 76)

	var msg notify.MsgNotificationFile
	mylog.Check(msg.UnmarshalBinary(bytes))

	c.Assert(msg, DeepEquals, notify.MsgNotificationFile{
		MsgNotificationOp: notify.MsgNotificationOp{
			MsgNotification: notify.MsgNotification{
				MsgHeader: notify.MsgHeader{
					Length:  76,
					Version: 3,
				},
				NotificationType: notify.APPARMOR_NOTIF_OP,
				ID:               2,
				Error:            -13,
			},
			Allow: 4,
			Deny:  4,
			Pid:   0x819,
			Label: "test-prompt",
			Class: notify.AA_CLASS_FILE,
		},
		Name: "/root/.ssh/",
	})

	buf := mylog.Check2(msg.MarshalBinary())

	c.Assert(buf, DeepEquals, bytes)
}

func (s *messageSuite) TestMsgNotificationValidate(c *C) {
	msg := notify.MsgNotification{}
	for _, t := range []notify.NotificationType{
		notify.APPARMOR_NOTIF_RESP,
		notify.APPARMOR_NOTIF_CANCEL,
		notify.APPARMOR_NOTIF_INTERRUPT,
		notify.APPARMOR_NOTIF_ALIVE,
		notify.APPARMOR_NOTIF_OP,
	} {
		msg.NotificationType = t
		c.Check(msg.Validate(), IsNil)
	}
	msg.NotificationType = notify.NotificationType(5)
	c.Check(msg.Validate(), ErrorMatches, "unsupported notification type: 5")
}

func (s *messageSuite) TestResponseForRequest(c *C) {
	req := notify.MsgNotification{
		ID:    1234,
		Error: 0xbad,
	}
	resp := notify.ResponseForRequest(&req)
	c.Assert(resp.NotificationType, Equals, notify.APPARMOR_NOTIF_RESP)
	c.Assert(resp.NoCache, Equals, uint8(1))
	c.Assert(resp.ID, Equals, req.ID)
	c.Assert(resp.MsgNotification.Error, Equals, req.Error)
}

func (s *messageSuite) TestMsgNotificationResponseMarshalBinary(c *C) {
	if arch.Endian() == binary.BigEndian {
		c.Skip("test only written for little-endian architectures")
	}
	msg := notify.MsgNotificationResponse{
		MsgNotification: notify.MsgNotification{
			NotificationType: 0x11,
			Signalled:        0x22,
			NoCache:          0x33,
			ID:               0x44,
			Error:            0x55,
		},
		Error: 0x66,
		Allow: 0x77,
		Deny:  0x88,
	}
	bytes := mylog.Check2(msg.MarshalBinary())

	c.Assert(bytes, DeepEquals, []byte{
		0x20, 0x0, // Length
		0x3, 0x0, // Version
		0x11, 0x0, // Notification Type
		0x22,                                    // Signalled
		0x33,                                    // Reserved
		0x44, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, // ID
		0x55, 0x0, 0x0, 0x0, // Error
		0x66, 0x0, 0x0, 0x0, // The other Error field?
		0x77, 0x0, 0x0, 0x0, // Allow
		0x88, 0x0, 0x0, 0x0, // Deny
	})
}

func (*messageSuite) TestDecodeFilePermissions(c *C) {
	msg := notify.MsgNotificationFile{
		MsgNotificationOp: notify.MsgNotificationOp{
			Allow: 5,
			Deny:  3,
			Class: notify.AA_CLASS_FILE,
		},
	}
	allow, deny := mylog.Check3(msg.DecodeFilePermissions())

	c.Check(allow, Equals, notify.AA_MAY_EXEC|notify.AA_MAY_READ)
	c.Check(deny, Equals, notify.AA_MAY_EXEC|notify.AA_MAY_WRITE)
}

func (*messageSuite) TestDecodeFilePermissionsWrongClass(c *C) {
	msg := notify.MsgNotificationFile{
		MsgNotificationOp: notify.MsgNotificationOp{
			Allow: 5,
			Deny:  3,
			Class: notify.AA_CLASS_DBUS,
		},
	}
	_, _ := mylog.Check3(msg.DecodeFilePermissions())
	c.Assert(err, ErrorMatches, "mediation class AA_CLASS_DBUS does not describe file permissions")
}
