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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/seccomp"
	apparmor_sandbox "github.com/snapcore/snapd/sandbox/apparmor"
	"github.com/snapcore/snapd/snap"
)

const pkcs11Summary = `allows use of pkcs11 framework and access to exposed tokens`

const pkcs11BaseDeclarationSlots = `
  pkcs11:
    allow-installation: false
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

func (iface *pkcs11Interface) getSocketPath(slot *snap.SlotInfo) (string, error) {
	socketAttr, isSet := slot.Attrs["pkcs11-socket"]
	if !isSet {
		return "", fmt.Errorf(`cannot use pkcs11 slot without "pkcs11-socket" attribute`)
	}

	socketPath, ok := socketAttr.(string)
	if !ok {
		return "", fmt.Errorf(`pkcs11 slot "pkcs11-socket" attribute must be a string, not %v`,
			socketAttr)
	}

	if ok := cleanSubPath(socketPath); !ok {
		return "", fmt.Errorf("pkcs11 unix socket path is not clean: %q", socketPath)
	}

	// separate socket name and check socket is in /run/p11-kit
	if filepath.Dir(socketPath) != "/run/p11-kit" {
		return "", fmt.Errorf("pkcs11 unix socket has to be in the /run/p11-kit directory: %q", socketPath)
	}

	return socketPath, nil
}

func (iface *pkcs11Interface) BeforePrepareSlot(slot *snap.SlotInfo) error {
	socketPath := mylog.Check2(iface.getSocketPath(slot))
	mylog.Check(apparmor_sandbox.ValidateNoAppArmorRegexp(socketPath))

	return err
}

func (iface *pkcs11Interface) SecCompPermanentSlot(spec *seccomp.Specification, slot *snap.SlotInfo) error {
	spec.AddSnippet(pkcs11PermanentSlotSecComp)
	return nil
}

func (iface *pkcs11Interface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	var socketPath string
	if mylog.Check(slot.Attr("pkcs11-socket", &socketPath)); err != nil || socketPath == "" {
		return fmt.Errorf(`internal error: pkcs11 slot %q must have a unix socket "pkcs11-socket" attribute`, slot.Ref())
	}

	// The validation from BeforePrepareSlot() ensures that the socket path starts with "/run/p11-kit"
	socketRule := fmt.Sprintf(`"/{,var/}%s" rw,`, socketPath[1:])
	// pkcs11 server unix socket
	spec.AddSnippet(fmt.Sprintf(`# pkcs11 socket
%s
# pkcs11 config for p11-proxy
/etc/pkcs11/{,**} r,
# pkcs11 tools
/usr/bin/p11tool ixr,
/usr/bin/pkcs11-tool ixr,
/usr/share/p11-kit/modules/ r,
/usr/share/p11-kit/modules/* r,`,
		socketRule))
	return nil
}

func (iface *pkcs11Interface) AppArmorPermanentSlot(spec *apparmor.Specification, slot *snap.SlotInfo) error {
	socketPath := mylog.Check2(iface.getSocketPath(slot))

	// The validation from BeforePrepareSlot() ensures that the socket path starts with "/run/p11-kit"
	socketRule := fmt.Sprintf(`"/{,var/}%s" rwk,`, socketPath[1:])
	spec.AddSnippet(fmt.Sprintf(`# pkcs11 socket dir
/{,var/}run/p11-kit/  rw,
%s
# pkcs11 config
/etc/pkcs11/{,**} r,
/usr/bin/p11-kit ixr,
/usr/bin/p11tool ixr,
/usr/bin/pkcs11-tool ixr,
/usr/libexec/p11-kit/p11-kit-server ixr,
/usr/libexec/p11-kit/p11-kit-remote ixr,`,
		socketRule))

	return nil
}

func (iface *pkcs11Interface) AutoConnect(*snap.PlugInfo, *snap.SlotInfo) bool {
	// allow what declarations allowed
	return true
}

func init() {
	registerIface(&pkcs11Interface{})
}
