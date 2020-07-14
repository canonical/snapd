package apparmor

import (
	"fmt"
	"strings"
)

// ModeSet is a bit mask of apparmor "modes".
type ModeSet uint32

const (
	// ModeSetAudit correspond to APPARMOR_MODESET_AUDIT
	ModeSetAudit ModeSet = 1 << iota
	// ModeSetAllowed corresponds to APPARMOR_MODESET_ALLOWED
	ModeSetAllowed
	// ModeSetEnforce corresponds to APPARMOR_MODESET_ENFORCE
	ModeSetEnforce
	// ModeSetHint corresponds to APPARMOR_MODESET_HINT
	ModeSetHint
	// ModeSetStatus corresponds to APPARMOR_MODESET_STATUS
	ModeSetStatus
	// ModeSetError corresponds to APPARMOR_MODESET_ERROR
	ModeSetError
	// ModeSetKill corresponds to APPARMOR_MODESET_KILL
	ModeSetKill
	// ModeSetUser corresponds to to APPARMOR_MODESET_USER
	ModeSetUser
)

const modeSetMask = ModeSetAudit | ModeSetAllowed | ModeSetEnforce | ModeSetHint | ModeSetStatus | ModeSetError | ModeSetKill | ModeSetUser

// String returns readable representation of the mode set value.
func (m ModeSet) String() string {
	frags := make([]string, 0, 8)
	if m&ModeSetAudit != 0 {
		frags = append(frags, "audit")
	}
	if m&ModeSetAllowed != 0 {
		frags = append(frags, "allowed")
	}
	if m&ModeSetEnforce != 0 {
		frags = append(frags, "enforce")
	}
	if m&ModeSetHint != 0 {
		frags = append(frags, "hint")
	}
	if m&ModeSetStatus != 0 {
		frags = append(frags, "status")
	}
	if m&ModeSetError != 0 {
		frags = append(frags, "error")
	}
	if m&ModeSetKill != 0 {
		frags = append(frags, "kill")
	}
	if m&ModeSetUser != 0 {
		frags = append(frags, "user")
	}
	if residue := m &^ modeSetMask; residue != 0 {
		frags = append(frags, fmt.Sprintf("%#x", uint(residue)))
	}
	return strings.Join(frags, "|")
}

// IsValid returns true if the given mode set contains only valid bits set.
func (m ModeSet) IsValid() bool {
	return m & ^modeSetMask == 0
}
