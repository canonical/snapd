package raw

// Message fields are defined as raw sized integer types as the same type may be
// packed as 16 bit or 32 bit integer, to accommodate other fields in the
// structure.

// MsgHeader describes the header of all apparmor notification messages.
//
// struct apparmor_notif_common {
//   __u16 len;          /* actual len data */
//   __u16 version;      /* interface version */
// } __attribute__((packed));
type MsgHeader struct {
	Length  uint16
	Version uint16
}

// MsgNotificationFilter describes the get/set filter request/response.
//
// struct apparmor_notif_filter {
//   struct apparmor_notif_common base;
//   __u32 modeset;      /* which notification mode */
//   __u32 ns;           /* offset into data, relative to start of the structure */
//   __u32 filter;       /* offset into data, relative to start of the structure */
//   __u8 data[];
// } __attribute__((packed));
type MsgNotificationFilter struct {
	MsgHeader
	ModeSet uint32
	NS      uint32
	Filter  uint32
}

// MsgNotification describes the notification request.
//
// struct apparmor_notif {
//   struct apparmor_notif_common base;
//   __u16 ntype;        /* notify type */
//   __u8 signalled;
//   __u8 reserved;
//   __u64 id;           /* unique id, not globally unique*/
//   __s32 error;        /* error if unchanged */
// } __attribute__((packed));
type MsgNotification struct {
	MsgHeader
	NotificationType uint16
	Signalled        uint8
	Reserved         uint8
	ID               uint64
	Error            int32
}

// MsgNotificationUpdate (TBD, document me)
//
// struct apparmor_notif_update {
//   struct apparmor_notif base;
//   __u16 ttl;          /* max keep alives left */
// } __attribute__((packed));
type MsgNotificationUpdate struct {
	MsgNotification
	TTL uint16
}

// MsgNotificationResponse (TBD, document me).
//
// struct apparmor_notif_resp {
//   struct apparmor_notif base;
//   __s32 error;        /* error if unchanged */
//   __u32 allow;
//   __u32 deny;
// } __attribute__((packed));
type MsgNotificationResponse struct {
	MsgNotification
	Error int32
	Allow uint32
	Deny  uint32
}

// MsgNotificationOp (TBD, document me).
//
// struct apparmor_notif_op {
//   struct apparmor_notif base;
//   __u32 allow;
//   __u32 deny;
//   pid_t pid;          /* pid of task causing notification */
//   __u32 label;        /* offset into data */
//   __u16 class;
//   __u16 op;
// } __attribute__((packed));
type MsgNotificationOp struct {
	MsgNotification
	Allow uint32
	Deny  uint32
	Pid   uint32
	Label uint32
	Class uint16
	Op    uint16
}

// MsgNotificationFile (TBD, document me).
//
// struct apparmor_notif_file {
//   struct apparmor_notif_op base;
//   uid_t suid, ouid;
//   __u32 name;         /* offset into data */
//   __u8 data[];
// } __attribute__((packed));
type MsgNotificationFile struct {
	MsgNotificationOp
	SUID uint32
	OUID uint32
	Name uint32
}
