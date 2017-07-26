// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package builtin

import (
	"fmt"
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/udev"
)

const broadcomAsicControlSummary = `allows using the broadcom-asic kernel module`

const broadcomAsicControlDescription = `
The broadcom-asic-control interfaces allows connected plugs to read and sometimes
write files required to use the broadcom asic kernel module.`

const broadcomAsicControlBaseDeclarationSlots = `
  broadcom-asic-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`
const broadcomAsicControlConnectedPlugAppArmor = `
# Description: Allow access to broadcom asic kernel module.

/sys/module/linux_kernel_bde/initstate r,
/sys/module/linux_user_bde/initstate r,
/sys/module/linux_kernel_bde/holders/ r,
/sys/module/linux_user_bde/holders/ r,
/sys/module/linux_user_bde/holders/** r,
/sys/module/linux_user_bde/refcnt r,
/sys/module/linux_bcm_knet/initstate r,
/sys/module/linux_bcm_knet/holders/ r,
/sys/module/linux_bcm_knet/refcnt r,
/dev/linux-user-bde rw,
/dev/linux-kernel-bde rw,
/dev/linux-bcm-knet wr,
`

const broadcomAsicControlConnectedPlugUDev = `
KERNEL=="linux-user-bde", TAG+="###SLOT_SECURITY_TAGS###"
KERNEL=="linux-kernel-bde", TAG+="###SLOT_SECURITY_TAGS###"
KERNEL=="linux-bcm-knet", TAG+="###SLOT_SECURITY_TAGS###"
`

// The type for broadcom-asic-control interface
type broadcomAsicControlInterface struct{}

// Getter for the name of the broadcom-asic-control interface
func (iface *broadcomAsicControlInterface) Name() string {
	return "broadcom-asic-control"
}

func (iface *broadcomAsicControlInterface) MetaData() interfaces.MetaData {
	return interfaces.MetaData{
		Summary:              broadcomAsicControlSummary,
		ImplicitOnCore:       true,
		ImplicitOnClassic:    true,
		BaseDeclarationSlots: broadcomAsicControlBaseDeclarationSlots,
	}
}

func (iface *broadcomAsicControlInterface) String() string {
	return iface.Name()
}

// Check validity of the defined slot
func (iface *broadcomAsicControlInterface) SanitizeSlot(slot *interfaces.Slot) error {
	// Does it have right type?
	if iface.Name() != slot.Interface {
		panic(fmt.Sprintf("slot is not of interface %q", iface))
	}

	// Creation of the slot of this type is allowed only by a gadget or os snap
	if !(slot.Snap.Type == "os") {
		return fmt.Errorf("%s slots only allowed on core snap", iface.Name())
	}
	return nil
}

// Checks and possibly modifies a plug
func (iface *broadcomAsicControlInterface) SanitizePlug(plug *interfaces.Plug) error {
	if iface.Name() != plug.Interface {
		panic(fmt.Sprintf("plug is not of interface %q", iface))
	}
	// Currently nothing is checked on the plug side
	return nil
}

func (iface *broadcomAsicControlInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error {
	spec.AddSnippet(broadcomAsicControlConnectedPlugAppArmor)
	return nil
}

func (iface *broadcomAsicControlInterface) UDevConnectedPlug(spec *udev.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error {
	old := "###SLOT_SECURITY_TAGS###"
	for appName := range plug.Apps {
		tag := udevSnapSecurityName(plug.Snap.Name(), appName)
		snippet := strings.Replace(broadcomAsicControlConnectedPlugUDev, old, tag, -1)
		spec.AddSnippet(snippet)
	}
	return nil
}

func (iface *broadcomAsicControlInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	// Allow what is allowed in the declarations
	return true
}

func init() {
	registerIface(&broadcomAsicControlInterface{})
}
