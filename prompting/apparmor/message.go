package apparmor

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/snapcore/snapd/arch"
)

// Message fields are defined as raw sized integer types as the same type may be
// packed as 16 bit or 32 bit integer, to accommodate other fields in the
// structure.

// MsgHeader is the header of all apparmor notification messages.
//
// The header encodes the size of the entire message, including variable-size
// components and the version of the notification protocol.
type MsgHeader struct {
	// Length is the length of the entire message, including any more
	// specialized messages appended after the header and any variable-length
	// objects referenced by any such message.
	Length uint16
	// Version is the version of the communication protocol.
	// Currently version 2 is implemented by the demo kernel based on Linux 5.7.7,
	// but version 3 was used in the past.
	Version uint16
}

// UnmarshalBinary unmarshals the header from binary form.
func (msg *MsgHeader) UnmarshalBinary(data []byte) error {
	const prefix = "cannot unmarshal apparmor message header"

	// Unpack fixed-size elements.
	order := arch.Endian() // ioctl messages are native byte order, but others might not be
	buf := bytes.NewBuffer(data)
	if err := binary.Read(buf, order, msg); err != nil {
		return fmt.Errorf("%s: %s", prefix, err)
	}

	if msg.Version != 2 {
		return fmt.Errorf("%s: unsupported version: %d", prefix, msg.Version)
	}
	if int(msg.Length) != len(data) {
		return fmt.Errorf("%s: length mismatch %d != %d",
			prefix, msg.Length, len(data))
	}

	return nil
}

// msgNotificationFilter describes the configuration of kernel-side message filtering.
//
// This structure corresponds to the kernel type struct apparmor_notif_filter
// described below. This type is only used for message marshaling and
// unmarshaling. Application code should use MsgNotificationFilter instead.
//
//	struct apparmor_notif_filter {
//	  struct apparmor_notif_common base;
//	  __u32 modeset;      /* which notification mode */
//	  __u32 ns;           /* offset into data, relative to start of the structure */
//	  __u32 filter;       /* offset into data, relative to start of the structure */
//	  __u8 data[];
//	} __attribute__((packed));
type msgNotificationFilter struct {
	MsgHeader
	ModeSet uint32
	NS      uint32
	Filter  uint32
}

// MsgNotificationFilter describes the configuration of kernel-side message filtering.
//
// This structure can be marshaled and unmarshaled to binary form and
// transmitted to the kernel using NotifyIoctl along with IoctlGetFilter and
// IoctlSetFilter.
type MsgNotificationFilter struct {
	MsgHeader
	// ModeSet is a bitmask. Specifying APPARMOR_MODESET_USER allows to
	// receive notification messages in userspace.
	ModeSet ModeSet
	// XXX: This is currently unused by the kernel and the value is ignored.
	NameSpace string
	// XXX: This is currently unused by the kernel and the format is unknown.
	Filter string
}

// UnmarshalBinary unmarshals the message from binary form.
func (msg *MsgNotificationFilter) UnmarshalBinary(data []byte) error {
	const prefix = "cannot unmarshal apparmor notification filter message"

	// Unpack the base structure.
	if err := msg.MsgHeader.UnmarshalBinary(data); err != nil {
		return fmt.Errorf("%s: %v", prefix, err)
	}

	// Unpack fixed-size elements.
	buf := bytes.NewBuffer(data)
	var raw msgNotificationFilter
	order := arch.Endian() // ioctl messages are native byte order, but others might not be
	if err := binary.Read(buf, order, &raw); err != nil {
		return fmt.Errorf("%s: cannot unpack: %s", prefix, err)
	}

	// Unpack variable length elements.
	unpacker := newStringUnpacker(data)
	ns, err := unpacker.UnpackString(raw.NS)
	if err != nil {
		return fmt.Errorf("%s: cannot unpack namespace: %v", prefix, err)
	}
	filter, err := unpacker.UnpackString(raw.Filter)
	if err != nil {
		return fmt.Errorf("%s: cannot unpack filter: %v", prefix, err)
	}

	// Put everything together.
	msg.ModeSet = ModeSet(raw.ModeSet)
	msg.NameSpace = ns
	msg.Filter = filter

	return nil
}

// MarshalBinary marshals the message into binary form.
func (msg *MsgNotificationFilter) MarshalBinary() (data []byte, err error) {
	var raw msgNotificationFilter
	packer := newStringPacker(raw)
	raw.Version = 2
	raw.ModeSet = uint32(msg.ModeSet)
	raw.NS = packer.PackString(msg.NameSpace)
	raw.Filter = packer.PackString(msg.Filter)
	raw.Length = packer.TotalLen()
	msgBuf := bytes.NewBuffer(make([]byte, 0, raw.Length))
	order := arch.Endian() // ioctl messages are native byte order, but others might not be
	if err := binary.Write(msgBuf, order, raw); err != nil {
		return nil, err
	}
	if _, err := msgBuf.Write(packer.Bytes()); err != nil {
		return nil, err
	}
	return msgBuf.Bytes(), nil
}

// Validate returns an error if the mssage contains invalid data.
func (msg *MsgNotificationFilter) Validate() error {
	if !msg.ModeSet.IsValid() {
		return fmt.Errorf("unsupported modeset: %d", msg.ModeSet)
	}
	return nil
}

// MsgNotification describes a kernel notification message.
//
// This structure corresponds to the kernel type struct apparmor_notif
// described below.
//
//	struct apparmor_notif {
//	  struct apparmor_notif_common base;
//	  __u16 ntype;        /* notify type */
//	  __u8 signalled;
//	  __u8 flags;
//	  __u64 id;           /* unique id, not globally unique*/
//	  __s32 error;        /* error if unchanged */
//	} __attribute__((packed));
type MsgNotification struct {
	MsgHeader
	// NotificationType describes the kind of notification message used.
	// Currently the kernel only sends APPARMOR_NOTIF_OP messages.
	NotificationType NotificationType
	// XXX: Signaled seems to be unused.
	Signalled uint8
	// Set to 1 to NOT cache
	Flags uint8
	// ID is an opaque kernel identifier of the notification message. It must be
	// repeated in the MsgNotificationResponse if one is sent back.
	ID uint64
	// XXX: This seems to be unused and clashes with identical field in
	// MsgNotificationOp.
	Error int32
}

// UnmarshalBinary unmarshals the message from binary form.
func (msg *MsgNotification) UnmarshalBinary(data []byte) error {
	const prefix = "cannot unmarshal apparmor notification message"

	// Unpack fixed-size elements.
	buf := bytes.NewBuffer(data)
	order := arch.Endian() // ioctl messages are native byte order, but others might not be
	if err := binary.Read(buf, order, msg); err != nil {
		return fmt.Errorf("%s: cannot unpack: %s", prefix, err)
	}

	return nil
}

// MarshalBinary marshals the message into binary form.
func (msg *MsgNotification) MarshalBinary() ([]byte, error) {
	msg.Version = 2
	msg.Length = uint16(binary.Size(*msg))
	buf := bytes.NewBuffer(make([]byte, 0, msg.Length))
	order := arch.Endian() // ioctl messages are native byte order, but others might not be
	if err := binary.Write(buf, order, msg); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Validate returns an error if the mssage contains invalid data.
func (msg *MsgNotification) Validate() error {
	if !msg.NotificationType.IsValid() {
		return fmt.Errorf("unsupported notification type: %d", msg.NotificationType)
	}
	return nil
}

// MsgNotificationUpdate (TBD, document me)
//
//	struct apparmor_notif_update {
//	  struct apparmor_notif base;
//	  __u16 ttl;          /* max keep alives left */
//	} __attribute__((packed));
type MsgNotificationUpdate struct {
	MsgNotification
	TTL uint16
}

// MsgNotificationResponse describes a response to a MsgNotification.
//
// This structure corresponds to the kernel type struct apparmor_notif
// described below.
//
//	struct apparmor_notif_resp {
//	  struct apparmor_notif base;
//	  __s32 error;        /* error if unchanged */
//	  __u32 allow;
//	  __u32 deny;
//	} __attribute__((packed));
type MsgNotificationResponse struct {
	MsgNotification
	// XXX: The embedded MsgNotification also has an Error field, why?
	Error int32
	// Allow somehow encodes the allowed operation mask.
	Allow uint32
	// Deny somehow encodes the denied operation mask.
	Deny uint32
}

// ResponseForRequest returns a response message for a given request.
func ResponseForRequest(req *MsgNotification) MsgNotificationResponse {
	return MsgNotificationResponse{
		MsgNotification: MsgNotification{
			NotificationType: APPARMOR_NOTIF_RESP,
			// XXX: should Signalled be copied?
			Signalled: req.Signalled,
			// XXX: should Flags be copied?
			Flags: req.Flags,
			ID:    req.ID,
			// XXX: should Error be copied?
			Error: req.Error,
		},
	}
}

// MarshalBinary marshals the message into binary form.
func (msg *MsgNotificationResponse) MarshalBinary() ([]byte, error) {
	msg.Version = 2
	msg.Length = uint16(binary.Size(*msg))
	buf := bytes.NewBuffer(make([]byte, 0, msg.Length))
	order := arch.Endian() // ioctl messages are native byte order, but others might not be
	if err := binary.Write(buf, order, msg); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// MsgNotificationOp (TBD, document me).
//
//	struct apparmor_notif_op {
//	  struct apparmor_notif base;
//	  __u32 allow;
//	  __u32 deny;
//	  pid_t pid;          /* pid of task causing notification */
//	  __u32 label;        /* offset into data */
//	  __u16 class;
//	  __u16 op;
//	} __attribute__((packed));
type msgNotificationOp struct {
	MsgNotification
	Allow uint32
	Deny  uint32
	Pid   uint32
	Label uint32
	Class uint16
	Op    uint16
}

// MsgNotificationOp describes a prompt request.
//
// The actual information about the prompted object is not encoded here.
// The mediation class can be used to deduce the type message that was actually sent
// and decode.
type MsgNotificationOp struct {
	MsgNotification
	// Allow describes the permissions the process, attempting to perform some
	// an operation, already possessed. Use DecodeFilePermissions to decode it,
	// if the mediation class is AA_CLASS_FILE.
	Allow uint32
	// Deny describes the permissions the process, attempting to perform some
	// operation, currently lacks. Use DecodeFilePermissions to decode it, if
	// the mediation class is AA_CLASS_FILE.
	Deny uint32
	// Pid of the process triggering the notification.
	Pid uint32
	// Label is the apparmor label of the process triggering the notification.
	Label string
	// Class of the mediation operation.
	// Currently only AA_CLASS_FILE is implemented in the kernel.
	Class MediationClass
	// XXX: This is unused.
	Op uint16
}

// DecodeFilePermissions returns a pair of permissions describing the state of a
// process attempting to perform an operation.
func (msg *MsgNotificationOp) DecodeFilePermissions() (allow, deny FilePermission, err error) {
	if msg.Class != AA_CLASS_FILE {
		return 0, 0, fmt.Errorf("mediation class %s does not describe file permissions", msg.Class)
	}
	return FilePermission(msg.Allow), FilePermission(msg.Deny), nil
}

// UnmarshalBinary unmarshals the message from binary form.
func (msg *MsgNotificationOp) UnmarshalBinary(data []byte) error {
	const prefix = "cannot unmarshal apparmor operation notification message"

	// Unpack the base structure.
	if err := msg.MsgNotification.UnmarshalBinary(data); err != nil {
		return err
	}

	// Unpack fixed-size elements.
	buf := bytes.NewBuffer(data)
	var raw msgNotificationOp
	order := arch.Endian() // ioctl messages are native byte order, but others might not be
	if err := binary.Read(buf, order, &raw); err != nil {
		return fmt.Errorf("%s: cannot unpack: %s", prefix, err)
	}

	// Unpack variable length elements.
	unpacker := newStringUnpacker(data)
	label, err := unpacker.UnpackString(raw.Label)
	if err != nil {
		return fmt.Errorf("%s: cannot unpack label: %v", prefix, err)
	}

	// Put everything together.
	msg.Allow = raw.Allow
	msg.Deny = raw.Deny
	msg.Pid = raw.Pid
	msg.Label = label
	msg.Class = MediationClass(raw.Class)
	msg.Op = raw.Op

	return nil
}

// msgNotificationFile (TBD, document me).
//
//	struct apparmor_notif_file {
//	  struct apparmor_notif_op base;
//	  uid_t suid, ouid;
//	  __u32 name;         /* offset into data */
//	  __u8 data[];
//	} __attribute__((packed));
type msgNotificationFile struct {
	msgNotificationOp
	SUID uint32
	OUID uint32
	Name uint32
}

// MsgNotificationFile describes a prompt to a specific file.
type MsgNotificationFile struct {
	MsgNotificationOp
	SUID uint32
	OUID uint32
	// Name of the file being accessed.
	// XXX: is this path valid from the point of view of the accessing process
	// or the prompting process? The name is insufficient to correctly identify
	// the actual object being accessed in some cases.
	Name string
}

// UnmarshalBinary unmarshals the message from binary form.
func (msg *MsgNotificationFile) UnmarshalBinary(data []byte) error {
	const prefix = "cannot unmarshal apparmor file notification message"

	// Unpack the base structure.
	if err := msg.MsgNotificationOp.UnmarshalBinary(data); err != nil {
		return err
	}

	// Unpack fixed-size elements.
	buf := bytes.NewBuffer(data)
	var raw msgNotificationFile
	order := arch.Endian() // ioctl messages are native byte order, but others might not be
	if err := binary.Read(buf, order, &raw); err != nil {
		return fmt.Errorf("%s: cannot unpack: %s", prefix, err)
	}

	// Unpack variable length elements.
	unpacker := newStringUnpacker(data)
	name, err := unpacker.UnpackString(raw.Name)
	if err != nil {
		return fmt.Errorf("%s: cannot unpack file name: %v", prefix, err)
	}

	// Put everything together.
	msg.SUID = raw.SUID
	msg.OUID = raw.OUID
	msg.Name = name

	return nil
}
