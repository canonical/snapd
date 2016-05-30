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

package udev_test

import (
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type backendSuite struct {
	backend    interfaces.SecurityBackend
	repo       *interfaces.Repository
	iface      *interfaces.TestInterface
	rootDir    string
	udevadmCmd *testutil.MockCmd
}

var _ = Suite(&backendSuite{backend: &udev.Backend{}})

func (s *backendSuite) SetUpTest(c *C) {
	// Isolate this test to a temporary directory
	s.rootDir = c.MkDir()
	dirs.SetRootDir(s.rootDir)
	// Mock away any real udev interaction
	s.udevadmCmd = testutil.MockCommand(c, "udevadm", "")
	// Prepare a directory for udev rules
	// NOTE: Normally this is a part of the OS snap.
	err := os.MkdirAll(dirs.SnapUdevRulesDir, 0700)
	c.Assert(err, IsNil)
	// Create a fresh repository for each test
	s.repo = interfaces.NewRepository()
	s.iface = &interfaces.TestInterface{InterfaceName: "iface"}
	err = s.repo.AddInterface(s.iface)
	c.Assert(err, IsNil)
}

func (s *backendSuite) TearDownTest(c *C) {
	s.udevadmCmd.Restore()
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
	c.Check(s.backend.Name(), Equals, "udev")
}

func (s *backendSuite) TestInstallingSnapWritesAndLoadsRules(c *C) {
	// NOTE: Hand out a permanent snippet so that .rules file is generated.
	s.iface.PermanentSlotSnippetCallback = func(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
		return []byte("dummy"), nil
	}
	for _, devMode := range []bool{true, false} {
		s.udevadmCmd.ForgetCalls()
		snapInfo := s.installSnap(c, devMode, sambaYamlV1)
		fname := filepath.Join(dirs.SnapUdevRulesDir, "70-snap.samba.smbd.rules")
		// file called "70-snap.sambda.smbd.rules" was created
		_, err := os.Stat(fname)
		c.Check(err, IsNil)
		// udevadm was used to reload rules and re-run triggers
		c.Check(s.udevadmCmd.Calls(), DeepEquals, []string{
			"control --reload-rules", "trigger",
		})
		s.removeSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestSecurityIsStable(c *C) {
	// NOTE: Hand out a permanent snippet so that .rules file is generated.
	s.iface.PermanentSlotSnippetCallback = func(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
		return []byte("dummy"), nil
	}
	for _, devMode := range []bool{true, false} {
		snapInfo := s.installSnap(c, devMode, sambaYamlV1)
		s.udevadmCmd.ForgetCalls()
		err := s.backend.Setup(snapInfo, devMode, s.repo)
		c.Assert(err, IsNil)
		// rules are not re-loaded when nothing changes
		c.Check(s.udevadmCmd.Calls(), HasLen, 0)
		s.removeSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestRemovingSnapRemovesAndReloadsRules(c *C) {
	// NOTE: Hand out a permanent snippet so that .rules file is generated.
	s.iface.PermanentSlotSnippetCallback = func(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
		return []byte("dummy"), nil
	}
	for _, devMode := range []bool{true, false} {
		snapInfo := s.installSnap(c, devMode, sambaYamlV1)
		s.udevadmCmd.ForgetCalls()
		s.removeSnap(c, snapInfo)
		fname := filepath.Join(dirs.SnapUdevRulesDir, "70-snap.samba.smbd.rules")
		// file called "70-snap.sambda.smbd.rules" was removed
		_, err := os.Stat(fname)
		c.Check(os.IsNotExist(err), Equals, true)
		// udevadm was used to reload rules and re-run triggers
		c.Check(s.udevadmCmd.Calls(), DeepEquals, []string{
			"control --reload-rules", "trigger",
		})
	}
}

func (s *backendSuite) TestUpdatingSnapToOneWithMoreApps(c *C) {
	// NOTE: Hand out a permanent snippet so that .rules file is generated.
	s.iface.PermanentSlotSnippetCallback = func(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
		return []byte("dummy"), nil
	}
	for _, devMode := range []bool{true, false} {
		snapInfo := s.installSnap(c, devMode, sambaYamlV1)
		s.udevadmCmd.ForgetCalls()
		snapInfo = s.updateSnap(c, snapInfo, devMode, sambaYamlV1WithNmbd)
		// NOTE the application is "nmbd", not "smbd"
		fname := filepath.Join(dirs.SnapUdevRulesDir, "70-snap.samba.nmbd.rules")
		// file called "70-snap.sambda.nmbd.rules" was created
		_, err := os.Stat(fname)
		c.Check(err, IsNil)
		// udevadm was used to reload rules and re-run triggers
		c.Check(s.udevadmCmd.Calls(), DeepEquals, []string{
			"control --reload-rules", "trigger",
		})
		s.removeSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestUpdatingSnapToOneWithFewerApps(c *C) {
	// NOTE: Hand out a permanent snippet so that .rules file is generated.
	s.iface.PermanentSlotSnippetCallback = func(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
		return []byte("dummy"), nil
	}
	for _, devMode := range []bool{true, false} {
		snapInfo := s.installSnap(c, devMode, sambaYamlV1WithNmbd)
		s.udevadmCmd.ForgetCalls()
		snapInfo = s.updateSnap(c, snapInfo, devMode, sambaYamlV1)
		// NOTE the application is "nmbd", not "smbd"
		fname := filepath.Join(dirs.SnapUdevRulesDir, "70-snap.samba.nmbd.rules")
		// file called "70-snap.sambda.nmbd.rules" was removed
		_, err := os.Stat(fname)
		c.Check(os.IsNotExist(err), Equals, true)
		// udevadm was used to reload rules and re-run triggers
		c.Check(s.udevadmCmd.Calls(), DeepEquals, []string{
			"control --reload-rules", "trigger",
		})
		s.removeSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestCombineSnippetsWithActualSnippets(c *C) {
	// NOTE: Hand out a permanent snippet so that .rules file is generated.
	s.iface.PermanentSlotSnippetCallback = func(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
		return []byte("dummy"), nil
	}
	for _, devMode := range []bool{false, true} {
		snapInfo := s.installSnap(c, devMode, sambaYamlV1)
		fname := filepath.Join(dirs.SnapUdevRulesDir, "70-snap.samba.smbd.rules")
		data, err := ioutil.ReadFile(fname)
		c.Assert(err, IsNil)
		c.Check(string(data), Equals, "# This file is automatically generated.\ndummy\n")
		stat, err := os.Stat(fname)
		c.Check(stat.Mode(), Equals, os.FileMode(0644))
		s.removeSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestCombineSnippetsWithoutAnySnippets(c *C) {
	for _, devMode := range []bool{false, true} {
		snapInfo := s.installSnap(c, devMode, sambaYamlV1)
		fname := filepath.Join(dirs.SnapUdevRulesDir, "70-snap.samba.smbd.rules")
		_, err := os.Stat(fname)
		// Without any snippets, there the .rules file is not created.
		c.Check(os.IsNotExist(err), Equals, true)
		s.removeSnap(c, snapInfo)
	}
}

// Support code for tests

// installSnap "installs" a snap from YAML.
func (s *backendSuite) installSnap(c *C, devMode bool, snapYaml string) *snap.Info {
	snapInfo, err := snap.InfoFromSnapYaml([]byte(snapYaml))
	c.Assert(err, IsNil)
	s.addPlugsSlots(c, snapInfo)
	err = s.backend.Setup(snapInfo, devMode, s.repo)
	c.Assert(err, IsNil)
	return snapInfo
}

// updateSnap "updates" an existing snap from YAML.
func (s *backendSuite) updateSnap(c *C, oldSnapInfo *snap.Info, devMode bool, snapYaml string) *snap.Info {
	newSnapInfo, err := snap.InfoFromSnapYaml([]byte(snapYaml))
	c.Assert(err, IsNil)
	c.Assert(newSnapInfo.Name(), Equals, oldSnapInfo.Name())
	s.removePlugsSlots(c, oldSnapInfo)
	s.addPlugsSlots(c, newSnapInfo)
	err = s.backend.Setup(newSnapInfo, devMode, s.repo)
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
