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
	"fmt"
	"path/filepath"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/snap"
)

const pkcs11Summary = `allows use of pkcs11 framework and access to exposed tokens`

const pkcs11BaseDeclarationSlots = `
  pkcs11:
    allow-installation:
      slot-snap-type:
        - core
        - gadget
    deny-auto-connection: true
`

type pkcs11Interface struct{}

// Name of the pkcs11 interface.
func (iface *pkcs11Interface) Name() string {
	return "pkcs11"
}

func (iface *pkcs11Interface) StaticInfo() interfaces.StaticInfo {
	return interfaces.StaticInfo{
		Summary:              pkcs11Summary,
		BaseDeclarationSlots: pkcs11BaseDeclarationSlots,
	}
}

func (iface *pkcs11Interface) String() string {
	return iface.Name()
}

const pkcs11PermanentSlotSecComp = `
# Description: Allow operating as an p11-kit server. This gives privileged access
# to the system.
# Needed for server launch
bind
listen

# Needed by server upon client connect
accept
accept4
`

func (iface *pkcs11Interface) socketName(slotRef *interfaces.SlotRef, attrs interfaces.Attrer) (string, error) {
	var socketName string
	if err := attrs.Attr("name", &socketName); err != nil || socketName == "" {
		return "", fmt.Errorf("slot %q must have a unix socket 'name' attribute", slotRef)
	}
	// TODO: token name should be checked
	socketName = filepath.Clean(socketName)
	return socketName, nil
}

func (iface *pkcs11Interface) BeforePrepareSlot(slot *snap.SlotInfo) error {
	_, err := iface.socketName(&interfaces.SlotRef{Snap: slot.Snap.InstanceName(), Name: slot.Name}, slot)
	return err
}

func (iface *pkcs11Interface) SecCompPermanentSlot(spec *seccomp.Specification, slot *snap.SlotInfo) error {
	if !implicitSystemPermanentSlot(slot) {
		spec.AddSnippet(pkcs11PermanentSlotSecComp)
	}
	return nil
}

func (iface *pkcs11Interface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	socketName, err := iface.socketName(slot.Ref(), slot)
	if err != nil {
		return nil
	}
	// pkcs11 server unix socket
	spec.AddSnippet(fmt.Sprintf(`# pkcs11 socket and socker env
/{,var/}run/p11-kit/%s rw,
/{,var/}run/p11-kit/env.%[1]s r,
# pkcs11 config
/etc/pkcs11/{,**} r,
# pkcs11 tools
/usr/bin/p11tool ixr,
/usr/bin/pkcs11-tool ixr,`,
		socketName))
	return nil
}

func (iface *pkcs11Interface) AppArmorPermanentSlot(spec *apparmor.Specification, slot *snap.SlotInfo) error {
	// Only apply slot snippet when running as application snap
	// on classic, slot side can be system or application
	if !implicitSystemPermanentSlot(slot) {
		spec.AddSnippet(`# pkcs11 socket dir
/{,var/}run/p11-kit/  rw,
/{,var/}run/p11-kit/pkcs11-* rwk,
/{,var/}run/p11-kit/env.pkcs11-* rw,
# pkcs11 config
/etc/pkcs11/{,**} r,
/usr/bin/p11-kit ixr,
/usr/bin/p11tool ixr,
/usr/bin/pkcs11-tool ixr,
/usr/libexec/p11-kit/p11-kit-server ixr,
/usr/libexec/p11-kit/p11-kit-remote ixr,`)
	}
	return nil
}

func (iface *pkcs11Interface) AutoConnect(*snap.PlugInfo, *snap.SlotInfo) bool {
	// allow what declarations allowed
	return true
}

func init() {
	registerIface(&pkcs11Interface{})
}
