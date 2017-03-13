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

type PppInterface struct{}

func (iface *PppInterface) Name() string {
	return "ppp"
}

func (iface *PppInterface) PermanentPlugSnippet(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *PppInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error {
	return spec.AddSnippet(pppConnectedPlugAppArmor)
}

func (iface *PppInterface) ConnectedPlugSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *PppInterface) PermanentSlotSnippet(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *PppInterface) ConnectedSlotSnippet(plug *interfaces.Plug, slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
	return nil, nil
}

func (iface *PppInterface) KModConnectedPlug(spec *kmod.Specification, plug *interfaces.Plug, slot *interfaces.Slot) error {
	return spec.AddModule(pppConnectedPlugKmod)
}

func (iface *PppInterface) SanitizePlug(plug *interfaces.Plug) error {
	return nil
}

func (iface *PppInterface) SanitizeSlot(slot *interfaces.Slot) error {
	return nil
}

func (iface *PppInterface) AutoConnect(*interfaces.Plug, *interfaces.Slot) bool {
	// allow what declarations allowed
	return true
}
