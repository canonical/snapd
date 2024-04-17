// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2018 Canonical Ltd
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
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/sandbox/cgroup"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/timings"
)

type backendSuite struct {
	ifacetest.BackendSuite

	udevadmCmd *testutil.MockCmd
	meas       *timings.Span
}

var _ = Suite(&backendSuite{})

var testedConfinementOpts = []interfaces.ConfinementOptions{
	{},
	{DevMode: true},
	{JailMode: true},
	{Classic: true},
}

func createSnippetForApps(apps map[string]*snap.AppInfo) string {
	var buffer bytes.Buffer
	for appName := range apps {
		buffer.WriteString(appName)
	}
	return buffer.String()
}

func (s *backendSuite) SetUpTest(c *C) {
	s.Backend = &udev.Backend{}

	s.BackendSuite.SetUpTest(c)
	c.Assert(s.Repo.AddBackend(s.Backend), IsNil)

	// Mock away any real udev interaction
	s.udevadmCmd = testutil.MockCommand(c, "udevadm", "")
	// Prepare a directory for udev rules
	// NOTE: Normally this is a part of the OS snap.
	err := os.MkdirAll(dirs.SnapUdevRulesDir, 0700)
	c.Assert(err, IsNil)

	perf := timings.New(nil)
	s.meas = perf.StartSpan("", "")
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
	s.Iface.UDevPermanentSlotCallback = func(spec *udev.Specification, slot *snap.SlotInfo) error {
		spec.AddSnippet("sample")
		return nil
	}
	for _, opts := range testedConfinementOpts {
		s.udevadmCmd.ForgetCalls()
		snapInfo := s.InstallSnap(c, opts, "", ifacetest.SambaYamlV1, 0)
		fname := filepath.Join(dirs.SnapUdevRulesDir, "70-snap.samba.rules")
		cgroupFname := filepath.Join(dirs.SnapCgroupPolicyDir, "snap.samba.device")
		// file called "70-snap.sambda.rules" was created
		c.Check(fname, testutil.FilePresent)
		// and the device file
		c.Check(cgroupFname, testutil.FilePresent)
		// udevadm was used to reload rules and re-run triggers
		c.Check(s.udevadmCmd.Calls(), DeepEquals, [][]string{
			{"udevadm", "control", "--reload-rules"},
			{"udevadm", "trigger", "--subsystem-nomatch=input"},
			// FIXME: temporary until spec.TriggerSubsystem() can
			// be called during disconnect
			{"udevadm", "trigger", "--property-match=ID_INPUT_JOYSTICK=1"},
			{"udevadm", "settle", "--timeout=10"},
		})
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestInstallingSnapWithHookWritesAndLoadsRules(c *C) {
	// NOTE: Hand out a permanent snippet so that .rules file is generated.
	s.Iface.UDevPermanentSlotCallback = func(spec *udev.Specification, slot *snap.SlotInfo) error {
		spec.AddSnippet("sample")
		return nil
	}
	s.Iface.UDevPermanentPlugCallback = func(spec *udev.Specification, slot *snap.PlugInfo) error {
		spec.AddSnippet("sample")
		return nil
	}
	for _, opts := range testedConfinementOpts {
		s.udevadmCmd.ForgetCalls()
		snapInfo := s.InstallSnap(c, opts, "", ifacetest.HookYaml, 0)
		fname := filepath.Join(dirs.SnapUdevRulesDir, "70-snap.foo.rules")

		// Verify that "70-snap.foo.rules" was created.
		_, err := os.Stat(fname)
		c.Check(err, IsNil)

		// Verify that udevadm was used to reload rules and re-run triggers.
		c.Check(s.udevadmCmd.Calls(), DeepEquals, [][]string{
			{"udevadm", "control", "--reload-rules"},
			{"udevadm", "trigger", "--subsystem-nomatch=input"},
			// FIXME: temporary until spec.TriggerSubsystem() can
			// be called during disconnect
			{"udevadm", "trigger", "--property-match=ID_INPUT_JOYSTICK=1"},
			{"udevadm", "settle", "--timeout=10"},
		})
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestSecurityIsStable(c *C) {
	// NOTE: Hand out a permanent snippet so that .rules file is generated.
	s.Iface.UDevPermanentSlotCallback = func(spec *udev.Specification, slot *snap.SlotInfo) error {
		spec.AddSnippet("sample")
		return nil
	}
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, "", ifacetest.SambaYamlV1, 0)
		appSet := interfaces.NewSnapAppSet(snapInfo)
		s.udevadmCmd.ForgetCalls()
		err := s.Backend.Setup(appSet, opts, s.Repo, s.meas)
		c.Assert(err, IsNil)
		// rules are not re-loaded when nothing changes
		c.Check(s.udevadmCmd.Calls(), HasLen, 0)
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestRemovingSnapRemovesAndReloadsRules(c *C) {
	// NOTE: Hand out a permanent snippet so that .rules file is generated.
	s.Iface.UDevPermanentSlotCallback = func(spec *udev.Specification, slot *snap.SlotInfo) error {
		spec.AddSnippet("sample")
		return nil
	}
	for _, opts := range testedConfinementOpts {
		fname := filepath.Join(dirs.SnapUdevRulesDir, "70-snap.samba.rules")
		cgroupFname := filepath.Join(dirs.SnapCgroupPolicyDir, "snap.samba.device")
		snapInfo := s.InstallSnap(c, opts, "", ifacetest.SambaYamlV1, 0)
		// both files present
		c.Check(fname, testutil.FilePresent)
		c.Check(cgroupFname, testutil.FilePresent)
		s.udevadmCmd.ForgetCalls()
		s.RemoveSnap(c, snapInfo)
		// file called "70-snap.sambda.rules" was removed
		c.Check(fname, testutil.FileAbsent)
		// and so was the device file
		c.Check(cgroupFname, testutil.FileAbsent)
		// udevadm was used to reload rules and re-run triggers
		c.Check(s.udevadmCmd.Calls(), DeepEquals, [][]string{
			{"udevadm", "control", "--reload-rules"},
			{"udevadm", "trigger", "--subsystem-nomatch=input"},
			// FIXME: temporary until spec.TriggerSubsystem() can
			// be called during disconnect
			{"udevadm", "trigger", "--property-match=ID_INPUT_JOYSTICK=1"},
			{"udevadm", "settle", "--timeout=10"},
		})
	}
}

func (s *backendSuite) TestUpdatingSnapToOneWithMoreApps(c *C) {
	// NOTE: Hand out a permanent snippet so that .rules file is generated.
	s.Iface.UDevPermanentSlotCallback = func(spec *udev.Specification, slot *snap.SlotInfo) error {
		spec.AddSnippet(createSnippetForApps(slot.Apps))
		return nil
	}
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, "", ifacetest.SambaYamlV1, 0)
		s.udevadmCmd.ForgetCalls()
		snapInfo = s.UpdateSnap(c, snapInfo, opts, ifacetest.SambaYamlV1WithNmbd, 0)
		fname := filepath.Join(dirs.SnapUdevRulesDir, "70-snap.samba.rules")
		// file called "70-snap.sambda.rules" was created
		_, err := os.Stat(fname)
		c.Check(err, IsNil)
		// udevadm was used to reload rules and re-run triggers
		c.Check(s.udevadmCmd.Calls(), DeepEquals, [][]string{
			{"udevadm", "control", "--reload-rules"},
			{"udevadm", "trigger", "--subsystem-nomatch=input"},
			// FIXME: temporary until spec.TriggerSubsystem() can
			// be called during disconnect
			{"udevadm", "trigger", "--property-match=ID_INPUT_JOYSTICK=1"},
			{"udevadm", "settle", "--timeout=10"},
		})
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestUpdatingSnapToOneWithMoreHooks(c *C) {
	// NOTE: Hand out a permanent snippet so that .rules file is generated.
	s.Iface.UDevPermanentSlotCallback = func(spec *udev.Specification, slot *snap.SlotInfo) error {
		spec.AddSnippet(createSnippetForApps(slot.Apps))
		return nil
	}
	s.Iface.UDevPermanentPlugCallback = func(spec *udev.Specification, slot *snap.PlugInfo) error {
		spec.AddSnippet("sample")
		return nil
	}
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, "", ifacetest.SambaYamlV1, 0)
		s.udevadmCmd.ForgetCalls()
		snapInfo = s.UpdateSnap(c, snapInfo, opts, ifacetest.SambaYamlWithHook, 0)
		fname := filepath.Join(dirs.SnapUdevRulesDir, "70-snap.samba.rules")

		// Verify that "70-snap.samba.rules" was created
		_, err := os.Stat(fname)
		c.Check(err, IsNil)

		// Verify that udevadm was used to reload rules and re-run triggers
		c.Check(s.udevadmCmd.Calls(), DeepEquals, [][]string{
			{"udevadm", "control", "--reload-rules"},
			{"udevadm", "trigger", "--subsystem-nomatch=input"},
			// FIXME: temporary until spec.TriggerSubsystem() can
			// be called during disconnect
			{"udevadm", "trigger", "--property-match=ID_INPUT_JOYSTICK=1"},
			{"udevadm", "settle", "--timeout=10"},
		})
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestUpdatingSnapToOneWithFewerApps(c *C) {
	// NOTE: Hand out a permanent snippet so that .rules file is generated.
	s.Iface.UDevPermanentSlotCallback = func(spec *udev.Specification, slot *snap.SlotInfo) error {
		spec.AddSnippet(createSnippetForApps(slot.Apps))
		return nil
	}
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, "", ifacetest.SambaYamlV1WithNmbd, 0)
		s.udevadmCmd.ForgetCalls()
		snapInfo = s.UpdateSnap(c, snapInfo, opts, ifacetest.SambaYamlV1, 0)
		fname := filepath.Join(dirs.SnapUdevRulesDir, "70-snap.samba.rules")
		// file called "70-snap.sambda.rules" still exists
		_, err := os.Stat(fname)
		c.Check(err, IsNil)
		// udevadm was used to reload rules and re-run triggers
		c.Check(s.udevadmCmd.Calls(), DeepEquals, [][]string{
			{"udevadm", "control", "--reload-rules"},
			{"udevadm", "trigger", "--subsystem-nomatch=input"},
			// FIXME: temporary until spec.TriggerSubsystem() can
			// be called during disconnect
			{"udevadm", "trigger", "--property-match=ID_INPUT_JOYSTICK=1"},
			{"udevadm", "settle", "--timeout=10"},
		})
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestUpdatingSnapToOneWithFewerHooks(c *C) {
	// NOTE: Hand out a permanent snippet so that .rules file is generated.
	s.Iface.UDevPermanentSlotCallback = func(spec *udev.Specification, slot *snap.SlotInfo) error {
		spec.AddSnippet(createSnippetForApps(slot.Apps))
		return nil
	}
	s.Iface.UDevPermanentPlugCallback = func(spec *udev.Specification, slot *snap.PlugInfo) error {
		spec.AddSnippet("sample")
		return nil
	}
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, "", ifacetest.SambaYamlWithHook, 0)
		s.udevadmCmd.ForgetCalls()
		snapInfo = s.UpdateSnap(c, snapInfo, opts, ifacetest.SambaYamlV1, 0)
		fname := filepath.Join(dirs.SnapUdevRulesDir, "70-snap.samba.rules")
		// file called "70-snap.sambda.rules" still exists
		_, err := os.Stat(fname)
		c.Check(err, IsNil)
		// Verify that udevadm was used to reload rules and re-run triggers
		c.Check(s.udevadmCmd.Calls(), DeepEquals, [][]string{
			{"udevadm", "control", "--reload-rules"},
			{"udevadm", "trigger", "--subsystem-nomatch=input"},
			// FIXME: temporary until spec.TriggerSubsystem() can
			// be called during disconnect
			{"udevadm", "trigger", "--property-match=ID_INPUT_JOYSTICK=1"},
			{"udevadm", "settle", "--timeout=10"},
		})
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestCombineSnippetsWithActualSnippets(c *C) {
	// NOTE: Hand out a permanent snippet so that .rules file is generated.
	s.Iface.UDevPermanentSlotCallback = func(spec *udev.Specification, slot *snap.SlotInfo) error {
		spec.AddSnippet("sample")
		return nil
	}
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, "", ifacetest.SambaYamlV1, 0)
		fname := filepath.Join(dirs.SnapUdevRulesDir, "70-snap.samba.rules")
		if opts.DevMode || opts.Classic {
			c.Check(fname, testutil.FileEquals, "# This file is automatically generated.\n# udev tagging/device cgroups disabled with non-strict mode snaps\n#sample\n")
		} else {
			c.Check(fname, testutil.FileEquals, "# This file is automatically generated.\nsample\n")
		}
		stat, err := os.Stat(fname)
		c.Assert(err, IsNil)
		c.Check(stat.Mode(), Equals, os.FileMode(0644))

		cgroupFname := filepath.Join(dirs.SnapCgroupPolicyDir, "snap.samba.device")
		if !opts.DevMode && !opts.Classic {
			c.Check(cgroupFname, testutil.FileEquals, "# This file is automatically generated.\n")
		} else {
			c.Check(cgroupFname, testutil.FileEquals, "# This file is automatically generated.\n"+
				"# snap uses non-strict confinement.\n"+
				"non-strict=true\n",
			)
		}

		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestControlsDeviceCgroup(c *C) {
	// NOTE: Hand out a permanent snippet so that .rules file is generated.
	s.Iface.UDevPermanentSlotCallback = func(spec *udev.Specification, slot *snap.SlotInfo) error {
		spec.AddSnippet("sample")
		spec.SetControlsDeviceCgroup()
		return nil
	}
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, "", ifacetest.SambaYamlV1, 0)
		fname := filepath.Join(dirs.SnapUdevRulesDir, "70-snap.samba.rules")
		c.Check(fname, testutil.FileAbsent)
		cgroupFname := filepath.Join(dirs.SnapCgroupPolicyDir, "snap.samba.device")
		if !opts.DevMode && !opts.Classic {
			c.Check(cgroupFname, testutil.FileEquals, "# This file is automatically generated.\n"+
				"# snap is allowed to manage own device cgroup.\n"+
				"self-managed=true\n",
			)
		} else {
			c.Check(cgroupFname, testutil.FileEquals, "# This file is automatically generated.\n"+
				"# snap is allowed to manage own device cgroup.\n"+
				"self-managed=true\n"+
				"# snap uses non-strict confinement.\n"+
				"non-strict=true\n",
			)
		}
		c.Check(s.udevadmCmd.Calls(), HasLen, 0)
		s.RemoveSnap(c, snapInfo)
		c.Check(cgroupFname, testutil.FileAbsent)
		c.Check(s.udevadmCmd.Calls(), HasLen, 0)
	}
}

func (s *backendSuite) TestControlsDeviceCgroupCleansUpRules(c *C) {
	// NOTE: Hand out a permanent snippet so that .rules file is generated.
	s.Iface.UDevPermanentSlotCallback = func(spec *udev.Specification, slot *snap.SlotInfo) error {
		spec.AddSnippet("sample")
		spec.SetControlsDeviceCgroup()
		return nil
	}
	fname := filepath.Join(dirs.SnapUdevRulesDir, "70-snap.samba.rules")
	c.Assert(os.MkdirAll(dirs.SnapUdevRulesDir, 0755), IsNil)
	c.Assert(os.WriteFile(fname, nil, 0644), IsNil)
	c.Check(fname, testutil.FilePresent)
	snapInfo := s.InstallSnap(c, interfaces.ConfinementOptions{}, "", ifacetest.SambaYamlV1, 0)
	// rules file got removed
	c.Check(fname, testutil.FileAbsent)
	// since rules were removed, udev was triggered
	c.Check(s.udevadmCmd.Calls(), DeepEquals, [][]string{
		{"udevadm", "control", "--reload-rules"},
		{"udevadm", "trigger", "--subsystem-nomatch=input"},
		{"udevadm", "trigger", "--property-match=ID_INPUT_JOYSTICK=1"},
		{"udevadm", "settle", "--timeout=10"},
	})
	// and we have the cgroup flag
	cgroupFname := filepath.Join(dirs.SnapCgroupPolicyDir, "snap.samba.device")
	c.Check(cgroupFname, testutil.FileEquals, "# This file is automatically generated.\n"+
		"# snap is allowed to manage own device cgroup.\n"+
		"self-managed=true\n",
	)
	s.udevadmCmd.ForgetCalls()
	// remove
	s.RemoveSnap(c, snapInfo)
	c.Check(cgroupFname, testutil.FileAbsent)
	// no calls to udev this time
	c.Check(s.udevadmCmd.Calls(), HasLen, 0)
}

func (s *backendSuite) TestDeviceCgroupAlwaysPresent(c *C) {
	// NOTE: Hand out a permanent snippet so that .rules file is generated.
	s.Iface.UDevPermanentSlotCallback = func(spec *udev.Specification, slot *snap.SlotInfo) error {
		spec.AddSnippet("sample")
		return nil
	}
	cgroupFname := filepath.Join(dirs.SnapCgroupPolicyDir, "snap.samba.device")
	c.Assert(os.MkdirAll(dirs.SnapCgroupPolicyDir, 0755), IsNil)
	c.Assert(os.WriteFile(cgroupFname, nil, 0644), IsNil)
	c.Check(cgroupFname, testutil.FilePresent)
	fname := filepath.Join(dirs.SnapUdevRulesDir, "70-snap.samba.rules")
	snapInfo := s.InstallSnap(c, interfaces.ConfinementOptions{}, "", ifacetest.SambaYamlV1, 0)
	// device cgroup self manage flag is gone now
	c.Check(cgroupFname, testutil.FileEquals, "# This file is automatically generated.\n")
	// and we have the rules file
	c.Check(fname, testutil.FileEquals, "# This file is automatically generated.\nsample\n")
	// and udev was called
	c.Check(s.udevadmCmd.Calls(), DeepEquals, [][]string{
		{"udevadm", "control", "--reload-rules"},
		{"udevadm", "trigger", "--subsystem-nomatch=input"},
		{"udevadm", "trigger", "--property-match=ID_INPUT_JOYSTICK=1"},
		{"udevadm", "settle", "--timeout=10"},
	})

	s.RemoveSnap(c, snapInfo)
}

func (s *backendSuite) TestCombineSnippetsWithActualSnippetsWithNewline(c *C) {
	// NOTE: Hand out a permanent snippet so that .rules file is generated.
	s.Iface.UDevPermanentSlotCallback = func(spec *udev.Specification, slot *snap.SlotInfo) error {
		spec.AddSnippet("sample1\nsample2")
		return nil
	}
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, "", ifacetest.SambaYamlV1, 0)
		fname := filepath.Join(dirs.SnapUdevRulesDir, "70-snap.samba.rules")
		if opts.DevMode || opts.Classic {
			c.Check(fname, testutil.FileEquals, "# This file is automatically generated.\n# udev tagging/device cgroups disabled with non-strict mode snaps\n#sample1\n#sample2\n")
		} else {
			c.Check(fname, testutil.FileEquals, "# This file is automatically generated.\nsample1\nsample2\n")
		}
		stat, err := os.Stat(fname)
		c.Assert(err, IsNil)
		c.Check(stat.Mode(), Equals, os.FileMode(0644))
		s.RemoveSnap(c, snapInfo)
	}
}
func (s *backendSuite) TestCombineSnippetsWithActualSnippetsWhenPlugNoApps(c *C) {
	// NOTE: Hand out a permanent snippet so that .rules file is generated.
	s.Iface.UDevPermanentPlugCallback = func(spec *udev.Specification, slot *snap.PlugInfo) error {
		spec.AddSnippet("sample")
		return nil
	}
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, "", ifacetest.PlugNoAppsYaml, 0)
		fname := filepath.Join(dirs.SnapUdevRulesDir, "70-snap.foo.rules")
		if opts.DevMode || opts.Classic {
			c.Check(fname, testutil.FileEquals, "# This file is automatically generated.\n# udev tagging/device cgroups disabled with non-strict mode snaps\n#sample\n")
		} else {
			c.Check(fname, testutil.FileEquals, "# This file is automatically generated.\nsample\n")
		}
		stat, err := os.Stat(fname)
		c.Assert(err, IsNil)
		c.Check(stat.Mode(), Equals, os.FileMode(0644))
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestCombineSnippetsWithActualSnippetsWhenSlotNoApps(c *C) {
	// NOTE: Hand out a permanent snippet so that .rules file is generated.
	s.Iface.UDevPermanentSlotCallback = func(spec *udev.Specification, slot *snap.SlotInfo) error {
		spec.AddSnippet("sample")
		return nil
	}
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, "", ifacetest.SlotNoAppsYaml, 0)
		fname := filepath.Join(dirs.SnapUdevRulesDir, "70-snap.foo.rules")
		if opts.DevMode || opts.Classic {
			c.Check(fname, testutil.FileEquals, "# This file is automatically generated.\n# udev tagging/device cgroups disabled with non-strict mode snaps\n#sample\n")
		} else {
			c.Check(fname, testutil.FileEquals, "# This file is automatically generated.\nsample\n")
		}
		stat, err := os.Stat(fname)
		c.Assert(err, IsNil)
		c.Check(stat.Mode(), Equals, os.FileMode(0644))
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestCombineSnippetsWithoutAnySnippets(c *C) {
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, "", ifacetest.SambaYamlV1, 0)
		fname := filepath.Join(dirs.SnapUdevRulesDir, "70-snap.samba.rules")
		_, err := os.Stat(fname)
		// Without any snippets, there the .rules file is not created.
		c.Check(os.IsNotExist(err), Equals, true)
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestUpdatingSnapToOneWithoutSlots(c *C) {
	// NOTE: Hand out a permanent snippet so that .rules file is generated.
	s.Iface.UDevPermanentSlotCallback = func(spec *udev.Specification, slot *snap.SlotInfo) error {
		spec.AddSnippet("sample")
		return nil
	}
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, "", ifacetest.SambaYamlV1, 0)
		s.udevadmCmd.ForgetCalls()
		snapInfo = s.UpdateSnap(c, snapInfo, opts, ifacetest.SambaYamlV1NoSlot, 0)
		fname := filepath.Join(dirs.SnapUdevRulesDir, "70-snap.samba.rules")
		// file called "70-snap.sambda.rules" was removed
		_, err := os.Stat(fname)
		c.Check(os.IsNotExist(err), Equals, true)
		// Verify that udevadm was used to reload rules and re-run triggers
		c.Check(s.udevadmCmd.Calls(), DeepEquals, [][]string{
			{"udevadm", "control", "--reload-rules"},
			{"udevadm", "trigger", "--subsystem-nomatch=input"},
			// FIXME: temporary until spec.TriggerSubsystem() can
			// be called during disconnect
			{"udevadm", "trigger", "--property-match=ID_INPUT_JOYSTICK=1"},
			{"udevadm", "settle", "--timeout=10"},
		})
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestUpdatingSnapWithoutSlotsToOneWithoutSlots(c *C) {
	// NOTE: Hand out a permanent snippet so that .rules file is generated.
	s.Iface.UDevPermanentSlotCallback = func(spec *udev.Specification, slot *snap.SlotInfo) error {
		spec.AddSnippet("sample")
		return nil
	}
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, "", ifacetest.SambaYamlV1NoSlot, 0)
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

func (s *backendSuite) TestInstallingSnapWritesAndLoadsRulesWithInputSubsystem(c *C) {
	// NOTE: Hand out a permanent snippet so that .rules file is generated.
	s.Iface.UDevPermanentSlotCallback = func(spec *udev.Specification, slot *snap.SlotInfo) error {
		spec.TriggerSubsystem("input")
		spec.AddSnippet("sample")
		return nil
	}
	for _, opts := range testedConfinementOpts {
		s.udevadmCmd.ForgetCalls()
		snapInfo := s.InstallSnap(c, opts, "", ifacetest.SambaYamlV1, 0)
		fname := filepath.Join(dirs.SnapUdevRulesDir, "70-snap.samba.rules")
		// file called "70-snap.sambda.rules" was created
		_, err := os.Stat(fname)
		c.Check(err, IsNil)
		// udevadm was used to reload rules and re-run triggers
		c.Check(s.udevadmCmd.Calls(), DeepEquals, [][]string{
			{"udevadm", "control", "--reload-rules"},
			{"udevadm", "trigger", "--subsystem-nomatch=input"},
			{"udevadm", "trigger", "--subsystem-match=input"},
			{"udevadm", "settle", "--timeout=10"},
		})
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestInstallingSnapWritesAndLoadsRulesWithInputJoystickSubsystem(c *C) {
	// NOTE: Hand out a permanent snippet so that .rules file is generated.
	s.Iface.UDevPermanentSlotCallback = func(spec *udev.Specification, slot *snap.SlotInfo) error {
		spec.TriggerSubsystem("input/joystick")
		spec.AddSnippet("sample")
		return nil
	}
	for _, opts := range testedConfinementOpts {
		s.udevadmCmd.ForgetCalls()
		snapInfo := s.InstallSnap(c, opts, "", ifacetest.SambaYamlV1, 0)
		fname := filepath.Join(dirs.SnapUdevRulesDir, "70-snap.samba.rules")
		// file called "70-snap.sambda.rules" was created
		_, err := os.Stat(fname)
		c.Check(err, IsNil)
		// udevadm was used to reload rules and re-run triggers
		c.Check(s.udevadmCmd.Calls(), DeepEquals, [][]string{
			{"udevadm", "control", "--reload-rules"},
			{"udevadm", "trigger", "--subsystem-nomatch=input"},
			{"udevadm", "trigger", "--property-match=ID_INPUT_JOYSTICK=1"},
			{"udevadm", "settle", "--timeout=10"},
		})
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestSandboxFeatures(c *C) {
	restore := cgroup.MockVersion(cgroup.V1, nil)
	defer restore()

	c.Assert(s.Backend.SandboxFeatures(), DeepEquals, []string{
		"tagging",
		"device-filtering",
		"device-cgroup-v1",
	})

	restore = cgroup.MockVersion(cgroup.V2, nil)
	defer restore()
	c.Assert(s.Backend.SandboxFeatures(), DeepEquals, []string{
		"tagging",
		"device-filtering",
		"device-cgroup-v2",
	})
}

func (s *backendSuite) TestPreseed(c *C) {
	err := s.Backend.Initialize(&interfaces.SecurityBackendOptions{
		Preseed: true,
	})
	c.Assert(err, IsNil)

	// NOTE: Hand out a permanent snippet so that .rules file is generated.
	s.Iface.UDevPermanentSlotCallback = func(spec *udev.Specification, slot *snap.SlotInfo) error {
		spec.AddSnippet("sample")
		return nil
	}
	cgroupFname := filepath.Join(dirs.SnapCgroupPolicyDir, "snap.samba.device")
	c.Assert(os.MkdirAll(dirs.SnapCgroupPolicyDir, 0755), IsNil)
	c.Assert(os.WriteFile(cgroupFname, nil, 0644), IsNil)
	c.Check(cgroupFname, testutil.FilePresent)
	fname := filepath.Join(dirs.SnapUdevRulesDir, "70-snap.samba.rules")
	s.InstallSnap(c, interfaces.ConfinementOptions{}, "", ifacetest.SambaYamlV1, 0)
	// device cgroup self manage flag is gone now
	c.Check(cgroupFname, testutil.FileEquals, "# This file is automatically generated.\n")
	// and we have the rules file
	c.Check(fname, testutil.FileEquals, "# This file is automatically generated.\nsample\n")

	c.Check(s.udevadmCmd.Calls(), HasLen, 0)
}
