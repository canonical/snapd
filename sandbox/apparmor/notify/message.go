package notify

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/snapcore/snapd/arch"
)

var ErrVersionUnset = errors.New("cannot marshal message without protocol version")

// MsgNotificationGeneric defines the methods which the message types for each
// mediation class must provide.
//
// Many of these methods, including ID, PID, ProcessLabel, and MediationClass,
// are implemented on MsgNotificationOp, so any struct which embeds a
// MsgNotificationOp needs only to implement the remaining methods.
type MsgNotificationGeneric interface {
	// ID returns the unique ID of the notification message.
	ID() uint64
	// PID returns the PID of the process triggering the notification.
	PID() int32
	// ProcessLabel returns the AppArmor label of the process triggering the notification.
	ProcessLabel() string
	// MediationClass returns the mediation class of the message.
	MediationClass() MediationClass

	// AllowedDeniedPermissions returns the AppArmor permission masks which
	// were originally allowed and originally denied by AppArmor rules.
	AllowedDeniedPermissions() (allowed, denied AppArmorPermission, err error)
	// SubjectUID returns the UID of the user triggering the notification.
	SubjectUID() uint32
	// Name is the identifier of the resource to which access is requested.
	// For mediation class file, Name is the filepath of the requested file.
	Name() string
}

// Message fields are defined as raw sized integer types as the same type may be
// packed as 16 bit or 32 bit integer, to accommodate other fields in the
// structure.

// MsgHeader is the header of all apparmor notification messages.
//
// The header encodes the size of the entire message, including variable-size
// components and the version of the notification protocol.
//
// This structure corresponds to the kernel type struct apparmor_notif_common
// described below.
//
//	/* base notification struct embedded as head of notifications to userspace */
//	struct apparmor_notif_common {
//		__u16 len;			/* actual len data */
//		__u16 version;			/* interface version */
//	} __attribute__((packed));
type MsgHeader struct {
	// Length is the length of the entire message, including any more
	// specialized messages appended after the header and any variable-length
	// objects referenced by any such message.
	Length uint16
	// Version is the version of the communication protocol.
	// Currently version 3 is implemented in the kernel, but other versions may
	// be used in the future.
	Version ProtocolVersion
}

const sizeofMsgHeader = 4

// UnmarshalBinary unmarshals the header from binary form.
func (msg *MsgHeader) UnmarshalBinary(data []byte) error {
	const prefix = "cannot unmarshal apparmor message header"
	if err := msg.unmarshalBinaryImpl(data); err != nil {
		return fmt.Errorf("%s: %s", prefix, err)
	}
	if !versionKnown(msg.Version) {
		return fmt.Errorf("%s: unsupported version: %d", prefix, msg.Version)
		// XXX: caller should also check that the version matches that of the
		// listener which is receiving this message.
	}
	if int(msg.Length) != len(data) {
		return fmt.Errorf("%s: length mismatch %d != %d",
			prefix, msg.Length, len(data))
	}
	return nil
}

func (msg *MsgHeader) unmarshalBinaryImpl(data []byte) error {
	// Unpack fixed-size elements.
	order := arch.Endian() // ioctl messages are native byte order, verify endianness if using for other messages
	buf := bytes.NewReader(data)
	if err := binary.Read(buf, order, msg); err != nil {
		return err
	}
	if msg.Length < sizeofMsgHeader {
		return fmt.Errorf("invalid length (must be >= %d): %d",
			sizeofMsgHeader, msg.Length)
	}
	return nil
}

// MsgLength returns the length of the first message in the given data buffer,
// assuming that it begins with a MsgHeader. If it does not, returns an error.
func MsgLength(data []byte) (int, error) {
	const prefix = "cannot parse message header"
	var msg MsgHeader
	if err := msg.unmarshalBinaryImpl(data); err != nil {
		return -1, fmt.Errorf("%s: %v", prefix, err)
	}
	return int(msg.Length), nil
}

// ExtractFirstMsg splits the given data buffer after the first message,
// assuming that it begins with a MsgHeader. If it is too short to be a
// MsgHeader, or if the encoded length exceeds the remaining data length,
// returns an error.
func ExtractFirstMsg(data []byte) (first []byte, rest []byte, err error) {
	const prefix = "cannot extract first message"
	length, err := MsgLength(data)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %v", prefix, err)
	}
	if len(data) < length {
		return nil, nil, fmt.Errorf("%s: length in header exceeds data length: %d > %d",
			prefix, length, len(data))
	}
	return data[:length], data[length:], nil
}

// msgNotificationFilterKernel describes the configuration of kernel-side message filtering.
//
// This structure corresponds to the kernel type struct apparmor_notif_filter
// described below. This type is only used for message marshaling and
// unmarshaling. Application code should use MsgNotificationFilter instead.
//
//	struct apparmor_notif_filter {
//		struct apparmor_notif_common base;
//		__u32 modeset;		/* which notification mode */
//		__u32 ns;			/* offset into data, relative to start of the structure */
//		__u32 filter;		/* offset into data, relative to start of the structure */
//		__u8 data[];
//	} __attribute__((packed));
type msgNotificationFilterKernel struct {
	MsgHeader
	ModeSet uint32
	NS      uint32
	Filter  uint32
}

// MsgNotificationFilter describes the configuration of kernel-side message filtering.
//
// This structure can be marshaled and unmarshaled to binary form and
// transmitted to the kernel using Ioctl along with APPARMOR_NOTIF_GET_FILTER
// and APPARMOR_NOTIF_SET_FILTER.
type MsgNotificationFilter struct {
	MsgHeader
	// ModeSet is a bitmask. Specifying APPARMOR_MODESET_USER allows to
	// receive notification messages in userspace.
	ModeSet ModeSet
	// NameSpace is only used if the namespace does not match the monitoring task.
	NameSpace string
	// Filter is a binary state machine in a specific format.
	Filter []byte
}

// UnmarshalBinary unmarshals the message from binary form.
func (msg *MsgNotificationFilter) UnmarshalBinary(data []byte) error {
	const prefix = "cannot unmarshal apparmor notification filter message"

	// Unpack the base structure.
	if err := msg.MsgHeader.UnmarshalBinary(data); err != nil {
		return err
	}

	// Unpack fixed-size elements.
	buf := bytes.NewReader(data)
	var raw msgNotificationFilterKernel
	order := arch.Endian() // ioctl messages are native byte order, verify endianness if using for other messages
	if err := binary.Read(buf, order, &raw); err != nil {
		return fmt.Errorf("%s: cannot unpack: %s", prefix, err)
	}

	// Unpack variable length elements.
	unpacker := newStringUnpacker(data)
	ns, err := unpacker.unpackString(raw.NS)
	if err != nil {
		return fmt.Errorf("%s: cannot unpack namespace: %v", prefix, err)
	}

	// Put everything together.
	msg.ModeSet = ModeSet(raw.ModeSet)
	msg.NameSpace = ns
	if raw.Filter != 0 {
		msg.Filter = data[raw.Filter:]
	}

	return nil
}

// MarshalBinary marshals the message into binary form.
func (msg *MsgNotificationFilter) MarshalBinary() (data []byte, err error) {
	if msg.Version == 0 {
		return nil, ErrVersionUnset
	}
	var raw msgNotificationFilterKernel
	packer := newStringPacker(raw)
	raw.Version = msg.Version
	raw.ModeSet = uint32(msg.ModeSet)
	raw.NS = packer.packString(msg.NameSpace)
	filter := msg.Filter
	if filter != nil {
		raw.Filter = uint32(packer.totalLen()) // filter []byte will follow the other packed strings
	} else {
		raw.Filter = 0 // use 0 to indicate that that filter is not included
	}
	raw.Length = packer.totalLen() + uint16(len(filter))
	msgBuf := bytes.NewBuffer(make([]byte, 0, raw.Length))
	order := arch.Endian() // ioctl messages are native byte order, verify endianness if using for other messages
	if err := binary.Write(msgBuf, order, raw); err != nil {
		return nil, err
	}
	if _, err := msgBuf.Write(packer.bytes()); err != nil {
		return nil, err
	}
	if filter != nil {
		if _, err := msgBuf.Write(filter); err != nil {
			return nil, err
		}
	}
	return msgBuf.Bytes(), nil
}

// Validate returns an error if the message contains invalid data.
func (msg *MsgNotificationFilter) Validate() error {
	if !msg.ModeSet.IsValid() {
		return fmt.Errorf("unsupported modeset: %d", msg.ModeSet)
	}
	return nil
}

// Flags for MsgNotification.
const (
	// URESPONSE_NO_CACHE tells the kernel not to cache the response.
	URESPONSE_NO_CACHE = iota
	// Other flags which are not currently needed by snapd:
	// URESPONSE_LOOKUP
	// URESPONSE_PROFILE
	// URESPONSE_TAILGLOB
)

// MsgNotification describes a kernel notification message.
//
// This structure corresponds to the kernel type struct apparmor_notif
// described below.
//
//	struct apparmor_notif {
//		struct apparmor_notif_common base;
//		__u16 ntype;			/* notify type */
//		__u8 signalled;
//		__u8 flags;
//		__u64 id;			/* unique id, not globally unique*/
//		__s32 error;			/* error if unchanged */
//	} __attribute__((packed));
type MsgNotification struct {
	MsgHeader
	// NotificationType describes the kind of notification message used.
	// Currently the kernel only sends APPARMOR_NOTIF_OP messages.
	// Responses to the kernel should be APPARMOR_NOTIF_RESP messages.
	NotificationType NotificationType
	// Signaled is unused, but previously used for interrupt information.
	Signalled uint8
	// Set NoCache to URESPONSE_NO_CACHE to NOT cache.
	NoCache uint8
	// KernelNotificationID is an opaque kernel identifier of the notification
	// message. It must be repeated in the MsgNotificationResponse if one is
	// sent back.
	KernelNotificationID uint64
	// Error is the error the kernel will return to the application if the
	// notification is denied.  In version 3, this is ignored in responses.
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
	buf := bytes.NewReader(data)
	order := arch.Endian() // ioctl messages are native byte order, verify endianness if using for other messages
	if err := binary.Read(buf, order, msg); err != nil {
		return fmt.Errorf("%s: cannot unpack: %s", prefix, err)
	}

	return nil
}

// MarshalBinary marshals the message into binary form.
func (msg *MsgNotification) MarshalBinary() ([]byte, error) {
	if msg.Version == 0 {
		return nil, ErrVersionUnset
	}
	msg.Length = uint16(binary.Size(*msg))
	buf := bytes.NewBuffer(make([]byte, 0, msg.Length))
	order := arch.Endian() // ioctl messages are native byte order, verify endianness if using for other messages
	if err := binary.Write(buf, order, msg); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Validate returns an error if the message contains invalid data.
func (msg *MsgNotification) Validate() error {
	if !msg.NotificationType.IsValid() {
		return fmt.Errorf("unsupported notification type: %d", msg.NotificationType)
	}
	return nil
}

// MsgNotificationResponse describes a response to a MsgNotification.
//
// This structure corresponds to the kernel type struct apparmor_notif_resp_perm
// described below. The kernel also has other response types we don't yet use.
//
//	/* userspace response to notification that expects a response */
//	struct apparmor_notif_resp_perm {
//		struct apparmor_notif base;
//		__s32 error;			/* error if unchanged */
//		__u32 allow;
//		__u32 deny;
//	} __attribute__((packed));
type MsgNotificationResponse struct {
	MsgNotification
	// In version 3, both the Error in MsgNotificationResponse and the
	// embedded MsgNotification are ignored in responses.
	Error int32
	// Allow encodes the allowed operation mask.
	Allow uint32
	// Deny encodes the denied operation mask.
	Deny uint32
	// Allow|Deny must cover at least the allow|deny from the original request,
	// or the notification will result in a denial.
}

// BuildResponse returns a MsgNotificationResponse with the given information.
func BuildResponse(version ProtocolVersion, id uint64, initiallyAllowed, requested, explicitlyAllowed AppArmorPermission) *MsgNotificationResponse {
	aaDenyMask := requested.AsAppArmorOpMask()
	// If permission was originally both allowed and denied in the message,
	// treat it as initially denied.
	aaAllowMask := initiallyAllowed.AsAppArmorOpMask() &^ aaDenyMask

	userAllowMask := uint32(0)
	if explicitlyAllowed != nil {
		userAllowMask = explicitlyAllowed.AsAppArmorOpMask()
	}

	// Allow permissions which AppArmor initially allowed, along with those
	// which were initially denied but the user then explicitly allowed.
	finalAllow := aaAllowMask | (userAllowMask & aaDenyMask)
	// Deny permissions which were initially denied and not explicitly allowed
	// by the user.
	finalDeny := aaDenyMask &^ userAllowMask

	// Any permissions which are omitted from both the allow and deny fields
	// will be default denied by the kernel.

	return &MsgNotificationResponse{
		MsgNotification: MsgNotification{
			MsgHeader: MsgHeader{
				Version: version,
			},
			NotificationType:     APPARMOR_NOTIF_RESP,
			NoCache:              1,
			KernelNotificationID: id,
			Error:                0, // ignored in response ?
		},
		Error: 0, // ignored in response ?
		Allow: finalAllow,
		Deny:  finalDeny,
	}
}

// MarshalBinary marshals the message into binary form.
func (msg *MsgNotificationResponse) MarshalBinary() ([]byte, error) {
	if msg.Version == 0 {
		return nil, ErrVersionUnset
	}
	msg.Length = uint16(binary.Size(*msg))
	buf := bytes.NewBuffer(make([]byte, 0, msg.Length))
	order := arch.Endian() // ioctl messages are native byte order, verify endianness if using for other messages
	if err := binary.Write(buf, order, msg); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// msgNotificationOpKernel
//
//	struct apparmor_notif_op {
//		struct apparmor_notif base;
//		__u32 allow;
//		__u32 deny;
//		pid_t pid;			/* pid of task causing notification */
//		__u32 label;		/* offset into data, relative to start of the structure */
//		__u16 class;
//		__u16 op;
//	} __attribute__((packed));
type msgNotificationOpKernel struct {
	MsgNotification
	Allow uint32
	Deny  uint32
	Pid   int32
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
	Pid int32
	// Label is the apparmor label of the process triggering the notification.
	Label string
	// Class of the mediation operation.
	// Currently only AA_CLASS_FILE is implemented in the kernel.
	Class MediationClass
	// Op provides supplemental information about the operation which caused
	// the notification. It may be set for notifications, but is ignored in
	// responses. At the moment, just used for debugging, and can be ignored.
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
	buf := bytes.NewReader(data)
	var raw msgNotificationOpKernel
	order := arch.Endian() // ioctl messages are native byte order, verify endianness if using for other messages
	if err := binary.Read(buf, order, &raw); err != nil {
		return fmt.Errorf("%s: cannot unpack: %s", prefix, err)
	}

	// Unpack variable length elements.
	unpacker := newStringUnpacker(data)
	label, err := unpacker.unpackString(raw.Label)
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

// tagsetHeader describes the configuration of a kernel-side tagset header.
//
//	struct apparmor_tags_header_v5 {
//		u32 mask;
//		u32 count;
//		u32 tagset;		/* offset into data, relative to the start of the message structure */
//	};
type tagsetHeader struct {
	PermissionMask uint32
	TagCount       uint32
	TagOffset      uint32
}

func (msg *MsgNotificationOp) ID() uint64 {
	return msg.KernelNotificationID
}

func (msg *MsgNotificationOp) PID() int32 {
	return msg.Pid
}

func (msg *MsgNotificationOp) ProcessLabel() string {
	return msg.Label
}

func (msg *MsgNotificationOp) MediationClass() MediationClass {
	return msg.Class
}

// msgNotificationFileKernelBase (protocol version <5)
//
//	struct apparmor_notif_file {
//		struct apparmor_notif_op base;
//		uid_t subj_uid, obj_uid;
//		__u32 name;		/* offset into data, relative to start of the structure */
//		__u8 data[];	/* data section contains padding and data for label and name */
//	} __attribute__((packed));
type msgNotificationFileKernelBase struct {
	msgNotificationOpKernel
	SUID uint32
	OUID uint32
	Name uint32
}

// msgNotificationFileKernelWithTags (protocol version >=5)
//
//	struct apparmor_notif_file_v5 {
//		struct apparmor_notif_op base;
//		uid_t subj_uid, obj_uid;
//		__u32 name;			/* offset into data, relative to the start of the structure */
//		__u32 tags;			/* offset into data of first tagset header, relative to start of the structure */
//		__u16 tags_count;
//		__u8 data[];
//	} __attribute__((packed));
type msgNotificationFileKernelWithTags struct {
	msgNotificationFileKernelBase
	// Tags and TagsetsCount require protocol version 5 or higher
	Tags         uint32
	TagsetsCount uint16
}

// MsgNotificationFile describes a prompt to a specific file.
type MsgNotificationFile struct {
	MsgNotificationOp
	// The UID of the user triggering the notification.
	SUID uint32
	// The UID of the owner of the file being accessed.
	OUID uint32
	// Filename of the file being accessed.
	// This is the path from the point of view of the process being mediated.
	// In the future, this should be mapped to the point of view of snapd, but
	// this is not always possible yet.
	Filename string
	// Tagsets maps from permission mask to the ordered list of tags associated
	// with those permissions. Tagsets requires protocol version 5 or greater.
	Tagsets map[AppArmorPermission][]string
}

// UnmarshalBinary unmarshals the message from binary form.
func (msg *MsgNotificationFile) UnmarshalBinary(data []byte) error {
	const prefix = "cannot unmarshal apparmor file notification message"

	// Unpack the base msgNotificationOp.
	if err := msg.MsgNotificationOp.UnmarshalBinary(data); err != nil {
		return err
	}

	// Unpack the base msgNotificationFile.
	if err := msg.unmarshalBase(data); err != nil {
		return fmt.Errorf("%v: %w", prefix, err)
	}

	// If protocol version supports tagging, unpack tags.
	if msg.Version >= 5 {
		if err := msg.unmarshalTags(data); err != nil {
			return fmt.Errorf("%v: %w", prefix, err)
		}
	}

	return nil
}

func (msg *MsgNotificationFile) unmarshalBase(data []byte) error {
	// Unpack fixed-size elements.
	buf := bytes.NewReader(data)
	var raw msgNotificationFileKernelBase
	order := arch.Endian() // ioctl messages are native byte order, verify endianness if using for other messages
	if err := binary.Read(buf, order, &raw); err != nil {
		return fmt.Errorf("cannot unpack: %v", err)
	}

	// Unpack variable length elements.
	unpacker := newStringUnpacker(data)
	name, err := unpacker.unpackString(raw.Name)
	if err != nil {
		return fmt.Errorf("cannot unpack file name: %v", err)
	}

	// Put everything together.
	msg.SUID = raw.SUID
	msg.OUID = raw.OUID
	msg.Filename = name

	return nil
}

func (msg *MsgNotificationFile) unmarshalTags(data []byte) error {
	// Unpack fixed-size elements to get tag metadata.
	buf := bytes.NewReader(data)
	var raw msgNotificationFileKernelWithTags
	order := arch.Endian() // ioctl messages are native byte order, verify endianness if using for other messages
	if err := binary.Read(buf, order, &raw); err != nil {
		return fmt.Errorf("cannot unpack tagset metadata: %v", err)
	}

	if raw.Tags == 0 || raw.TagsetsCount == 0 {
		return nil
	}

	// Unpack each tagset header and its associated tags.
	tagsets := make(map[AppArmorPermission][]string, raw.TagsetsCount)
	hdrBuf := bytes.NewReader(data[raw.Tags:])
	unpacker := newStringUnpacker(data)
	for i := uint16(0); i < raw.TagsetsCount; i++ {
		var header tagsetHeader
		if err := binary.Read(hdrBuf, order, &header); err != nil {
			return fmt.Errorf("cannot unpack tagset header: %v", err)
		}
		tags, err := unpacker.unpackStrings(header.TagOffset, header.TagCount)
		if err != nil {
			return fmt.Errorf("cannot unpack tags for header %+v: %v", header, err)
		}
		perm := FilePermission(header.PermissionMask)
		tagsets[perm] = tags
	}

	msg.Tagsets = tagsets

	return nil
}

// It should not be necessary to marshal MsgNotificationFile structs outside of
// test code.
func (msg *MsgNotificationFile) MarshalBinary() ([]byte, error) {
	if msg.Version == 0 {
		return nil, ErrVersionUnset
	}
	var raw msgNotificationFileKernelWithTags // prepare the superset of the msgNotificationFileKernel
	var ptr any
	if msg.Version < 5 {
		ptr = &raw.msgNotificationFileKernelBase
	} else {
		ptr = &raw
	}
	packer := newStringPacker(ptr)
	raw.Version = msg.Version
	raw.NotificationType = msg.NotificationType
	raw.Signalled = msg.Signalled
	raw.NoCache = msg.NoCache
	raw.KernelNotificationID = msg.KernelNotificationID
	raw.Error = msg.Error
	raw.Allow = msg.Allow
	raw.Deny = msg.Deny
	raw.Pid = msg.Pid
	raw.Label = packer.packString(msg.Label)
	raw.Class = uint16(msg.Class)
	raw.Op = msg.Op
	raw.SUID = msg.SUID
	raw.OUID = msg.OUID
	raw.Name = packer.packString(msg.Filename)

	if msg.Version >= 5 {
		raw.Tags = packer.packTagsets(msg.Tagsets)
		raw.TagsetsCount = uint16(len(msg.Tagsets))
	}

	raw.Length = packer.totalLen()
	msgBuf := bytes.NewBuffer(make([]byte, 0, raw.Length))
	order := arch.Endian() // ioctl messages are native byte order, verify endianness if using for other messages
	if err := binary.Write(msgBuf, order, ptr); err != nil {
		return nil, err
	}
	if _, err := msgBuf.Write(packer.bytes()); err != nil {
		return nil, err
	}
	return msgBuf.Bytes(), nil
}

func (msg *MsgNotificationFile) AllowedDeniedPermissions() (allowed, denied AppArmorPermission, err error) {
	return msg.DecodeFilePermissions()
}

func (msg *MsgNotificationFile) SubjectUID() uint32 {
	return msg.SUID
}

func (msg *MsgNotificationFile) Name() string {
	return msg.Filename
}
