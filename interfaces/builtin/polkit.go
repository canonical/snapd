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
	"path"
	"path/filepath"
	"strings"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/polkit"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/polkit/validate"
	"github.com/snapcore/snapd/snap"
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
	mylog.Check(attribs.Attr("action-prefix", &prefix))
	mylog.Check(interfaces.ValidateDBusBusName(prefix))

	return prefix, nil
}

func loadPolkitPolicy(filename, actionPrefix string) (polkit.Policy, error) {
	content := mylog.Check2(os.ReadFile(filename))

	// Check that the file content is a valid polkit policy file
	actionIDs := mylog.Check2(validate.ValidatePolicy(bytes.NewReader(content)))

	// Check that the action IDs in the policy file match the action prefix
	for _, id := range actionIDs {
		if id != actionPrefix && !strings.HasPrefix(id, actionPrefix+".") {
			return nil, fmt.Errorf(`policy file %q contains unexpected action ID %q`, filename, id)
		}
	}

	return polkit.Policy(content), nil
}

func (iface *polkitInterface) PolkitConnectedPlug(spec *polkit.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	actionPrefix := mylog.Check2(iface.getActionPrefix(plug))

	mountDir := plug.Snap().MountDir()
	policyFiles := mylog.Check2(filepath.Glob(filepath.Join(mountDir, "meta", "polkit", plug.Name()+".*.policy")))

	if len(policyFiles) == 0 {
		return fmt.Errorf("cannot find any policy files for plug %q", plug.Name())
	}
	for _, filename := range policyFiles {
		suffix := strings.TrimSuffix(filepath.Base(filename), ".policy")
		policy := mylog.Check2(loadPolkitPolicy(filename, actionPrefix))
		mylog.Check(spec.AddPolicy(suffix, policy))

	}
	return nil
}

func (iface *polkitInterface) BeforePreparePlug(plug *snap.PlugInfo) error {
	_ := mylog.Check2(iface.getActionPrefix(plug))
	return err
}

func isPathMountedWritable(mntProfile *osutil.MountProfile, fsPath string) bool {
	mntMap := make(map[string]*osutil.MountEntry, len(mntProfile.Entries))
	for i := range mntProfile.Entries {
		mnt := &mntProfile.Entries[i]
		mntMap[mnt.Dir] = mnt
	}

	// go backwards in path until we hit a match
	currentPath := fsPath
	for {
		if mnt, ok := mntMap[currentPath]; ok {
			return mnt.Options[0] == "rw"
		}

		// Make sure we terminate on the last path token
		if currentPath == "/" || !strings.Contains(currentPath, "/") {
			break
		}
		currentPath = path.Dir(currentPath)
	}
	return false
}

// hasPolkitDaemonExecutable checks known paths on core for the presence of
// the polkit daemon executable. This function can be shortened but keep it like
// this for readability.
func hasPolkitDaemonExecutable() bool {
	// On core22(+core-desktop?) polkitd is at /usr/libexec/polkitd
	if osutil.IsExecutable("/usr/libexec/polkitd") {
		return true
	}
	// On core24 polkitd is at /usr/lib/polkit-1/polkitd
	return osutil.IsExecutable("/usr/lib/polkit-1/polkitd")
}

func polkitPoliciesSupported() bool {
	// We must have the polkit daemon present on the system
	if !hasPolkitDaemonExecutable() {
		return false
	}

	mntProfile := mylog.Check2(osutil.LoadMountProfile("/proc/self/mounts"))

	// XXX: we are called from init() what can we do here?

	// For core22+ polkitd is present, but it's not possible to install
	// any policy files, as polkit only checks /usr/share/polkit-1/actions,
	// which is readonly on core.
	return isPathMountedWritable(mntProfile, "/usr/share/polkit-1/actions")
}

func init() {
	registerIface(&polkitInterface{
		commonInterface{
			name:                  "polkit",
			summary:               polkitSummary,
			implicitOnCore:        polkitPoliciesSupported(),
			implicitOnClassic:     true,
			baseDeclarationPlugs:  polkitBaseDeclarationPlugs,
			baseDeclarationSlots:  polkitBaseDeclarationSlots,
			connectedPlugAppArmor: polkitConnectedPlugAppArmor,
		},
	})
}
