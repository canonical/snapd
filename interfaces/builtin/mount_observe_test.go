// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

type MountObserveInterfaceSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

var _ = Suite(&MountObserveInterfaceSuite{
	iface: builtin.MustInterface("mount-observe"),
})

func (s *MountObserveInterfaceSuite) SetUpTest(c *C) {
	const mockPlugSnapInfoYaml = `name: other
version: 1.0
apps:
 app:
  command: foo
  plugs: [mount-observe]
`
	const mockSlotSnapInfoYaml = `name: core
version: 1.0
type: os
slots:
 mount-observe:
  interface: mount-observe
`
	s.slot, s.slotInfo = MockConnectedSlot(c, mockSlotSnapInfoYaml, nil, "mount-observe")
	s.plug, s.plugInfo = MockConnectedPlug(c, mockPlugSnapInfoYaml, nil, "mount-observe")
}

func (s *MountObserveInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "mount-observe")
}

func (s *MountObserveInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *MountObserveInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *MountObserveInterfaceSuite) TestUsedSecuritySystems(c *C) {
	// connected plugs have a non-nil security snippet for apparmor
	apparmorSpec := apparmor.NewSpecification(s.plug.AppSet())
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app"), testutil.Contains, "/etc/fstab")
}

func (s *MountObserveInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}

func (s *MountObserveInterfaceSuite) TestPrioritizedSnippetMountInfo(c *C) {
	spec := apparmor.NewSpecification(s.plug.AppSet())
	spec.AddBasePrioritizedSnippet(`
deny @{PROC}/self/mountinfo r,
deny @{PROC}/@{pid}/mountinfo r,
`, apparmor.MountInfoKey)

	snippet := spec.SnippetForTag("snap.other.app")
	// contains the denials but not the allows
	c.Assert(snippet, testutil.Contains, "deny @{PROC}/@{pid}/mountinfo r,")
	c.Assert(snippet, testutil.Contains, "deny @{PROC}/self/mountinfo r,")
	c.Assert(snippet, Not(testutil.Contains), "owner @{PROC}/@{pid}/mountinfo r,")
	c.Assert(snippet, Not(testutil.Contains), "owner @{PROC}/self/mountinfo r,")

	err := spec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)

	snippet = spec.SnippetForTag("snap.other.app")
	// contains the allows but not the denials
	c.Assert(snippet, testutil.Contains, "owner @{PROC}/@{pid}/mountinfo r,")
	c.Assert(snippet, testutil.Contains, "owner @{PROC}/self/mountinfo r,")
	c.Assert(snippet, Not(Matches), "(?s).*\ndeny [^\n]*/mountinfo [^\n]*.*")
	c.Assert(snippet, Not(Matches), "(?s).*\ndeny [^\n]*/mountinfo [^\n]*.*")
}
