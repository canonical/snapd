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
	"fmt"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/seccomp"
	apparmor_sandbox "github.com/snapcore/snapd/sandbox/apparmor"
	"github.com/snapcore/snapd/strutil"
)

const userNSSummary = `allows the ability to use user namespaces`

// This interface is super-privileged
const userNSBaseDeclarationSlots = `
  userns:
    allow-installation: false
    deny-connection: true
    deny-auto-connection: true
`

const userNSConnectedPlugAppArmorSupported = `
# Description: allow to use user namespaces

userns,
`

const userNSConnectedPlugSeccomp = `
# allow the use of unshare to use new user namespaces
unshare
`

type userNSInterface struct {
	commonInterface
}

func (iface *userNSInterface) StaticInfo() interfaces.StaticInfo {
	return interfaces.StaticInfo{
		Summary:              userNSSummary,
		BaseDeclarationSlots: userNSBaseDeclarationSlots,
	}
}

func (iface *userNSInterface) Name() string {
	return "userns"
}

func (iface *userNSInterface) checkUserNSAppArmorSupport() error {
	if apparmor_sandbox.ProbedLevel() == apparmor_sandbox.Unsupported {
		// AppArmor is not supported at all; no need to add rules
		return nil
	}

	features, err := apparmor_sandbox.ParserFeatures()
	if err != nil {
		return err
	}

	if !strutil.ListContains(features, "userns") {
		return fmt.Errorf("AppArmor does not support userns mediation")
	}

	return nil
}

func (iface *userNSInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	if err := iface.checkUserNSAppArmorSupport(); err == nil {
		spec.AddSnippet(userNSConnectedPlugAppArmorSupported)
	}

	// ignore err since in the case the userns is not supported by AppArmor
	// it will not be restricted so we still want the interface to appear
	// connected
	return nil
}

func (iface *userNSInterface) SecCompConnectedPlug(spec *seccomp.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	spec.AddSnippet(userNSConnectedPlugSeccomp)
	return nil
}

func init() {
	registerIface(&userNSInterface{})
}
