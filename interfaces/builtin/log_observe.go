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

const logObserveSummary = `allows read access to system logs`

const logObserveBaseDeclarationSlots = `
  log-observe:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

// http://bazaar.launchpad.net/~ubuntu-security/ubuntu-core-security/trunk/view/head:/data/apparmor/policygroups/ubuntu-core/16.04/log-observe
const logObserveConnectedPlugAppArmor = `
# Description: Can read system logs and set kernel log rate-limiting

/var/log/ r,
/var/log/** r,

# for accessing journald and journalctl
/run/log/journal/ r,
/run/log/journal/** r,
/var/lib/systemd/catalog/database r,
/{,usr/}bin/journalctl ixr,
# allow using journalctl on the host to support new logs on classic systems
/var/lib/snapd/hostfs/bin/journalctl ixr,
/var/lib/snapd/hostfs/lib/systemd/*.so* mr,

# journalctl wants this but it grants far more than 'observe' so don't enable
# it. We could silence the denial, but let's avoid that for now.
# capability sys_resource,

# Allow sysctl -w kernel.printk_ratelimit=#
/{,usr/}sbin/sysctl ixr,
@{PROC}/sys/kernel/printk_ratelimit rw,

# Allow resolving kernel seccomp denials
/usr/bin/scmp_sys_resolver ixr,

# Needed since we are root and the owner/group doesn't match :\
# So long as we have this, the cap must be reserved.
capability dac_override,
`

func init() {
	registerIface(&commonInterface{
		name:                  "log-observe",
		summary:               logObserveSummary,
		implicitOnCore:        true,
		implicitOnClassic:     true,
		baseDeclarationSlots:  logObserveBaseDeclarationSlots,
		connectedPlugAppArmor: logObserveConnectedPlugAppArmor,
		reservedForOS:         true,
	})
}
