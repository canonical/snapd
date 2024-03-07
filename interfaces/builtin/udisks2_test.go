// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/dbus"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type UDisks2InterfaceSuite struct {
	testutil.BaseTest

	iface                interfaces.Interface
	slotInfo             *snap.SlotInfo
	slot                 *interfaces.ConnectedSlot
	slotInfoWithUdevFile *snap.SlotInfo
	slotWithUdevFile     *interfaces.ConnectedSlot
	classicSlotInfo      *snap.SlotInfo
	classicSlot          *interfaces.ConnectedSlot
	plugInfo             *snap.PlugInfo
	plug                 *interfaces.ConnectedPlug
}

var _ = Suite(&UDisks2InterfaceSuite{
	iface: builtin.MustInterface("udisks2"),
})

const udisks2ConsumerYaml = `name: consumer
version: 0
apps:
 app:
  plugs: [udisks2]
`

const udisks2ConsumerTwoAppsYaml = `name: consumer
version: 0
apps:
 app1:
  plugs: [udisks2]
 app2:
  plugs: [udisks2]
`

const udisks2ConsumerThreeAppsYaml = `name: consumer
version: 0
apps:
 app1:
  plugs: [udisks2]
 app2:
  plugs: [udisks2]
 app3:
`

const udisks2ProducerYaml = `name: producer
version: 0
apps:
 app:
  slots: [udisks2]
`

const udisks2WithUdevFileProducerYaml = `name: producer
version: 0
slots:
  udisks2:
    interface: udisks2
    udev-file: lib/udev/rules.d/99-udisks.rules
apps:
 app:
  slots: [udisks2]
`

const udisks2WithUdevFileProducerYamlTemplate = `name: producer
version: 0
slots:
  udisks2:
    interface: udisks2
    udev-file: %s
apps:
 app:
  slots: [udisks2]
`

const udisks2ProducerTwoAppsYaml = `name: producer
version: 0
apps:
 app1:
  slots: [udisks2]
 app2:
  slots: [udisks2]
`

const udisks2ProducerThreeAppsYaml = `name: producer
version: 0
apps:
 app1:
  slots: [udisks2]
 app2:
 app3:
  slots: [udisks2]
`

const udisks2ClassicYaml = `name: core
version: 0
type: os
slots:
 udisks2:
  interface: udisks2
`

func (s *UDisks2InterfaceSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() {
		dirs.SetRootDir("/")
	})

	s.plug, s.plugInfo = MockConnectedPlug(c, udisks2ConsumerYaml, nil, "udisks2")
	s.slot, s.slotInfo = MockConnectedSlot(c, udisks2ProducerYaml, nil, "udisks2")
	s.slotWithUdevFile, s.slotInfoWithUdevFile = MockConnectedSlot(c, udisks2WithUdevFileProducerYaml, nil, "udisks2")
	s.classicSlot, s.classicSlotInfo = MockConnectedSlot(c, udisks2ClassicYaml, nil, "udisks2")

	producerDir := s.slotInfoWithUdevFile.Snap.MountDir()
	ruleFile := filepath.Join(producerDir, "lib/udev/rules.d/99-udisks.rules")
	os.MkdirAll(filepath.Dir(ruleFile), 0777)
	err := os.WriteFile(ruleFile, []byte("# Test UDev file\n"), 0777)
	c.Assert(err, IsNil)
}

func (s *UDisks2InterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "udisks2")
}

func (s *UDisks2InterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.classicSlotInfo), IsNil)
}

func (s *UDisks2InterfaceSuite) TestAppArmorSpec(c *C) {
	// The label uses short form when exactly one app is bound to the udisks2 slot
	spec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap(), nil))
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `peer=(label="snap.producer.app"),`)

	// The label glob when all apps are bound to the udisks2 slot
	slot, _ := MockConnectedSlot(c, udisks2ProducerTwoAppsYaml, nil, "udisks2")
	spec = apparmor.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap(), nil))
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `peer=(label="snap.producer.*"),`)

	// The label uses alternation when some, but not all, apps is bound to the udisks2 slot
	slot, _ = MockConnectedSlot(c, udisks2ProducerThreeAppsYaml, nil, "udisks2")
	spec = apparmor.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap(), nil))
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `peer=(label="snap.producer.{app1,app3}"),`)

	// The label uses short form when exactly one app is bound to the udisks2 plug
	spec = apparmor.NewSpecification(interfaces.NewSnapAppSet(s.slot.Snap(), nil))
	c.Assert(spec.AddConnectedSlot(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.producer.app"})
	c.Assert(spec.SnippetForTag("snap.producer.app"), testutil.Contains, `peer=(label="snap.consumer.app"),`)

	// The label glob when all apps are bound to the udisks2 plug
	plug, _ := MockConnectedPlug(c, udisks2ConsumerTwoAppsYaml, nil, "udisks2")
	spec = apparmor.NewSpecification(interfaces.NewSnapAppSet(s.slot.Snap(), nil))
	c.Assert(spec.AddConnectedSlot(s.iface, plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.producer.app"})
	c.Assert(spec.SnippetForTag("snap.producer.app"), testutil.Contains, `peer=(label="snap.consumer.*"),`)

	// The label uses alternation when some, but not all, apps is bound to the udisks2 plug
	plug, _ = MockConnectedPlug(c, udisks2ConsumerThreeAppsYaml, nil, "udisks2")
	spec = apparmor.NewSpecification(interfaces.NewSnapAppSet(s.slot.Snap(), nil))
	c.Assert(spec.AddConnectedSlot(s.iface, plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.producer.app"})
	c.Assert(spec.SnippetForTag("snap.producer.app"), testutil.Contains, `peer=(label="snap.consumer.{app1,app2}"),`)

	// permanent slot have a non-nil security snippet for apparmor
	spec = apparmor.NewSpecification(interfaces.NewSnapAppSet(s.slotInfo.Snap, nil))
	c.Assert(spec.AddPermanentSlot(s.iface, s.slotInfo), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.producer.app"})
	c.Assert(spec.SnippetForTag("snap.producer.app"), testutil.Contains, `peer=(label=unconfined),`)

	spec = apparmor.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap(), nil))
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `peer=(label="snap.producer.app"),`)
}

func (s *UDisks2InterfaceSuite) TestAppArmorSpecOnClassic(c *C) {
	// connected plug to core slot
	spec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap(), nil))
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.classicSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `peer=(label=unconfined),`)

	// connected classic slot to plug
	spec = apparmor.NewSpecification(interfaces.NewSnapAppSet(s.classicSlot.Snap(), nil))
	c.Assert(spec.AddConnectedSlot(s.iface, s.plug, s.classicSlot), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)

	// permanent classic slot
	spec = apparmor.NewSpecification(interfaces.NewSnapAppSet(s.classicSlotInfo.Snap, nil))
	c.Assert(spec.AddPermanentSlot(s.iface, s.classicSlotInfo), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)
}

func (s *UDisks2InterfaceSuite) TestDBusSpec(c *C) {
	spec := dbus.NewSpecification(interfaces.NewSnapAppSet(s.slotInfo.Snap, nil))
	c.Assert(spec.AddPermanentSlot(s.iface, s.slotInfo), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.producer.app"})
	c.Assert(spec.SnippetForTag("snap.producer.app"), testutil.Contains, `<policy user="root">`)
}

func (s *UDisks2InterfaceSuite) TestDBusSpecOnClassic(c *C) {
	// connected plug to core slot
	spec := dbus.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap(), nil))
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.classicSlot), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)

	spec = dbus.NewSpecification(interfaces.NewSnapAppSet(s.classicSlotInfo.Snap, nil))
	c.Assert(spec.AddPermanentSlot(s.iface, s.classicSlotInfo), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)
}

func (s *UDisks2InterfaceSuite) TestUDevSpec(c *C) {
	spec := udev.NewSpecification(interfaces.NewSnapAppSet(s.slotInfo.Snap, nil))
	c.Assert(spec.AddPermanentSlot(s.iface, s.slotInfo), IsNil)
	c.Assert(spec.Snippets(), HasLen, 4)
	c.Assert(spec.Snippets()[0], testutil.Contains, `LABEL="udisks_probe_end"`)
	c.Assert(spec.Snippets(), testutil.Contains, `# udisks2
SUBSYSTEM=="block", TAG+="snap_producer_app"`)
	c.Assert(spec.Snippets(), testutil.Contains, `# udisks2
SUBSYSTEM=="usb", TAG+="snap_producer_app"`)
	c.Assert(spec.Snippets(), testutil.Contains, fmt.Sprintf(`TAG=="snap_producer_app", SUBSYSTEM!="module", SUBSYSTEM!="subsystem", RUN+="%v/snap-device-helper $env{ACTION} snap_producer_app $devpath $major:$minor"`, dirs.DistroLibExecDir))
}

func (s *UDisks2InterfaceSuite) TestUDevSpecFile(c *C) {
	spec := udev.NewSpecification(interfaces.NewSnapAppSet(s.slotInfoWithUdevFile.Snap, nil))
	c.Assert(spec.AddPermanentSlot(s.iface, s.slotInfoWithUdevFile), IsNil)
	c.Assert(spec.Snippets(), HasLen, 1)
	c.Assert(spec.Snippets()[0], testutil.Contains, `# Test UDev file`)
}

func (s *UDisks2InterfaceSuite) TestUDevSpecFileEvilPathRel(c *C) {
	outsideFile := filepath.Join(dirs.GlobalRootDir, "outside")
	c.Assert(os.WriteFile(outsideFile, []byte(""), 0777), IsNil)
	producerDir := s.slotInfoWithUdevFile.Snap.MountDir()
	outsideFileRel, err := filepath.Rel(producerDir, outsideFile)
	c.Assert(err, IsNil)

	yaml := fmt.Sprintf(udisks2WithUdevFileProducerYamlTemplate, outsideFileRel)
	_, slotInfo := MockConnectedSlot(c, yaml, nil, "udisks2")

	spec := udev.NewSpecification(interfaces.NewSnapAppSet(slotInfo.Snap, nil))
	c.Assert(spec.AddPermanentSlot(s.iface, slotInfo), ErrorMatches, "cannot resolve udev-file: invalid escaping path")
}

func (s *UDisks2InterfaceSuite) TestUDevSpecFileEvilAbsSymlink(c *C) {
	outsideFile := filepath.Join(dirs.GlobalRootDir, "outside")
	c.Assert(os.WriteFile(outsideFile, []byte(""), 0777), IsNil)
	producerDir := s.slotInfoWithUdevFile.Snap.MountDir()
	c.Assert(os.Symlink(outsideFile, filepath.Join(producerDir, "link")), IsNil)

	yaml := fmt.Sprintf(udisks2WithUdevFileProducerYamlTemplate, "link")
	_, slotInfo := MockConnectedSlot(c, yaml, nil, "udisks2")

	spec := udev.NewSpecification(interfaces.NewSnapAppSet(slotInfo.Snap, nil))
	c.Assert(spec.AddPermanentSlot(s.iface, slotInfo), ErrorMatches, "cannot resolve udev-file: invalid absolute symlink")
}

func (s *UDisks2InterfaceSuite) TestUDevSpecFileEvilRelSymlink(c *C) {
	outsideFile := filepath.Join(dirs.GlobalRootDir, "outside")
	c.Assert(os.WriteFile(outsideFile, []byte(""), 0777), IsNil)
	producerDir := s.slotInfoWithUdevFile.Snap.MountDir()
	outsideFileRel, err := filepath.Rel(producerDir, outsideFile)
	c.Assert(err, IsNil)
	c.Assert(os.Symlink(outsideFileRel, filepath.Join(producerDir, "link")), IsNil)

	yaml := fmt.Sprintf(udisks2WithUdevFileProducerYamlTemplate, "link")
	_, slotInfo := MockConnectedSlot(c, yaml, nil, "udisks2")

	spec := udev.NewSpecification(interfaces.NewSnapAppSet(slotInfo.Snap, nil))
	c.Assert(spec.AddPermanentSlot(s.iface, slotInfo), ErrorMatches, "cannot resolve udev-file: invalid escaping path")
}

func (s *UDisks2InterfaceSuite) TestUDevSpecFileDoesNotExist(c *C) {
	yaml := fmt.Sprintf(udisks2WithUdevFileProducerYamlTemplate, "non-existent")
	_, slotInfo := MockConnectedSlot(c, yaml, nil, "udisks2")

	spec := udev.NewSpecification(interfaces.NewSnapAppSet(slotInfo.Snap, nil))
	c.Assert(spec.AddPermanentSlot(s.iface, slotInfo), ErrorMatches, "cannot resolve udev-file: .*")
}

func (s *UDisks2InterfaceSuite) TestUDevSpecFileCannotOpen(c *C) {
	producerDir := s.slotInfoWithUdevFile.Snap.MountDir()
	c.Assert(os.WriteFile(filepath.Join(producerDir, "non-readable"), []byte(""), 0222), IsNil)
	yaml := fmt.Sprintf(udisks2WithUdevFileProducerYamlTemplate, "non-readable")
	_, slotInfo := MockConnectedSlot(c, yaml, nil, "udisks2")

	spec := udev.NewSpecification(interfaces.NewSnapAppSet(slotInfo.Snap, nil))
	c.Assert(spec.AddPermanentSlot(s.iface, slotInfo), ErrorMatches, "cannot open udev-file: .*")
}

func (s *UDisks2InterfaceSuite) TestUDevSpecOnClassic(c *C) {
	spec := udev.NewSpecification(interfaces.NewSnapAppSet(s.classicSlotInfo.Snap, nil))
	c.Assert(spec.AddPermanentSlot(s.iface, s.classicSlotInfo), IsNil)
	c.Assert(spec.Snippets(), HasLen, 0)
}

func (s *UDisks2InterfaceSuite) TestSecCompSpec(c *C) {
	spec := seccomp.NewSpecification(interfaces.NewSnapAppSet(s.slotInfo.Snap, nil))
	c.Assert(spec.AddPermanentSlot(s.iface, s.slotInfo), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.producer.app"})
	c.Assert(spec.SnippetForTag("snap.producer.app"), testutil.Contains, "mount\n")
}

func (s *UDisks2InterfaceSuite) TestSecCompSpecOnClassic(c *C) {
	spec := seccomp.NewSpecification(interfaces.NewSnapAppSet(s.classicSlotInfo.Snap, nil))
	c.Assert(spec.AddPermanentSlot(s.iface, s.classicSlotInfo), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)
}

func (s *UDisks2InterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, false)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows operating as or interacting with the UDisks2 service`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "udisks2")
}

func (s *UDisks2InterfaceSuite) TestAutoConnect(c *C) {
	c.Assert(s.iface.AutoConnect(s.plugInfo, s.slotInfo), Equals, true)
}

func (s *UDisks2InterfaceSuite) TestInterfaces(c *C) {
	c.Assert(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
