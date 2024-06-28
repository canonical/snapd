// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

type ThumbnailerServiceInterfaceSuite struct {
	iface           interfaces.Interface
	coreSlotInfo    *snap.SlotInfo
	coreSlot        *interfaces.ConnectedSlot
	classicSlotInfo *snap.SlotInfo
	classicSlot     *interfaces.ConnectedSlot
	plugInfo        *snap.PlugInfo
	plug            *interfaces.ConnectedPlug
}

var _ = Suite(&ThumbnailerServiceInterfaceSuite{
	iface: builtin.MustInterface("thumbnailer-service"),
})

func (s *ThumbnailerServiceInterfaceSuite) SetUpTest(c *C) {
	// a thumbnailer slot on a thumbnailer snap
	const thumbnailerServiceMockCoreSlotSnapInfoYaml = `name: thumbnailer-service
version: 1.0
apps:
 app:
  command: foo
  slots: [thumbnailer-service]
`
	// a thumbnailer-service slot on the core snap (as automatically added on classic)
	const thumbnailerServiceMockClassicSlotSnapInfoYaml = `name: core
version: 0
type: os
slots:
 thumbnailer-service:
  interface: thumbnailer-service
`
	const mockPlugSnapInfo = `name: client
version: 1.0
apps:
 app:
  command: foo
  plugs: [thumbnailer-service]
`
	// thumbnailer-service snap with thumbnailer-service slot on an core/all-snap install.
	s.coreSlot, s.coreSlotInfo = MockConnectedSlot(c, thumbnailerServiceMockCoreSlotSnapInfoYaml, nil, "thumbnailer-service")
	// thumbnailer-service slot on a core snap in a classic install.
	s.classicSlot, s.classicSlotInfo = MockConnectedSlot(c, thumbnailerServiceMockClassicSlotSnapInfoYaml, nil, "thumbnailer-service")

	s.plug, s.plugInfo = MockConnectedPlug(c, mockPlugSnapInfo, nil, "thumbnailer-service")
}

func (s *ThumbnailerServiceInterfaceSuite) TestName(c *C) {
	c.Check(s.iface.Name(), Equals, "thumbnailer-service")
}

func (s *ThumbnailerServiceInterfaceSuite) TestUsedSecuritySystems(c *C) {
	// connected slots have a non-nil security snippet for apparmor
	appSet, err := interfaces.NewSnapAppSet(s.coreSlot.Snap(), nil)
	c.Assert(err, IsNil)
	apparmorSpec := apparmor.NewSpecification(appSet)
	err = apparmorSpec.AddConnectedSlot(s.iface, s.plug, s.coreSlot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.thumbnailer-service.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.thumbnailer-service.app"), testutil.Contains, `interface=com.canonical.Thumbnailer`)

	// slots have no permanent snippet on classic
	appSet, err = interfaces.NewSnapAppSet(s.classicSlot.Snap(), nil)
	c.Assert(err, IsNil)
	apparmorSpec = apparmor.NewSpecification(appSet)
	err = apparmorSpec.AddConnectedSlot(s.iface, s.plug, s.classicSlot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), HasLen, 0)

	// slots have a permanent non-nil security snippet for apparmor
	appSet, err = interfaces.NewSnapAppSet(s.coreSlotInfo.Snap, nil)
	c.Assert(err, IsNil)
	apparmorSpec = apparmor.NewSpecification(appSet)
	err = apparmorSpec.AddPermanentSlot(s.iface, s.coreSlotInfo)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.thumbnailer-service.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.thumbnailer-service.app"), testutil.Contains, `member={RequestName,ReleaseName,GetConnectionCredentials}`)
}

func (s *ThumbnailerServiceInterfaceSuite) TestSlotGrantedAccessToPlugFiles(c *C) {
	appSet, err := interfaces.NewSnapAppSet(s.coreSlot.Snap(), nil)
	c.Assert(err, IsNil)
	apparmorSpec := apparmor.NewSpecification(appSet)
	err = apparmorSpec.AddConnectedSlot(s.iface, s.plug, s.coreSlot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.thumbnailer-service.app"})
	snippet := apparmorSpec.SnippetForTag("snap.thumbnailer-service.app")
	c.Check(snippet, testutil.Contains, `@{INSTALL_DIR}/client/**`)
	c.Check(snippet, testutil.Contains, `@{HOME}/snap/client/**`)
	c.Check(snippet, testutil.Contains, `/var/snap/client/**`)
}

func (s *ThumbnailerServiceInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
