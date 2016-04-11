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

package dbus_test

import (
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/interfaces"
	"github.com/ubuntu-core/snappy/interfaces/dbus"
	"github.com/ubuntu-core/snappy/snap"
)

type backendSuite struct {
	backend *dbus.Backend
	repo    *interfaces.Repository
	iface   *interfaces.TestInterface
	rootDir string
}

var _ = Suite(&backendSuite{backend: &dbus.Backend{}})

func (s *backendSuite) SetUpTest(c *C) {
	// Isolate this test to a temporary directory
	s.rootDir = c.MkDir()
	dirs.SetRootDir(s.rootDir)
	// Prepare a directory for DBus configuration files.
	// NOTE: Normally this is a part of the OS snap.
	err := os.MkdirAll(dirs.SnapBusPolicyDir, 0700)
	c.Assert(err, IsNil)
	// Create a fresh repository for each test
	s.repo = interfaces.NewRepository()
	s.iface = &interfaces.TestInterface{InterfaceName: "iface"}
	err = s.repo.AddInterface(s.iface)
	c.Assert(err, IsNil)
}

func (s *backendSuite) TearDownTest(c *C) {
	dirs.SetRootDir("/")
}

// Tests for Setup() and Remove()
const sambaYamlV1 = `
name: samba
version: 1
developer: acme
apps:
    smbd:
slots:
    iface:
`
const sambaYamlV1WithNmbd = `
name: samba
version: 1
developer: acme
apps:
    smbd:
    nmbd:
slots:
    iface:
`
const sambaYamlV2 = `
name: samba
version: 2
developer: acme
apps:
    smbd:
slots:
    iface:
`

func (s *backendSuite) TestName(c *C) {
	c.Check(s.backend.Name(), Equals, "dbus")
}

func (s *backendSuite) TestInstallingSnapWritesConfigFiles(c *C) {
	// NOTE: Hand out a permanent snippet so that .conf file is generated.
	s.iface.PermanentSlotSnippetCallback = func(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
		return []byte("<policy/>"), nil
	}
	for _, developerMode := range []bool{true, false} {
		snapInfo := s.installSnap(c, developerMode, sambaYamlV1)
		profile := filepath.Join(dirs.SnapBusPolicyDir, "snap.samba.smbd.conf")
		// file called "snap.sambda.smbd.conf" was created
		_, err := os.Stat(profile)
		c.Check(err, IsNil)
		s.removeSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestRemovingSnapRemovesConfigFiles(c *C) {
	// NOTE: Hand out a permanent snippet so that .conf file is generated.
	s.iface.PermanentSlotSnippetCallback = func(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
		return []byte("<policy/>"), nil
	}
	for _, developerMode := range []bool{true, false} {
		snapInfo := s.installSnap(c, developerMode, sambaYamlV1)
		s.removeSnap(c, snapInfo)
		profile := filepath.Join(dirs.SnapBusPolicyDir, "snap.samba.smbd.conf")
		// file called "snap.sambda.smbd.conf" was removed
		_, err := os.Stat(profile)
		c.Check(os.IsNotExist(err), Equals, true)
	}
}

func (s *backendSuite) TestUpdatingSnapToOneWithMoreApps(c *C) {
	// NOTE: Hand out a permanent snippet so that .conf file is generated.
	s.iface.PermanentSlotSnippetCallback = func(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
		return []byte("<policy/>"), nil
	}
	for _, developerMode := range []bool{true, false} {
		snapInfo := s.installSnap(c, developerMode, sambaYamlV1)
		snapInfo = s.updateSnap(c, snapInfo, developerMode, sambaYamlV1WithNmbd)
		profile := filepath.Join(dirs.SnapBusPolicyDir, "snap.samba.nmbd.conf")
		// file called "snap.sambda.nmbd.conf" was created
		_, err := os.Stat(profile)
		c.Check(err, IsNil)
		s.removeSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestUpdatingSnapToOneWithFewerApps(c *C) {
	// NOTE: Hand out a permanent snippet so that .conf file is generated.
	s.iface.PermanentSlotSnippetCallback = func(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
		return []byte("<policy/>"), nil
	}
	for _, developerMode := range []bool{true, false} {
		snapInfo := s.installSnap(c, developerMode, sambaYamlV1WithNmbd)
		snapInfo = s.updateSnap(c, snapInfo, developerMode, sambaYamlV1)
		profile := filepath.Join(dirs.SnapBusPolicyDir, "snap.samba.nmbd.conf")
		// file called "snap.sambda.nmbd.conf" was removed
		_, err := os.Stat(profile)
		c.Check(os.IsNotExist(err), Equals, true)
		s.removeSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestCombineSnippetsWithActualSnippets(c *C) {
	// NOTE: replace the real template with a shorter variant
	restore := dbus.MockXMLEnvelope([]byte("<?xml>\n"), []byte("</xml>"))
	defer restore()
	s.iface.PermanentSlotSnippetCallback = func(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
		return []byte("<policy>...</policy>"), nil
	}
	for _, developerMode := range []bool{false, true} {
		snapInfo := s.installSnap(c, developerMode, sambaYamlV1)
		profile := filepath.Join(dirs.SnapBusPolicyDir, "snap.samba.smbd.conf")
		data, err := ioutil.ReadFile(profile)
		c.Assert(err, IsNil)
		c.Check(string(data), Equals, "<?xml>\n<policy>...</policy>\n</xml>")
		stat, err := os.Stat(profile)
		c.Check(stat.Mode(), Equals, os.FileMode(0644))
		s.removeSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestCombineSnippetsWithoutAnySnippets(c *C) {
	for _, developerMode := range []bool{false, true} {
		snapInfo := s.installSnap(c, developerMode, sambaYamlV1)
		profile := filepath.Join(dirs.SnapBusPolicyDir, "snap.samba.smbd.conf")
		_, err := os.Stat(profile)
		// Without any snippets, there the .conf file is not created.
		c.Check(os.IsNotExist(err), Equals, true)
		s.removeSnap(c, snapInfo)
	}
}

const sambaYamlWithIfaceBoundToNmbd = `
name: samba
version: 1
developer: acme
apps:
    smbd:
    nmbd:
        slots: [iface]
`

func (s *backendSuite) TestAppBoundIfaces(c *C) {
	// NOTE: Hand out a permanent snippet so that .conf file is generated.
	s.iface.PermanentSlotSnippetCallback = func(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
		return []byte("<policy/>"), nil
	}
	// Install a snap with two apps, only one of which needs a .conf file
	// because the interface is app-bound.
	snapInfo := s.installSnap(c, false, sambaYamlWithIfaceBoundToNmbd)
	defer s.removeSnap(c, snapInfo)
	// Check that only one of the .conf files is actually created
	_, err := os.Stat(filepath.Join(dirs.SnapBusPolicyDir, "snap.samba.smbd.conf"))
	c.Check(os.IsNotExist(err), Equals, true)
	_, err = os.Stat(filepath.Join(dirs.SnapBusPolicyDir, "snap.samba.nmbd.conf"))
	c.Check(err, IsNil)
}

// Support code for tests

// installSnap "installs" a snap from YAML.
func (s *backendSuite) installSnap(c *C, developerMode bool, snapYaml string) *snap.Info {
	snapInfo, err := snap.InfoFromSnapYaml([]byte(snapYaml))
	c.Assert(err, IsNil)
	s.addPlugsSlots(c, snapInfo)
	err = s.backend.Setup(snapInfo, developerMode, s.repo)
	c.Assert(err, IsNil)
	return snapInfo
}

// updateSnap "updates" an existing snap from YAML.
func (s *backendSuite) updateSnap(c *C, oldSnapInfo *snap.Info, developerMode bool, snapYaml string) *snap.Info {
	newSnapInfo, err := snap.InfoFromSnapYaml([]byte(snapYaml))
	c.Assert(err, IsNil)
	c.Assert(newSnapInfo.Name(), Equals, oldSnapInfo.Name())
	s.removePlugsSlots(c, oldSnapInfo)
	s.addPlugsSlots(c, newSnapInfo)
	err = s.backend.Setup(newSnapInfo, developerMode, s.repo)
	c.Assert(err, IsNil)
	return newSnapInfo
}

// removeSnap "removes" an "installed" snap.
func (s *backendSuite) removeSnap(c *C, snapInfo *snap.Info) {
	err := s.backend.Remove(snapInfo.Name())
	c.Assert(err, IsNil)
	s.removePlugsSlots(c, snapInfo)
}

func (s *backendSuite) addPlugsSlots(c *C, snapInfo *snap.Info) {
	for _, plugInfo := range snapInfo.Plugs {
		plug := &interfaces.Plug{PlugInfo: plugInfo}
		err := s.repo.AddPlug(plug)
		c.Assert(err, IsNil)
	}
	for _, slotInfo := range snapInfo.Slots {
		slot := &interfaces.Slot{SlotInfo: slotInfo}
		err := s.repo.AddSlot(slot)
		c.Assert(err, IsNil)
	}
}

func (s *backendSuite) removePlugsSlots(c *C, snapInfo *snap.Info) {
	for _, plug := range s.repo.Plugs(snapInfo.Name()) {
		err := s.repo.RemovePlug(plug.Snap.Name(), plug.Name)
		c.Assert(err, IsNil)
	}
	for _, slot := range s.repo.Slots(snapInfo.Name()) {
		err := s.repo.RemoveSlot(slot.Snap.Name(), slot.Name)
		c.Assert(err, IsNil)
	}
}
