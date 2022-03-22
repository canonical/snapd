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
	"errors"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type Pkcs11InterfaceSuite struct {
	testutil.BaseTest

	iface            interfaces.Interface
	testSlot0Info    *snap.SlotInfo
	testSlot0        *interfaces.ConnectedSlot
	testSlot1Info    *snap.SlotInfo
	testSlot1        *interfaces.ConnectedSlot
	testSlot2Info    *snap.SlotInfo
	testSlot2        *interfaces.ConnectedSlot
	testBadSlot0Info *snap.SlotInfo
	testBadSlot0     *interfaces.ConnectedSlot
	testBadSlot1Info *snap.SlotInfo
	testBadSlot1     *interfaces.ConnectedSlot
	testBadSlot2Info *snap.SlotInfo
	testBadSlot2     *interfaces.ConnectedSlot
	testBadSlot3Info *snap.SlotInfo
	testBadSlot3     *interfaces.ConnectedSlot
	testBadSlot4Info *snap.SlotInfo
	testBadSlot4     *interfaces.ConnectedSlot
	testBadSlot5Info *snap.SlotInfo
	testBadSlot5     *interfaces.ConnectedSlot
	testBadSlot6Info *snap.SlotInfo
	testBadSlot6     *interfaces.ConnectedSlot

	testPlug0Info *snap.PlugInfo
	testPlug0     *interfaces.ConnectedPlug
	testPlug1Info *snap.PlugInfo
	testPlug1     *interfaces.ConnectedPlug
	testPlug2Info *snap.PlugInfo
	testPlug2     *interfaces.ConnectedPlug
}

var _ = Suite(&Pkcs11InterfaceSuite{
	iface: builtin.MustInterface("pkcs11"),
})

func (s *Pkcs11InterfaceSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	gadgetSnapInfo := snaptest.MockInfo(c, `name: gadget
version: 0
type: gadget
slots:
  pkcs11-optee-slot-0:
    interface: pkcs11
    pkcs11-socket: /run/p11-kit/pkcs11-optee-slot-0
  pkcs11-optee-slot-1:
    interface: pkcs11
    pkcs11-socket: /run/p11-kit/pkcs11-optee-slot-1
  pkcs11-optee-slot-2:
    interface: pkcs11
    pkcs11-socket: /run/p11-kit/pkcs11-optee-slot-2
  pkcs11-bad-optee-slot-0:
    interface: pkcs11
    pkcs11-socket: /run/p12-kit/pkcs11-optee-slot-0
  pkcs11-bad-optee-slot-1:
    interface: pkcs11
    pkcs11-socket: /run/p11-kit/spkcs11-optee-slot-1
  pkcs11-bad-optee-slot-2:
    interface: pkcs11
    pkcs11-socket: /run/p11-kit/pkcs11a-optee-slot-2
  pkcs11-bad-optee-slot-3:
    interface: pkcs11
    pkcs11-socket: 22
  pkcs11-bad-optee-slot-4:
    interface: pkcs11
    pkcs11-socket: /run/p11-kit/pkcs11-optee-slot-*
  pkcs11-bad-optee-slot-5:
    interface: pkcs11
    pkcs11-socket: /run/p11-kit/../pkcs11-optee-slot-0
  pkcs11-bad-optee-slot-6:
    interface: pkcs11

apps:
  p11-server:
    slots:
      - pkcs11-optee-slot-0
      - pkcs11-optee-slot-1
`, nil)
	s.testSlot0Info = gadgetSnapInfo.Slots["pkcs11-optee-slot-0"]
	s.testSlot0 = interfaces.NewConnectedSlot(s.testSlot0Info, nil, nil)
	s.testSlot1Info = gadgetSnapInfo.Slots["pkcs11-optee-slot-1"]
	s.testSlot1 = interfaces.NewConnectedSlot(s.testSlot1Info, nil, nil)
	s.testSlot2Info = gadgetSnapInfo.Slots["pkcs11-optee-slot-2"]
	s.testSlot2 = interfaces.NewConnectedSlot(s.testSlot2Info, nil, nil)
	s.testBadSlot0Info = gadgetSnapInfo.Slots["pkcs11-bad-optee-slot-0"]
	s.testBadSlot0 = interfaces.NewConnectedSlot(s.testBadSlot0Info, nil, nil)
	s.testBadSlot1Info = gadgetSnapInfo.Slots["pkcs11-bad-optee-slot-1"]
	s.testBadSlot1 = interfaces.NewConnectedSlot(s.testBadSlot1Info, nil, nil)
	s.testBadSlot2Info = gadgetSnapInfo.Slots["pkcs11-bad-optee-slot-2"]
	s.testBadSlot2 = interfaces.NewConnectedSlot(s.testBadSlot2Info, nil, nil)
	s.testBadSlot3Info = gadgetSnapInfo.Slots["pkcs11-bad-optee-slot-3"]
	s.testBadSlot3 = interfaces.NewConnectedSlot(s.testBadSlot3Info, nil, nil)
	s.testBadSlot4Info = gadgetSnapInfo.Slots["pkcs11-bad-optee-slot-4"]
	s.testBadSlot4 = interfaces.NewConnectedSlot(s.testBadSlot4Info, nil, nil)
	s.testBadSlot5Info = gadgetSnapInfo.Slots["pkcs11-bad-optee-slot-5"]
	s.testBadSlot5 = interfaces.NewConnectedSlot(s.testBadSlot5Info, nil, nil)
	s.testBadSlot6Info = gadgetSnapInfo.Slots["pkcs11-bad-optee-slot-6"]
	s.testBadSlot6 = interfaces.NewConnectedSlot(s.testBadSlot6Info, nil, nil)

	consumingSnapInfo := snaptest.MockInfo(c, `name: consumer
version: 0
plugs:
  plug-for-socket-0:
    interface: pkcs11
  plug-for-socket-1:
    interface: pkcs11
  plug-for-socket-2:
    interface: pkcs11

apps:
  app-accessing-1-slot:
    command: foo
    plugs: [plug-for-socket-0]
  app-accessing-2-slots:
    command: bar
    plugs: [plug-for-socket-0, plug-for-socket-1]
  app-accessing-3rd-slot:
    command: foo
    plugs: [plug-for-socket-2]
`, nil)
	s.testPlug0Info = consumingSnapInfo.Plugs["plug-for-socket-0"]
	s.testPlug0 = interfaces.NewConnectedPlug(s.testPlug0Info, nil, nil)
	s.testPlug1Info = consumingSnapInfo.Plugs["plug-for-socket-1"]
	s.testPlug1 = interfaces.NewConnectedPlug(s.testPlug1Info, nil, nil)
	s.testPlug2Info = consumingSnapInfo.Plugs["plug-for-socket-2"]
	s.testPlug2 = interfaces.NewConnectedPlug(s.testPlug2Info, nil, nil)
}

func (s *Pkcs11InterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "pkcs11")
}

func (s *Pkcs11InterfaceSuite) TestPermanentSlotSnippetAppArmor(c *C) {
	apparmorSpec := &apparmor.Specification{}

	err := apparmorSpec.AddPermanentSlot(s.iface, s.testSlot0Info)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.gadget.p11-server"})
	c.Assert(apparmorSpec.SnippetForTag("snap.gadget.p11-server"), Not(IsNil))
	c.Assert(apparmorSpec.SnippetForTag("snap.gadget.p11-server"), testutil.Contains,
		`# pkcs11 socket dir`)
	c.Assert(apparmorSpec.SnippetForTag("snap.gadget.p11-server"), testutil.Contains,
		`/{,var/}run/p11-kit/  rw,`)
	c.Assert(apparmorSpec.SnippetForTag("snap.gadget.p11-server"), testutil.Contains,
		`"/{,var/}run/p11-kit/pkcs11-optee-slot-0" rwk,`)
	c.Assert(apparmorSpec.SnippetForTag("snap.gadget.p11-server"), testutil.Contains,
		`# pkcs11 config`)
	c.Assert(apparmorSpec.SnippetForTag("snap.gadget.p11-server"), testutil.Contains,
		`/etc/pkcs11/{,**} r,`)
	c.Assert(apparmorSpec.SnippetForTag("snap.gadget.p11-server"), testutil.Contains,
		`/usr/bin/p11-kit ixr,`)
	c.Assert(apparmorSpec.SnippetForTag("snap.gadget.p11-server"), testutil.Contains,
		`/usr/bin/p11tool ixr,`)
	c.Assert(apparmorSpec.SnippetForTag("snap.gadget.p11-server"), testutil.Contains,
		`/usr/bin/pkcs11-tool ixr,`)
	c.Assert(apparmorSpec.SnippetForTag("snap.gadget.p11-server"), testutil.Contains,
		`/usr/libexec/p11-kit/p11-kit-server ixr,`)
	err = apparmorSpec.AddPermanentSlot(s.iface, s.testSlot1Info)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SnippetForTag("snap.gadget.p11-server"), testutil.Contains,
		`"/{,var/}run/p11-kit/pkcs11-optee-slot-1" rwk,`)
}

func (s *Pkcs11InterfaceSuite) TestConnectedPlugSnippetAppArmor(c *C) {
	apparmorSpec := &apparmor.Specification{}

	err := apparmorSpec.AddConnectedPlug(s.iface, s.testPlug1, s.testSlot1)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.consumer.app-accessing-2-slots"})
	c.Assert(err, IsNil)
	err = apparmorSpec.AddConnectedPlug(s.iface, s.testPlug0, s.testSlot0)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.consumer.app-accessing-1-slot", "snap.consumer.app-accessing-2-slots"})
	c.Assert(apparmorSpec.SnippetForTag("snap.consumer.app-accessing-1-slot"), Not(IsNil))
	c.Assert(apparmorSpec.SnippetForTag("snap.consumer.app-accessing-1-slot"), testutil.Contains,
		`# pkcs11 config for p11-proxy`)
	c.Assert(apparmorSpec.SnippetForTag("snap.consumer.app-accessing-1-slot"), testutil.Contains,
		`/etc/pkcs11/{,**} r,`)
	c.Assert(apparmorSpec.SnippetForTag("snap.consumer.app-accessing-1-slot"), testutil.Contains,
		`# pkcs11 tools`)
	c.Assert(apparmorSpec.SnippetForTag("snap.consumer.app-accessing-1-slot"), testutil.Contains,
		`/usr/bin/p11tool ixr,`)
	c.Assert(apparmorSpec.SnippetForTag("snap.consumer.app-accessing-1-slot"), testutil.Contains,
		`/usr/bin/pkcs11-tool ixr,`)
	c.Assert(apparmorSpec.SnippetForTag("snap.consumer.app-accessing-1-slot"), testutil.Contains,
		`"/{,var/}run/p11-kit/pkcs11-optee-slot-0" rw,`)
	c.Assert(apparmorSpec.SnippetForTag("snap.consumer.app-accessing-1-slot"), Not(testutil.Contains),
		`"/{,var/}run/p11-kit/pkcs11-optee-slot-1" rw,`)
	c.Assert(apparmorSpec.SnippetForTag("snap.consumer.app-accessing-1-slot"), Not(testutil.Contains),
		`"/{,var/}run/p11-kit/pkcs11-optee-slot-2" rw,`)

	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SnippetForTag("snap.consumer.app-accessing-2-slots"), testutil.Contains,
		`"/{,var/}run/p11-kit/pkcs11-optee-slot-0" rw,`)
	c.Assert(apparmorSpec.SnippetForTag("snap.consumer.app-accessing-2-slots"), testutil.Contains,
		`"/{,var/}run/p11-kit/pkcs11-optee-slot-1" rw,`)
	c.Assert(apparmorSpec.SnippetForTag("snap.consumer.app-accessing-2-slots"), Not(testutil.Contains),
		`"/{,var/}run/p11-kit/pkcs11-optee-slot-2" rw,`)

	err = apparmorSpec.AddConnectedPlug(s.iface, s.testPlug2, s.testSlot2)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.consumer.app-accessing-1-slot", "snap.consumer.app-accessing-2-slots", "snap.consumer.app-accessing-3rd-slot"})
	c.Assert(apparmorSpec.SnippetForTag("snap.consumer.app-accessing-3rd-slot"), Not(testutil.Contains),
		`"/{,var/}run/p11-kit/pkcs11-optee-slot-0" rw,`)
	c.Assert(apparmorSpec.SnippetForTag("snap.consumer.app-accessing-3rd-slot"), Not(testutil.Contains),
		`"/{,var/}run/p11-kit/pkcs11-optee-slot-1" rw,`)
	c.Assert(apparmorSpec.SnippetForTag("snap.consumer.app-accessing-3rd-slot"), testutil.Contains,
		`"/{,var/}run/p11-kit/pkcs11-optee-slot-2" rw,`)
}

func (s *Pkcs11InterfaceSuite) TestSanitizeGadgetSnapSlots(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.testSlot0Info), IsNil)
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.testSlot1Info), IsNil)
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.testSlot2Info), IsNil)
}

func (s *Pkcs11InterfaceSuite) TestSanitizeBadGadgetSnapSlots(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.testBadSlot0Info), ErrorMatches, "slot \"pkcs11-bad-optee-slot-0\", a unix socket has to be in /run/p11-kit directory")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.testBadSlot1Info), ErrorMatches, "slot \"pkcs11-bad-optee-slot-1\", a unix socket name has to start with 'pkcs11-'")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.testBadSlot2Info), ErrorMatches, "slot \"pkcs11-bad-optee-slot-2\", a unix socket name has to start with 'pkcs11-'")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.testBadSlot3Info), ErrorMatches, "pkcs11: \"pkcs11-socket\" attribute must be a string, not 22")
	err := errors.New("pkcs11 interface socket path is invalid: \"/run/p11-kit/pkcs11-optee-slot-*\" contains a reserved apparmor char from ?*[]{}^\"\x00")
	c.Assert(err, ErrorMatches, `pkcs11 interface socket path is invalid: "/run/p11-kit/pkcs11-optee-slot-\*" contains a reserved apparmor char .*`)
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.testBadSlot5Info), ErrorMatches, "slot \"pkcs11-bad-optee-slot-5\", a unix socket has to be in /run/p11-kit directory")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.testBadSlot6Info), ErrorMatches, "cannot use pkcs11 slot pkcs11-bad-optee-slot-6 without \"pkcs11-socket\" attribute")
}

func (s *Pkcs11InterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Check(si.ImplicitOnCore, Equals, false)
	c.Check(si.ImplicitOnClassic, Equals, false)
	c.Check(si.Summary, Equals, `allows use of pkcs11 framework and access to exposed tokens`)
	c.Check(si.BaseDeclarationSlots, testutil.Contains, "pkcs11")
}

func (s *Pkcs11InterfaceSuite) TestAutoConnect(c *C) {
	c.Assert(s.iface.AutoConnect(s.testPlug0Info, s.testSlot1Info), Equals, true)
}

func (s *Pkcs11InterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
