package notify

import "fmt"

// MediationClass is an enumeration of mediation ttypes in the apparmor policy.
type MediationClass uint16

const (
	AA_CLASS_FILE MediationClass = 2
	AA_CLASS_DBUS MediationClass = 32
)

func (mcls MediationClass) String() string {
	switch mcls {
	case AA_CLASS_FILE:
		return "AA_CLASS_FILE"
	case AA_CLASS_DBUS:
		return "AA_CLASS_DBUS"
	default:
		return fmt.Sprintf("MediationClass(%#x)", uint16(mcls))
	}
}
