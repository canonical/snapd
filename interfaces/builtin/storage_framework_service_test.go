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
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type StorageFrameworkServiceInterfaceSuite struct {
	iface       interfaces.Interface
	coreSlot    *interfaces.Slot
	classicSlot *interfaces.Slot
	plug        *interfaces.Plug
}

var _ = Suite(&StorageFrameworkServiceInterfaceSuite{})

func (s *StorageFrameworkServiceInterfaceSuite) SetUpTest(c *C) {
	// a storage-framework-service slot on a storage-framework-service snap
	const storageFrameworkServiceMockCoreSlotSnapInfoYaml = `name: storage-framework-service
version: 1.0
apps:
 app:
  command: foo
  slots: [storage-framework-service]
`
	// a storage-framework-service slot on the core snap (as automatically added on classic)
	const storageFrameworkServiceMockClassicSlotSnapInfoYaml = `name: core
type: os
slots:
 storage-framework-service:
  interface: storage-framework-service
`
	const mockPlugSnapInfo = `name: client
version: 1.0
apps:
 app:
  command: foo
  plugs: [storage-framework-service]
`
	s.iface = &builtin.StorageFrameworkServiceInterface{}
	// storage-framework-service snap with storage-framework-service slot on an core/all-snap install.
	snapInfo := snaptest.MockInfo(c, storageFrameworkServiceMockCoreSlotSnapInfoYaml, nil)
	s.coreSlot = &interfaces.Slot{SlotInfo: snapInfo.Slots["storage-framework-service"]}
	// storage-framework-service slot on a core snap in a classic install.
	snapInfo = snaptest.MockInfo(c, storageFrameworkServiceMockClassicSlotSnapInfoYaml, nil)
	s.classicSlot = &interfaces.Slot{SlotInfo: snapInfo.Slots["storage-framework-service"]}

	plugSnap := snaptest.MockInfo(c, mockPlugSnapInfo, nil)
	s.plug = &interfaces.Plug{PlugInfo: plugSnap.Plugs["storage-framework-service"]}
}

func (s *StorageFrameworkServiceInterfaceSuite) TestName(c *C) {
	c.Check(s.iface.Name(), Equals, "storage-framework-service")
}

func (s *StorageFrameworkServiceInterfaceSuite) TestSanitizeIncorrectInterface(c *C) {
	c.Check(func() { s.iface.SanitizeSlot(&interfaces.Slot{SlotInfo: &snap.SlotInfo{Interface: "other"}}) },
		PanicMatches, `slot is not of interface "storage-framework-service"`)
	c.Check(func() { s.iface.SanitizePlug(&interfaces.Plug{PlugInfo: &snap.PlugInfo{Interface: "other"}}) },
		PanicMatches, `plug is not of interface "storage-framework-service"`)
}

func (s *StorageFrameworkServiceInterfaceSuite) TestUsedSecuritySystems(c *C) {
	// connected slots have a non-nil security snippet for apparmor
	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedSlot(s.iface, s.plug, s.coreSlot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.storage-framework-service.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.storage-framework-service.app"), testutil.Contains, `interface=com.canonical.StorageFramework`)

	// slots have no permanent snippet on classic
	apparmorSpec = &apparmor.Specification{}
	err = apparmorSpec.AddConnectedSlot(s.iface, s.plug, s.classicSlot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), HasLen, 0)

	// slots have a permanent non-nil security snippet for apparmor
	apparmorSpec = &apparmor.Specification{}
	err = apparmorSpec.AddPermanentSlot(s.iface, s.coreSlot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.storage-framework-service.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.storage-framework-service.app"), testutil.Contains, `member={RequestName,ReleaseName,GetConnectionCredentials}`)
}
