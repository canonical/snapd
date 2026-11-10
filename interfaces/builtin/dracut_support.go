// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
 *
 * This program is free software; you can redistribute it and/or modify
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
	"github.com/snapcore/snapd/interfaces/seccomp"
)

const dracutSupportSummary = `allows creating files with setuid/setgid bits set (eg staging initrd images)`

// This interface is super-privileged.
//
// The default seccomp policy denies creating files with the setuid or setgid
// bits set (via chmod/fchmod/fchmodat/fchmodat2, creat, mknod/mknodat and
// open/openat with O_CREAT or O_TMPFILE). This interface suppresses those deny
// rules so that a snap can preserve setuid/setgid modes when staging files,
// eg with cp -a, as needed by dracut when building initrd images.
//
// This effectively allows the snap to create setuid-root binaries in its
// writable areas. Note that snap data directories such as
// /var/snap/<snap>/common are created world-readable and world-executable
// (mode 0755), so on a suid-enabled host filesystem any unconfined local
// user can directly execute a root-owned 04755 binary staged there by a
// root-running snap and gain root: this interface is a privilege escalation
// vector for the host, not just for the snap sandbox. Connections therefore
// grant host-root trust and must be reviewed accordingly.
//
// This re-opens the attack surface that the default hardening (commit
// f32fe221c5bcccac3a328efb89fe06286405385e) removed. Note that openat2
// remains unconditionally denied (ENOSYS) for all snaps as its arguments
// cannot be inspected by seccomp; callers fall back to openat.
const dracutSupportBaseDeclarationPlugs = `
  dracut-support:
    allow-installation: false
    deny-auto-connection: true
    deny-connection: true
`

const dracutSupportBaseDeclarationSlots = `
  dracut-support:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
    deny-connection: true
`

type dracutSupportInterface struct {
	commonInterface
}

// SecCompConnectedPlug suppresses the default seccomp deny rules for setuid
// and setgid file creation.
func (iface *dracutSupportInterface) SecCompConnectedPlug(spec *seccomp.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	spec.SetSuppressSuidCreationDeny()
	return nil
}

func init() {
	registerIface(&dracutSupportInterface{commonInterface: commonInterface{
		name:                 "dracut-support",
		summary:              dracutSupportSummary,
		implicitOnCore:       true,
		implicitOnClassic:    true,
		baseDeclarationPlugs: dracutSupportBaseDeclarationPlugs,
		baseDeclarationSlots: dracutSupportBaseDeclarationSlots,
	}})
}
