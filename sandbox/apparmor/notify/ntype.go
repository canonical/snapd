package notify

import (
	"fmt"
)

// NotificationType denotes the type of response message.
type NotificationType uint16

const (
	APPARMOR_NOTIF_RESP NotificationType = iota
	APPARMOR_NOTIF_CANCEL
	APPARMOR_NOTIF_INTERRUPT
	APPARMOR_NOTIF_ALIVE
	APPARMOR_NOTIF_OP
)

// String returns readable representation of a notification type.
func (ntype NotificationType) String() string {
	switch ntype {
	case APPARMOR_NOTIF_RESP:
		return "APPARMOR_NOTIF_RESP"
	case APPARMOR_NOTIF_CANCEL:
		return "APPARMOR_NOTIF_CANCEL"
	case APPARMOR_NOTIF_INTERRUPT:
		return "APPARMOR_NOTIF_INTERRUPT"
	case APPARMOR_NOTIF_ALIVE:
		return "APPARMOR_NOTIF_ALIVE"
	case APPARMOR_NOTIF_OP:
		return "APPARMOR_NOTIF_OP"
	}
	return fmt.Sprintf("NotificationType(%d)", ntype)
}

// IsValid returns true if the notification type has a valid value.
func (ntype NotificationType) IsValid() bool {
	switch ntype {
	case APPARMOR_NOTIF_RESP, APPARMOR_NOTIF_CANCEL, APPARMOR_NOTIF_INTERRUPT, APPARMOR_NOTIF_ALIVE, APPARMOR_NOTIF_OP:
		return true
	default:
		return false
	}
}
