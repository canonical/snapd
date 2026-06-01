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

package ifacestate_test

import (
	"testing"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/configfiles"
	"github.com/snapcore/snapd/interfaces/dbus"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/interfaces/kmod"
	"github.com/snapcore/snapd/interfaces/ldconfig"
	"github.com/snapcore/snapd/interfaces/polkit"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/interfaces/symlinks"
	"github.com/snapcore/snapd/interfaces/systemd"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

func setupInterfaceRepo(t *testing.T, st *state.State) *interfaces.Repository {
	t.Helper()

	repo := interfaces.NewRepository()
	st.Lock()
	ifacerepo.Replace(st, repo)
	st.Unlock()

	for _, iface := range []interfaces.Interface{
		&ifacetest.TestInterface{InterfaceName: "network", InterfaceStaticInfo: interfaces.StaticInfo{Summary: "network"}},
		&ifacetest.TestInterface{InterfaceName: "audio-playback", InterfaceStaticInfo: interfaces.StaticInfo{Summary: "audio"}},
	} {
		if err := repo.AddInterface(iface); err != nil {
			t.Fatalf("cannot add interface %q: %v", iface.Name(), err)
		}
	}

	consumer := mustInfoFromYAML(t, `name: consumer
version: 1
apps:
  app:
    command: bin/app
plugs:
  network-plug:
    interface: network
  audio-plug:
    interface: audio-playback
`)
	provider := mustInfoFromYAML(t, `name: provider
version: 1
apps:
  app:
    command: bin/app
slots:
  network-slot:
    interface: network
  audio-slot:
    interface: audio-playback
`)

	for _, info := range []*snap.Info{consumer, provider} {
		appSet, err := interfaces.NewSnapAppSet(info, nil)
		if err != nil {
			t.Fatalf("cannot create app set for %q: %v", info.InstanceName(), err)
		}
		if err := repo.AddAppSet(appSet); err != nil {
			t.Fatalf("cannot add app set for %q: %v", info.InstanceName(), err)
		}
	}

	return repo
}

func mustInfoFromYAML(t *testing.T, yaml string) *snap.Info {
	t.Helper()
	info, err := snap.InfoFromSnapYaml([]byte(yaml))
	if err != nil {
		t.Fatalf("cannot parse snap yaml:\n%s\nerror: %v", yaml, err)
	}
	return info
}

type minimalTestInterface struct {
	name string
	info interfaces.StaticInfo
}

func (i minimalTestInterface) Name() string {
	return i.name
}

func (i minimalTestInterface) StaticInfo() interfaces.StaticInfo {
	return i.info
}

func (minimalTestInterface) AutoConnect(*snap.PlugInfo, *snap.SlotInfo) bool {
	return true
}

// Keep these compile-time checks local to ensure test interface signatures stay aligned.
var _ interface {
	AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
} = &ifacetest.TestInterface{}

var _ interface {
	SecCompConnectedPlug(spec *seccomp.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
} = &ifacetest.TestInterface{}

var _ interface {
	UDevConnectedPlug(spec *udev.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
} = &ifacetest.TestInterface{}

var _ interface {
	KModConnectedPlug(spec *kmod.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
} = &ifacetest.TestInterface{}

var _ interface {
	DBusConnectedPlug(spec *dbus.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
} = &ifacetest.TestInterface{}

var _ interface {
	ConfigfilesConnectedPlug(spec *configfiles.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
} = &ifacetest.TestInterface{}

var _ interface {
	LdconfigConnectedPlug(spec *ldconfig.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
} = &ifacetest.TestInterface{}

var _ interface {
	PolkitConnectedPlug(spec *polkit.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
} = &ifacetest.TestInterface{}

var _ interface {
	SymlinksConnectedPlug(spec *symlinks.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
} = &ifacetest.TestInterface{}

var _ interface {
	SystemdConnectedPlug(spec *systemd.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
} = &ifacetest.TestInterface{}
