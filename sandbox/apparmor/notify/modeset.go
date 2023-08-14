package notify

import (
	"fmt"
	"strings"
)

// ModeSet is a bit mask of apparmor "modes".
type ModeSet uint32

const (
	APPARMOR_MODESET_AUDIT ModeSet = 1 << iota
	APPARMOR_MODESET_ALLOWED
	APPARMOR_MODESET_ENFORCE
	APPARMOR_MODESET_HINT
	APPARMOR_MODESET_STATUS
	APPARMOR_MODESET_ERROR
	APPARMOR_MODESET_KILL
	APPARMOR_MODESET_USER
)

const modeSetMask = APPARMOR_MODESET_AUDIT | APPARMOR_MODESET_ALLOWED | APPARMOR_MODESET_ENFORCE | APPARMOR_MODESET_HINT | APPARMOR_MODESET_STATUS | APPARMOR_MODESET_ERROR | APPARMOR_MODESET_KILL | APPARMOR_MODESET_USER

// String returns readable representation of the mode set value.
func (m ModeSet) String() string {
	frags := make([]string, 0, 9)
	if m&APPARMOR_MODESET_AUDIT != 0 {
		frags = append(frags, "audit")
	}
	if m&APPARMOR_MODESET_ALLOWED != 0 {
		frags = append(frags, "allowed")
	}
	if m&APPARMOR_MODESET_ENFORCE != 0 {
		frags = append(frags, "enforce")
	}
	if m&APPARMOR_MODESET_HINT != 0 {
		frags = append(frags, "hint")
	}
	if m&APPARMOR_MODESET_STATUS != 0 {
		frags = append(frags, "status")
	}
	if m&APPARMOR_MODESET_ERROR != 0 {
		frags = append(frags, "error")
	}
	if m&APPARMOR_MODESET_KILL != 0 {
		frags = append(frags, "kill")
	}
	if m&APPARMOR_MODESET_USER != 0 {
		frags = append(frags, "user")
	}
	if unaccounted := m &^ modeSetMask; unaccounted != 0 {
		frags = append(frags, fmt.Sprintf("%#x", uint(unaccounted)))
	}
	if len(frags) == 0 {
		return "none"
	}
	return strings.Join(frags, "|")
}

// IsValid returns true if the given mode set contains only known bits set.
func (m ModeSet) IsValid() bool {
	return m & ^modeSetMask == 0
}
