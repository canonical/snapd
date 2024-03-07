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

package builtin

import (
	"fmt"
	"io/fs"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/testutil"
)

type commonIfaceSuite struct{}

var _ = Suite(&commonIfaceSuite{})

func (s *commonIfaceSuite) TestUDevSpec(c *C) {
	plug, _ := MockConnectedPlug(c, `
name: consumer
version: 0
apps:
  app-a:
    plugs: [common]
  app-b:
  app-c:
    plugs: [common]
`, nil, "common")
	slot, _ := MockConnectedSlot(c, `
name: producer
version: 0
slots:
  common:
`, nil, "common")

	// common interface can define connected plug udev rules
	iface := &commonInterface{
		name:              "common",
		connectedPlugUDev: []string{`KERNEL=="foo"`},
	}
	spec := udev.NewSpecification(interfaces.NewSnapAppSet(plug.Snap(), nil))
	c.Assert(spec.AddConnectedPlug(iface, plug, slot), IsNil)
	c.Assert(spec.Snippets(), DeepEquals, []string{
		`# common
KERNEL=="foo", TAG+="snap_consumer_app-a"`,
		fmt.Sprintf(`TAG=="snap_consumer_app-a", SUBSYSTEM!="module", SUBSYSTEM!="subsystem", RUN+="%v/snap-device-helper $env{ACTION} snap_consumer_app-a $devpath $major:$minor"`, dirs.DistroLibExecDir),
		// NOTE: app-b is unaffected as it doesn't have a plug reference.
		`# common
KERNEL=="foo", TAG+="snap_consumer_app-c"`,
		fmt.Sprintf(`TAG=="snap_consumer_app-c", SUBSYSTEM!="module", SUBSYSTEM!="subsystem", RUN+="%v/snap-device-helper $env{ACTION} snap_consumer_app-c $devpath $major:$minor"`, dirs.DistroLibExecDir),
	})

	// connected plug udev rules are optional
	iface = &commonInterface{
		name: "common",
	}
	spec = udev.NewSpecification(interfaces.NewSnapAppSet(plug.Snap(), nil))
	c.Assert(spec.AddConnectedPlug(iface, plug, slot), IsNil)
	c.Assert(spec.Snippets(), HasLen, 0)
}

// MockEvalSymlinks replaces the path/filepath.EvalSymlinks function used inside the caps package.
func MockEvalSymlinks(test *testutil.BaseTest, fn func(string) (string, error)) {
	orig := evalSymlinks
	evalSymlinks = fn
	test.AddCleanup(func() {
		evalSymlinks = orig
	})
}

// MockReadDir replaces the os.ReadDir function used inside the caps package.
func MockReadDir(test *testutil.BaseTest, fn func(string) ([]fs.DirEntry, error)) {
	orig := readDir
	readDir = fn
	test.AddCleanup(func() {
		readDir = orig
	})
}

func (s *commonIfaceSuite) TestSuppressFeatures(c *C) {
	plug, _ := MockConnectedPlug(c, `
name: consumer
version: 0
apps:
  app:
    plugs: [common]
`, nil, "common")
	slot, _ := MockConnectedSlot(c, `
name: producer
version: 0
slots:
  common:
`, nil, "common")

	type Checks []struct {
		getter        func(spec *apparmor.Specification) bool
		expectedValue bool
	}

	tests := []struct {
		iface  *commonInterface
		checks Checks
	}{
		// PtraceTrace
		{
			// setting nothing
			&commonInterface{name: "common", suppressPtraceTrace: false, usesPtraceTrace: false},
			Checks{
				{(*apparmor.Specification).UsesPtraceTrace, false},
				{(*apparmor.Specification).SuppressPtraceTrace, false},
			},
		},
		{
			// setting only uses
			&commonInterface{name: "common", suppressPtraceTrace: false, usesPtraceTrace: true},
			Checks{
				{(*apparmor.Specification).UsesPtraceTrace, true},
				{(*apparmor.Specification).SuppressPtraceTrace, false},
			},
		},
		{
			// setting only suppress
			&commonInterface{name: "common", suppressPtraceTrace: true, usesPtraceTrace: false},
			Checks{
				{(*apparmor.Specification).UsesPtraceTrace, false},
				{(*apparmor.Specification).SuppressPtraceTrace, true},
			},
		},
		{
			// setting both, only uses is set
			&commonInterface{name: "common", suppressPtraceTrace: true, usesPtraceTrace: true},
			Checks{
				{(*apparmor.Specification).UsesPtraceTrace, true},
				{(*apparmor.Specification).SuppressPtraceTrace, false},
			},
		},
		// HomeIx
		{
			// setting nothing
			&commonInterface{name: "common", suppressHomeIx: false},
			Checks{
				{(*apparmor.Specification).SuppressHomeIx, false},
			},
		},
		{
			// setting suppress
			&commonInterface{name: "common", suppressHomeIx: true},
			Checks{
				{(*apparmor.Specification).SuppressHomeIx, true},
			},
		},
		// PycacheDeny
		{
			// setting nothing
			&commonInterface{name: "common", suppressPycacheDeny: false},
			Checks{
				{(*apparmor.Specification).SuppressPycacheDeny, false},
			},
		},
		{
			// setting suppress
			&commonInterface{name: "common", suppressPycacheDeny: true},
			Checks{
				{(*apparmor.Specification).SuppressPycacheDeny, true},
			},
		},
		// sys_module capability
		{
			// setting nothing
			&commonInterface{name: "common", suppressSysModuleCapability: false, usesSysModuleCapability: false},
			Checks{
				{(*apparmor.Specification).UsesSysModuleCapability, false},
				{(*apparmor.Specification).SuppressSysModuleCapability, false},
			},
		},
		{
			// setting only uses
			&commonInterface{name: "common", suppressSysModuleCapability: false, usesSysModuleCapability: true},
			Checks{
				{(*apparmor.Specification).UsesSysModuleCapability, true},
				{(*apparmor.Specification).SuppressSysModuleCapability, false},
			},
		},
		{
			// setting only suppress
			&commonInterface{name: "common", suppressSysModuleCapability: true, usesSysModuleCapability: false},
			Checks{
				{(*apparmor.Specification).UsesSysModuleCapability, false},
				{(*apparmor.Specification).SuppressSysModuleCapability, true},
			},
		},
		{
			// setting both, only uses is set
			&commonInterface{name: "common", suppressSysModuleCapability: true, usesSysModuleCapability: true},
			Checks{
				{(*apparmor.Specification).UsesSysModuleCapability, true},
				{(*apparmor.Specification).SuppressSysModuleCapability, false},
			},
		},
	}

	for _, test := range tests {
		spec := apparmor.NewSpecification(interfaces.NewSnapAppSet(plug.Snap(), nil))
		iface := test.iface
		// before connection, everything should be set to false
		for _, check := range test.checks {
			c.Check(check.getter(spec), Equals, false)
		}
		c.Check(spec.AddConnectedPlug(iface, plug, slot), IsNil)
		for _, check := range test.checks {
			c.Check(check.getter(spec), Equals, check.expectedValue)
		}
	}
}

func (s *commonIfaceSuite) TestControlsDeviceCgroup(c *C) {
	plug, _ := MockConnectedPlug(c, `
name: consumer
version: 0
apps:
  app:
    plugs: [common]
`, nil, "common")
	slot, _ := MockConnectedSlot(c, `
name: producer
version: 0
slots:
  common:
`, nil, "common")

	// setting nothing
	iface := &commonInterface{
		name:                 "common",
		controlsDeviceCgroup: false,
	}
	spec := udev.NewSpecification(interfaces.NewSnapAppSet(plug.Snap(), nil))
	c.Assert(spec.ControlsDeviceCgroup(), Equals, false)
	c.Assert(spec.AddConnectedPlug(iface, plug, slot), IsNil)
	c.Assert(spec.ControlsDeviceCgroup(), Equals, false)

	iface = &commonInterface{
		name:                 "common",
		controlsDeviceCgroup: true,
	}
	spec = udev.NewSpecification(interfaces.NewSnapAppSet(plug.Snap(), nil))
	c.Assert(spec.ControlsDeviceCgroup(), Equals, false)
	c.Assert(spec.AddConnectedPlug(iface, plug, slot), IsNil)
	c.Assert(spec.ControlsDeviceCgroup(), Equals, true)
}
