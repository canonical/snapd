// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2021 Canonical Ltd
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

package systemd_test

import (
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/interfaces/systemd"
	"github.com/snapcore/snapd/snap"
	sysd "github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/testutil"
)

type backendSuite struct {
	ifacetest.BackendSuite

	systemctlArgs     [][]string
	systemctlRestorer func()
}

var _ = Suite(&backendSuite{})

var testedConfinementOpts = []interfaces.ConfinementOptions{
	{},
	{DevMode: true},
	{JailMode: true},
	{Classic: true},
}

func (s *backendSuite) SetUpTest(c *C) {
	s.Backend = &systemd.Backend{}
	s.BackendSuite.SetUpTest(c)
	c.Assert(s.Repo.AddBackend(s.Backend), IsNil)
	s.systemctlRestorer = sysd.MockSystemctl(func(args ...string) ([]byte, error) {
		s.systemctlArgs = append(s.systemctlArgs, append([]string{"systemctl"}, args...))
		return []byte("ActiveState=inactive"), nil
	})
	s.systemctlArgs = nil
}

func (s *backendSuite) TearDownTest(c *C) {
	s.systemctlRestorer()
	s.BackendSuite.TearDownTest(c)
}

func (s *backendSuite) TestName(c *C) {
	c.Check(s.Backend.Name(), Equals, interfaces.SecuritySystemd)
}

func (s *backendSuite) TestInstallingSnapWritesStartsServices(c *C) {
	var sysdLog [][]string

	r := sysd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		sysdLog = append(sysdLog, cmd)
		if cmd[0] == "show" {
			return []byte("ActiveState=inactive\n"), nil
		}
		return []byte{}, nil
	})
	defer r()

	s.Iface.SystemdPermanentSlotCallback = func(spec *systemd.Specification, slot *snap.SlotInfo) error {
		return spec.AddService("foo", &systemd.Service{ExecStart: "/bin/true"})
	}
	s.InstallSnap(c, interfaces.ConfinementOptions{}, "", ifacetest.SambaYamlV1, 1)
	service := filepath.Join(dirs.SnapServicesDir, "snap.samba.interface.foo.service")
	// the service file was created
	_ := mylog.Check2(os.Stat(service))
	c.Check(err, IsNil)
	// the service was also started (whee)
	c.Check(sysdLog, DeepEquals, [][]string{
		// units added removed
		{"daemon-reload"},
		{"--no-reload", "enable", "snap.samba.interface.foo.service"},
		{"stop", "snap.samba.interface.foo.service"},
		{"show", "--property=ActiveState", "snap.samba.interface.foo.service"},
		{"start", "snap.samba.interface.foo.service"},
		// update systemd's enabled/disabled state
		{"daemon-reload"},
	})
}

func (s *backendSuite) TestRemovingSnapRemovesAndStopsServices(c *C) {
	s.Iface.SystemdPermanentSlotCallback = func(spec *systemd.Specification, slot *snap.SlotInfo) error {
		return spec.AddService("foo", &systemd.Service{ExecStart: "/bin/true"})
	}
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, "", ifacetest.SambaYamlV1, 1)
		s.systemctlArgs = nil
		s.RemoveSnap(c, snapInfo)
		service := filepath.Join(dirs.SnapServicesDir, "snap.samba.interface.foo.service")
		// the service file was removed
		_ := mylog.Check2(os.Stat(service))
		c.Check(os.IsNotExist(err), Equals, true)
		// the service was stopped
		c.Check(s.systemctlArgs, DeepEquals, [][]string{
			{"systemctl", "--no-reload", "disable", "snap.samba.interface.foo.service"},
			{"systemctl", "stop", "snap.samba.interface.foo.service"},
			{"systemctl", "show", "--property=ActiveState", "snap.samba.interface.foo.service"},
			{"systemctl", "daemon-reload"},
		})
	}
}

func (s *backendSuite) TestSettingInstallManyUpdateSecurityWithFewerServices(c *C) {
	s.Iface.SystemdPermanentSlotCallback = func(spec *systemd.Specification, slot *snap.SlotInfo) error {
		mylog.Check(spec.AddService("foo", &systemd.Service{ExecStart: "/bin/true"}))

		return spec.AddService("bar", &systemd.Service{ExecStart: "/bin/false"})
	}
	serviceFoo := filepath.Join(dirs.SnapServicesDir, "snap.samba.interface.foo.service")
	serviceBar := filepath.Join(dirs.SnapServicesDir, "snap.samba.interface.bar.service")
	// verify known test state
	c.Check(serviceFoo, testutil.FileAbsent)
	c.Check(serviceBar, testutil.FileAbsent)
	snapInfo := s.InstallSnap(c, interfaces.ConfinementOptions{}, "", ifacetest.SambaYamlV1, 1)
	// the services were created
	c.Check(serviceFoo, testutil.FilePresent)
	c.Check(serviceBar, testutil.FilePresent)
	c.Check(s.systemctlArgs, DeepEquals, [][]string{
		{"systemctl", "daemon-reload"},
		// units were added
		{"systemctl", "--no-reload", "enable", "snap.samba.interface.bar.service", "snap.samba.interface.foo.service"},
		{"systemctl", "stop", "snap.samba.interface.bar.service", "snap.samba.interface.foo.service"},
		{"systemctl", "show", "--property=ActiveState", "snap.samba.interface.bar.service"},
		{"systemctl", "show", "--property=ActiveState", "snap.samba.interface.foo.service"},
		{"systemctl", "start", "snap.samba.interface.bar.service", "snap.samba.interface.foo.service"},
		// update state in systemd
		{"systemctl", "daemon-reload"},
	})
	s.systemctlArgs = nil

	// Change what the interface returns to simulate some useful change
	s.Iface.SystemdPermanentSlotCallback = func(spec *systemd.Specification, slot *snap.SlotInfo) error {
		return spec.AddService("foo", &systemd.Service{ExecStart: "/bin/true"})
	}
	// Update over to the same snap to regenerate security
	s.UpdateSnap(c, snapInfo, interfaces.ConfinementOptions{}, ifacetest.SambaYamlV1, 0)
	// The bar service should have been stopped, foo service is unchanged
	c.Check(s.systemctlArgs, DeepEquals, [][]string{
		{"systemctl", "--no-reload", "disable", "snap.samba.interface.bar.service"},
		{"systemctl", "stop", "snap.samba.interface.bar.service"},
		{"systemctl", "show", "--property=ActiveState", "snap.samba.interface.bar.service"},
		{"systemctl", "daemon-reload"},
	})
}

func (s *backendSuite) TestSandboxFeatures(c *C) {
	c.Assert(s.Backend.SandboxFeatures(), IsNil)
}

func (s *backendSuite) TestInstallingSnapWhenPreseeding(c *C) {
	s.Backend = &systemd.Backend{}
	opts := &interfaces.SecurityBackendOptions{Preseed: true}
	s.Backend.Initialize(opts)

	var sysdLog [][]string
	r := sysd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		sysdLog = append(sysdLog, cmd)
		return []byte{}, nil
	})
	defer r()

	s.Iface.SystemdPermanentSlotCallback = func(spec *systemd.Specification, slot *snap.SlotInfo) error {
		return spec.AddService("foo", &systemd.Service{ExecStart: "/bin/true"})
	}
	s.InstallSnap(c, interfaces.ConfinementOptions{}, "", ifacetest.SambaYamlV1, 1)
	service := filepath.Join(dirs.SnapServicesDir, "snap.samba.interface.foo.service")
	// the service file was created
	_ := mylog.Check2(os.Stat(service))
	c.Check(err, IsNil)
	// the service was enabled but not started
	c.Check(sysdLog, DeepEquals, [][]string{
		{"--root", dirs.GlobalRootDir, "enable", "snap.samba.interface.foo.service"},
	})
}

// not a viable scenario, but tested for completness
func (s *backendSuite) TestRemovingSnapWhenPreseeding(c *C) {
	s.Backend = &systemd.Backend{}
	opts := &interfaces.SecurityBackendOptions{Preseed: true}
	s.Backend.Initialize(opts)

	s.Iface.SystemdPermanentSlotCallback = func(spec *systemd.Specification, slot *snap.SlotInfo) error {
		return spec.AddService("foo", &systemd.Service{ExecStart: "/bin/true"})
	}
	for _, opts := range testedConfinementOpts {
		snapInfo := s.InstallSnap(c, opts, "", ifacetest.SambaYamlV1, 1)
		s.systemctlArgs = nil
		s.RemoveSnap(c, snapInfo)
		service := filepath.Join(dirs.SnapServicesDir, "snap.samba.interface.foo.service")
		// the service file was removed
		_ := mylog.Check2(os.Stat(service))
		c.Check(os.IsNotExist(err), Equals, true)
		// the service was disabled (but no other systemctl calls)
		c.Check(s.systemctlArgs, DeepEquals, [][]string{
			{"systemctl", "--root", dirs.GlobalRootDir, "disable", "snap.samba.interface.foo.service"},
		})
	}
}
