package apparmor

import (
	"fmt"
)

// NotificationType denotes the type of response message.
type NotificationType uint16

const (
	// Response corresponds to APPARMOR_NOTIF_RESP.
	Response NotificationType = iota
	// Cancel corresponds to APPARMOR_NOTIF_CANCEL.
	Cancel
	// Interrupt corresponds to APPARMOR_NOTIF_INTERUPT.
	Interrupt
	// Alive corresponds to APPARMOR_NOTIF_ALIVE.
	Alive
	// Operation corresponds to APPARMOR_NOTIF_OP.
	Operation
)

// String returns readable representation of a notification type.
func (ntype NotificationType) String() string {
	switch ntype {
	case Response:
		return "response"
	case Cancel:
		return "cancel"
	case Interrupt:
		return "interrupt"
	case Alive:
		return "alive"
	case Operation:
		return "operation"
	}
	return fmt.Sprintf("NotificationType(%d)", ntype)
}

// IsValid returns true if the notification type has a valid value.
func (ntype NotificationType) IsValid() bool {
	return ntype == Response || ntype == Cancel || ntype == Interrupt ||
		ntype == Alive || ntype == Operation
}
