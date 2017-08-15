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
	"github.com/snapcore/snapd/interfaces/kmod"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/snap"
)

const broadcomAsicControlSummary = `allows using the broadcom-asic kernel module`

const broadcomAsicControlBaseDeclarationSlots = `
  broadcom-asic-control:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const broadcomAsicControlConnectedPlugAppArmor = `
# Description: Allow access to broadcom asic kernel module.

/sys/module/linux_bcm_knet/{,**} r,
/sys/module/linux_kernel_bde/{,**} r,
/sys/module/linux_user_bde/{,**} r,
/dev/linux-user-bde rw,
/dev/linux-kernel-bde rw,
/dev/linux-bcm-knet rw,
`

const broadcomAsicControlConnectedPlugUDev = `
KERNEL=="linux-user-bde", TAG+="###SLOT_SECURITY_TAGS###"
KERNEL=="linux-kernel-bde", TAG+="###SLOT_SECURITY_TAGS###"
KERNEL=="linux-bcm-knet", TAG+="###SLOT_SECURITY_TAGS###"
`

// The upstream linux kernel doesn't come with support for the
// necessary kernel modules we need to drive a Broadcom ASIC.
// All necessary modules need to be loaded on demand if the
// kernel the device runs with provides them.
var broadcomAsicControlConnectedPlugKMod = []string{
	"linux-user-bde",
	"linux-kernel-bde",
	"linux-bcm-knet",
}

type broadcomAsicControlInterface struct{}

func (iface *broadcomAsicControlInterface) Name() string {
	return "broadcom-asic-control"
}

// MetaData returns various meta-data about this interface.
func (iface *broadcomAsicControlInterface) MetaData() interfaces.MetaData {
	return interfaces.MetaData{
		Summary:              broadcomAsicControlSummary,
		ImplicitOnCore:       true,
		ImplicitOnClassic:    true,
		BaseDeclarationSlots: broadcomAsicControlBaseDeclarationSlots,
	}
}

func (iface *broadcomAsicControlInterface) SanitizeSlot(slot *interfaces.Slot) error {
	if iface.Name() != slot.Interface {
		panic(fmt.Sprintf("slot is not of interface %q", iface.Name()))
	}
	if slot.Snap.Type != snap.TypeOS {
		return fmt.Errorf("%s slots are reserved for the core snap", iface.Name())
	}
	return nil
}

func (iface *broadcomAsicControlInterface) SanitizePlug(plug *interfaces.Plug) error {
	if iface.Name() != plug.Interface {
		panic(fmt.Sprintf("plug is not of interface %q", iface.Name()))
	}
	// NOTE: currently we don't check anything on the plug side.
	return nil
}

func (iface *broadcomAsicControlInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error {
	spec.AddSnippet(broadcomAsicControlConnectedPlugAppArmor)
	return nil
}

func (iface *broadcomAsicControlInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	return true
}

func (iface *broadcomAsicControlInterface) KModConnectedPlug(spec *kmod.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error {
	for _, m := range broadcomAsicControlConnectedPlugKMod {
		if err := spec.AddModule(m); err != nil {
			return err
		}
	}
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

func init() {
	registerIface(&broadcomAsicControlInterface{})
}
