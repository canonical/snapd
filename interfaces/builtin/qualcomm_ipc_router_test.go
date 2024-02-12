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
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/release"
	apparmor_sandbox "github.com/snapcore/snapd/sandbox/apparmor"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type QrtrInterfaceSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

var _ = Suite(&QrtrInterfaceSuite{
	iface: builtin.MustInterface("qualcomm-ipc-router"),
})

const qipcrtrClientYaml = `name: client
version: 0
plugs:
  qc-router:
    interface: qualcomm-ipc-router
    qcipc: monitor
apps:
  app:
    plugs: [qc-router]
`

const qipcrtrServerYaml = `name: server
version: 0
apps:
  app:
    slot: [qc-router]
slots:
  qc-router:
    interface: qualcomm-ipc-router
    qcipc: monitor
    address: '@\x00\x00'
`

func (s *QrtrInterfaceSuite) SetUpTest(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, qipcrtrClientYaml, nil, "qc-router")
	s.slot, s.slotInfo = MockConnectedSlot(c, qipcrtrServerYaml, nil, "qc-router")
}

func (s *QrtrInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "qualcomm-ipc-router")
}

func (s *QrtrInterfaceSuite) TestSanitizeSlot(c *C) {
	r := apparmor_sandbox.MockFeatures(nil, nil, []string{"qipcrtr-socket"}, nil)
	defer r()
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *QrtrInterfaceSuite) TestValidPlugQcipcAttr(c *C) {
	const clientYamlTmpl = `name: client
version: 0
plugs:
  qc-router:
    interface: qualcomm-ipc-router
    qcipc: ##QCIPC##
apps:
  app:
    plugs: [qc-router]
`

	for _, tc := range []struct {
		tag, err string
	}{
		{"foo", ""},
		{"abc-1232", ""},
		{"bad[", `bad name for qcipc attribute: invalid tag name: "bad["`},
		{"''", "qualcomm-ipc-router qcipc attribute cannot be empty"},
	} {
		c.Logf("tc: %v %v", tc.tag, tc.err)
		clientYaml := strings.ReplaceAll(clientYamlTmpl, "##QCIPC##", tc.tag)
		s.plug, s.plugInfo = MockConnectedPlug(c, clientYaml, nil, "qc-router")

		spec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap()))
		err := spec.AddConnectedPlug(s.iface, s.plug, s.slot)
		if tc.err == "" {
			c.Assert(err, IsNil)
		} else {
			c.Assert(err.Error(), Equals, tc.err)
		}
	}
}

func skipIfNoQipcrtrSocketSupport(c *C) {
	if release.ReleaseInfo.ID == "ubuntu" &&
		(release.ReleaseInfo.VersionID == "14.04" || release.ReleaseInfo.VersionID == "16.04") {
		c.Skip("qipcrtr socket is unsupported in 14.04/16.04")
	}
}

func (s *QrtrInterfaceSuite) TestValidSlotQcipcAttr(c *C) {
	skipIfNoQipcrtrSocketSupport(c)

	const serverYamlTmpl = `name: server
version: 0
apps:
  app:
    slot: [qc-router]
slots:
  qc-router:
    interface: qualcomm-ipc-router
    qcipc: ##QCIPC##
    address: abcd
`

	for _, tc := range []struct {
		tag, err string
	}{
		{"foo", ""},
		{"abc-1232", ""},
		{"bad[", `bad name for qcipc attribute: invalid tag name: "bad["`},
		{"''", "qualcomm-ipc-router qcipc attribute cannot be empty"},
	} {
		c.Logf("tc: %v %v", tc.tag, tc.err)
		serverYaml := strings.ReplaceAll(serverYamlTmpl, "##QCIPC##", tc.tag)
		s.slot, s.slotInfo = MockConnectedSlot(c, serverYaml, nil, "qc-router")

		r := apparmor_sandbox.MockLevel(apparmor_sandbox.Full)
		defer r()
		r = apparmor_sandbox.MockFeatures(nil, nil, []string{"qipcrtr-socket"}, nil)
		defer r()
		err := interfaces.BeforePrepareSlot(s.iface, s.slotInfo)
		if tc.err == "" {
			c.Assert(err, IsNil)
		} else {
			c.Assert(err.Error(), Equals, tc.err)
		}
	}
}

func (s *QrtrInterfaceSuite) TestValidSlotAddressAttr(c *C) {
	skipIfNoQipcrtrSocketSupport(c)

	const serverYamlTmpl = `name: server
version: 0
apps:
  app:
    slot: [qc-router]
slots:
  qc-router:
    interface: qualcomm-ipc-router
    qcipc: monitor
    address: ##ADDRESS##
`

	for _, tc := range []struct {
		addr, err string
	}{
		{"foo", ""},
		{`"@abstract"`, ""},
		{"bad[", `address is invalid: "bad[" contains a reserved apparmor char from ?*[]{}^"` + "\x00"},
		{"''", "qualcomm-ipc-router qcipc attribute cannot be empty"},
	} {
		c.Logf("tc: %v %v", tc.addr, tc.err)
		serverYaml := strings.ReplaceAll(serverYamlTmpl, "##ADDRESS##", tc.addr)
		s.slot, s.slotInfo = MockConnectedSlot(c, serverYaml, nil, "qc-router")

		r := apparmor_sandbox.MockLevel(apparmor_sandbox.Full)
		defer r()
		r = apparmor_sandbox.MockFeatures(nil, nil, []string{"qipcrtr-socket"}, nil)
		defer r()
		err := interfaces.BeforePrepareSlot(s.iface, s.slotInfo)
		if tc.err == "" {
			c.Assert(err, IsNil)
		} else {
			c.Logf(err.Error())
			c.Assert(err.Error(), Equals, tc.err)
		}
	}
}

func (s *QrtrInterfaceSuite) TestSanitizeSlotMissingFeature(c *C) {
	r := apparmor_sandbox.MockLevel(apparmor_sandbox.Full)
	defer r()
	r = apparmor_sandbox.MockFeatures(nil, nil, nil, nil)
	defer r()
	err := interfaces.BeforePrepareSlot(s.iface, s.slotInfo)
	c.Assert(err, ErrorMatches, "cannot prepare slot on system without qipcrtr socket support")
}

func (s *QrtrInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *QrtrInterfaceSuite) TestSanitizePlugConnectionFullAppArmorSandboxFeatures(c *C) {
	r := apparmor_sandbox.MockFeatures(nil, nil, []string{"qipcrtr-socket"}, nil)
	defer r()
	c.Assert(interfaces.BeforeConnectPlug(s.iface, s.plug), IsNil)
}

func (s *QrtrInterfaceSuite) TestSanitizePlugConnectionMissingAppArmorSandboxFeatures(c *C) {
	r := apparmor_sandbox.MockLevel(apparmor_sandbox.Full)
	defer r()
	r = apparmor_sandbox.MockFeatures(nil, nil, nil, nil)
	defer r()
	err := interfaces.BeforeConnectPlug(s.iface, s.plug)
	c.Assert(err, ErrorMatches, "cannot connect plug on system without qipcrtr socket support")
}

func (s *QrtrInterfaceSuite) TestAppArmorSpec(c *C) {
	spec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.slotInfo.Snap))
	c.Assert(spec.AddPermanentSlot(s.iface, s.slotInfo), IsNil)
	c.Assert(spec.AddConnectedSlot(s.iface, s.plug, s.slot), IsNil)

	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.server.app"})

	c.Assert(spec.SnippetForTag("snap.server.app"), testutil.Contains, "network qipcrtr,\n")
	c.Assert(spec.SnippetForTag("snap.server.app"), testutil.Contains, "capability net_admin,\n")
	c.Assert(spec.SnippetForTag("snap.server.app"), testutil.Contains, `unix (accept, send, receive) type=seqpacket addr="@\x00\x00" peer=(label="snap.client.app"),`)
	c.Assert(spec.SnippetForTag("snap.server.app"), testutil.Contains, `unix (bind, listen) type=seqpacket addr="@\x00\x00",`)

	spec = apparmor.NewSpecification(interfaces.NewSnapAppSet(s.plugInfo.Snap))
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)

	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.client.app"})

	c.Assert(spec.SnippetForTag("snap.client.app"), testutil.Contains, "network qipcrtr,\n")
	c.Assert(spec.SnippetForTag("snap.client.app"), Not(testutil.Contains), "capability net_admin,\n")
	c.Assert(spec.SnippetForTag("snap.client.app"), testutil.Contains, `unix (connect, send, receive) type=seqpacket addr="@\x00\x00" peer=(label="snap.server.app"),`)

}

func (s *QrtrInterfaceSuite) TestSecCompSpec(c *C) {
	spec := seccomp.NewSpecification(interfaces.NewSnapAppSet(s.slotInfo.Snap))
	c.Assert(spec.AddPermanentSlot(s.iface, s.slotInfo), IsNil)

	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.server.app"})
	c.Assert(spec.SnippetForTag("snap.server.app"), testutil.Contains, "bind\n")
}

func (s *QrtrInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, true)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows access to the Qualcomm IPC Router sockets`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "qualcomm-ipc-router")
}

func (s *QrtrInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}

type QrtrInterfaceCompatSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

var _ = Suite(&QrtrInterfaceCompatSuite{
	iface: builtin.MustInterface("qualcomm-ipc-router"),
})

const qipcrtrConsumerCompatYaml = `name: consumer
version: 0
apps:
  app:
    plugs: [qualcomm-ipc-router]
`

const qipcrtrCoreCompatYaml = `name: core
version: 0
type: os
slots:
  qualcomm-ipc-router:
`

func (s *QrtrInterfaceCompatSuite) SetUpTest(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, qipcrtrConsumerCompatYaml, nil, "qualcomm-ipc-router")
	s.slot, s.slotInfo = MockConnectedSlot(c, qipcrtrCoreCompatYaml, nil, "qualcomm-ipc-router")
}

func (s *QrtrInterfaceCompatSuite) TestNoTagAllowed(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, `name: consumer
version: 0
plugs:
  qualcomm-ipc-router:
    interface: qualcomm-ipc-router
    qcipc: some-name
apps:
  app:
    plugs: [qualcomm-ipc-router]
`, nil, "qualcomm-ipc-router")

	spec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap()))
	err := spec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err.Error(), Equals, `"qcipc" attribute not allowed if connecting to a system slot`)
}

func (s *QrtrInterfaceCompatSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "qualcomm-ipc-router")
}

func (s *QrtrInterfaceCompatSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *QrtrInterfaceCompatSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *QrtrInterfaceCompatSuite) TestSanitizePlugConnectionFullAppArmorSandboxFeatures(c *C) {
	r := apparmor_sandbox.MockFeatures(nil, nil, []string{"qipcrtr-socket"}, nil)
	defer r()
	c.Assert(interfaces.BeforeConnectPlug(s.iface, s.plug), IsNil)
}

func (s *QrtrInterfaceCompatSuite) TestSanitizePlugConnectionMissingAppArmorSandboxFeatures(c *C) {
	r := apparmor_sandbox.MockLevel(apparmor_sandbox.Full)
	defer r()
	r = apparmor_sandbox.MockFeatures(nil, nil, nil, nil)
	defer r()
	err := interfaces.BeforeConnectPlug(s.iface, s.plug)
	c.Assert(err, ErrorMatches, "cannot connect plug on system without qipcrtr socket support")
}

func (s *QrtrInterfaceCompatSuite) TestSanitizePlugConnectionMissingNoAppArmor(c *C) {
	r := apparmor_sandbox.MockLevel(apparmor_sandbox.Unsupported)
	defer r()
	err := interfaces.BeforeConnectPlug(s.iface, s.plug)
	c.Assert(err, IsNil)
}

func (s *QrtrInterfaceCompatSuite) TestAppArmorSpec(c *C) {
	spec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap()))
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "network qipcrtr,\n")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "capability net_admin,\n")
}

func (s *QrtrInterfaceCompatSuite) TestSecCompSpec(c *C) {
	spec := seccomp.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap()))
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "bind\n")
}

func (s *QrtrInterfaceCompatSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, true)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows access to the Qualcomm IPC Router sockets`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "qualcomm-ipc-router")
}

func (s *QrtrInterfaceCompatSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
