package builtin

import (
	"bytes"

	"github.com/snapcore/snapd/interfaces"
)

var tpm12PermanentSlotAppArmor = []byte(`
# Description: for tcsd
# Usage: common

#include <abstractions/nameservice>

/dev/tpm0 rw,
`)

var tpm12ConnectedPlugAppArmor = []byte(`
# Description: for tpm*
# Usage: common

#include <abstractions/nameservice>
`)

var tpm12PermanentSlotSecComp = []byte(`
# Description: for tcsd
# Usage: common
accept
bind
listen
recvfrom
sendto
setsockopt
`)

var tpm12ConnectedPlugSecComp = []byte(`
# Description: for tpm*
# Usage: common
recvfrom
sendto
`)

type Tpm12Interface struct { }

func (iface *Tpm12Interface) Name() string {
	return "tpm12"
}

func (iface *Tpm12Interface) PermanentPlugSnippet(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityDBus, interfaces.SecurityAppArmor, interfaces.SecuritySecComp, interfaces.SecurityUDev:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

func (iface *Tpm12Interface) ConnectedPlugSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		old := []byte("###SLOT_SECURITY_TAGS###")
		new := slotAppLabelExpr(slot)
		snippet := bytes.Replace(tpm12ConnectedPlugAppArmor, old, new, -1)
		return snippet, nil
	case interfaces.SecuritySecComp:
		return tpm12ConnectedPlugSecComp, nil
	case interfaces.SecurityUDev, interfaces.SecurityDBus:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

func (iface *Tpm12Interface) PermanentSlotSnippet(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityAppArmor:
		return tpm12PermanentSlotAppArmor, nil
	case interfaces.SecuritySecComp:
		return tpm12PermanentSlotSecComp, nil
	case interfaces.SecurityDBus:
		return nil, nil
	case interfaces.SecurityUDev:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

func (iface *Tpm12Interface) ConnectedSlotSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	switch securitySystem {
	case interfaces.SecurityDBus, interfaces.SecurityAppArmor, interfaces.SecuritySecComp, interfaces.SecurityUDev:
		return nil, nil
	default:
		return nil, interfaces.ErrUnknownSecurity
	}
}

func (iface *Tpm12Interface) SanitizePlug(plug *interfaces.Plug) error {
	return nil
}

func (iface *Tpm12Interface) SanitizeSlot(slot *interfaces.Slot) error {
	return nil
}

func (iface *Tpm12Interface) AutoConnect() bool {
	return false
}

