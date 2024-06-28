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
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type StorageFrameworkServiceInterfaceSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

var _ = Suite(&StorageFrameworkServiceInterfaceSuite{
	iface: builtin.MustInterface("storage-framework-service"),
})

func (s *StorageFrameworkServiceInterfaceSuite) SetUpTest(c *C) {
	const providerYaml = `name: provider
version: 1.0
apps:
 app:
  command: foo
  slots: [storage-framework-service]
`
	s.slot, s.slotInfo = MockConnectedSlot(c, providerYaml, nil, "storage-framework-service")
	const consumerYaml = `name: consumer
version: 1.0
apps:
 app:
  command: foo
  plugs: [storage-framework-service]
`
	s.plug, s.plugInfo = MockConnectedPlug(c, consumerYaml, nil, "storage-framework-service")
}

func (s *StorageFrameworkServiceInterfaceSuite) TestName(c *C) {
	c.Check(s.iface.Name(), Equals, "storage-framework-service")
}

func (s *StorageFrameworkServiceInterfaceSuite) TestAppArmorConnectedPlug(c *C) {
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	spec := apparmor.NewSpecification(appSet)
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `interface=com.canonical.StorageFramework.Registry`)
}

func (s *StorageFrameworkServiceInterfaceSuite) TestAppArmorConnectedSlot(c *C) {
	appSet, err := interfaces.NewSnapAppSet(s.slot.Snap(), nil)
	c.Assert(err, IsNil)
	spec := apparmor.NewSpecification(appSet)
	c.Assert(spec.AddConnectedSlot(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.provider.app"})
	c.Assert(spec.SnippetForTag("snap.provider.app"), testutil.Contains, `interface=com.canonical.StorageFramework`)
}

func (s *StorageFrameworkServiceInterfaceSuite) TestAppArmorPermanentSlot(c *C) {
	appSet, err := interfaces.NewSnapAppSet(s.slotInfo.Snap, nil)
	c.Assert(err, IsNil)
	spec := apparmor.NewSpecification(appSet)
	c.Assert(spec.AddPermanentSlot(s.iface, s.slotInfo), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.provider.app"})
	c.Assert(spec.SnippetForTag("snap.provider.app"), testutil.Contains, `member={RequestName,ReleaseName,GetConnectionCredentials}`)
}

func (s *StorageFrameworkServiceInterfaceSuite) TestSecCompPermanentSlot(c *C) {
	appSet, err := interfaces.NewSnapAppSet(s.slotInfo.Snap, nil)
	c.Assert(err, IsNil)
	spec := seccomp.NewSpecification(appSet)
	c.Assert(spec.AddPermanentSlot(s.iface, s.slotInfo), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.provider.app"})
	c.Assert(spec.SnippetForTag("snap.provider.app"), testutil.Contains, "bind\n")
}

func (s *StorageFrameworkServiceInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
