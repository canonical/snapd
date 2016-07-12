// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

// package backendtest contains common code for testing backends
package backendtest

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/snap"
)

type BackendSuite struct {
	Backend interfaces.SecurityBackend
	Repo    *interfaces.Repository
	Iface   *interfaces.TestInterface
	RootDir string
}

func (s *BackendSuite) SetUpTest(c *C) {
	// Isolate this test to a temporary directory
	s.RootDir = c.MkDir()
	dirs.SetRootDir(s.RootDir)
	// Create a fresh repository for each test
	s.Repo = interfaces.NewRepository()
	s.Iface = &interfaces.TestInterface{InterfaceName: "iface"}
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
    iface:
`
const SambaYamlV1WithNmbd = `
name: samba
version: 1
developer: acme
apps:
    smbd:
    nmbd:
slots:
    iface:
`
const SambaYamlV2 = `
name: samba
version: 2
developer: acme
apps:
    smbd:
slots:
    iface:
`
const SambaYamlWithHook = `
name: samba
apps:
    smbd:
    nmbd:
hooks:
    apply-config:
        plugs: [iface]
slots:
    iface:
`
const HookYaml = `
name: foo
version: 1
developer: acme
hooks:
    apply-config:
plugs:
    iface:
`

// Support code for tests

// InstallSnap "installs" a snap from YAML.
func (s *BackendSuite) InstallSnap(c *C, devMode bool, snapYaml string, revision int) *snap.Info {
	snapInfo, err := snap.InfoFromSnapYaml([]byte(snapYaml))
	c.Assert(err, IsNil)
	// this won't come from snap.yaml
	snapInfo.Revision = snap.R(revision)
	snapInfo.Developer = "acme"

	s.addPlugsSlots(c, snapInfo)
	err = s.Backend.Setup(snapInfo, devMode, s.Repo)
	c.Assert(err, IsNil)
	return snapInfo
}

// UpdateSnap "updates" an existing snap from YAML.
func (s *BackendSuite) UpdateSnap(c *C, oldSnapInfo *snap.Info, devMode bool, snapYaml string, revision int) *snap.Info {
	newSnapInfo, err := snap.InfoFromSnapYaml([]byte(snapYaml))
	c.Assert(err, IsNil)
	// this won't come from snap.yaml
	newSnapInfo.Revision = snap.R(revision)
	newSnapInfo.Developer = "acme"

	c.Assert(newSnapInfo.Name(), Equals, oldSnapInfo.Name())
	s.removePlugsSlots(c, oldSnapInfo)
	s.addPlugsSlots(c, newSnapInfo)
	err = s.Backend.Setup(newSnapInfo, devMode, s.Repo)
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
