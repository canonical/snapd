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
	"fmt"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/dbus"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/snapdtool"
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
	err := os.MkdirAll(dirs.SnapDBusSystemPolicyDir, 0700)
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
		snapInfo := s.InstallSnap(c, opts, "", ifacetest.SambaYamlV1, 0)
		profile := filepath.Join(dirs.SnapDBusSystemPolicyDir, "snap.samba.smbd.conf")
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
		snapInfo := s.InstallSnap(c, opts, "", ifacetest.HookYaml, 0)
		profile := filepath.Join(dirs.SnapDBusSystemPolicyDir, "snap.foo.hook.configure.conf")

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
		snapInfo := s.InstallSnap(c, opts, "", ifacetest.SambaYamlV1, 0)
		s.RemoveSnap(c, snapInfo)
		profile := filepath.Join(dirs.SnapDBusSystemPolicyDir, "snap.samba.smbd.conf")
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
		snapInfo := s.InstallSnap(c, opts, "", ifacetest.HookYaml, 0)
		s.RemoveSnap(c, snapInfo)
		profile := filepath.Join(dirs.SnapDBusSystemPolicyDir, "snap.foo.hook.configure.conf")

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
		snapInfo := s.InstallSnap(c, opts, "", ifacetest.SambaYamlV1, 0)
		snapInfo = s.UpdateSnap(c, snapInfo, opts, ifacetest.SambaYamlV1WithNmbd, 0)
		profile := filepath.Join(dirs.SnapDBusSystemPolicyDir, "snap.samba.nmbd.conf")
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
		snapInfo := s.InstallSnap(c, opts, "", ifacetest.SambaYamlV1, 0)
		snapInfo = s.UpdateSnap(c, snapInfo, opts, ifacetest.SambaYamlWithHook, 0)
		profile := filepath.Join(dirs.SnapDBusSystemPolicyDir, "snap.samba.hook.configure.conf")

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
		snapInfo := s.InstallSnap(c, opts, "", ifacetest.SambaYamlV1WithNmbd, 0)
		snapInfo = s.UpdateSnap(c, snapInfo, opts, ifacetest.SambaYamlV1, 0)
		profile := filepath.Join(dirs.SnapDBusSystemPolicyDir, "snap.samba.nmbd.conf")
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
		snapInfo := s.InstallSnap(c, opts, "", ifacetest.SambaYamlWithHook, 0)
		snapInfo = s.UpdateSnap(c, snapInfo, opts, ifacetest.SambaYamlV1, 0)
		profile := filepath.Join(dirs.SnapDBusSystemPolicyDir, "snap.samba.hook.configure.conf")

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
		snapInfo := s.InstallSnap(c, opts, "", ifacetest.SambaYamlV1, 0)
		profile := filepath.Join(dirs.SnapDBusSystemPolicyDir, "snap.samba.smbd.conf")
		c.Check(profile, testutil.FileEquals, "<?xml>\n<policy>...</policy>\n</xml>")
		stat, err := os.Stat(profile)
		c.Assert(err, IsNil)
		c.Check(stat.Mode(), Equals, os.FileMode(0644))
		s.RemoveSnap(c, snapInfo)
	}
}

func (s *backendSuite) TestCombineSnippetsWithoutAnySnippets(c *C) {
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, "", ifacetest.SambaYamlV1, 0)
		profile := filepath.Join(dirs.SnapDBusSystemPolicyDir, "snap.samba.smbd.conf")
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
	snapInfo := s.InstallSnap(c, interfaces.ConfinementOptions{}, "", sambaYamlWithIfaceBoundToNmbd, 0)
	defer s.RemoveSnap(c, snapInfo)
	// Check that only one of the .conf files is actually created
	_, err := os.Stat(filepath.Join(dirs.SnapDBusSystemPolicyDir, "snap.samba.smbd.conf"))
	c.Check(os.IsNotExist(err), Equals, true)
	_, err = os.Stat(filepath.Join(dirs.SnapDBusSystemPolicyDir, "snap.samba.nmbd.conf"))
	c.Check(err, IsNil)
}

func (s *backendSuite) TestSandboxFeatures(c *C) {
	c.Assert(s.Backend.SandboxFeatures(), DeepEquals, []string{"mediated-bus-access"})
}

func makeFakeDbusConfigAndUserdServiceFiles(c *C, coreOrSnapdSnap *snap.Info) {
	err := os.MkdirAll(filepath.Join(coreOrSnapdSnap.MountDir(), "/usr/share/dbus-1/session.d"), 0755)
	c.Assert(err, IsNil)
	content := fmt.Sprintf("content of snapd.session-services.conf for snap %s", coreOrSnapdSnap.InstanceName())
	err = os.WriteFile(filepath.Join(coreOrSnapdSnap.MountDir(), "/usr/share/dbus-1/session.d/snapd.session-services.conf"), []byte(content), 0644)
	c.Assert(err, IsNil)

	err = os.MkdirAll(filepath.Join(coreOrSnapdSnap.MountDir(), "/usr/share/dbus-1/system.d"), 0755)
	c.Assert(err, IsNil)
	content = fmt.Sprintf("content of snapd.system-services.conf for snap %s", coreOrSnapdSnap.InstanceName())
	err = os.WriteFile(filepath.Join(coreOrSnapdSnap.MountDir(), "/usr/share/dbus-1/system.d/snapd.system-services.conf"), []byte(content), 0644)
	c.Assert(err, IsNil)

	err = os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "/usr/share/dbus-1/services"), 0755)
	c.Assert(err, IsNil)

	servicesPath := filepath.Join(coreOrSnapdSnap.MountDir(), "/usr/share/dbus-1/services")
	err = os.MkdirAll(servicesPath, 0755)
	c.Assert(err, IsNil)

	for _, fn := range []string{
		"io.snapcraft.Launcher.service",
		"io.snapcraft.Settings.service",
	} {
		content := fmt.Sprintf("content of %s for snap %s", fn, coreOrSnapdSnap.InstanceName())
		err = os.WriteFile(filepath.Join(servicesPath, fn), []byte(content), 0644)
		c.Assert(err, IsNil)
	}
}

var expectedDBusConfigFiles = []string{
	"/usr/share/dbus-1/services/io.snapcraft.Launcher.service",
	"/usr/share/dbus-1/services/io.snapcraft.Settings.service",
	"/usr/share/dbus-1/session.d/snapd.session-services.conf",
	"/usr/share/dbus-1/system.d/snapd.system-services.conf",
}

func (s *backendSuite) testSetupWritesDbusFilesForCoreOrSnapd(c *C, coreOrSnapdYaml string) {
	coreOrSnapdInfo := snaptest.MockInfo(c, coreOrSnapdYaml, &snap.SideInfo{Revision: snap.R(2)})
	makeFakeDbusConfigAndUserdServiceFiles(c, coreOrSnapdInfo)

	// Config files are not copied if we haven't reexecuted
	err := s.Backend.Setup(coreOrSnapdInfo, interfaces.ConfinementOptions{}, s.Repo, nil)
	c.Assert(err, IsNil)

	for _, fn := range expectedDBusConfigFiles {
		c.Check(filepath.Join(dirs.GlobalRootDir, fn), testutil.FileAbsent)
	}

	// Now make it look like snapd was reexecuted
	restore := snapdtool.MockOsReadlink(func(string) (string, error) {
		return filepath.Join(coreOrSnapdInfo.MountDir(), "/usr/lib/snapd/snapd"), nil
	})
	defer restore()

	err = s.Backend.Setup(coreOrSnapdInfo, interfaces.ConfinementOptions{}, s.Repo, nil)
	c.Assert(err, IsNil)

	for _, fn := range expectedDBusConfigFiles {
		c.Check(filepath.Join(dirs.GlobalRootDir, fn), testutil.FilePresent)
	}
}

var (
	coreYaml  string = "name: core\nversion: 1\ntype: os"
	snapdYaml string = "name: snapd\nversion: 1\ntype: snapd"
)

func (s *backendSuite) TestSetupWritesDbusFilesForCore(c *C) {
	s.testSetupWritesDbusFilesForCoreOrSnapd(c, coreYaml)
}

func (s *backendSuite) TestSetupWritesDbusFilesForSnapd(c *C) {
	s.testSetupWritesDbusFilesForCoreOrSnapd(c, snapdYaml)
}

func (s *backendSuite) TestSetupDeletesDbusFilesWhenServiceRemoved(c *C) {
	snapdInfo := snaptest.MockInfo(c, snapdYaml, &snap.SideInfo{Revision: snap.R(2)})
	makeFakeDbusConfigAndUserdServiceFiles(c, snapdInfo)

	vestigialConfigFile := "/usr/share/dbus-1/services/io.snapcraft.Prompt.service"
	existingConfigFile := expectedDBusConfigFiles[0]

	// Create config files to be present before setup
	for _, fn := range []string{vestigialConfigFile, existingConfigFile} {
		f, err := os.Create(filepath.Join(dirs.GlobalRootDir, fn))
		c.Assert(err, IsNil)
		f.Close()
	}

	// Config files are not modified if we haven't reexecuted
	err := s.Backend.Setup(snapdInfo, interfaces.ConfinementOptions{}, s.Repo, nil)
	c.Assert(err, IsNil)

	for _, fn := range expectedDBusConfigFiles {
		if fn != existingConfigFile {
			c.Check(filepath.Join(dirs.GlobalRootDir, fn), testutil.FileAbsent)
		}
	}

	c.Check(filepath.Join(dirs.GlobalRootDir, vestigialConfigFile), testutil.FilePresent)
	c.Check(filepath.Join(dirs.GlobalRootDir, existingConfigFile), testutil.FilePresent)

	// Now make it look like snapd was reexecuted
	restore := snapdtool.MockOsReadlink(func(string) (string, error) {
		return filepath.Join(snapdInfo.MountDir(), "/usr/lib/snapd/snapd"), nil
	})
	defer restore()

	err = s.Backend.Setup(snapdInfo, interfaces.ConfinementOptions{}, s.Repo, nil)
	c.Assert(err, IsNil)

	for _, fn := range expectedDBusConfigFiles {
		c.Check(filepath.Join(dirs.GlobalRootDir, fn), testutil.FilePresent)
		c.Check(filepath.Join(dirs.GlobalRootDir, fn), testutil.FileEquals, fmt.Sprintf("content of %s for snap snapd", filepath.Base(fn)))
	}

	// Check that old config file was removed
	c.Check(filepath.Join(dirs.GlobalRootDir, vestigialConfigFile), testutil.FileAbsent)
}

func (s *backendSuite) TestSetupWritesDbusFilesBothSnapdAndCoreInstalled(c *C) {
	err := os.MkdirAll(filepath.Join(dirs.SnapMountDir, "snapd/current"), 0755)
	c.Assert(err, IsNil)

	coreInfo := snaptest.MockInfo(c, coreYaml, &snap.SideInfo{Revision: snap.R(2)})
	makeFakeDbusConfigAndUserdServiceFiles(c, coreInfo)
	snapdInfo := snaptest.MockInfo(c, snapdYaml, &snap.SideInfo{Revision: snap.R(3)})
	makeFakeDbusConfigAndUserdServiceFiles(c, snapdInfo)

	restore := snapdtool.MockOsReadlink(func(string) (string, error) {
		return filepath.Join(snapdInfo.MountDir(), "/usr/lib/snapd/snapd"), nil
	})
	defer restore()

	// first setup snapd which writes the files
	err = s.Backend.Setup(snapdInfo, interfaces.ConfinementOptions{}, s.Repo, nil)
	c.Assert(err, IsNil)

	// then setup core - if both are installed snapd should win
	err = s.Backend.Setup(coreInfo, interfaces.ConfinementOptions{}, s.Repo, nil)
	c.Assert(err, IsNil)

	for _, fn := range expectedDBusConfigFiles {
		c.Check(filepath.Join(dirs.GlobalRootDir, fn), testutil.FileEquals, fmt.Sprintf("content of %s for snap snapd", filepath.Base(fn)))
	}
}
