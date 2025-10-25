package notify_test

import (
	"encoding/binary"

	"github.com/snapcore/snapd/sandbox/apparmor/notify"

	. "gopkg.in/check.v1"
)

type messageSuite struct{}

var _ = Suite(&messageSuite{})

func (*messageSuite) TestMsgLength(c *C) {
	if notify.NativeByteOrder == binary.BigEndian {
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
				0x5, 0x0, // Protocol
			},
			length: 255,
		},
		{
			bytes: []byte{
				0x10, 0x0, // Length
				0xff, 0xff, // Protocol
				0x80, 0x0, 0x0, 0x0, // Mode Set
				0x0, 0x0, 0x0, 0x0, // Namespace
				0x0, 0x0, 0x0, 0x0, // Filter
			},
			length: 16,
		},
		{
			bytes: []byte{
				0x4, 0x0, // Length
				0xAB, 0xCD, // Protocol
				// Next 4 bytes should be next header, but no validation done here
				0x80, 0x0, 0x0, 0x0, // Mode Set
				0x0, 0x0, 0x0, 0x0, // Namespace
				0x0, 0x0, 0x0, 0x0, // Filter
			},
			length: 4,
		},
	} {
		length, err := notify.MsgLength(t.bytes)
		c.Check(err, IsNil)
		c.Check(length, Equals, t.length)
	}
}

func (*messageSuite) TestMsgLengthErrors(c *C) {
	if notify.NativeByteOrder == binary.BigEndian {
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
		length, err := notify.MsgLength(t.bytes)
		c.Check(err, ErrorMatches, t.err, Commentf("bytes: %v", t.bytes))
		c.Check(length, Equals, -1)
	}
}

func (*messageSuite) TestExtractFirstMsg(c *C) {
	if notify.NativeByteOrder == binary.BigEndian {
		c.Skip("test only written for little-endian architectures")
	}

	simple := []byte{
		0x4, 0x0, // Length
		0x3, 0x0, // Protocol
	}
	first, rest, err := notify.ExtractFirstMsg(simple)
	c.Assert(err, IsNil)
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
		0x5, 0x0, // Protocol
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
				0x5, 0x0, // Protocol
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
				0x5, 0x0, // Protocol
				// Next 4 bytes should be next header, but no validation done here
				0x80, 0x0, 0x0, 0x0, // Mode Set
				0x0, 0x0, 0x0, 0x0, // Namespace
				0x0, 0x0, 0x0, 0x0, // Filter
			},
		},
		{
			first: []byte{
				0x4, 0x0, // Length
				0x5, 0x0, // Protocol
			},
			rest: []byte{
				// Next 4 bytes should be next header, but no validation done here
				0x80, 0x0, 0x0, 0x0, // Mode Set
				0x0, 0x0, 0x0, 0x0, // Namespace
				0x0, 0x0, 0x0, 0x0, // Filter
			},
		},
	} {
		first, rest, err := notify.ExtractFirstMsg(origBytes)
		c.Check(err, IsNil)
		c.Check(first, DeepEquals, t.first)
		c.Check(rest, DeepEquals, t.rest)
		origBytes = rest
	}
}

func (*messageSuite) TestExtractFirstMsgErrors(c *C) {
	if notify.NativeByteOrder == binary.BigEndian {
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
		first, rest, err := notify.ExtractFirstMsg(t.bytes)
		c.Check(err, ErrorMatches, t.err, Commentf("bytes: %v", t.bytes))
		c.Check(first, IsNil)
		c.Check(rest, IsNil)
	}
}

func (*messageSuite) TestMessageMarshalErrors(c *C) {
	// Try to marshal message structs without setting Version, check that
	// ErrVersionUnset is returned

	register := notify.MsgNotificationRegister{}
	bytes, err := register.MarshalBinary()
	c.Check(err, Equals, notify.ErrVersionUnset)
	c.Check(bytes, IsNil)

	resend := notify.MsgNotificationResend{}
	bytes, err = resend.MarshalBinary()
	c.Check(err, Equals, notify.ErrVersionUnset)
	c.Check(bytes, IsNil)

	filter := notify.MsgNotificationFilter{}
	bytes, err = filter.MarshalBinary()
	c.Check(err, Equals, notify.ErrVersionUnset)
	c.Check(bytes, IsNil)

	notif := notify.MsgNotification{}
	bytes, err = notif.MarshalBinary()
	c.Check(err, Equals, notify.ErrVersionUnset)
	c.Check(bytes, IsNil)

	resp := notify.MsgNotificationResponse{}
	bytes, err = resp.MarshalBinary()
	c.Check(err, Equals, notify.ErrVersionUnset)
	c.Check(bytes, IsNil)

	file := notify.MsgNotificationFile{}
	bytes, err = file.MarshalBinary()
	c.Check(err, Equals, notify.ErrVersionUnset)
	c.Check(bytes, IsNil)
}

func (*messageSuite) TestMsgNotificationRegisterMarshalUnmarshal(c *C) {
	if notify.NativeByteOrder != binary.LittleEndian {
		c.Skip("test only written for little-endian architectures")
	}
	for _, t := range []struct {
		bytes []byte
		msg   notify.MsgNotificationRegister
	}{
		{
			bytes: []byte{
				0xc, 0x0, // Length
				0x5, 0x0, // Protocol
				0x88, 0x77, 0x66, 0x55, 0x44, 0x33, 0x22, 0x11, // Listener ID
			},
			msg: notify.MsgNotificationRegister{
				MsgHeader: notify.MsgHeader{
					Length:  0xc,
					Version: 0x5,
				},
				KernelListenerID: 0x1122334455667788,
			},
		},
		{
			bytes: []byte{
				0xc, 0x0, // Length
				0x5, 0x0, // Protocol
				0xb, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, // Listener ID
			},
			msg: notify.MsgNotificationRegister{
				MsgHeader: notify.MsgHeader{
					Length:  0xc,
					Version: 0x5,
				},
				KernelListenerID: 0xb,
			},
		},
	} {
		bytes, err := t.msg.MarshalBinary()
		c.Check(err, IsNil)
		c.Check(bytes, DeepEquals, t.bytes)

		var msg notify.MsgNotificationRegister
		err = msg.UnmarshalBinary(t.bytes)
		c.Check(err, IsNil)
		c.Check(msg, DeepEquals, t.msg)
	}
}

func (*messageSuite) TestMsgNotificationResendMarshalUnmarshal(c *C) {
	if notify.NativeByteOrder != binary.LittleEndian {
		c.Skip("test only written for little-endian architectures")
	}
	for _, t := range []struct {
		bytes []byte
		msg   notify.MsgNotificationResend
	}{
		{
			bytes: []byte{
				0x14, 0x0, // Length
				0x5, 0x0, // Protocol
				0x88, 0x77, 0x66, 0x55, 0x44, 0x33, 0x22, 0x11, // Listener ID
				0x0, 0x0, 0x0, 0x0, // Ready
				0x0, 0x0, 0x0, 0x0, // Pending
			},
			msg: notify.MsgNotificationResend{
				MsgHeader: notify.MsgHeader{
					Length:  0x14,
					Version: 0x5,
				},
				KernelListenerID: 0x1122334455667788,
				Ready:            0,
				Pending:          0,
			},
		},
		{
			bytes: []byte{
				0x14, 0x0, // Length
				0x5, 0x0, // Protocol
				0xb, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, // Listener ID
				0x44, 0x33, 0x22, 0x11, // Ready
				0xaa, 0xbb, 0xcc, 0xdd, // Pending
			},
			msg: notify.MsgNotificationResend{
				MsgHeader: notify.MsgHeader{
					Length:  0x14,
					Version: 0x5,
				},
				KernelListenerID: 0xb,
				Ready:            0x11223344,
				Pending:          0xddccbbaa,
			},
		},
	} {
		bytes, err := t.msg.MarshalBinary()
		c.Check(err, IsNil)
		c.Check(bytes, DeepEquals, t.bytes)

		var msg notify.MsgNotificationResend
		err = msg.UnmarshalBinary(t.bytes)
		c.Check(err, IsNil)
		c.Check(msg, DeepEquals, t.msg)
	}
}

func (*messageSuite) TestMsgNotificationFilterMarshalUnmarshal(c *C) {
	if notify.NativeByteOrder == binary.BigEndian {
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
		// TODO: add test cases for other versions once they are supported
	} {
		bytes, err := t.msg.MarshalBinary()
		c.Check(err, IsNil)
		c.Check(bytes, DeepEquals, t.bytes)

		var msg notify.MsgNotificationFilter
		err = msg.UnmarshalBinary(t.bytes)
		c.Assert(err, IsNil)
		c.Assert(msg, DeepEquals, t.msg)
	}
}

func (*messageSuite) TestMsgNotificationFilterUnmarshalErrors(c *C) {
	if notify.NativeByteOrder == binary.BigEndian {
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
			comment: "message with namespace without proper termination",
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
		err := msg.UnmarshalBinary(t.bytes)
		c.Assert(err, ErrorMatches, t.errMsg, Commentf("%s", t.comment))
	}
}

func (*messageSuite) TestMsgNotificationFilterValidate(c *C) {
	msg := notify.MsgNotificationFilter{}
	c.Check(msg.Validate(), IsNil)
	msg = notify.MsgNotificationFilter{ModeSet: 10000}
	c.Check(msg.Validate(), ErrorMatches, "unsupported modeset: 10000")
}

func (*messageSuite) TestFlags(c *C) {
	c.Check(notify.URESPONSE_NO_CACHE, Equals, 0x1)
	c.Check(notify.URESPONSE_LOOKUP, Equals, 0x2)
	c.Check(notify.URESPONSE_PROFILE, Equals, 0x4)
	c.Check(notify.URESPONSE_TAILGLOB, Equals, 0x8)
	c.Check(notify.UNOTIF_RESENT, Equals, 0x10)
}

func (*messageSuite) TestMsgNotificationMarshalBinary(c *C) {
	if notify.NativeByteOrder == binary.BigEndian {
		c.Skip("test only written for little-endian architectures")
	}
	msg := notify.MsgNotification{
		NotificationType:     notify.APPARMOR_NOTIF_RESP,
		Signalled:            1,
		Flags:                3,
		KernelNotificationID: 0x1234,
		Error:                0xFF,
	}
	msg.Version = notify.ProtocolVersion(0xAA)
	data, err := msg.MarshalBinary()
	c.Assert(err, IsNil)
	c.Check(data, HasLen, 20)
	c.Check(data, DeepEquals, []byte{
		0x14, 0x0, // Length
		0xAA, 0x0, // Protocol
		0x0, 0x0, // Notification Type
		0x1,                                            // Signalled
		0x3,                                            // Flags
		0x34, 0x12, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // ID
		0xFF, 0x00, 0x00, 0x00, // Error
	})
}

func (s *messageSuite) TestMsgNotificationFileUnmarshalBinaryV3(c *C) {
	if notify.NativeByteOrder == binary.BigEndian {
		c.Skip("test only written for little-endian architectures")
	}
	// Notification for accessing the /root/.ssh/ directory.
	bytes := []byte{
		0x4c, 0x0, // Length == 76 bytes
		0x3, 0x0, // Protocol
		0x4, 0x0, // Notification type == notify.APPARMOR_NOTIF_OP
		0x0,                                    // Signalled
		0x3,                                    // Flags
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
		0x40, 0x0, 0x0, 0x0, // Filename at +64 bytes into buffer
		0x74, 0x65, 0x73, 0x74, 0x2d, 0x70, 0x72, 0x6f, 0x6d, 0x70, 0x74, 0x0, // "test-prompt\0"
		0x2f, 0x72, 0x6f, 0x6f, 0x74, 0x2f, 0x2e, 0x73, 0x73, 0x68, 0x2f, 0x0, // "/root/.ssh/\0"
	}
	c.Assert(bytes, HasLen, 76)

	var msg notify.MsgNotificationFile
	err := msg.UnmarshalBinary(bytes)
	c.Assert(err, IsNil)
	expected := notify.MsgNotificationFile{
		MsgNotificationOp: notify.MsgNotificationOp{
			MsgNotification: notify.MsgNotification{
				MsgHeader: notify.MsgHeader{
					Length:  76,
					Version: 3,
				},
				NotificationType:     notify.APPARMOR_NOTIF_OP,
				Flags:                0x3,
				KernelNotificationID: 2,
				Error:                -13,
			},
			Allow: 4,
			Deny:  4,
			Pid:   0x819,
			Label: "test-prompt",
			Class: notify.AA_CLASS_FILE,
		},
		Filename: "/root/.ssh/",
	}
	c.Assert(msg, DeepEquals, expected)

	// Check that MsgNotificationFiles can be marshalled and are identical
	// after unmarshal.
	buf, err := msg.MarshalBinary()
	c.Assert(err, IsNil)
	var roundTripMsg notify.MsgNotificationFile
	err = roundTripMsg.UnmarshalBinary(buf)
	c.Assert(err, IsNil)
	c.Assert(roundTripMsg, DeepEquals, expected)
}

func (s *messageSuite) TestMsgNotificationFileUnmarshalBinaryV5WithoutTags(c *C) {
	if notify.NativeByteOrder == binary.BigEndian {
		c.Skip("test only written for little-endian architectures")
	}
	// Notification for accessing the /root/.ssh/ directory.
	bytes := []byte{
		0x52, 0x0, // Length == 82 bytes
		0x5, 0x0, // Protocol
		0x4, 0x0, // Notification type == notify.APPARMOR_NOTIF_OP
		0x0,                                    // Signalled
		0x2,                                    // Flags
		0x2, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, // ID (request #2, just a number)
		0xf3, 0xff, 0xff, 0xff, // Error -13 EACCESS
		0x4, 0x0, 0x0, 0x0, // Allow - ???
		0x4, 0x0, 0x0, 0x0, // Deny - ???
		0x19, 0x8, 0x0, 0x0, // PID
		0x3a, 0x0, 0x0, 0x0, // Label at +58 bytes into buffer
		0x2, 0x0, // Class - AA_CLASS_FILE
		0x0, 0x0, // Op - ???
		0x0, 0x0, 0x0, 0x0, // SUID
		0x0, 0x0, 0x0, 0x0, // OUID
		0x46, 0x0, 0x0, 0x0, // Name at +70 bytes into buffer
		0x0, 0x0, 0x0, 0x0, // Tagset headers empty
		0x0, 0x0, // Tagset count - 0
		0x74, 0x65, 0x73, 0x74, 0x2d, 0x70, 0x72, 0x6f, 0x6d, 0x70, 0x74, 0x0, // "test-prompt\0"
		0x2f, 0x72, 0x6f, 0x6f, 0x74, 0x2f, 0x2e, 0x73, 0x73, 0x68, 0x2f, 0x0, // "/root/.ssh/\0"
	}
	c.Assert(bytes, HasLen, 82)

	var msg notify.MsgNotificationFile
	err := msg.UnmarshalBinary(bytes)
	c.Assert(err, IsNil)
	expected := notify.MsgNotificationFile{
		MsgNotificationOp: notify.MsgNotificationOp{
			MsgNotification: notify.MsgNotification{
				MsgHeader: notify.MsgHeader{
					Length:  82,
					Version: 5,
				},
				NotificationType:     notify.APPARMOR_NOTIF_OP,
				Flags:                0x2,
				KernelNotificationID: 2,
				Error:                -13,
			},
			Allow: 4,
			Deny:  4,
			Pid:   0x819,
			Label: "test-prompt",
			Class: notify.AA_CLASS_FILE,
		},
		Filename: "/root/.ssh/",
	}
	c.Assert(msg, DeepEquals, expected)

	// Check that MsgNotificationFiles can be marshalled and are identical
	// after unmarshal.
	buf, err := msg.MarshalBinary()
	c.Assert(err, IsNil)
	var roundTripMsg notify.MsgNotificationFile
	err = roundTripMsg.UnmarshalBinary(buf)
	c.Assert(err, IsNil)
	c.Assert(roundTripMsg, DeepEquals, expected)
}

func (s *messageSuite) TestMsgNotificationFileUnmarshalBinaryV5(c *C) {
	if notify.NativeByteOrder == binary.BigEndian {
		c.Skip("test only written for little-endian architectures")
	}
	// Notification for accessing /file
	bytes := []byte{
		0x7f, 0x0, // Length == 127 bytes
		0x5, 0x0, // Protocol
		0x4, 0x0, // Notification type == notify.APPARMOR_NOTIF_OP
		0x0,                                    // Signalled
		0xaa,                                   // Flags
		0x2, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, // ID (request #2, just a number)
		0xf3, 0xff, 0xff, 0xff, // Error -13 EACCESS
		0xaa, 0xaa, 0xaa, 0xaa, // Allow - ???
		0x55, 0x55, 0x55, 0x55, // Deny - ???
		0x19, 0x8, 0x0, 0x0, // PID
		0x40, 0x0, 0x0, 0x0, // Label at +64 bytes into buffer
		0x2, 0x0, // Class - AA_CLASS_FILE
		0x0, 0x0, // Op - ???
		0xe8, 0x03, 0x0, 0x0, // SUID - 1000
		0xe8, 0x03, 0x0, 0x0, // OUID
		0x48, 0x0, 0x0, 0x0, // Name at +72 bytes into buffer
		0x50, 0x0, 0x0, 0x0, // Tagset headers at +80 bytes into the buffer (need to be 8-byte-aligned)
		0x02, 0x0, // Tagset count - 2
		0x0, 0x0, 0x0, 0x0, 0x0, 0x0, // padding to make []data 8-byte-aligned
		0x70, 0x72, 0x6f, 0x66, 0x69, 0x6c, 0x65, 0x0, // "profile\0"
		0x2f, 0x66, 0x69, 0x6c, 0x65, 0x0, // "/file\0"
		0x0, 0x0, // padding to make tagset headers 8-byte-aligned
		0x03, 0x01, 0x00, 0x00, // tagset 1 permission mask
		0x03, 0x00, 0x00, 0x00, // tagset 1 tag count
		0x68, 0x00, 0x00, 0x00, // tagset 1 starts at +104 into the buffer
		0x0c, 0x00, 0x00, 0x00, // tagset 2 permission mask
		0x02, 0x00, 0x00, 0x00, // tagset 2 tag count
		0x77, 0x00, 0x00, 0x00, // tagset 2 starts at +119 into the buffer
		0x6f, 0x6e, 0x65, 0x00, // "one\0"
		0x74, 0x77, 0x6f, 0x00, // "two\0"
		0x74, 0x68, 0x72, 0x65, 0x65, 0x00, // "three\0"
		0x00,                   // end of tagset 1
		0x61, 0x62, 0x63, 0x00, // "abc\0"
		0x65, 0x66, 0x00, // "ef\0"
		0x00, // end of tagset 2
	}
	c.Assert(bytes, HasLen, 127)

	var msg notify.MsgNotificationFile
	err := msg.UnmarshalBinary(bytes)
	c.Assert(err, IsNil)
	expected := notify.MsgNotificationFile{
		MsgNotificationOp: notify.MsgNotificationOp{
			MsgNotification: notify.MsgNotification{
				MsgHeader: notify.MsgHeader{
					Length:  127,
					Version: 5,
				},
				NotificationType:     notify.APPARMOR_NOTIF_OP,
				Flags:                0xaa,
				KernelNotificationID: 2,
				Error:                -13,
			},
			Allow: 0xaaaaaaaa,
			Deny:  0x55555555,
			Pid:   0x819,
			Label: "profile",
			Class: notify.AA_CLASS_FILE,
		},
		SUID:     1000,
		OUID:     1000,
		Filename: "/file",
		Tagsets: notify.TagsetMap{
			notify.FilePermission(0x0103): {
				"one",
				"two",
				"three",
			},
			notify.FilePermission(0x000c): {
				"abc",
				"ef",
			},
		},
	}
	c.Assert(msg, DeepEquals, expected)

	// Check that MsgNotificationFiles can be marshalled and are identical
	// after unmarshal.
	buf, err := msg.MarshalBinary()
	c.Assert(err, IsNil)
	var roundTripMsg notify.MsgNotificationFile
	err = roundTripMsg.UnmarshalBinary(buf)
	c.Assert(err, IsNil)
	// Messages may be marshalled slightly different, so length needs to be adjusted
	expected.Length = roundTripMsg.Length
	c.Assert(roundTripMsg, DeepEquals, expected)
}

func (s *messageSuite) TestMsgNotificationFileUnmarshalBinaryV5WithOverlappingAndEmptyTagsets(c *C) {
	if notify.NativeByteOrder == binary.BigEndian {
		c.Skip("test only written for little-endian architectures")
	}
	// Notification for accessing /file
	bytes := []byte{
		0x93, 0x0, // Length == 147 bytes
		0x5, 0x0, // Protocol
		0x4, 0x0, // Notification type == notify.APPARMOR_NOTIF_OP
		0x0,                                    // Signalled
		0x43,                                   // Flags
		0x2, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, // ID (request #2, just a number)
		0xf3, 0xff, 0xff, 0xff, // Error -13 EACCESS
		0xaa, 0xaa, 0xaa, 0xaa, // Allow - ???
		0x55, 0x55, 0x55, 0x55, // Deny - ???
		0x19, 0x8, 0x0, 0x0, // PID
		0x40, 0x0, 0x0, 0x0, // Label at +64 bytes into buffer
		0x2, 0x0, // Class - AA_CLASS_FILE
		0x0, 0x0, // Op - ???
		0xe8, 0x03, 0x0, 0x0, // SUID - 1000
		0xe8, 0x03, 0x0, 0x0, // OUID
		0x48, 0x0, 0x0, 0x0, // Name at +72 bytes into buffer
		0x50, 0x0, 0x0, 0x0, // Tagset headers at +80 bytes into the buffer (need to be 8-byte-aligned)
		0x04, 0x0, // Tagset count - 4
		0x0, 0x0, 0x0, 0x0, 0x0, 0x0, // padding to make []data 8-byte-aligned
		0x70, 0x72, 0x6f, 0x66, 0x69, 0x6c, 0x65, 0x0, // "profile\0"
		0x2f, 0x66, 0x69, 0x6c, 0x65, 0x0, // "/file\0"
		0x0, 0x0, // padding to make tagset headers 8-byte-aligned
		// tagset 1 has no tags -- this should not happen in practice
		0x01, 0x00, 0x00, 0x00, // tagset 1 permission mask
		0x00, 0x00, 0x00, 0x00, // tagset 1 tag count
		0x00, 0x00, 0x00, 0x00, // tagset 1 is empty
		// tagsets 2-4 overlap, 3->(2/4), where 4 is a sebset of 2
		0x02, 0x00, 0x00, 0x00, // tagset 2 permission mask
		0x03, 0x00, 0x00, 0x00, // tagset 2 tag count
		0x84, 0x00, 0x00, 0x00, // tagset 2 starts at +132 into the buffer
		0x04, 0x00, 0x00, 0x00, // tagset 3 permission mask
		0x02, 0x00, 0x00, 0x00, // tagset 3 tag count
		0x80, 0x00, 0x00, 0x00, // tagset 3 starts at +128 into the buffer
		0x08, 0x00, 0x00, 0x00, // tagset 4 permission mask
		0x02, 0x00, 0x00, 0x00, // tagset 4 tag count
		0x84, 0x00, 0x00, 0x00, // tagset 4 starts at +132 into the buffer
		0x6f, 0x6e, 0x65, 0x00, // "one\0"
		0x74, 0x77, 0x6f, 0x00, // "two\0"
		0x74, 0x68, 0x72, 0x65, 0x65, 0x00, // "three\0"
		0x66, 0x6f, 0x75, 0x72, 0x00, // "four\0"
	}
	c.Assert(bytes, HasLen, 147)

	var msg notify.MsgNotificationFile
	err := msg.UnmarshalBinary(bytes)
	c.Assert(err, IsNil)
	expected := notify.MsgNotificationFile{
		MsgNotificationOp: notify.MsgNotificationOp{
			MsgNotification: notify.MsgNotification{
				MsgHeader: notify.MsgHeader{
					Length:  147,
					Version: 5,
				},
				NotificationType:     notify.APPARMOR_NOTIF_OP,
				Flags:                0x43,
				KernelNotificationID: 2,
				Error:                -13,
			},
			Allow: 0xaaaaaaaa,
			Deny:  0x55555555,
			Pid:   0x819,
			Label: "profile",
			Class: notify.AA_CLASS_FILE,
		},
		SUID:     1000,
		OUID:     1000,
		Filename: "/file",
		Tagsets: notify.TagsetMap{
			notify.FilePermission(0x01): notify.MetadataTags(nil),
			notify.FilePermission(0x02): {
				"two",
				"three",
				"four",
			},
			notify.FilePermission(0x04): {
				"one",
				"two",
			},
			notify.FilePermission(0x08): {
				"two",
				"three",
			},
		},
	}
	c.Assert(msg, DeepEquals, expected)

	// Check that MsgNotificationFiles can be marshalled and are identical
	// after unmarshal.
	buf, err := msg.MarshalBinary()
	c.Assert(err, IsNil)
	var roundTripMsg notify.MsgNotificationFile
	err = roundTripMsg.UnmarshalBinary(buf)
	c.Assert(err, IsNil)
	// Messages may be marshalled slightly different, so length needs to be adjusted
	expected.Length = roundTripMsg.Length
	c.Assert(roundTripMsg, DeepEquals, expected)
}

func (s *messageSuite) TestMsgNotificationFileUnmarshalBinaryV5Errors(c *C) {
	if notify.NativeByteOrder == binary.BigEndian {
		c.Skip("test only written for little-endian architectures")
	}
	bytesTemplate := []byte{
		0x60, 0x0, // Length == 96 bytes
		0x5, 0x0, // Protocol
		0x4, 0x0, // Notification type == notify.APPARMOR_NOTIF_OP
		0x0,                                    // Signalled
		0x1,                                    // Flags
		0x2, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, // ID (request #2, just a number)
		0xf3, 0xff, 0xff, 0xff, // Error -13 EACCESS
		0xaa, 0xaa, 0xaa, 0xaa, // Allow - ???
		0x55, 0x55, 0x55, 0x55, // Deny - ???
		0x19, 0x8, 0x0, 0x0, // PID
		0x40, 0x0, 0x0, 0x0, // Label at +64 bytes into buffer
		0x2, 0x0, // Class - AA_CLASS_FILE
		0x0, 0x0, // Op - ???
		0xe8, 0x03, 0x0, 0x0, // SUID - 1000
		0xe8, 0x03, 0x0, 0x0, // OUID
		0x48, 0x0, 0x0, 0x0, // Name at +72 bytes into buffer
		0x50, 0x0, 0x0, 0x0, // Tagset headers at +80 bytes into the buffer (need to be 8-byte-aligned)
		0x01, 0x0, // Tagset count - 1
		0x0, 0x0, 0x0, 0x0, 0x0, 0x0, // padding to make []data 8-byte-aligned
		0x70, 0x72, 0x6f, 0x66, 0x69, 0x6c, 0x65, 0x0, // "profile\0"
		0x2f, 0x66, 0x69, 0x6c, 0x65, 0x0, // "/file\0"
		0x0, 0x0, // padding to make tagset headers 8-byte-aligned
		// put tagset headers here
	}

	for _, testCase := range []struct {
		remainingBytes []byte
		expectedError  string
	}{
		{
			remainingBytes: []byte{
				0x01, 0x02, 0x03, 0x04, // permission mask
				0x01, 0x00, 0x00, 0x00, // tag count
				0x60, 0x00, 0x00, 0x00, // tag offset (out of range)
				0x00, 0x00, 0x00, 0x00, // filler data to make the lengths the same
			},
			expectedError: ".* address 96 points outside of message body",
		},
		{
			remainingBytes: []byte{
				0x87, 0x65, 0x43, 0x21, // permission mask
				0x01, 0x00, 0x00, 0x00, // tag count
				0x5c, 0x00, 0x00, 0x00, // tag starts at +92 into the buffer
				0x61, 0x62, 0x63, 0x64, // "abcd" but missing the trailing '\0'
			},
			expectedError: ".* unterminated string at address 92",
		},
	} {
		bytes := append(bytesTemplate, testCase.remainingBytes...)
		var msg notify.MsgNotificationFile
		err := msg.UnmarshalBinary(bytes)
		c.Check(err, ErrorMatches, testCase.expectedError)
	}
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

func (s *messageSuite) TestBuildResponse(c *C) {
	var (
		protocol  = notify.ProtocolVersion(123)
		id        = uint64(456)
		aaAllowed = notify.FilePermission(0b0101)
		requested = notify.FilePermission(0b0011)
	)

	for _, testCase := range []struct {
		userAllowed   notify.AppArmorPermission
		expectedAllow uint32
		expectedDeny  uint32
	}{
		{
			nil,
			0b0100,
			0b0011,
		},
		{
			notify.FilePermission(0b0000),
			0b0100,
			0b0011,
		},
		{
			notify.FilePermission(0b0001),
			0b0101,
			0b0010,
		},
		{
			notify.FilePermission(0b0010),
			0b0110,
			0b0001,
		},
		{
			notify.FilePermission(0b0011),
			0b0111,
			0b0000,
		},
		{
			notify.FilePermission(0b0100),
			0b0100,
			0b0011,
		},
		{
			notify.FilePermission(0b0101),
			0b0101,
			0b0010,
		},
		{
			notify.FilePermission(0b0110),
			0b0110,
			0b0001,
		},
		{
			notify.FilePermission(0b0111),
			0b0111,
			0b0000,
		},
		{
			notify.FilePermission(0b1000),
			0b1100,
			0b0011,
		},
		{
			notify.FilePermission(0b1001),
			0b1101,
			0b0010,
		},
		{
			notify.FilePermission(0b1010),
			0b1110,
			0b0001,
		},
		{
			notify.FilePermission(0b1011),
			0b1111,
			0b0000,
		},
		{
			notify.FilePermission(0b1100),
			0b1100,
			0b0011,
		},
		{
			notify.FilePermission(0b1101),
			0b1101,
			0b0010,
		},
		{
			notify.FilePermission(0b1110),
			0b1110,
			0b0001,
		},
		{
			notify.FilePermission(0b1111),
			0b1111,
			0b0000,
		},
	} {
		resp := notify.BuildResponse(protocol, id, aaAllowed, requested, testCase.userAllowed)
		c.Check(resp.Version, Equals, protocol)
		c.Check(resp.NotificationType, Equals, notify.APPARMOR_NOTIF_RESP)
		c.Check(resp.Flags, Equals, uint8(1))
		c.Check(resp.KernelNotificationID, Equals, id)
		c.Check(resp.Allow, Equals, testCase.expectedAllow)
		c.Check(resp.Deny, Equals, testCase.expectedDeny)
	}
}

func (s *messageSuite) TestMsgNotificationResponseMarshalBinary(c *C) {
	if notify.NativeByteOrder == binary.BigEndian {
		c.Skip("test only written for little-endian architectures")
	}
	msg := notify.MsgNotificationResponse{
		MsgNotification: notify.MsgNotification{
			MsgHeader: notify.MsgHeader{
				Version: 43,
			},
			NotificationType:     0x11,
			Signalled:            0x22,
			Flags:                0x33,
			KernelNotificationID: 0x44,
			Error:                0x55,
		},
		Error: 0x66,
		Allow: 0x77,
		Deny:  0x88,
	}
	bytes, err := msg.MarshalBinary()
	c.Assert(err, IsNil)
	c.Assert(bytes, DeepEquals, []byte{
		0x20, 0x0, // Length
		43, 0x0, // Version
		0x11, 0x0, // Notification Type
		0x22,                                    // Signalled
		0x33,                                    // Flags
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
	allow, deny, err := msg.DecodeFilePermissions()
	c.Assert(err, IsNil)
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
	_, _, err := msg.DecodeFilePermissions()
	c.Assert(err, ErrorMatches, "mediation class AA_CLASS_DBUS does not describe file permissions")
}

func (*messageSuite) TestMsgNotificationFileAsGeneric(c *C) {
	var msg notify.MsgNotificationFile
	msg.Flags = 0
	msg.KernelNotificationID = 123
	msg.Pid = 456
	msg.Label = "hello there"
	msg.Class = notify.AA_CLASS_FILE
	msg.Allow = 0b0011
	msg.Deny = 0b0101
	msg.SUID = 789
	msg.Filename = "/foo/bar"
	msg.Tagsets = notify.TagsetMap{
		notify.FilePermission(0b0001): {"foo"},
		notify.FilePermission(0b0010): {"bar", "baz"},
		notify.FilePermission(0b1100): {"qux"},
	}

	expectedTagsets := notify.TagsetMap{
		notify.FilePermission(0b0001): {"foo"},
		notify.FilePermission(0b0100): {"qux"},
	}

	testMsgNotificationGeneric(c, &msg, msg.KernelNotificationID, false, msg.Pid, msg.Label, msg.Class, msg.Allow, msg.Deny, msg.SUID, msg.Filename, expectedTagsets)

	msg.Flags = notify.UNOTIF_RESENT
	testMsgNotificationGeneric(c, &msg, msg.KernelNotificationID, true, msg.Pid, msg.Label, msg.Class, msg.Allow, msg.Deny, msg.SUID, msg.Filename, expectedTagsets)
}

func testMsgNotificationGeneric(c *C, generic notify.MsgNotificationGeneric, id uint64, resent bool, pid int32, label string, class notify.MediationClass, allowed, denied, suid uint32, name string, tagsets notify.TagsetMap) {
	c.Check(generic.ID(), Equals, id)
	c.Check(generic.Resent(), Equals, resent)
	c.Check(generic.PID(), Equals, pid)
	c.Check(generic.ProcessLabel(), Equals, label)
	c.Check(generic.MediationClass(), Equals, class)
	msgAllow, msgDeny, err := generic.AllowedDeniedPermissions()
	c.Check(err, IsNil)
	c.Check(msgAllow.AsAppArmorOpMask(), Equals, allowed)
	c.Check(msgDeny.AsAppArmorOpMask(), Equals, denied)
	c.Check(generic.SubjectUID(), Equals, suid)
	c.Check(generic.Name(), Equals, name)
	c.Check(generic.DeniedMetadataTagsets(), DeepEquals, tagsets)
}
