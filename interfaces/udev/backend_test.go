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
	"bytes"
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type backendSuite struct {
	ifacetest.BackendSuite

	udevadmCmd *testutil.MockCmd
}

var _ = Suite(&backendSuite{})

var testedConfinementOpts = []interfaces.ConfinementOptions{
	{},
	{DevMode: true},
	{JailMode: true},
	{Classic: true},
}

func createSnippetForApps(apps map[string]*snap.AppInfo) []byte {
	var buffer bytes.Buffer
	for appName := range apps {
		buffer.WriteString(appName)
	}
	return buffer.Bytes()
}

func (s *backendSuite) SetUpTest(c *C) {
	s.Backend = &udev.Backend{}

	s.BackendSuite.SetUpTest(c)

	// Mock away any real udev interaction
	s.udevadmCmd = testutil.MockCommand(c, "udevadm", "")
	// Prepare a directory for udev rules
	// NOTE: Normally this is a part of the OS snap.
	err := os.MkdirAll(dirs.SnapUdevRulesDir, 0700)
	c.Assert(err, IsNil)
}

func (s *backendSuite) TearDownTest(c *C) {
	s.udevadmCmd.Restore()

	s.BackendSuite.TearDownTest(c)
}

// Tests for Setup() and Remove()
func (s *backendSuite) TestName(c *C) {
	c.Check(s.Backend.Name(), Equals, interfaces.SecurityUDev)
}

func (s *backendSuite) TestInstallingSnapWritesAndLoadsRules(c *C) {
	// NOTE: Hand out a permanent snippet so that .rules file is generated.
	s.Iface.PermanentSlotSnippetCallback = func(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
		return []byte("dummy"), nil
	}
	for _, opts := range testedConfinementOpts {
		s.udevadmCmd.ForgetCalls()
		snapInfo := s.InstallSnap(c, opts, ifacetest.SambaYamlV1, 0)
		fname := filepath.Join(dirs.SnapUdevRulesDir, "70-snap.samba.rules")
		// file called "70-snap.sambda.rules" was created
		_, err := os.Stat(fname)
		c.Check(err, IsNil)
		// udevadm was used to reload rules and re-run triggers
		c.Check(s.udevadmCmd.Calls(), DeepEquals, [][]string{
			{"udevadm", "control", "--reload-rules"},
			{"udevadm", "trigger"},
		})
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestInstallingSnapWithHookWritesAndLoadsRules(c *C) {
	// NOTE: Hand out a permanent snippet so that .rules file is generated.
	s.Iface.PermanentSlotSnippetCallback = func(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
		return []byte("dummy"), nil
	}
	s.Iface.PermanentPlugSnippetCallback = func(slot *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
		return []byte("dummy"), nil
	}
	for _, opts := range testedConfinementOpts {
		s.udevadmCmd.ForgetCalls()
		snapInfo := s.InstallSnap(c, opts, ifacetest.HookYaml, 0)
		fname := filepath.Join(dirs.SnapUdevRulesDir, "70-snap.foo.rules")

		// Verify that "70-snap.foo.rules" was created.
		_, err := os.Stat(fname)
		c.Check(err, IsNil)

		// Verify that udevadm was used to reload rules and re-run triggers.
		c.Check(s.udevadmCmd.Calls(), DeepEquals, [][]string{
			{"udevadm", "control", "--reload-rules"},
			{"udevadm", "trigger"},
		})
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestSecurityIsStable(c *C) {
	// NOTE: Hand out a permanent snippet so that .rules file is generated.
	s.Iface.PermanentSlotSnippetCallback = func(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
		return []byte("dummy"), nil
	}
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, ifacetest.SambaYamlV1, 0)
		s.udevadmCmd.ForgetCalls()
		err := s.Backend.Setup(snapInfo, opts, s.Repo)
		c.Assert(err, IsNil)
		// rules are not re-loaded when nothing changes
		c.Check(s.udevadmCmd.Calls(), HasLen, 0)
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestRemovingSnapRemovesAndReloadsRules(c *C) {
	// NOTE: Hand out a permanent snippet so that .rules file is generated.
	s.Iface.PermanentSlotSnippetCallback = func(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
		return []byte("dummy"), nil
	}
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, ifacetest.SambaYamlV1, 0)
		s.udevadmCmd.ForgetCalls()
		s.RemoveSnap(c, snapInfo)
		fname := filepath.Join(dirs.SnapUdevRulesDir, "70-snap.samba.rules")
		// file called "70-snap.sambda.rules" was removed
		_, err := os.Stat(fname)
		c.Check(os.IsNotExist(err), Equals, true)
		// udevadm was used to reload rules and re-run triggers
		c.Check(s.udevadmCmd.Calls(), DeepEquals, [][]string{
			{"udevadm", "control", "--reload-rules"},
			{"udevadm", "trigger"},
		})
	}
}

func (s *backendSuite) TestUpdatingSnapToOneWithMoreApps(c *C) {
	// NOTE: Hand out a permanent snippet so that .rules file is generated.
	s.Iface.PermanentSlotSnippetCallback = func(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
		return createSnippetForApps(slot.Apps), nil
	}
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, ifacetest.SambaYamlV1, 0)
		s.udevadmCmd.ForgetCalls()
		snapInfo = s.UpdateSnap(c, snapInfo, opts, ifacetest.SambaYamlV1WithNmbd, 0)
		fname := filepath.Join(dirs.SnapUdevRulesDir, "70-snap.samba.rules")
		// file called "70-snap.sambda.rules" was created
		_, err := os.Stat(fname)
		c.Check(err, IsNil)
		// udevadm was used to reload rules and re-run triggers
		c.Check(s.udevadmCmd.Calls(), DeepEquals, [][]string{
			{"udevadm", "control", "--reload-rules"},
			{"udevadm", "trigger"},
		})
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestUpdatingSnapToOneWithMoreHooks(c *C) {
	// NOTE: Hand out a permanent snippet so that .rules file is generated.
	s.Iface.PermanentSlotSnippetCallback = func(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
		return createSnippetForApps(slot.Apps), nil
	}
	s.Iface.PermanentPlugSnippetCallback = func(slot *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
		return []byte("dummy"), nil
	}
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, ifacetest.SambaYamlV1, 0)
		s.udevadmCmd.ForgetCalls()
		snapInfo = s.UpdateSnap(c, snapInfo, opts, ifacetest.SambaYamlWithHook, 0)
		fname := filepath.Join(dirs.SnapUdevRulesDir, "70-snap.samba.rules")

		// Verify that "70-snap.samba.rules" was created
		_, err := os.Stat(fname)
		c.Check(err, IsNil)

		// Verify that udevadm was used to reload rules and re-run triggers
		c.Check(s.udevadmCmd.Calls(), DeepEquals, [][]string{
			{"udevadm", "control", "--reload-rules"},
			{"udevadm", "trigger"},
		})
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestUpdatingSnapToOneWithFewerApps(c *C) {
	// NOTE: Hand out a permanent snippet so that .rules file is generated.
	s.Iface.PermanentSlotSnippetCallback = func(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
		return createSnippetForApps(slot.Apps), nil
	}
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, ifacetest.SambaYamlV1WithNmbd, 0)
		s.udevadmCmd.ForgetCalls()
		snapInfo = s.UpdateSnap(c, snapInfo, opts, ifacetest.SambaYamlV1, 0)
		fname := filepath.Join(dirs.SnapUdevRulesDir, "70-snap.samba.rules")
		// file called "70-snap.sambda.rules" still exists
		_, err := os.Stat(fname)
		c.Check(err, IsNil)
		// udevadm was used to reload rules and re-run triggers
		c.Check(s.udevadmCmd.Calls(), DeepEquals, [][]string{
			{"udevadm", "control", "--reload-rules"},
			{"udevadm", "trigger"},
		})
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestUpdatingSnapToOneWithFewerHooks(c *C) {
	// NOTE: Hand out a permanent snippet so that .rules file is generated.
	s.Iface.PermanentSlotSnippetCallback = func(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
		return createSnippetForApps(slot.Apps), nil
	}
	s.Iface.PermanentPlugSnippetCallback = func(slot *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
		return []byte("dummy"), nil
	}
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, ifacetest.SambaYamlWithHook, 0)
		s.udevadmCmd.ForgetCalls()
		snapInfo = s.UpdateSnap(c, snapInfo, opts, ifacetest.SambaYamlV1, 0)
		fname := filepath.Join(dirs.SnapUdevRulesDir, "70-snap.samba.rules")
		// file called "70-snap.sambda.rules" still exists
		_, err := os.Stat(fname)
		c.Check(err, IsNil)
		// Verify that udevadm was used to reload rules and re-run triggers
		c.Check(s.udevadmCmd.Calls(), DeepEquals, [][]string{
			{"udevadm", "control", "--reload-rules"},
			{"udevadm", "trigger"},
		})
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestCombineSnippetsWithActualSnippets(c *C) {
	// NOTE: Hand out a permanent snippet so that .rules file is generated.
	s.Iface.PermanentSlotSnippetCallback = func(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
		return []byte("dummy"), nil
	}
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, ifacetest.SambaYamlV1, 0)
		fname := filepath.Join(dirs.SnapUdevRulesDir, "70-snap.samba.rules")
		data, err := ioutil.ReadFile(fname)
		c.Assert(err, IsNil)
		c.Check(string(data), Equals, "# This file is automatically generated.\ndummy\n")
		stat, err := os.Stat(fname)
		c.Assert(err, IsNil)
		c.Check(stat.Mode(), Equals, os.FileMode(0644))
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestCombineSnippetsWithActualSnippetsWhenPlugNoApps(c *C) {
	// NOTE: Hand out a permanent snippet so that .rules file is generated.
	s.Iface.PermanentPlugSnippetCallback = func(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
		return []byte("dummy"), nil
	}
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, ifacetest.PlugNoAppsYaml, 0)
		fname := filepath.Join(dirs.SnapUdevRulesDir, "70-snap.foo.rules")
		data, err := ioutil.ReadFile(fname)
		c.Assert(err, IsNil)
		c.Check(string(data), Equals, "# This file is automatically generated.\ndummy\n")
		stat, err := os.Stat(fname)
		c.Assert(err, IsNil)
		c.Check(stat.Mode(), Equals, os.FileMode(0644))
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestCombineSnippetsWithActualSnippetsWhenSlotNoApps(c *C) {
	// NOTE: Hand out a permanent snippet so that .rules file is generated.
	s.Iface.PermanentSlotSnippetCallback = func(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
		return []byte("dummy"), nil
	}
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, ifacetest.SlotNoAppsYaml, 0)
		fname := filepath.Join(dirs.SnapUdevRulesDir, "70-snap.foo.rules")
		data, err := ioutil.ReadFile(fname)
		c.Assert(err, IsNil)
		c.Check(string(data), Equals, "# This file is automatically generated.\ndummy\n")
		stat, err := os.Stat(fname)
		c.Assert(err, IsNil)
		c.Check(stat.Mode(), Equals, os.FileMode(0644))
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestCombineSnippetsWithoutAnySnippets(c *C) {
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, ifacetest.SambaYamlV1, 0)
		fname := filepath.Join(dirs.SnapUdevRulesDir, "70-snap.samba.rules")
		_, err := os.Stat(fname)
		// Without any snippets, there the .rules file is not created.
		c.Check(os.IsNotExist(err), Equals, true)
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestUpdatingSnapToOneWithoutSlots(c *C) {
	// NOTE: Hand out a permanent snippet so that .rules file is generated.
	s.Iface.PermanentSlotSnippetCallback = func(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
		return []byte("dummy"), nil
	}
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, ifacetest.SambaYamlV1, 0)
		s.udevadmCmd.ForgetCalls()
		snapInfo = s.UpdateSnap(c, snapInfo, opts, ifacetest.SambaYamlV1NoSlot, 0)
		fname := filepath.Join(dirs.SnapUdevRulesDir, "70-snap.samba.rules")
		// file called "70-snap.sambda.rules" was removed
		_, err := os.Stat(fname)
		c.Check(os.IsNotExist(err), Equals, true)
		// Verify that udevadm was used to reload rules and re-run triggers
		c.Check(s.udevadmCmd.Calls(), DeepEquals, [][]string{
			{"udevadm", "control", "--reload-rules"},
			{"udevadm", "trigger"},
		})
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestUpdatingSnapWithoutSlotsToOneWithoutSlots(c *C) {
	// NOTE: Hand out a permanent snippet so that .rules file is generated.
	s.Iface.PermanentSlotSnippetCallback = func(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
		return []byte("dummy"), nil
	}
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, ifacetest.SambaYamlV1NoSlot, 0)
		// file called "70-snap.sambda.rules" does not exist
		fname := filepath.Join(dirs.SnapUdevRulesDir, "70-snap.samba.rules")
		_, err := os.Stat(fname)
		c.Check(os.IsNotExist(err), Equals, true)
		s.udevadmCmd.ForgetCalls()

		snapInfo = s.UpdateSnap(c, snapInfo, opts, ifacetest.SambaYamlV1WithNmbdNoSlot, 0)
		// file called "70-snap.sambda.rules" still does not exist
		_, err = os.Stat(fname)
		c.Check(os.IsNotExist(err), Equals, true)
		// Verify that udevadm was used to reload rules and re-run triggers
		c.Check(len(s.udevadmCmd.Calls()), Equals, 0)
		s.RemoveSnap(c, snapInfo)
	}
}
