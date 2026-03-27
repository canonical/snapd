// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package ifacestate

import (
	"fmt"
	"sort"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/configfiles"
	"github.com/snapcore/snapd/interfaces/dbus"
	"github.com/snapcore/snapd/interfaces/kmod"
	"github.com/snapcore/snapd/interfaces/ldconfig"
	"github.com/snapcore/snapd/interfaces/polkit"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/interfaces/symlinks"
	"github.com/snapcore/snapd/interfaces/systemd"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
	"github.com/snapcore/snapd/overlord/mcp"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

var readOnlyToolExecution = mcp.ToolExecution{TaskSupport: mcp.ToolTaskSupportForbidden}

func validateOptionalString(args map[string]any, name string) error {
	v, ok := args[name]
	if !ok {
		return nil
	}
	if _, ok := v.(string); !ok {
		return fmt.Errorf("%s must be a string", name)
	}
	return nil
}

func validateOptionalBool(args map[string]any, name string) error {
	v, ok := args[name]
	if !ok {
		return nil
	}
	if _, ok := v.(bool); !ok {
		return fmt.Errorf("%s must be a boolean", name)
	}
	return nil
}

func repoFromState(st *state.State) (repo *interfaces.Repository, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("cannot access interface repository: %v", rec)
		}
	}()

	st.Lock()
	defer st.Unlock()

	repo = ifacerepo.Get(st)
	return repo, nil
}

func plugToMap(plug *snap.PlugInfo, includeDetails bool) map[string]any {
	result := map[string]any{
		"snap_name": plug.Snap.InstanceName(),
		"name":      plug.Name,
		"interface": plug.Interface,
	}
	if !includeDetails {
		return result
	}

	attrs := plug.Attrs
	if attrs == nil {
		attrs = map[string]any{}
	}
	result["label"] = plug.Label
	result["attrs"] = attrs
	result["apps"] = sortedAppNames(plug.Apps)
	result["unscoped"] = plug.Unscoped
	return result
}

func slotToMap(slot *snap.SlotInfo, includeDetails bool) map[string]any {
	result := map[string]any{
		"snap_name": slot.Snap.InstanceName(),
		"name":      slot.Name,
		"interface": slot.Interface,
	}
	if !includeDetails {
		return result
	}

	attrs := slot.Attrs
	if attrs == nil {
		attrs = map[string]any{}
	}
	result["label"] = slot.Label
	result["attrs"] = attrs
	result["apps"] = sortedAppNames(slot.Apps)
	result["unscoped"] = slot.Unscoped
	result["hotplug_key"] = string(slot.HotplugKey)
	return result
}

func sortedAppNames(apps map[string]*snap.AppInfo) []string {
	names := make([]string, 0, len(apps))
	for name := range apps {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func implementedBackends(iface interfaces.Interface) []string {
	type appArmorBackend interface {
		AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
	}
	type seccompBackend interface {
		SecCompConnectedPlug(spec *seccomp.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
	}
	type udevBackend interface {
		UDevConnectedPlug(spec *udev.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
	}
	type kmodBackend interface {
		KModConnectedPlug(spec *kmod.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
	}
	type dbusBackend interface {
		DBusConnectedPlug(spec *dbus.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
	}
	type configfilesBackend interface {
		ConfigfilesConnectedPlug(spec *configfiles.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
	}
	type ldconfigBackend interface {
		LdconfigConnectedPlug(spec *ldconfig.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
	}
	type polkitBackend interface {
		PolkitConnectedPlug(spec *polkit.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
	}
	type symlinksBackend interface {
		SymlinksConnectedPlug(spec *symlinks.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
	}
	type systemdBackend interface {
		SystemdConnectedPlug(spec *systemd.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
	}

	backends := make([]string, 0, 10)
	if _, ok := iface.(appArmorBackend); ok {
		backends = append(backends, string(interfaces.SecurityAppArmor))
	}
	if _, ok := iface.(seccompBackend); ok {
		backends = append(backends, string(interfaces.SecuritySecComp))
	}
	if _, ok := iface.(dbusBackend); ok {
		backends = append(backends, string(interfaces.SecurityDBus))
	}
	if _, ok := iface.(udevBackend); ok {
		backends = append(backends, string(interfaces.SecurityUDev))
	}
	if _, ok := iface.(kmodBackend); ok {
		backends = append(backends, string(interfaces.SecurityKMod))
	}
	if _, ok := iface.(systemdBackend); ok {
		backends = append(backends, string(interfaces.SecuritySystemd))
	}
	if _, ok := iface.(polkitBackend); ok {
		backends = append(backends, string(interfaces.SecurityPolkit))
	}
	if _, ok := iface.(ldconfigBackend); ok {
		backends = append(backends, string(interfaces.SecurityLdconfig))
	}
	if _, ok := iface.(configfilesBackend); ok {
		backends = append(backends, string(interfaces.SecurityConfigfiles))
	}
	if _, ok := iface.(symlinksBackend); ok {
		backends = append(backends, string(interfaces.SecuritySymlinks))
	}
	return backends
}
