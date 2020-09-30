package apparmor_test

import (
	"github.com/snapcore/cerberus/apparmor"

	. "gopkg.in/check.v1"
)

type messageSuite struct{}

var _ = Suite(&messageSuite{})

func (*messageSuite) TestMsgNotificationFilterMarshalUnmarshal(c *C) {
	for _, t := range []struct {
		bytes []byte
		msg   apparmor.MsgNotificationFilter
	}{
		{
			bytes: []byte{
				0x10, 0x0, // Length
				0x2, 0x0, // Protocol
				0x80, 0x0, 0x0, 0x0, // Mode Set
				0x0, 0x0, 0x0, 0x0, // Namespace
				0x0, 0x0, 0x0, 0x0, // Filter
			},
			msg: apparmor.MsgNotificationFilter{
				MsgHeader: apparmor.MsgHeader{
					Length:  0x10,
					Version: 0x02,
				},
				ModeSet: apparmor.ModeSetUser,
			},
		},
		{
			bytes: []byte{
				0x18, 0x0, // Length
				0x2, 0x0, // Protocol
				0x80, 0x0, 0x0, 0x0, // Mode Set
				0x10, 0x0, 0x0, 0x0, // Namespace (offset)
				0x14, 0x0, 0x0, 0x0, // Filter
				'f', 'o', 'o', 0x0, // Packed namespace string.
				'b', 'a', 'r', 0x0, // Packed namespace string.
			},
			msg: apparmor.MsgNotificationFilter{
				MsgHeader: apparmor.MsgHeader{
					Length:  0x18,
					Version: 0x02,
				},
				ModeSet:   apparmor.ModeSetUser,
				NameSpace: "foo",
				Filter:    "bar",
			},
		},
	} {
		bytes, err := t.msg.MarshalBinary()
		c.Assert(err, IsNil)
		c.Assert(bytes, DeepEquals, t.bytes)

		var msg apparmor.MsgNotificationFilter
		err = msg.UnmarshalBinary(t.bytes)
		c.Assert(err, IsNil)
		c.Assert(msg, DeepEquals, t.msg)
	}
}

func (*messageSuite) TestMsgNotificationFilterUnmarshalErrors(c *C) {
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
			bytes:   []byte{0x10, 0x00, 0x02, 0x00},
			errMsg:  `cannot unmarshal apparmor message header: length mismatch 16 != 4`,
		},
		{
			comment: "unsupported protocol version",
			bytes:   []byte{0x04, 0x0, 0x3, 0x0},
			errMsg:  `cannot unmarshal apparmor message header: unsupported version: 3`,
		},
		{
			comment: "message with truncated mode set",
			bytes: []byte{
				0x10, 0x0, // Length
				0x2, 0x0, // Protocol
				0x80, 0x0, 0x0, // Mode Set, short of one byte
			},
			errMsg: `cannot unmarshal apparmor message header: length mismatch 16 != 7`,
		},
		{
			comment: "message with truncated namespace",
			bytes: []byte{
				0x10, 0x0, // Length
				0x2, 0x0, // Protocol
				0x80, 0x0, 0x0, 0x0, // Mode Set
				0x0, 0x0, 0x0, // Namespace, short of one byte
			},
			errMsg: `cannot unmarshal apparmor message header: length mismatch 16 != 11`,
		},
		{
			comment: "message with truncated filter",
			bytes: []byte{
				0x10, 0x0, // Length
				0x2, 0x0, // Protocol
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
				0x2, 0x0, // Protocol
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
				0x2, 0x0, // Protocol
				0x80, 0x0, 0x0, 0x0, // Mode Set
				0x10, 0x0, 0x0, 0x0, // Namespace, pointing to invalid address
				0x0, 0x0, 0x0, 0x0, // Filter
				'f', 'o', 'o',
			},
			errMsg: `cannot unmarshal apparmor notification filter message: cannot unpack namespace: unterminated string at address 16`,
		},
	} {
		var msg apparmor.MsgNotificationFilter
		err := msg.UnmarshalBinary(t.bytes)
		c.Assert(err, ErrorMatches, t.errMsg, Commentf("%s", t.comment))
	}
}

func (*messageSuite) TestMsgNotificationFilterValidate(c *C) {
	msg := apparmor.MsgNotificationFilter{}
	c.Check(msg.Validate(), IsNil)
	msg = apparmor.MsgNotificationFilter{ModeSet: 10000}
	c.Check(msg.Validate(), ErrorMatches, "unsupported modeset: 10000")
}

func (*messageSuite) TestMsgNotificationMarshalBinary(c *C) {
	msg := apparmor.MsgNotification{
		NotificationType: apparmor.Response,
		Signalled:        1,
		Reserved:         0,
		ID:               0x1234,
		Error:            0xFF,
	}
	data, err := msg.MarshalBinary()
	c.Assert(err, IsNil)
	c.Check(data, HasLen, 20)
	c.Check(data, DeepEquals, []byte{
		0x14, 0x0, // Length
		0x2, 0x0, // Protocol
		0x0, 0x0, // Notification Type
		0x1,                                            // Signalled
		0x0,                                            // Reserved
		0x34, 0x12, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // ID
		0xFF, 0x00, 0x00, 0x00, // Error
	})
}

func (*messageSuite) TestRequestBuffer(c *C) {
	buf := apparmor.RequestBuffer()
	c.Assert(buf, HasLen, 0xFFFF)
	var header apparmor.MsgHeader
	err := header.UnmarshalBinary(buf)
	c.Assert(err, IsNil)
	c.Check(header, Equals, apparmor.MsgHeader{
		Length:  0xFFFF,
		Version: 2,
	})
}

func (s *messageSuite) TestMsgNotificationFileUnmarshalBinary(c *C) {
	// Notification for accessing a the /root/.ssh/ file.
	bytes := []byte{
		0x4c, 0x0, // Length == 76 bytes
		0x2, 0x0, // Protocol 2
		0x4, 0x0, // Notification type == apparmor.Operation
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

	var msg apparmor.MsgNotificationFile
	err := msg.UnmarshalBinary(bytes)
	c.Assert(err, IsNil)
	c.Check(msg, DeepEquals, apparmor.MsgNotificationFile{
		MsgNotificationOp: apparmor.MsgNotificationOp{
			MsgNotification: apparmor.MsgNotification{
				MsgHeader: apparmor.MsgHeader{
					Length:  76,
					Version: 2,
				},
				NotificationType: apparmor.Operation,
				ID:               2,
				Error:            -13,
			},
			Allow: 4,
			Deny:  4,
			Pid:   0x819,
			Label: "test-prompt",
			Class: apparmor.MediationClassFile,
		},
		Name: "/root/.ssh/",
	})
}

func (s *messageSuite) TestMsgNotificationResponseMarshalBinary(c *C) {
	msg := apparmor.MsgNotificationResponse{
		MsgNotification: apparmor.MsgNotification{
			NotificationType: 0x11,
			Signalled:        0x22,
			Reserved:         0x33,
			ID:               0x44,
			Error:            0x55,
		},
		Error: 0x66,
		Allow: 0x77,
		Deny:  0x88,
	}
	bytes, err := msg.MarshalBinary()
	c.Assert(err, IsNil)
	c.Assert(bytes, DeepEquals, []byte{
		0x20, 0x0, // Length
		0x2, 0x0, // Version
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
	msg := apparmor.MsgNotificationFile{
		MsgNotificationOp: apparmor.MsgNotificationOp{
			Allow: 5,
			Deny:  3,
			Class: apparmor.MediationClassFile,
		},
	}
	allow, deny, err := msg.DecodeFilePermissions()
	c.Assert(err, IsNil)
	c.Check(allow, Equals, apparmor.MayExecutePermission|apparmor.MayReadPermission)
	c.Check(deny, Equals, apparmor.MayExecutePermission|apparmor.MayWritePermission)
}

func (*messageSuite) TestDecodeFilePermissionsWrongClass(c *C) {
	msg := apparmor.MsgNotificationFile{
		MsgNotificationOp: apparmor.MsgNotificationOp{
			Allow: 5,
			Deny:  3,
			Class: apparmor.MediationClassDBus,
		},
	}
	_, _, err := msg.DecodeFilePermissions()
	c.Assert(err, ErrorMatches, "mediation class D-Bus does not describe file permissions")
}
