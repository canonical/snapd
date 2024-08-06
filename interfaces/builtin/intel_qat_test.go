// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type intelQatSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

const intelQatMockPlugSnapInfoYaml = `name: qat
version: 1.0
apps:
 app:
  command: foo
  plugs: [intel-qat]
`

const intelQatCoreYaml = `name: core
version: 0
type: os
slots:
  intel-qat:
`

var _ = Suite(&intelQatSuite{
	iface: builtin.MustInterface("intel-qat"),
})

func (s *intelQatSuite) SetUpTest(c *C) {
	s.slot, s.slotInfo = MockConnectedSlot(c, intelQatCoreYaml, nil, "intel-qat")
	s.plug, s.plugInfo = MockConnectedPlug(c, intelQatMockPlugSnapInfoYaml, nil, "intel-qat")
}

func (s *intelQatSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "intel-qat")
}

func (s *intelQatSuite) TestUsedSecuritySystems(c *C) {
	// connected plugs have a non-nil security snippet for apparmor
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	apparmorSpec := apparmor.NewSpecification(appSet)
	err = apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), HasLen, 1)

	// connected plugs have a nil security snippet for seccomp
	appSet, err = interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	seccompSpec := seccomp.NewSpecification(appSet)
	err = seccompSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(seccompSpec.Snippets(), HasLen, 0)
}

func (s *intelQatSuite) TestConnectedPlugSnippet(c *C) {
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	apparmorSpec := apparmor.NewSpecification(appSet)
	err = apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.qat.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.qat.app"), testutil.Contains, "/dev/vfio/* rw,\n")
	c.Assert(apparmorSpec.SnippetForTag("snap.qat.app"), testutil.Contains, "/sys/kernel/iommu_groups/{,**} r,\n")
	c.Assert(apparmorSpec.SnippetForTag("snap.qat.app"), testutil.Contains, "/sys/devices/pci*/**/{device,vendor} r,\n")
	c.Assert(apparmorSpec.SnippetForTag("snap.qat.app"), testutil.Contains, "/sys/bus/pci/drivers/4xxx/{,**} r,\n")
	c.Assert(apparmorSpec.SnippetForTag("snap.qat.app"), testutil.Contains, "/dev/qat_adf_ctl rw,\n")
	c.Assert(apparmorSpec.SnippetForTag("snap.qat.app"), testutil.Contains, "/run/qat/qatmgr.sock rw,\n")

	appSet, err = interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	seccompSpec := seccomp.NewSpecification(appSet)
	err = seccompSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(seccompSpec.SecurityTags(), DeepEquals, []string(nil))
}

func (s *intelQatSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *intelQatSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *intelQatSuite) TestUDevConnectedPlug(c *C) {
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	spec := udev.NewSpecification(appSet)
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.Snippets(), HasLen, 4)
	c.Assert(spec.Snippets(), testutil.Contains, `# intel-qat
SUBSYSTEM=="vfio", KERNEL=="*", TAG+="snap_qat_app"`)
	c.Assert(spec.Snippets(), testutil.Contains, `# intel-qat
SUBSYSTEM=="misc", KERNEL=="vfio", TAG+="snap_qat_app"`)
	c.Assert(spec.Snippets(), testutil.Contains, `# intel-qat
SUBSYSTEM=="qat_adf_ctl", KERNEL=="qat_adf_ctl", TAG+="snap_qat_app"`)
	c.Assert(spec.Snippets(), testutil.Contains, fmt.Sprintf(
		`TAG=="snap_qat_app", SUBSYSTEM!="module", SUBSYSTEM!="subsystem", RUN+="%v/snap-device-helper $env{ACTION} snap_qat_app $devpath $major:$minor"`, dirs.DistroLibExecDir))
}

func (s *intelQatSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, true)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows access to Intel QuickAssist Technology (QAT)`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "deny-auto-connection: true")
}

func (s *intelQatSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
