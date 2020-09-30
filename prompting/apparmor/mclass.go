package apparmor

import "fmt"

// MediationClass is an enumeration of mediation ttypes in the apparmor policy.
type MediationClass uint16

const (
	// MediationClassFile corresponds to AA_CLASS_FILE.
	MediationClassFile MediationClass = 2
	// MediationClassDBus corresponds to AA_CLASS_DBUS.
	MediationClassDBus MediationClass = 32
)

func (mcls MediationClass) String() string {
	switch mcls {
	case MediationClassFile:
		return "file"
	case MediationClassDBus:
		return "D-Bus"
	default:
		return fmt.Sprintf("MediationClass(%#x)", uint16(mcls))
	}
}
