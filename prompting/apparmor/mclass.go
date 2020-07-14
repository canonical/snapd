package apparmor

// MediationClass is an enumeration of mediation ttypes in the apparmor policy.
type MediationClass uint16

const (
	// MediationClassFile corresponds to AA_CLASS_FILE.
	MediationClassFile MediationClass = 2
)
