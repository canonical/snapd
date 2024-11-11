// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/polkit"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/polkit/validate"
	"github.com/snapcore/snapd/snap"
	"golang.org/x/sys/unix"
)

const polkitSummary = `allows access to polkitd to check authorisation`

const polkitBaseDeclarationPlugs = `
  polkit:
    allow-installation: false
    deny-auto-connection: true
`

const polkitBaseDeclarationSlots = `
  polkit:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const polkitConnectedPlugAppArmor = `
# Description: Can talk to polkitd's CheckAuthorization API

#include <abstractions/dbus-strict>

dbus (send)
    bus=system
    path="/org/freedesktop/PolicyKit1/Authority"
    interface="org.freedesktop.PolicyKit1.Authority"
    member="{,Cancel}CheckAuthorization"
    peer=(label=unconfined),
dbus (send)
    bus=system
    path="/org/freedesktop/PolicyKit1/Authority"
    interface="org.freedesktop.PolicyKit1.Authority"
    member="RegisterAuthenticationAgentWithOptions"
    peer=(label=unconfined),
dbus (send)
    bus=system
    path="/org/freedesktop/PolicyKit1/Authority"
    interface="org.freedesktop.DBus.Properties"
    peer=(label=unconfined),
dbus (send)
    bus=system
    path="/org/freedesktop/PolicyKit1/Authority"
    interface="org.freedesktop.DBus.Introspectable"
    member="Introspect"
    peer=(label=unconfined),
`

type polkitInterface struct {
	commonInterface
}

func (iface *polkitInterface) getActionPrefix(attribs interfaces.Attrer) (string, error) {
	var prefix string
	if err := attribs.Attr("action-prefix", &prefix); err != nil {
		return "", err
	}
	if err := interfaces.ValidateDBusBusName(prefix); err != nil {
		return "", fmt.Errorf("plug has invalid action-prefix: %q", prefix)
	}
	return prefix, nil
}

func loadPolkitPolicy(filename, actionPrefix string) (polkit.Policy, error) {
	content, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf(`cannot read file %q: %v`, filename, err)
	}

	// Check that the file content is a valid polkit policy file
	actionIDs, err := validate.ValidatePolicy(bytes.NewReader(content))
	if err != nil {
		return nil, fmt.Errorf(`cannot validate policy file %q: %v`, filename, err)
	}

	// Check that the action IDs in the policy file match the action prefix
	for _, id := range actionIDs {
		if id != actionPrefix && !strings.HasPrefix(id, actionPrefix+".") {
			return nil, fmt.Errorf(`policy file %q contains unexpected action ID %q`, filename, id)
		}
	}

	return polkit.Policy(content), nil
}

func (iface *polkitInterface) PolkitConnectedPlug(spec *polkit.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	actionPrefix, err := iface.getActionPrefix(plug)
	if err != nil {
		return err
	}

	mountDir := plug.Snap().MountDir()
	policyFiles, err := filepath.Glob(filepath.Join(mountDir, "meta", "polkit", plug.Name()+".*.policy"))
	if err != nil {
		return err
	}
	if len(policyFiles) == 0 {
		return fmt.Errorf("cannot find any policy files for plug %q", plug.Name())
	}
	for _, filename := range policyFiles {
		suffix := strings.TrimSuffix(filepath.Base(filename), ".policy")
		policy, err := loadPolkitPolicy(filename, actionPrefix)
		if err != nil {
			return err
		}
		if err := spec.AddPolicy(suffix, policy); err != nil {
			return err
		}
	}
	return nil
}

func (iface *polkitInterface) BeforePreparePlug(plug *snap.PlugInfo) error {
	_, err := iface.getActionPrefix(plug)
	return err
}

var (
	// polkitDaemonPath1 is the path of polkitd on core<24.
	polkitDaemonPath1 = "/usr/libexec/polkitd"
	// polkitDaemonPath2 is the path of polkid on core>=24.
	polkitDaemonPath2 = "/usr/lib/polkit-1/polkitd"
)

// hasPolkitDaemonExecutable checks known paths on core for the presence of
// the polkit daemon executable. This function can be shortened but keep it like
// this for readability.
func hasPolkitDaemonExecutable() bool {
	return osutil.IsExecutable(polkitDaemonPath1) || osutil.IsExecutable(polkitDaemonPath2)
}

func canWriteToPolkitActionsDir() bool {
	return unix.Access(dirs.SnapPolkitPolicyDir, unix.W_OK) == nil
}

func polkitPoliciesSupported() bool {
	// We must have the polkit daemon present on the system and be able to write
	// to the polkit actions directory.
	return hasPolkitDaemonExecutable() && canWriteToPolkitActionsDir()
}

func (iface *polkitInterface) StaticInfo() interfaces.StaticInfo {
	info := iface.commonInterface.StaticInfo()
	info.ImplicitOnCore = polkitPoliciesSupported()
	return info
}

func init() {
	registerIface(&polkitInterface{
		commonInterface{
			name:    "polkit",
			summary: polkitSummary,
			// implicitOnCore is computed dynamically
			implicitOnClassic:     true,
			baseDeclarationPlugs:  polkitBaseDeclarationPlugs,
			baseDeclarationSlots:  polkitBaseDeclarationSlots,
			connectedPlugAppArmor: polkitConnectedPlugAppArmor,
		},
	})
}
