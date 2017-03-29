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
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type DockerSupportInterfaceSuite struct {
	iface interfaces.Interface
	slot  *interfaces.Slot
	plug  *interfaces.Plug
}

const dockerSupportMockPlugSnapInfoYaml = `name: docker
version: 1.0
apps:
 app:
  command: foo
  plugs: [docker-support]
`

var _ = Suite(&DockerSupportInterfaceSuite{})

func (s *DockerSupportInterfaceSuite) SetUpTest(c *C) {
	s.iface = &builtin.DockerSupportInterface{}
	s.slot = &interfaces.Slot{
		SlotInfo: &snap.SlotInfo{
			Snap: &snap.Info{
				SuggestedName: "core",
				Type:          snap.TypeOS},
			Name:      "docker-support",
			Interface: "docker-support",
		},
	}
	plugSnap := snaptest.MockInfo(c, dockerSupportMockPlugSnapInfoYaml, nil)
	s.plug = &interfaces.Plug{PlugInfo: plugSnap.Plugs["docker-support"]}
}

func (s *DockerSupportInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "docker-support")
}

func (s *DockerSupportInterfaceSuite) TestUsedSecuritySystems(c *C) {
	// connected plugs have a non-nil security snippet for apparmor
	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, nil, s.slot, nil)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), HasLen, 1)

	// connected plugs have a non-nil security snippet for seccomp
	seccompSpec := &seccomp.Specification{}
	err = seccompSpec.AddConnectedPlug(s.iface, s.plug, nil, s.slot, nil)
	c.Assert(err, IsNil)
	c.Assert(seccompSpec.Snippets(), HasLen, 1)
}

func (s *DockerSupportInterfaceSuite) TestConnectedPlugSnippet(c *C) {
	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, nil, s.slot, nil)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.docker.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.docker.app"), testutil.Contains, `pivot_root`)

	seccompSpec := &seccomp.Specification{}
	err = seccompSpec.AddConnectedPlug(s.iface, s.plug, nil, s.slot, nil)
	c.Assert(err, IsNil)
	c.Assert(seccompSpec.SecurityTags(), DeepEquals, []string{"snap.docker.app"})
	c.Check(seccompSpec.SnippetForTag("snap.docker.app"), testutil.Contains, "pivot_root\n")
}

func (s *DockerSupportInterfaceSuite) TestSanitizeSlot(c *C) {
	err := s.iface.SanitizeSlot(s.slot)
	c.Assert(err, IsNil)
}

func (s *DockerSupportInterfaceSuite) TestSanitizePlug(c *C) {
	err := s.iface.SanitizePlug(s.plug)
	c.Assert(err, IsNil)
}

func (s *DockerSupportInterfaceSuite) TestSanitizePlugWithPrivilegedTrue(c *C) {
	var mockSnapYaml = []byte(`name: docker
version: 1.0
plugs:
 privileged:
  interface: docker-support
  privileged-containers: true
apps:
 app:
  command: foo
  plugs:
   - privileged
`)

	info, err := snap.InfoFromSnapYaml(mockSnapYaml)
	c.Assert(err, IsNil)

	plug := &interfaces.Plug{PlugInfo: info.Plugs["privileged"]}
	err = s.iface.SanitizePlug(plug)
	c.Assert(err, IsNil)

	apparmorSpec := &apparmor.Specification{}
	err = apparmorSpec.AddConnectedPlug(s.iface, plug, nil, s.slot, nil)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.docker.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.docker.app"), testutil.Contains, `change_profile -> *,`)

	seccompSpec := &seccomp.Specification{}
	err = seccompSpec.AddConnectedPlug(s.iface, plug, nil, s.slot, nil)
	c.Assert(err, IsNil)
	c.Assert(seccompSpec.SecurityTags(), DeepEquals, []string{"snap.docker.app"})
	c.Check(seccompSpec.SnippetForTag("snap.docker.app"), testutil.Contains, "@unrestricted")
}

func (s *DockerSupportInterfaceSuite) TestSanitizePlugWithPrivilegedFalse(c *C) {
	var mockSnapYaml = []byte(`name: docker
version: 1.0
plugs:
 privileged:
  interface: docker-support
  privileged-containers: false
apps:
 app:
  command: foo
  plugs:
   - privileged
`)

	info, err := snap.InfoFromSnapYaml(mockSnapYaml)
	c.Assert(err, IsNil)

	plug := &interfaces.Plug{PlugInfo: info.Plugs["privileged"]}
	err = s.iface.SanitizePlug(plug)
	c.Assert(err, IsNil)

	apparmorSpec := &apparmor.Specification{}
	err = apparmorSpec.AddConnectedPlug(s.iface, plug, nil, s.slot, nil)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.docker.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.docker.app"), Not(testutil.Contains), `change_profile -> *,`)

	seccompSpec := &seccomp.Specification{}
	err = seccompSpec.AddConnectedPlug(s.iface, plug, nil, s.slot, nil)
	c.Assert(err, IsNil)
	c.Assert(seccompSpec.SecurityTags(), DeepEquals, []string{"snap.docker.app"})
	c.Check(seccompSpec.SnippetForTag("snap.docker.app"), Not(testutil.Contains), "@unrestricted")
}

func (s *DockerSupportInterfaceSuite) TestSanitizePlugWithPrivilegedBad(c *C) {
	var mockSnapYaml = []byte(`name: docker
version: 1.0
plugs:
 privileged:
  interface: docker-support
  privileged-containers: bad
`)

	info, err := snap.InfoFromSnapYaml(mockSnapYaml)
	c.Assert(err, IsNil)

	plug := &interfaces.Plug{PlugInfo: info.Plugs["privileged"]}
	err = s.iface.SanitizePlug(plug)
	c.Assert(err, Not(IsNil))
	c.Assert(err, ErrorMatches, "docker-support plug requires bool with 'privileged-containers'")
}
