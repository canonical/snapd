// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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
	"fmt"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type sdControlSuite struct {
	iface interfaces.Interface

	dualSDPlugInfo   *snap.PlugInfo
	dualSDPlug       *interfaces.ConnectedPlug
	noFlavorPlugInfo *snap.PlugInfo
	noFlavorPlug     *interfaces.ConnectedPlug
	slotInfo         *snap.SlotInfo
	slot             *interfaces.ConnectedSlot
}

var _ = Suite(&sdControlSuite{
	iface: builtin.MustInterface("sd-control"),
})

const sdControlMockPlugSnapInfoYaml = `
 name: my-device
 version: 1.0
 plugs:
   dual-sd:
     interface: sd-control
     flavor: dual-sd
   no-flavor:
     interface: sd-control
 apps:
   svc:
     command: bin/foo.sh
     plugs:
       - dual-sd
       - no-flavor
 `

const coreSDControlSlotYaml = `name: core
version: 0
type: os
slots:
  sd-control:
`

func (s *sdControlSuite) SetUpTest(c *C) {
	s.dualSDPlug, s.dualSDPlugInfo = MockConnectedPlug(c, sdControlMockPlugSnapInfoYaml, nil, "dual-sd")
	s.noFlavorPlug, s.noFlavorPlugInfo = MockConnectedPlug(c, sdControlMockPlugSnapInfoYaml, nil, "no-flavor")
	s.slot, s.slotInfo = MockConnectedSlot(c, coreSDControlSlotYaml, nil, "sd-control")

}

func (s *sdControlSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "sd-control")
}

func (s *sdControlSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.noFlavorPlugInfo), IsNil)
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.dualSDPlugInfo), IsNil)
}

func (s *sdControlSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *sdControlSuite) TestApparmorConnectedPlugDualSD(c *C) {
	spec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.dualSDPlug.Snap(), nil))
	err := spec.AddConnectedPlug(s.iface, s.dualSDPlug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(spec.SnippetForTag("snap.my-device.svc"), testutil.Contains, "/dev/DualSD rw,\n")
}

func (s *sdControlSuite) TestUDevConnectedPlugDualSD(c *C) {
	spec := udev.NewSpecification(interfaces.NewSnapAppSet(s.dualSDPlug.Snap(), nil))
	err := spec.AddConnectedPlug(s.iface, s.dualSDPlug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(spec.Snippets(), HasLen, 2)
	c.Assert(spec.Snippets(), testutil.Contains, `# sd-control
KERNEL=="DualSD", TAG+="snap_my-device_svc"`)
	c.Assert(spec.Snippets(), testutil.Contains, fmt.Sprintf(`TAG=="snap_my-device_svc", SUBSYSTEM!="module", SUBSYSTEM!="subsystem", RUN+="%v/snap-device-helper $env{ACTION} snap_my-device_svc $devpath $major:$minor"`, dirs.DistroLibExecDir))
}

func (s *sdControlSuite) TestUDevConnectedPlugNoFlavor(c *C) {
	spec := udev.NewSpecification(interfaces.NewSnapAppSet(s.noFlavorPlug.Snap(), nil))
	err := spec.AddConnectedPlug(s.iface, s.noFlavorPlug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(spec.Snippets(), HasLen, 0)
}

func (s *sdControlSuite) TestApparmorConnectedPlugNoFlavor(c *C) {
	spec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.noFlavorPlug.Snap(), nil))
	err := spec.AddConnectedPlug(s.iface, s.noFlavorPlug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(spec.Snippets(), HasLen, 0)
}

func (s *sdControlSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
