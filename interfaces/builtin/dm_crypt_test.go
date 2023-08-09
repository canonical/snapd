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
	"github.com/snapcore/snapd/interfaces/kmod"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type DmCryptInterfaceSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

var _ = Suite(&DmCryptInterfaceSuite{
	iface: builtin.MustInterface("dm-crypt"),
})

const dmCryptConsumerYaml = `name: consumer
version: 0
apps:
 app:
  plugs: [dm-crypt]
`

const dmCryptCoreYaml = `name: core
version: 0
type: os
slots:
  dm-crypt:
`

func (s *DmCryptInterfaceSuite) SetUpTest(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, dmCryptConsumerYaml, nil, "dm-crypt")
	s.slot, s.slotInfo = MockConnectedSlot(c, dmCryptCoreYaml, nil, "dm-crypt")
}

func (s *DmCryptInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "dm-crypt")
}

func (s *DmCryptInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *DmCryptInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *DmCryptInterfaceSuite) TestAppArmorSpec(c *C) {
	spec := &apparmor.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "/dev/mapper/control")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "/dev/dm-[0-9]*")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "/run/systemd/seats/*")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "/{,var/}run/cryptsetup/ rw,")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "/{,var/}run/cryptsetup/* rwk,")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "/{,run/}media/{,**}")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "mount options=(ro,nosuid,nodev) /dev/dm-[0-9]*")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "mount options=(rw,nosuid,nodev) /dev/dm-[0-9]*")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "/{,usr/}sbin/cryptsetup ixr,")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "/{,usr/}bin/mount ixr,")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "/{,usr/}bin/umount ixr,")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "/{,var/}run/mount/utab* wrlk,")
}

func (s *DmCryptInterfaceSuite) TestUDevSpec(c *C) {
	spec := &udev.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.Snippets(), HasLen, 4)
	c.Assert(spec.Snippets(), testutil.Contains, `# dm-crypt
KERNEL=="device-mapper", TAG+="snap_consumer_app"`)
	c.Assert(spec.Snippets(), testutil.Contains, `# dm-crypt
KERNEL=="dm-[0-9]", TAG+="snap_consumer_app"`)
	c.Assert(spec.Snippets(), testutil.Contains, `# dm-crypt
SUBSYSTEM=="block", TAG+="snap_consumer_app"`)
	c.Assert(spec.Snippets(), testutil.Contains,
		fmt.Sprintf(`TAG=="snap_consumer_app", SUBSYSTEM!="module", SUBSYSTEM!="subsystem", RUN+="%v/snap-device-helper snap_consumer_app"`, dirs.DistroLibExecDir))
}

func (s *DmCryptInterfaceSuite) TestSeccompSpec(c *C) {
	spec := &seccomp.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "add_key\n")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "keyctl\n")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "request_key\n")
}

func (s *DmCryptInterfaceSuite) TestKModSpec(c *C) {
	spec := &kmod.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.Modules(), DeepEquals, map[string]bool{
		"dm_crypt": true,
	})
}

func (s *DmCryptInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, true)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows encryption and decryption of block storage devices`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "dm-crypt")
	c.Assert(si.BaseDeclarationPlugs, testutil.Contains, "allow-installation: false")
}

func (s *DmCryptInterfaceSuite) TestAutoConnect(c *C) {
	c.Assert(s.iface.AutoConnect(s.plugInfo, s.slotInfo), Equals, true)
}

func (s *DmCryptInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
