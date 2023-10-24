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
	"github.com/snapcore/snapd/interfaces/seccomp"
	apparmor_sandbox "github.com/snapcore/snapd/sandbox/apparmor"
	"github.com/snapcore/snapd/strutil"
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
@{PROC}/**/attr/{,apparmor/}current r,
/{,usr/}{,s}bin/aa-exec ux,

# Allow discovering the os-release of the host
/var/lib/snapd/hostfs/{etc,usr/lib}/os-release r,
`

const lxdSupportConnectedPlugAppArmorWithUserNS = `
# allow use of user namespaces
userns,
`

const lxdSupportConnectedPlugSecComp = `
# Description: Can access all syscalls of the system so LXD may manage what to
# give to its containers, giving device ownership to connected snaps.
@unrestricted
`

const lxdSupportServiceSnippet = `Delegate=true`

type lxdSupportInterface struct {
	commonInterface
}

func (iface *lxdSupportInterface) Name() string {
	return "lxd-support"
}

func (iface *lxdSupportInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	// lxd-support should be unconfined
	spec.SetUnconfined()
	spec.AddSnippet(lxdSupportConnectedPlugAppArmor)
	// if apparmor supports userns mediation then add this too
	if apparmor_sandbox.ProbedLevel() != apparmor_sandbox.Unsupported {
		features, err := apparmor_sandbox.ParserFeatures()
		if err != nil {
			return err
		}
		if strutil.ListContains(features, "userns") {
			spec.AddSnippet(lxdSupportConnectedPlugAppArmorWithUserNS)
		}
	}
	return nil
}

func (iface *lxdSupportInterface) SecCompConnectedPlug(spec *seccomp.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	spec.AddSnippet(lxdSupportConnectedPlugSecComp)
	return nil
}

func init() {
	registerIface(&lxdSupportInterface{commonInterface{
		name:                 "lxd-support",
		summary:              lxdSupportSummary,
		implicitOnCore:       true,
		implicitOnClassic:    true,
		baseDeclarationSlots: lxdSupportBaseDeclarationSlots,
		baseDeclarationPlugs: lxdSupportBaseDeclarationPlugs,
		serviceSnippets:      []string{lxdSupportServiceSnippet}},
	})
}
