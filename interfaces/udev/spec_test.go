// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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
	"fmt"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
)

type specSuite struct {
	iface    *ifacetest.TestInterface
	spec     *udev.Specification
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
}

var _ = Suite(&specSuite{
	iface: &ifacetest.TestInterface{
		InterfaceName: "test",
		UDevConnectedPlugCallback: func(spec *udev.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
			spec.AddSnippet("connected-plug")
			return nil
		},
		UDevConnectedSlotCallback: func(spec *udev.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
			spec.AddSnippet("connected-slot")
			return nil
		},
		UDevPermanentPlugCallback: func(spec *udev.Specification, plug *snap.PlugInfo) error {
			spec.AddSnippet("permanent-plug")
			return nil
		},
		UDevPermanentSlotCallback: func(spec *udev.Specification, slot *snap.SlotInfo) error {
			spec.AddSnippet("permanent-slot")
			return nil
		},
	},
})

func (s *specSuite) SetUpSuite(c *C) {
	info1 := snaptest.MockInfo(c, `name: snap1
version: 0
plugs:
    name:
        interface: test
apps:
    foo:
        command: bin/foo
hooks:
    configure:
`, nil)
	info2 := snaptest.MockInfo(c, `name: snap2
version: 0
slots:
    name:
        interface: test
`, nil)
	s.plugInfo = info1.Plugs["name"]
	s.plug = interfaces.NewConnectedPlug(s.plugInfo, nil, nil)
	s.slotInfo = info2.Slots["name"]
	s.slot = interfaces.NewConnectedSlot(s.slotInfo, nil, nil)
}

func (s *specSuite) SetUpTest(c *C) {
	s.spec = udev.NewSpecification(interfaces.NewSnapAppSet(s.plugInfo.Snap))
}

func (s *specSuite) TestAddSnippte(c *C) {
	s.spec.AddSnippet("foo")
	c.Assert(s.spec.Snippets(), DeepEquals, []string{"foo"})
}

func (s *specSuite) testTagDevice(c *C, helperDir string) {
	// TagDevice acts in the scope of the plug/slot (as appropriate) and
	// affects all of the apps and hooks related to the given plug or slot
	// (with the exception that slots cannot have hooks).
	iface := &ifacetest.TestInterface{
		InterfaceName: "iface-1",
		UDevConnectedPlugCallback: func(spec *udev.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
			spec.TagDevice(`kernel="voodoo"`)
			return nil
		},
	}
	c.Assert(s.spec.AddConnectedPlug(iface, s.plug, s.slot), IsNil)

	iface = &ifacetest.TestInterface{
		InterfaceName: "iface-2",
		UDevConnectedPlugCallback: func(spec *udev.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
			spec.TagDevice(`kernel="hoodoo"`)
			return nil
		},
	}
	c.Assert(s.spec.AddConnectedPlug(iface, s.plug, s.slot), IsNil)

	c.Assert(s.spec.Snippets(), DeepEquals, []string{
		`# iface-1
kernel="voodoo", TAG+="snap_snap1_foo"`,
		`# iface-2
kernel="hoodoo", TAG+="snap_snap1_foo"`,
		fmt.Sprintf(`TAG=="snap_snap1_foo", SUBSYSTEM!="module", SUBSYSTEM!="subsystem", RUN+="%s/snap-device-helper snap_snap1_foo"`, helperDir),
		`# iface-1
kernel="voodoo", TAG+="snap_snap1_hook_configure"`,
		`# iface-2
kernel="hoodoo", TAG+="snap_snap1_hook_configure"`,
		fmt.Sprintf(`TAG=="snap_snap1_hook_configure", SUBSYSTEM!="module", SUBSYSTEM!="subsystem", RUN+="%[1]s/snap-device-helper snap_snap1_hook_configure"`, helperDir),
	})
}

func (s *specSuite) TestTagDevice(c *C) {
	defer func() { dirs.SetRootDir("") }()
	restore := release.MockReleaseInfo(&release.OS{ID: "ubuntu"})
	defer restore()
	dirs.SetRootDir("")
	s.testTagDevice(c, "/usr/lib/snapd")
}

func (s *specSuite) TestTagDeviceAltLibexecdir(c *C) {
	defer func() { dirs.SetRootDir("") }()
	restore := release.MockReleaseInfo(&release.OS{ID: "fedora"})
	defer restore()
	dirs.SetRootDir("")
	// validity
	c.Check(dirs.DistroLibExecDir, Equals, "/usr/libexec/snapd")
	s.testTagDevice(c, "/usr/libexec/snapd")
}

// The spec.Specification can be used through the interfaces.Specification interface
func (s *specSuite) TestSpecificationIface(c *C) {
	var r interfaces.Specification = s.spec
	c.Assert(r.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(r.AddConnectedSlot(s.iface, s.plug, s.slot), IsNil)
	c.Assert(r.AddPermanentPlug(s.iface, s.plugInfo), IsNil)
	c.Assert(r.AddPermanentSlot(s.iface, s.slotInfo), IsNil)
	c.Assert(s.spec.Snippets(), DeepEquals, []string{"connected-plug", "connected-slot", "permanent-plug", "permanent-slot"})
}

func (s *specSuite) TestControlsDeviceCgroup(c *C) {
	c.Assert(s.spec.ControlsDeviceCgroup(), Equals, false)
	s.spec.SetControlsDeviceCgroup()
	c.Assert(s.spec.ControlsDeviceCgroup(), Equals, true)
}
