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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type dspSuite struct {
	iface interfaces.Interface

	ambarellaSlotInfo *snap.SlotInfo
	ambarellaSlot     *interfaces.ConnectedSlot
	noFlavorSlotInfo  *snap.SlotInfo
	noFlavorSlot      *interfaces.ConnectedSlot
	plugInfo          *snap.PlugInfo
	plug              *interfaces.ConnectedPlug
}

var _ = Suite(&dspSuite{
	iface: builtin.MustInterface("dsp"),
})

const dspMockPlugSnapInfoYaml = `
name: my-device
version: 1.0
apps:
  svc:
    command: bin/foo.sh
    plugs:
      - dsp
`

const gadgetDspSlotYaml = `
name: my-gadget
version: 1.0
type: gadget
slots:
  dsp-ambarella:
    interface: dsp
    flavor: ambarella
  dsp-no-flavor:
    interface: dsp
`

func (s *dspSuite) SetUpTest(c *C) {
	s.noFlavorSlot, s.noFlavorSlotInfo = MockConnectedSlot(c, gadgetDspSlotYaml, nil, "dsp-no-flavor")
	s.ambarellaSlot, s.ambarellaSlotInfo = MockConnectedSlot(c, gadgetDspSlotYaml, nil, "dsp-ambarella")
	s.plug, s.plugInfo = MockConnectedPlug(c, dspMockPlugSnapInfoYaml, nil, "dsp")
}

func (s *dspSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "dsp")
}

func (s *dspSuite) TestSanitizeSlotNoFlavor(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.noFlavorSlotInfo), IsNil)
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.ambarellaSlotInfo), IsNil)
}

func (s *dspSuite) TestSanitizeSlotAmbarella(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.ambarellaSlotInfo), IsNil)
}

func (s *dspSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *dspSuite) TestApparmorConnectedPlugAmbarella(c *C) {
	appSet := mylog.Check2(interfaces.NewSnapAppSet(s.plug.Snap(), nil))

	spec := apparmor.NewSpecification(appSet)
	mylog.Check(spec.AddConnectedPlug(s.iface, s.plug, s.ambarellaSlot))

	c.Assert(spec.SnippetForTag("snap.my-device.svc"), testutil.Contains, "/proc/ambarella/vin[0-9]_idsp r,\n")
}

func (s *dspSuite) TestUDevConnectedPlugAmbarella(c *C) {
	appSet := mylog.Check2(interfaces.NewSnapAppSet(s.plug.Snap(), nil))

	spec := udev.NewSpecification(appSet)
	mylog.Check(spec.AddConnectedPlug(s.iface, s.plug, s.ambarellaSlot))

	c.Assert(spec.Snippets(), HasLen, 6)
	c.Assert(spec.Snippets(), testutil.Contains, `# dsp
KERNEL=="iav", TAG+="snap_my-device_svc"`)
	c.Assert(spec.Snippets(), testutil.Contains, fmt.Sprintf(`TAG=="snap_my-device_svc", SUBSYSTEM!="module", SUBSYSTEM!="subsystem", RUN+="%v/snap-device-helper $env{ACTION} snap_my-device_svc $devpath $major:$minor"`, dirs.DistroLibExecDir))
}

func (s *dspSuite) TestUDevConnectedPlugNoFlavor(c *C) {
	appSet := mylog.Check2(interfaces.NewSnapAppSet(s.plug.Snap(), nil))

	spec := udev.NewSpecification(appSet)
	mylog.Check(spec.AddConnectedPlug(s.iface, s.plug, s.noFlavorSlot))

	c.Assert(spec.Snippets(), HasLen, 0)
}

func (s *dspSuite) TestApparmorConnectedPlugNoFlavor(c *C) {
	appSet := mylog.Check2(interfaces.NewSnapAppSet(s.plug.Snap(), nil))

	spec := apparmor.NewSpecification(appSet)
	mylog.Check(spec.AddConnectedPlug(s.iface, s.plug, s.noFlavorSlot))

	c.Assert(spec.Snippets(), HasLen, 0)
}

func (s *dspSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
