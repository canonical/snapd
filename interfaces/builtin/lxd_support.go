// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/seccomp"
)

const lxdSupportSummary = `allows operating as the LXD service`

const lxdSupportBaseDeclarationPlugs = `
  lxd-support:
    allow-installation: false
    deny-auto-connection: true
`

const lxdSupportBaseDeclarationSlots = `
  lxd-support:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const lxdSupportConnectedPlugAppArmor = `
# Description: Can change to any apparmor profile (including unconfined) thus
# giving access to all resources of the system so LXD may manage what to give
# to its containers. This gives device ownership to connected snaps.
@{PROC}/**/attr/current r,
/usr/sbin/aa-exec ux,
`

const lxdSupportConnectedPlugSecComp = `
# Description: Can access all syscalls of the system so LXD may manage what to
# give to its containers, giving device ownership to connected snaps.
@unrestricted
`

type lxdSupportInterface struct{}

func (iface *lxdSupportInterface) Name() string {
	return "lxd-support"
}

func (iface *lxdSupportInterface) MetaData() interfaces.MetaData {
	return interfaces.MetaData{
		Summary:              lxdSupportSummary,
		ImplicitOnCore:       true,
		ImplicitOnClassic:    true,
		BaseDeclarationPlugs: lxdSupportBaseDeclarationPlugs,
		BaseDeclarationSlots: lxdSupportBaseDeclarationSlots,
	}
}

func (iface *lxdSupportInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error {
	spec.AddSnippet(lxdSupportConnectedPlugAppArmor)
	return nil
}

func (iface *lxdSupportInterface) SecCompConnectedPlug(spec *seccomp.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error {
	spec.AddSnippet(lxdSupportConnectedPlugSecComp)
	return nil
}

func (iface *lxdSupportInterface) SanitizePlug(plug *interfaces.Plug) error {
	return nil
}

func (iface *lxdSupportInterface) SanitizeSlot(slot *interfaces.Slot) error {
	return nil
}

func (iface *lxdSupportInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	// allow what declarations allowed
	return true
}

func init() {
	registerIface(&lxdSupportInterface{})
}
