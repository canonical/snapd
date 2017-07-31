// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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
	"github.com/snapcore/snapd/interfaces/kmod"
)

const pppSummary = `allows operating as the ppp service`

const pppBaseDeclarationSlots = `
  ppp:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const pppConnectedPlugAppArmor = `
# Description: Allow operating ppp daemon. This gives privileged access to the
# ppp daemon.

# Needed for modem connections using PPP
/usr/sbin/pppd ix,
/etc/ppp/** rwix,
/dev/ppp rw,
/dev/tty[^0-9]* rw,
/run/lock/*tty[^0-9]* rw,
/run/ppp* rw,
/var/run/ppp* rw,
/var/log/ppp* rw,
/bin/run-parts ix,
@{PROC}/@{pid}/loginuid r,
capability setgid,
capability setuid,
`

// ppp_generic creates /dev/ppp. Other ppp modules will be automatically loaded
// by the kernel on different ioctl calls for this device. Note also that
// in many cases ppp_generic is statically linked into the kernel (CONFIG_PPP=y)
const pppConnectedPlugKmod = "ppp_generic"

type pppInterface struct{}

func (iface *pppInterface) Name() string {
	return "ppp"
}

func (iface *pppInterface) StaticInfo() interfaces.StaticInfo {
	return interfaces.StaticInfo{
		Summary:              pppSummary,
		ImplicitOnCore:       true,
		ImplicitOnClassic:    true,
		BaseDeclarationSlots: pppBaseDeclarationSlots,
	}
}

func (iface *pppInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error {
	spec.AddSnippet(pppConnectedPlugAppArmor)
	return nil
}

func (iface *pppInterface) KModConnectedPlug(spec *kmod.Specification, plug *interfaces.Plug, plugAttrs map[string]interface{}, slot *interfaces.Slot, slotAttrs map[string]interface{}) error {
	return spec.AddModule(pppConnectedPlugKmod)
}

func (iface *pppInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	// allow what declarations allowed
	return true
}

func init() {
	registerIface(&pppInterface{})
}
