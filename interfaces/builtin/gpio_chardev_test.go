// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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

package builtin_test

import (
	"errors"
	"fmt"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/kmod"
	"github.com/snapcore/snapd/interfaces/systemd"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type GpioChardevInterfaceSuite struct {
	testutil.BaseTest

	iface    interfaces.Interface
	slot     *interfaces.ConnectedSlot
	slotInfo *snap.SlotInfo
	plug     *interfaces.ConnectedPlug
	plugInfo *snap.PlugInfo

	rootdir string
}

var _ = Suite(&GpioChardevInterfaceSuite{
	iface: builtin.MustInterface("gpio-chardev"),
})

const gpioChardevGadgetYaml = `name: my-device
version: 0
type: gadget
slots:
  gpio-chardev-good-slot:
    interface: gpio-chardev
    source-chip:
      - chip0
      - chip1
    lines: 3,4,1-2,5
`

const gpioChardevConsumerYaml = `name: consumer
version: 0
apps:
  app:
    plugs:
      - gpio-chardev-good-plug
plugs:
  gpio-chardev-good-plug:
    interface: gpio-chardev
`

func (s *GpioChardevInterfaceSuite) SetUpTest(c *C) {
	restore := release.MockReleaseInfo(&release.OS{ID: "ubuntu"})
	s.AddCleanup(restore)

	s.rootdir = c.MkDir()
	dirs.SetRootDir(s.rootdir)
	s.AddCleanup(func() { dirs.SetRootDir("") })

	s.slot, s.slotInfo = MockConnectedSlot(c, gpioChardevGadgetYaml, nil, "gpio-chardev-good-slot")
	s.plug, s.plugInfo = MockConnectedPlug(c, gpioChardevConsumerYaml, nil, "gpio-chardev-good-plug")
}

func (s *GpioChardevInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "gpio-chardev")
}

func (s *GpioChardevInterfaceSuite) TestSanitizeSlot(c *C) {
	// Happy case
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)

	const badGpioChardevGadgetYaml = `name: my-device
version: 0
type: gadget
slots:
  no-source-chip-attr:
    interface: gpio-chardev
    lines: 3,4,1-2,5
  no-lines-attr:
    interface: gpio-chardev
    source-chip: [chip2]
  duplicate-source-chip:
    interface: gpio-chardev
    source-chip: ["chip","chip"]
    lines: 3,4,1-2,5
  duplicate-line:
    interface: gpio-chardev
    source-chip: [chip3]
    lines: 2-6,3
  bad-source-chip-0:
    interface: gpio-chardev
    source-chip: []
    lines: 3,4,1-2,5
  bad-source-chip-1:
    interface: gpio-chardev
    source-chip: [" s"]
    lines: 3,4,1-2,5
  bad-source-chip-2:
    interface: gpio-chardev
    source-chip: ["s "]
    lines: 3,4,1-2,5
  bad-source-chip-3:
    interface: gpio-chardev
    source-chip: [""]
    lines: 3,4,1-2,5
  bad-range-0:
    interface: gpio-chardev
    source-chip: [chip4]
    lines: 2-
  bad-range-1:
    interface: gpio-chardev
    source-chip: [chip5]
    lines: a-3
  bad-range-2:
    interface: gpio-chardev
    source-chip: [chip6]
    lines: 0-10000000
  bad-range-3:
    interface: gpio-chardev
    source-chip: [chip7]
    lines: 4-2
  bad-range-4:
    interface: gpio-chardev
    source-chip: [chip8]
    lines: 0--1
  bad-line-0:
    interface: gpio-chardev
    source-chip: [chip9]
    lines: a
  bad-line-1:
    interface: gpio-chardev
    source-chip: [chip10]
    lines: "-1"
  bad-lines-count:
    interface: gpio-chardev
    source-chip: [chip11]
    lines: 0,1-512
`
	info := snaptest.MockInvalidInfo(c, badGpioChardevGadgetYaml, nil)
	expectedError := map[string]string{
		"no-source-chip-attr":   `snap "my-device" does not have attribute "source-chip" for interface "gpio-chardev"`,
		"no-lines-attr":         `snap "my-device" does not have attribute "lines" for interface "gpio-chardev"`,
		"duplicate-line":        `invalid "lines" attribute: overlapping range span found "3"`,
		"duplicate-source-chip": `invalid "source-chip" attribute: cannot contain duplicate chip names, found "chip"`,
		"bad-source-chip-0":     `invalid "source-chip" attribute: cannot be empty`,
		"bad-source-chip-1":     `invalid "source-chip" attribute: chip cannot contain leading or trailing white space, found " s"`,
		"bad-source-chip-2":     `invalid "source-chip" attribute: chip cannot contain leading or trailing white space, found "s "`,
		"bad-source-chip-3":     `invalid "source-chip" attribute: chip cannot be empty`,
		"bad-range-0":           `invalid "lines" attribute: invalid range span end "2-":.*: invalid syntax`,
		"bad-range-1":           `invalid "lines" attribute: invalid range span start "a-3":.*: invalid syntax`,
		"bad-range-2":           `invalid "lines" attribute: range size cannot exceed 512, found 10000001`,
		"bad-range-3":           `invalid "lines" attribute: invalid range span "4-2": ends before it starts`,
		"bad-range-4":           `invalid "lines" attribute: invalid range span end "0--1":.*: invalid syntax`,
		"bad-line-0":            `invalid "lines" attribute:.*: invalid syntax`,
		"bad-line-1":            `invalid "lines" attribute: invalid range span start "-1":.*: invalid syntax`,
		"bad-lines-count":       `invalid "lines" attribute: range size cannot exceed 512, found 513`,
	}
	c.Assert(len(info.Slots), Equals, len(expectedError))
	for slotName, slotInfo := range info.Slots {
		c.Check(interfaces.BeforePrepareSlot(s.iface, slotInfo), ErrorMatches, expectedError[slotName])
	}
}

func (s *GpioChardevInterfaceSuite) TestSanitizePlug(c *C) {
	// There is no plug-side sanitization since there is no attributes.
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *GpioChardevInterfaceSuite) TestBeforeConnectPlug(c *C) {
	restore := builtin.MockGpioCheckConfigfsSupport(func() error { return nil })
	defer restore()
	// BeforeConnectPlug only checks that the gpio-aggregator kernel driver
	// support the new configfs interface.
	c.Assert(interfaces.BeforeConnectPlug(s.iface, s.plug), IsNil)

	restore = builtin.MockGpioCheckConfigfsSupport(func() error { return errors.New("boom!") })
	defer restore()
	c.Assert(interfaces.BeforeConnectPlug(s.iface, s.plug), ErrorMatches, "boom!")
}

func (s *GpioChardevInterfaceSuite) TestSystemdConnectedSlot(c *C) {
	spec := &systemd.Specification{}
	err := spec.AddConnectedSlot(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(spec.Services(), DeepEquals, map[string]*systemd.Service{
		"gpio-chardev-gpio-chardev-good-slot": {
			Type:            "oneshot",
			RemainAfterExit: true,
			ExecStart:       fmt.Sprintf(`%s/usr/lib/snapd/snap-gpio-helper export-chardev "chip0,chip1" "3,4,1-2,5" "my-device" "gpio-chardev-good-slot"`, s.rootdir),
			ExecStop:        fmt.Sprintf(`%s/usr/lib/snapd/snap-gpio-helper unexport-chardev "chip0,chip1" "3,4,1-2,5" "my-device" "gpio-chardev-good-slot"`, s.rootdir),
			WantedBy:        "snapd.gpio-chardev-setup.target",
			Before:          "snapd.gpio-chardev-setup.target",
		},
	})
}

func (s *GpioChardevInterfaceSuite) TestSystemdConnectedPlug(c *C) {
	spec := &systemd.Specification{}
	err := spec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)

	target := fmt.Sprintf("%s/my-device/gpio-chardev-good-slot", dirs.SnapGpioChardevDir)
	symlink := fmt.Sprintf("%s/consumer/gpio-chardev-good-plug", dirs.SnapGpioChardevDir)

	expectedExecStart := fmt.Sprintf("/bin/sh -c 'mkdir -p %q && ln -s %q %q'", filepath.Dir(symlink), target, symlink)
	expectedExecStop := fmt.Sprintf("/bin/rm -f %q", symlink)
	c.Assert(spec.Services(), DeepEquals, map[string]*systemd.Service{
		"gpio-chardev-gpio-chardev-good-plug": {
			Type:            "oneshot",
			RemainAfterExit: true,
			ExecStart:       expectedExecStart,
			ExecStop:        expectedExecStop,
			WantedBy:        "snapd.gpio-chardev-setup.target",
			Before:          "snapd.gpio-chardev-setup.target",
		},
	})
}

func (s *GpioChardevInterfaceSuite) TestKModConnectedSlot(c *C) {
	spec := &kmod.Specification{}
	c.Assert(spec.AddConnectedSlot(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.Modules(), DeepEquals, map[string]bool{
		"gpio-aggregator": true,
	})
}

func (s *GpioChardevInterfaceSuite) TestApparmorConnectedPlug(c *C) {
	spec := apparmor.NewSpecification(s.plug.AppSet())
	err := spec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `/dev/snap/gpio-chardev/my-device/gpio-chardev-good-slot rwk`)
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `/dev/snap/gpio-chardev/consumer/{,*} r`)
}

func (s *GpioChardevInterfaceSuite) TestUDevConnectedPlug(c *C) {
	spec := udev.NewSpecification(s.plug.AppSet())
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.Snippets(), testutil.Contains, `# gpio-chardev
TAG=="snap_my-device_interface_gpio_chardev_gpio-chardev-good-slot", TAG+="snap_consumer_app"`)
}

func (s *GpioChardevInterfaceSuite) TestServicePermanentPlugSnippets(c *C) {
	snips, err := interfaces.PermanentPlugServiceSnippets(s.iface, s.plugInfo)
	c.Assert(err, IsNil)
	c.Assert(snips, HasLen, 2)
	c.Check(string(snips[0].SystemdConfSection()), Equals, "Unit")
	c.Check(snips[0].String(), Equals, "After=snapd.gpio-chardev-setup.target")
	c.Check(string(snips[1].SystemdConfSection()), Equals, "Unit")
	c.Check(snips[1].String(), Equals, "Wants=snapd.gpio-chardev-setup.target")
}

func (s *GpioChardevInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, false)
	c.Assert(si.ImplicitOnClassic, Equals, false)
	c.Assert(si.Summary, Equals, `allows access to specific GPIO chardev lines`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "gpio-chardev")
}

func (s *GpioChardevInterfaceSuite) TestAutoConnect(c *C) {
	c.Assert(s.iface.AutoConnect(s.plugInfo, s.slotInfo), Equals, true)
}

func (s *GpioChardevInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
