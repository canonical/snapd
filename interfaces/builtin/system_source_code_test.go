// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type systemSourceCodeSuite struct {
	iface        interfaces.Interface
	coreSlotInfo *snap.SlotInfo
	coreSlot     *interfaces.ConnectedSlot
	plugInfo     *snap.PlugInfo
	plug         *interfaces.ConnectedPlug
}

var _ = Suite(&systemSourceCodeSuite{iface: builtin.MustInterface("system-source-code")})

const systemSourceCodeConsumerYaml = `name: consumer
version: 0
apps:
 app:
  plugs: [system-source-code]
`

const systemSourceCodeCoreYaml = `name: core
version: 0
type: os
slots:
  system-source-code:
`

func (s *systemSourceCodeSuite) SetUpTest(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, systemSourceCodeConsumerYaml, nil, "system-source-code")
	s.coreSlot, s.coreSlotInfo = MockConnectedSlot(c, systemSourceCodeCoreYaml, nil, "system-source-code")
}

func (s *systemSourceCodeSuite) TearDownTest(c *C) {
	dirs.SetRootDir("/")
}

func (s *systemSourceCodeSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "system-source-code")
}

func (s *systemSourceCodeSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.coreSlotInfo), IsNil)
}

func (s *systemSourceCodeSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *systemSourceCodeSuite) TestAppArmorSpec(c *C) {
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	spec := apparmor.NewSpecification(appSet)
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.coreSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "# Description: can access /usr/src for kernel headers, etc")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "/usr/src/{,**} r,")
}

func (s *systemSourceCodeSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, true)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows read-only access to /usr/src on the system`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "system-source-code")
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "deny-auto-connection: true")
}

func (s *systemSourceCodeSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
