// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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
	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	apparmor_sandbox "github.com/snapcore/snapd/sandbox/apparmor"
	"github.com/snapcore/snapd/strutil"
)

const userNSSummary = `allows the ability to use user namespaces`

// This interface is super-privileged
const userNSBaseDeclarationPlugs = `
  userns:
    allow-installation: false
    deny-auto-connection: true
`

const userNSBaseDeclarationSlots = `
  userns:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const userNSConnectedPlugAppArmorSupported = `
# Description: allow to use user namespaces

userns,
`

const userNSConnectedPlugSeccomp = `
# allow the use of clone and unshare to use new user namespaces
clone
unshare
`

type userNSInterface struct {
	commonInterface
}

func (iface *userNSInterface) Name() string {
	return "userns"
}

func (iface *userNSInterface) userNSAppArmorSupported() (bool, error) {
	if apparmor_sandbox.ProbedLevel() == apparmor_sandbox.Unsupported {
		// AppArmor is not supported at all; no need to add rules
		return false, nil
	}

	features := mylog.Check2(apparmor_sandbox.ParserFeatures())

	if !strutil.ListContains(features, "userns") {
		return false, nil
	}

	return true, nil
}

func (iface *userNSInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	supported := mylog.Check2(iface.userNSAppArmorSupported())

	if supported {
		spec.AddSnippet(userNSConnectedPlugAppArmorSupported)
	}

	return nil
}

func init() {
	registerIface(&userNSInterface{commonInterface: commonInterface{
		name:                 "userns",
		summary:              userNSSummary,
		implicitOnCore:       true,
		implicitOnClassic:    true,
		baseDeclarationPlugs: userNSBaseDeclarationPlugs,
		baseDeclarationSlots: userNSBaseDeclarationSlots,
		connectedPlugSecComp: userNSConnectedPlugSeccomp,
	}})
}
