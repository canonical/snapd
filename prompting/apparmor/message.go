package apparmor

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"golang.org/x/xerrors"

	"github.com/snapcore/cerberus/apparmor/raw"
)

// overwrite implements io.Writer that writes over an existing buffer.
//
// It is used to perform in-place modifications of a larger memory buffer.
type overwrite struct {
	Buffer []byte
	Offset int
}

// Write overwrites the buffer at a given offest.
func (o *overwrite) Write(p []byte) (n int, err error) {
	if n := len(p); n+o.Offset < len(o.Buffer) {
		copy(o.Buffer[o.Offset:o.Offset+n], p)
		return n, nil
	}
	return 0, fmt.Errorf("insufficient space to write")
}

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
	order := binary.LittleEndian
	buf := bytes.NewBuffer(data)
	var raw raw.MsgHeader
	if err := binary.Read(buf, order, &raw); err != nil {
		return xerrors.Errorf("%s: %s", prefix, err)
	}

	if raw.Version != 2 {
		return xerrors.Errorf("%s: unsupported version: %d", prefix, raw.Version)
	}
	if int(raw.Length) != len(data) {
		return xerrors.Errorf("%s: length mismatch %d != %d",
			prefix, raw.Length, len(data))
	}

	// Put everything together.
	msg.Length = raw.Length
	msg.Version = raw.Version

	return nil
}

// RequestBuffer returns a new buffer for communication with the kernel.
// The buffer contains encoded information about its size and protocol version.
func RequestBuffer() []byte {
	buf := make([]byte, 0xFFFF)
	header := MsgHeader{Version: 2, Length: uint16(len(buf))}
	binary.Write(&overwrite{Buffer: buf}, binary.LittleEndian, &header)
	return buf
}

// MsgNotificationFilter represents the high-level get/set filter request/response.
//
// This structure corresponds to struct apparmor_notif_filter. It can be
// marshaled and unmarshaled to binary form and transmitted to the kernel.
type MsgNotificationFilter struct {
	MsgHeader
	// ModeSet is a bitmask. Specifying ModeSetUser allows to receive notification
	// messages in userspace.
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
		return err
	}

	// Unpack fixed-size elements.
	order := binary.LittleEndian
	buf := bytes.NewBuffer(data)
	var raw raw.MsgNotificationFilter
	if err := binary.Read(buf, order, &raw); err != nil {
		return xerrors.Errorf("%s: cannot unpack: %s", prefix, err)
	}

	// Unpack variable length elements.
	unpacker := StringUnpacker{Bytes: data}
	ns, err := unpacker.UnpackString(raw.NS)
	if err != nil {
		return xerrors.Errorf("%s: cannot unpack namespace: %v", prefix, err)
	}
	filter, err := unpacker.UnpackString(raw.Filter)
	if err != nil {
		return xerrors.Errorf("%s: cannot unpack filter: %v", prefix, err)
	}

	// Put everything together.
	msg.ModeSet = ModeSet(raw.ModeSet)
	msg.NameSpace = ns
	msg.Filter = filter

	return nil
}

// MarshalBinary marshals the message into binary form.
func (msg *MsgNotificationFilter) MarshalBinary() (data []byte, err error) {
	var raw raw.MsgNotificationFilter
	var packer StringPacker
	packer.BaseOffset = uint16(binary.Size(raw))
	raw.Length = packer.BaseOffset
	raw.Version = 2
	raw.ModeSet = uint32(msg.ModeSet)
	raw.NS = packer.PackString(msg.NameSpace)
	raw.Filter = packer.PackString(msg.Filter)
	raw.Length += uint16(packer.Buffer.Len())
	buf := bytes.NewBuffer(make([]byte, 0, int(packer.BaseOffset)+packer.Buffer.Len()))
	// FIXME: encoding should be native
	if err := binary.Write(buf, binary.LittleEndian, raw); err != nil {
		return nil, err
	}
	if _, err := buf.Write(packer.Buffer.Bytes()); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Validate returns an error if the mssage contains invalid data.
func (msg *MsgNotificationFilter) Validate() error {
	if !msg.ModeSet.IsValid() {
		return xerrors.Errorf("unsupported modeset: %d", msg.ModeSet)
	}
	return nil
}

// MsgNotification describes a kernel notification message.
type MsgNotification struct {
	MsgHeader
	// NotificationType describes the kind of notification message used.
	// Currently the kernel only sends Operation messages.
	NotificationType NotificationType
	// XXX: This is unused.
	Signalled uint8
	// XXX: This is unused.
	Reserved uint8
	// ID is an opaque kernel identifier of the notification message. It must be
	// repeated in the MsgNotificationResponse if one is sent back.
	ID uint64
	// XXX: This is unused and clashes with identical field in MsgNotificationOp
	// that the kernel actually sends.
	Error int32
}

// UnmarshalBinary unmarshals the message from binary form.
func (msg *MsgNotification) UnmarshalBinary(data []byte) error {
	const prefix = "cannot unmarshal apparmor notification message"

	// Unpack the base structure.
	if err := msg.MsgHeader.UnmarshalBinary(data); err != nil {
		return err
	}

	// Unpack fixed-size elements.
	order := binary.LittleEndian
	buf := bytes.NewBuffer(data)
	var raw raw.MsgNotification
	if err := binary.Read(buf, order, &raw); err != nil {
		return xerrors.Errorf("%s: cannot unpack: %s", prefix, err)
	}

	// Put everything together.
	ntype := NotificationType(raw.NotificationType)
	msg.NotificationType = ntype
	msg.Signalled = raw.Signalled
	msg.Reserved = raw.Reserved
	msg.ID = raw.ID
	msg.Error = raw.Error

	return nil
}

// MarshalBinary marshals the message into binary form.
func (msg *MsgNotification) MarshalBinary() ([]byte, error) {
	var raw raw.MsgNotification
	raw.Version = 2
	raw.Length = uint16(binary.Size(raw))
	raw.NotificationType = uint16(msg.NotificationType)
	raw.Signalled = msg.Signalled
	raw.Reserved = msg.Reserved
	raw.ID = msg.ID
	raw.Error = msg.Error
	buf := bytes.NewBuffer(make([]byte, 0, raw.Length))
	// FIXME: encoding should be native
	if err := binary.Write(buf, binary.LittleEndian, raw); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Validate returns an error if the mssage contains invalid data.
func (msg *MsgNotification) Validate() error {
	if !msg.NotificationType.IsValid() {
		return xerrors.Errorf("unsupported notification type: %d", msg.NotificationType)
	}
	return nil
}

// MsgNotificationOp describes a prompt request.
//
// The actual information about the prompted object is not encoded here.
// The mediation class can be used to deduce the message that was actually sent
// and decode.
type MsgNotificationOp struct {
	MsgNotification
	// XXX: Those are used but the meaning is unknown.
	Allow uint32
	Deny  uint32
	// Pid of the process triggering the notification.
	Pid uint32
	// Apparmor label of the process triggering the notification.
	Label string
	// Class of the mediation operation.
	// Currently only MediationClassFile is implemented in the kernel.
	Class MediationClass
	// XXX: This is unused.
	Op uint16
}

// UnmarshalBinary unmarshals the message from binary form.
func (msg *MsgNotificationOp) UnmarshalBinary(data []byte) error {
	const prefix = "cannot unmarshal apparmor operation notification message"

	// Unpack the base structure.
	if err := msg.MsgNotification.UnmarshalBinary(data); err != nil {
		return err
	}

	// Unpack fixed-size elements.
	order := binary.LittleEndian
	buf := bytes.NewBuffer(data)
	var raw raw.MsgNotificationOp
	if err := binary.Read(buf, order, &raw); err != nil {
		return xerrors.Errorf("%s: cannot unpack: %s", prefix, err)
	}

	// Unpack variable length elements.
	unpacker := StringUnpacker{Bytes: data}
	label, err := unpacker.UnpackString(raw.Label)
	if err != nil {
		return xerrors.Errorf("%s: cannot unpack label: %v", prefix, err)
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
	order := binary.LittleEndian
	buf := bytes.NewBuffer(data)
	var raw raw.MsgNotificationFile
	if err := binary.Read(buf, order, &raw); err != nil {
		return xerrors.Errorf("%s: cannot unpack: %s", prefix, err)
	}

	// Unpack variable length elements.
	unpacker := StringUnpacker{Bytes: data}
	name, err := unpacker.UnpackString(raw.Name)
	if err != nil {
		return xerrors.Errorf("%s: cannot unpack file name: %v", prefix, err)
	}

	// Put everything together.
	msg.SUID = raw.SUID
	msg.OUID = raw.OUID
	msg.Name = name

	return nil
}

// MsgNotificationResponse describes a response to a MsgNotification.
//
// The field are not documented yet.
type MsgNotificationResponse struct {
	MsgNotification
	Error int32
	Allow uint32
	Deny  uint32
}

// ResponseForRequest returns a response message for a given request.
func ResponseForRequest(req *MsgNotification) MsgNotificationResponse {
	return MsgNotificationResponse{
		MsgNotification: MsgNotification{
			NotificationType: Response,
			// XXX: should Signalled be copied?
			Signalled: req.Signalled,
			// XXX: should Reserved be copied?
			Reserved: req.Reserved,
			ID:       req.ID,
			// XXX: should Error be copied?
			Error: req.Error,
		},
	}
}

// MarshalBinary marshals the message into binary form.
func (msg *MsgNotificationResponse) MarshalBinary() ([]byte, error) {
	var raw raw.MsgNotificationResponse
	raw.Version = 2
	raw.Length = uint16(binary.Size(raw))
	raw.NotificationType = uint16(msg.NotificationType)
	raw.Signalled = msg.Signalled
	raw.Reserved = msg.Reserved
	raw.ID = msg.ID
	// XXX: There are two distinct fields called Error, why?
	raw.MsgNotification.Error = msg.MsgNotification.Error
	raw.Error = msg.Error
	raw.Allow = msg.Allow
	raw.Deny = msg.Deny
	buf := bytes.NewBuffer(make([]byte, 0, raw.Length))
	// FIXME: encoding should be native
	if err := binary.Write(buf, binary.LittleEndian, raw); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
