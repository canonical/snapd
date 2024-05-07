// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type daemoNotifySuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

var _ = Suite(&daemoNotifySuite{
	iface: builtin.MustInterface("daemon-notify"),
})

const daemoNotifyMockSlotSnapInfoYaml = `name: provider
version: 1.0
type: os
slots:
  daemon-notify:
    interface: daemon-notify
`
const daemoNotifyMockPlugSnapInfoYaml = `name: consumer
version: 1.0
apps:
 app:
  command: foo
  plugs: [daemon-notify]
`

func (s *daemoNotifySuite) SetUpTest(c *C) {
	s.slot, s.slotInfo = builtin.MockConnectedSlot(c, daemoNotifyMockSlotSnapInfoYaml, nil, "daemon-notify")
	s.plug, s.plugInfo = builtin.MockConnectedPlug(c, daemoNotifyMockPlugSnapInfoYaml, nil, "daemon-notify")
}

func (s *daemoNotifySuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "daemon-notify")
}

func (s *daemoNotifySuite) TestBeforePrepareSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *daemoNotifySuite) TestBeforePreparePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *daemoNotifySuite) TestAppArmorConnectedPlugNotifySocketDefault(c *C) {
	restore := builtin.MockOsGetenv(func(what string) string {
		c.Assert(what, Equals, "NOTIFY_SOCKET")
		return ""
	})
	defer restore()

	// connected plugs have a non-nil security snippet for apparmor
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	spec := apparmor.NewSpecification(appSet)
	err = spec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "\n\"/run/systemd/notify\" w,")
}

func (s *daemoNotifySuite) TestAppArmorConnectedPlugNotifySocketEnvAbstractSpecial(c *C) {
	restore := builtin.MockOsGetenv(func(what string) string {
		c.Assert(what, Equals, "NOTIFY_SOCKET")
		return "@/org/freedesktop/systemd1/notify/13334051644891137417"
	})
	defer restore()

	// connected plugs have a non-nil security snippet for apparmor
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	spec := apparmor.NewSpecification(appSet)
	err = spec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains,
		"\nunix (connect, send) type=dgram peer=(label=unconfined,addr=\"@/org/freedesktop/systemd1/notify/[0-9]*\"),")
}

func (s *daemoNotifySuite) TestAppArmorConnectedPlugNotifySocketEnvAbstractAny(c *C) {
	restore := builtin.MockOsGetenv(func(what string) string {
		c.Assert(what, Equals, "NOTIFY_SOCKET")
		return "@foo/bar"
	})
	defer restore()

	// connected plugs have a non-nil security snippet for apparmor
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	spec := apparmor.NewSpecification(appSet)
	err = spec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains,
		"\nunix (connect, send) type=dgram peer=(label=unconfined,addr=\"@foo/bar\"),")
}

func (s *daemoNotifySuite) TestAppArmorConnectedPlugNotifySocketEnvFsPath(c *C) {
	restore := builtin.MockOsGetenv(func(what string) string {
		c.Assert(what, Equals, "NOTIFY_SOCKET")
		return "/foo/bar"
	})
	defer restore()

	// connected plugs have a non-nil security snippet for apparmor
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	spec := apparmor.NewSpecification(appSet)
	err = spec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "\n\"/foo/bar\" w,")
}

func (s *daemoNotifySuite) TestAppArmorConnectedPlugNotifySocketEnvBadFormat(c *C) {
	var socketPath string
	restore := builtin.MockOsGetenv(func(what string) string {
		c.Assert(what, Equals, "NOTIFY_SOCKET")
		return socketPath
	})
	defer restore()

	for idx, tc := range []struct {
		format string
		error  string
	}{
		{"foo/bar", `cannot use \".*\" as notify socket path: not absolute`},
		{"[", `cannot use ".*" as notify socket path: not absolute`},
		{"@^", `cannot use \".*\" as notify socket path: \".*\" contains a reserved apparmor char from .*`},
		{`/foo/bar"[]`, `cannot use \".*\" as notify socket path: \".*\" contains a reserved apparmor char from .*`},
	} {
		c.Logf("trying %d: %v", idx, tc)
		socketPath = tc.format
		// connected plugs have a non-nil security snippet for apparmor
		appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
		c.Assert(err, IsNil)
		spec := apparmor.NewSpecification(appSet)
		err = spec.AddConnectedPlug(s.iface, s.plug, s.slot)
		c.Assert(err, ErrorMatches, tc.error)
	}
}

func (s *daemoNotifySuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
