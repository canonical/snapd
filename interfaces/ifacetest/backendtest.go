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

package ifacetest

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
)

type BackendSuite struct {
	Backend interfaces.SecurityBackend
	Repo    *interfaces.Repository
	Iface   *TestInterface
	RootDir string
}

func (s *BackendSuite) SetUpTest(c *C) {
	// Isolate this test to a temporary directory
	s.RootDir = c.MkDir()
	dirs.SetRootDir(s.RootDir)
	// Create a fresh repository for each test
	s.Repo = interfaces.NewRepository()
	s.Iface = &TestInterface{InterfaceName: "iface"}
	err := s.Repo.AddInterface(s.Iface)
	c.Assert(err, IsNil)
}

func (s *BackendSuite) TearDownTest(c *C) {
	dirs.SetRootDir("/")
}

// Tests for Setup() and Remove()
const SambaYamlV1 = `
name: samba
version: 1
developer: acme
apps:
    smbd:
slots:
    slot:
        interface: iface
`
const SambaYamlV1WithNmbd = `
name: samba
version: 1
developer: acme
apps:
    smbd:
    nmbd:
slots:
    slot:
        interface: iface
`
const SambaYamlV1NoSlot = `
name: samba
version: 1
developer: acme
apps:
    smbd:
`
const SambaYamlV1WithNmbdNoSlot = `
name: samba
version: 1
developer: acme
apps:
    smbd:
    nmbd:
`
const SambaYamlV2 = `
name: samba
version: 2
developer: acme
apps:
    smbd:
slots:
    slot:
        interface: iface
`
const SambaYamlWithHook = `
name: samba
apps:
    smbd:
    nmbd:
hooks:
    configure:
        plugs: [plug]
slots:
    slot:
        interface: iface
plugs:
    plug:
        interface: iface
`
const HookYaml = `
name: foo
version: 1
developer: acme
hooks:
    configure:
plugs:
    plug:
        interface: iface
`
const PlugNoAppsYaml = `
name: foo
version: 1
developer: acme
plugs:
    plug:
        interface: iface
`
const SlotNoAppsYaml = `
name: foo
version: 1
developer: acme
slots:
    slots:
        interface: iface
`

// Support code for tests

// InstallSnap "installs" a snap from YAML.
func (s *BackendSuite) InstallSnap(c *C, opts interfaces.ConfinementOptions, snapYaml string, revision int) *snap.Info {
	snapInfo := snaptest.MockInfo(c, snapYaml, &snap.SideInfo{
		Revision: snap.R(revision),
	})
	s.addPlugsSlots(c, snapInfo)
	err := s.Backend.Setup(snapInfo, opts, s.Repo)
	c.Assert(err, IsNil)
	return snapInfo
}

// UpdateSnap "updates" an existing snap from YAML.
func (s *BackendSuite) UpdateSnap(c *C, oldSnapInfo *snap.Info, opts interfaces.ConfinementOptions, snapYaml string, revision int) *snap.Info {
	newSnapInfo := snaptest.MockInfo(c, snapYaml, &snap.SideInfo{
		Revision: snap.R(revision),
	})
	c.Assert(newSnapInfo.Name(), Equals, oldSnapInfo.Name())
	s.removePlugsSlots(c, oldSnapInfo)
	s.addPlugsSlots(c, newSnapInfo)
	err := s.Backend.Setup(newSnapInfo, opts, s.Repo)
	c.Assert(err, IsNil)
	return newSnapInfo
}

// RemoveSnap "removes" an "installed" snap.
func (s *BackendSuite) RemoveSnap(c *C, snapInfo *snap.Info) {
	err := s.Backend.Remove(snapInfo.Name())
	c.Assert(err, IsNil)
	s.removePlugsSlots(c, snapInfo)
}

func (s *BackendSuite) addPlugsSlots(c *C, snapInfo *snap.Info) {
	for _, plugInfo := range snapInfo.Plugs {
		plug := &interfaces.Plug{PlugInfo: plugInfo}
		err := s.Repo.AddPlug(plug)
		c.Assert(err, IsNil)
	}
	for _, slotInfo := range snapInfo.Slots {
		slot := &interfaces.Slot{SlotInfo: slotInfo}
		err := s.Repo.AddSlot(slot)
		c.Assert(err, IsNil)
	}
}

func (s *BackendSuite) removePlugsSlots(c *C, snapInfo *snap.Info) {
	for _, plug := range s.Repo.Plugs(snapInfo.Name()) {
		err := s.Repo.RemovePlug(plug.Snap.Name(), plug.Name)
		c.Assert(err, IsNil)
	}
	for _, slot := range s.Repo.Slots(snapInfo.Name()) {
		err := s.Repo.RemoveSlot(slot.Snap.Name(), slot.Name)
		c.Assert(err, IsNil)
	}
}
