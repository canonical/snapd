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
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/dbus"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type backendSuite struct {
	ifacetest.BackendSuite
}

var _ = Suite(&backendSuite{})

var testedConfinementOpts = []interfaces.ConfinementOptions{
	{},
	{DevMode: true},
	{JailMode: true},
	{Classic: true},
}

func (s *backendSuite) SetUpTest(c *C) {
	s.Backend = &dbus.Backend{}
	s.BackendSuite.SetUpTest(c)
	c.Assert(s.Repo.AddBackend(s.Backend), IsNil)

	// Prepare a directory for DBus configuration files.
	// NOTE: Normally this is a part of the OS snap.
	err := os.MkdirAll(dirs.SnapBusPolicyDir, 0700)
	c.Assert(err, IsNil)
}

func (s *backendSuite) TearDownTest(c *C) {
	s.BackendSuite.TearDownTest(c)
}

// Tests for Setup() and Remove()
func (s *backendSuite) TestName(c *C) {
	c.Check(s.Backend.Name(), Equals, interfaces.SecurityDBus)
}

func (s *backendSuite) TestInstallingSnapWritesConfigFiles(c *C) {
	// NOTE: Hand out a permanent snippet so that .conf file is generated.
	s.Iface.DBusPermanentSlotCallback = func(spec *dbus.Specification, slot *snap.SlotInfo) error {
		spec.AddSnippet("<policy/>")
		return nil
	}
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, "samba", ifacetest.SambaYamlV1, 0)
		profile := filepath.Join(dirs.SnapBusPolicyDir, "snap.samba.smbd.conf")
		// file called "snap.sambda.smbd.conf" was created
		_, err := os.Stat(profile)
		c.Check(err, IsNil)
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestInstallingSnapWithHookWritesConfigFiles(c *C) {
	// NOTE: Hand out a permanent snippet so that .conf file is generated.
	s.Iface.DBusPermanentSlotCallback = func(spec *dbus.Specification, slot *snap.SlotInfo) error {
		spec.AddSnippet("<policy/>")
		return nil
	}
	s.Iface.DBusPermanentPlugCallback = func(spec *dbus.Specification, plug *snap.PlugInfo) error {
		spec.AddSnippet("<policy/>")
		return nil
	}
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, "foo", ifacetest.HookYaml, 0)
		profile := filepath.Join(dirs.SnapBusPolicyDir, "snap.foo.hook.configure.conf")

		// Verify that "snap.foo.hook.configure.conf" was created
		_, err := os.Stat(profile)
		c.Check(err, IsNil)
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestRemovingSnapRemovesConfigFiles(c *C) {
	// NOTE: Hand out a permanent snippet so that .conf file is generated.
	s.Iface.DBusPermanentSlotCallback = func(spec *dbus.Specification, slot *snap.SlotInfo) error {
		spec.AddSnippet("<policy/>")
		return nil
	}
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, "samba", ifacetest.SambaYamlV1, 0)
		s.RemoveSnap(c, snapInfo)
		profile := filepath.Join(dirs.SnapBusPolicyDir, "snap.samba.smbd.conf")
		// file called "snap.sambda.smbd.conf" was removed
		_, err := os.Stat(profile)
		c.Check(os.IsNotExist(err), Equals, true)
	}
}

func (s *backendSuite) TestRemovingSnapWithHookRemovesConfigFiles(c *C) {
	// NOTE: Hand out a permanent snippet so that .conf file is generated.
	s.Iface.DBusPermanentSlotCallback = func(spec *dbus.Specification, slot *snap.SlotInfo) error {
		spec.AddSnippet("<policy/>")
		return nil
	}
	s.Iface.DBusPermanentPlugCallback = func(spec *dbus.Specification, plug *snap.PlugInfo) error {
		spec.AddSnippet("<policy/>")
		return nil
	}
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, "foo", ifacetest.HookYaml, 0)
		s.RemoveSnap(c, snapInfo)
		profile := filepath.Join(dirs.SnapBusPolicyDir, "snap.foo.hook.configure.conf")

		// Verify that "snap.foo.hook.configure.conf" was removed
		_, err := os.Stat(profile)
		c.Check(os.IsNotExist(err), Equals, true)
	}
}

func (s *backendSuite) TestUpdatingSnapToOneWithMoreApps(c *C) {
	// NOTE: Hand out a permanent snippet so that .conf file is generated.
	s.Iface.DBusPermanentSlotCallback = func(spec *dbus.Specification, slot *snap.SlotInfo) error {
		spec.AddSnippet("<policy/>")
		return nil
	}
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, "samba", ifacetest.SambaYamlV1, 0)
		snapInfo = s.UpdateSnap(c, snapInfo, opts, ifacetest.SambaYamlV1WithNmbd, 0)
		profile := filepath.Join(dirs.SnapBusPolicyDir, "snap.samba.nmbd.conf")
		// file called "snap.sambda.nmbd.conf" was created
		_, err := os.Stat(profile)
		c.Check(err, IsNil)
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestUpdatingSnapToOneWithMoreHooks(c *C) {
	// NOTE: Hand out a permanent snippet so that .conf file is generated.
	s.Iface.DBusPermanentSlotCallback = func(spec *dbus.Specification, slot *snap.SlotInfo) error {
		spec.AddSnippet("<policy/>")
		return nil
	}
	s.Iface.DBusPermanentPlugCallback = func(spec *dbus.Specification, plug *snap.PlugInfo) error {
		spec.AddSnippet("<policy/>")
		return nil
	}
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, "samba", ifacetest.SambaYamlV1, 0)
		snapInfo = s.UpdateSnap(c, snapInfo, opts, ifacetest.SambaYamlWithHook, 0)
		profile := filepath.Join(dirs.SnapBusPolicyDir, "snap.samba.hook.configure.conf")

		// Verify that "snap.samba.hook.configure.conf" was created
		_, err := os.Stat(profile)
		c.Check(err, IsNil)
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestUpdatingSnapToOneWithFewerApps(c *C) {
	// NOTE: Hand out a permanent snippet so that .conf file is generated.
	s.Iface.DBusPermanentSlotCallback = func(spec *dbus.Specification, slot *snap.SlotInfo) error {
		spec.AddSnippet("<policy/>")
		return nil
	}
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, "samba", ifacetest.SambaYamlV1WithNmbd, 0)
		snapInfo = s.UpdateSnap(c, snapInfo, opts, ifacetest.SambaYamlV1, 0)
		profile := filepath.Join(dirs.SnapBusPolicyDir, "snap.samba.nmbd.conf")
		// file called "snap.sambda.nmbd.conf" was removed
		_, err := os.Stat(profile)
		c.Check(os.IsNotExist(err), Equals, true)
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestUpdatingSnapToOneWithFewerHooks(c *C) {
	// NOTE: Hand out a permanent snippet so that .conf file is generated.
	s.Iface.DBusPermanentSlotCallback = func(spec *dbus.Specification, slot *snap.SlotInfo) error {
		spec.AddSnippet("<policy/>")
		return nil
	}
	s.Iface.DBusPermanentPlugCallback = func(spec *dbus.Specification, plug *snap.PlugInfo) error {
		spec.AddSnippet("<policy/>")
		return nil
	}
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, "samba", ifacetest.SambaYamlWithHook, 0)
		snapInfo = s.UpdateSnap(c, snapInfo, opts, ifacetest.SambaYamlV1, 0)
		profile := filepath.Join(dirs.SnapBusPolicyDir, "snap.samba.hook.configure.conf")

		// Verify that "snap.samba.hook.configure.conf" was removed
		_, err := os.Stat(profile)
		c.Check(os.IsNotExist(err), Equals, true)
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestCombineSnippetsWithActualSnippets(c *C) {
	// NOTE: replace the real template with a shorter variant
	restore := dbus.MockXMLEnvelope([]byte("<?xml>\n"), []byte("</xml>"))
	defer restore()
	s.Iface.DBusPermanentSlotCallback = func(spec *dbus.Specification, slot *snap.SlotInfo) error {
		spec.AddSnippet("<policy>...</policy>")
		return nil
	}
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, "samba", ifacetest.SambaYamlV1, 0)
		profile := filepath.Join(dirs.SnapBusPolicyDir, "snap.samba.smbd.conf")
		c.Check(profile, testutil.FileEquals, "<?xml>\n<policy>...</policy>\n</xml>")
		stat, err := os.Stat(profile)
		c.Assert(err, IsNil)
		c.Check(stat.Mode(), Equals, os.FileMode(0644))
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestCombineSnippetsWithoutAnySnippets(c *C) {
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, "samba", ifacetest.SambaYamlV1, 0)
		profile := filepath.Join(dirs.SnapBusPolicyDir, "snap.samba.smbd.conf")
		_, err := os.Stat(profile)
		// Without any snippets, there the .conf file is not created.
		c.Check(os.IsNotExist(err), Equals, true)
		s.RemoveSnap(c, snapInfo)
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
	s.Iface.DBusPermanentSlotCallback = func(spec *dbus.Specification, slot *snap.SlotInfo) error {
		spec.AddSnippet("<policy/>")
		return nil
	}
	// Install a snap with two apps, only one of which needs a .conf file
	// because the interface is app-bound.
	snapInfo := s.InstallSnap(c, interfaces.ConfinementOptions{}, "samba", sambaYamlWithIfaceBoundToNmbd, 0)
	defer s.RemoveSnap(c, snapInfo)
	// Check that only one of the .conf files is actually created
	_, err := os.Stat(filepath.Join(dirs.SnapBusPolicyDir, "snap.samba.smbd.conf"))
	c.Check(os.IsNotExist(err), Equals, true)
	_, err = os.Stat(filepath.Join(dirs.SnapBusPolicyDir, "snap.samba.nmbd.conf"))
	c.Check(err, IsNil)
}

func (s *backendSuite) TestSandboxFeatures(c *C) {
	c.Assert(s.Backend.SandboxFeatures(), DeepEquals, []string{"mediated-bus-access"})
}
