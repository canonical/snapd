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

type mediatekAccelSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

const mediatekAccelWithNoUnitEnabledMockPlugSnapInfoYaml = `name: consumer
version: 1.0
apps:
 app:
  command: foo
  plugs: [mediatek-accel]
`

const mediatekAccelWithVcuMockPlugSnapInfoYaml = `name: consumer
version: 1.0
apps:
 app:
  command: foo
  plugs: [mediatek-accel]
plugs:
 mediatek-accel:
  vcu: true
`
const mediatekAccelWithApuMockPlugSnapInfoYaml = `name: consumer
version: 1.0
apps:
 app:
  command: foo
  plugs: [mediatek-accel]
plugs:
 mediatek-accel:
  apu: true
`
const mediatekAccelMockPlugSnapInfoYaml = `name: consumer
version: 1.0
apps:
 app:
  command: foo
  plugs: [mediatek-accel]
plugs:
 mediatek-accel:
  vcu: true
  apu: true
`

const mediatekAccelCoreYaml = `name: core
version: 0
type: os
slots:
  mediatek-accel:
`

var _ = Suite(&mediatekAccelSuite{
	iface: builtin.MustInterface("mediatek-accel"),
})

func (s *mediatekAccelSuite) SetUpTest(c *C) {
	s.slot, s.slotInfo = MockConnectedSlot(c, mediatekAccelCoreYaml, nil, "mediatek-accel")
	s.plug, s.plugInfo = MockConnectedPlug(c, mediatekAccelMockPlugSnapInfoYaml, nil, "mediatek-accel")
}

func (s *mediatekAccelSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "mediatek-accel")
}

func (s *mediatekAccelSuite) TestUsedSecuritySystems(c *C) {
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

func (s *mediatekAccelSuite) TestConnectedPlugSnippet(c *C) {
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	apparmorSpec := apparmor.NewSpecification(appSet)
	err = apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.consumer.app"), testutil.Contains, "/dev/apusys rw,\n")
	c.Assert(apparmorSpec.SnippetForTag("snap.consumer.app"), testutil.Contains, "/dev/vcu rw,\n")
	c.Assert(apparmorSpec.SnippetForTag("snap.consumer.app"), testutil.Contains, "/dev/vcu[0-9]* rw,\n")

	appSet, err = interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	seccompSpec := seccomp.NewSpecification(appSet)
	err = seccompSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(seccompSpec.SecurityTags(), DeepEquals, []string(nil))
}

func (s *mediatekAccelSuite) TestConnectedPlugWithVcuSnippet(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, mediatekAccelWithVcuMockPlugSnapInfoYaml, nil, "mediatek-accel")
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	apparmorSpec := apparmor.NewSpecification(appSet)
	err = apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.consumer.app"), testutil.Contains, "/dev/vcu rw,\n")
	c.Assert(apparmorSpec.SnippetForTag("snap.consumer.app"), testutil.Contains, "/dev/vcu[0-9]* rw,\n")

	appSet, err = interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	seccompSpec := seccomp.NewSpecification(appSet)
	err = seccompSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(seccompSpec.SecurityTags(), DeepEquals, []string(nil))
}

func (s *mediatekAccelSuite) TestConnectedPlugWithApuSnippet(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, mediatekAccelWithApuMockPlugSnapInfoYaml, nil, "mediatek-accel")
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	apparmorSpec := apparmor.NewSpecification(appSet)
	err = apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.consumer.app"), testutil.Contains, "/dev/apusys rw,\n")

	appSet, err = interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	seccompSpec := seccomp.NewSpecification(appSet)
	err = seccompSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(seccompSpec.SecurityTags(), DeepEquals, []string(nil))
}

func (s *mediatekAccelSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *mediatekAccelSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *mediatekAccelSuite) TestSanitizePlugWithVcu(c *C) {
	_, s.plugInfo = MockConnectedPlug(c, mediatekAccelWithVcuMockPlugSnapInfoYaml, nil, "mediatek-accel")
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *mediatekAccelSuite) TestSanitizePlugWithNoUnitEnabled(c *C) {
	_, s.plugInfo = MockConnectedPlug(c, mediatekAccelWithNoUnitEnabledMockPlugSnapInfoYaml, nil, "mediatek-accel")
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), ErrorMatches,
		`cannot connect mediatek-accel interface without any units enabled`)
}

func (s *mediatekAccelSuite) TestSanitizePlugWithInvalid(c *C) {
	const badVcu = `name: consumer
version: 0
apps:
 app:
  plugs: [mediatek-accel]
plugs:
 mediatek-accel:
  vcu: yes-please
`
	_, s.plugInfo = MockConnectedPlug(c, badVcu, nil, "mediatek-accel")
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), ErrorMatches,
		`mediatek-accel "vcu" attribute must be boolean`)

	const badApu = `name: consumer
version: 0
apps:
 app:
  plugs: [mediatek-accel]
plugs:
 mediatek-accel:
  apu: no-sorry
`
	_, s.plugInfo = MockConnectedPlug(c, badApu, nil, "mediatek-accel")
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), ErrorMatches,
		`mediatek-accel "apu" attribute must be boolean`)
}

func (s *mediatekAccelSuite) TestUDevConnectedPlug(c *C) {
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	spec := udev.NewSpecification(appSet)
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.Snippets(), HasLen, 4)
	c.Assert(spec.Snippets(), testutil.Contains, `# mediatek-accel
SUBSYSTEM=="misc", KERNEL=="apusys", TAG+="snap_consumer_app"`)
	c.Assert(spec.Snippets(), testutil.Contains, `# mediatek-accel
SUBSYSTEM=="vcu", KERNEL=="vcu", TAG+="snap_consumer_app"`)
	c.Assert(spec.Snippets(), testutil.Contains, `# mediatek-accel
SUBSYSTEM=="vcu[0-9]*", KERNEL=="vcu[0-9]*", TAG+="snap_consumer_app"`)
	c.Assert(spec.Snippets(), testutil.Contains, fmt.Sprintf(
		`TAG=="snap_consumer_app", SUBSYSTEM!="module", SUBSYSTEM!="subsystem", RUN+="%v/snap-device-helper $env{ACTION} snap_consumer_app $devpath $major:$minor"`, dirs.DistroLibExecDir))
}

func (s *mediatekAccelSuite) TestUDevVcuConnectedPlug(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, mediatekAccelWithVcuMockPlugSnapInfoYaml, nil, "mediatek-accel")
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	spec := udev.NewSpecification(appSet)
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.Snippets(), HasLen, 3)
	c.Assert(spec.Snippets(), testutil.Contains, `# mediatek-accel
SUBSYSTEM=="vcu", KERNEL=="vcu", TAG+="snap_consumer_app"`)
	c.Assert(spec.Snippets(), testutil.Contains, `# mediatek-accel
SUBSYSTEM=="vcu[0-9]*", KERNEL=="vcu[0-9]*", TAG+="snap_consumer_app"`)
	c.Assert(spec.Snippets(), testutil.Contains, fmt.Sprintf(
		`TAG=="snap_consumer_app", SUBSYSTEM!="module", SUBSYSTEM!="subsystem", RUN+="%v/snap-device-helper $env{ACTION} snap_consumer_app $devpath $major:$minor"`, dirs.DistroLibExecDir))
}

func (s *mediatekAccelSuite) TestUDevApuConnectedPlug(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, mediatekAccelWithApuMockPlugSnapInfoYaml, nil, "mediatek-accel")
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	spec := udev.NewSpecification(appSet)
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.Snippets(), HasLen, 2)
	c.Assert(spec.Snippets(), testutil.Contains, `# mediatek-accel
SUBSYSTEM=="misc", KERNEL=="apusys", TAG+="snap_consumer_app"`)
	c.Assert(spec.Snippets(), testutil.Contains, fmt.Sprintf(
		`TAG=="snap_consumer_app", SUBSYSTEM!="module", SUBSYSTEM!="subsystem", RUN+="%v/snap-device-helper $env{ACTION} snap_consumer_app $devpath $major:$minor"`, dirs.DistroLibExecDir))
}

func (s *mediatekAccelSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, true)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows access to the hardware accelerators on MediaTek Genio devices`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "deny-auto-connection: true")
}

func (s *mediatekAccelSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
