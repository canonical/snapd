// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2020 Canonical Ltd
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
	"github.com/snapcore/snapd/interfaces/kmod"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/release"
	apparmor_sandbox "github.com/snapcore/snapd/sandbox/apparmor"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type DockerSupportInterfaceSuite struct {
	iface                    interfaces.Interface
	slotInfo                 *snap.SlotInfo
	slot                     *interfaces.ConnectedSlot
	plugInfo                 *snap.PlugInfo
	plug                     *interfaces.ConnectedPlug
	networkCtrlSlotInfo      *snap.SlotInfo
	networkCtrlSlot          *interfaces.ConnectedSlot
	networkCtrlPlugInfo      *snap.PlugInfo
	networkCtrlPlug          *interfaces.ConnectedPlug
	privContainersPlugInfo   *snap.PlugInfo
	privContainersPlug       *interfaces.ConnectedPlug
	noPrivContainersPlugInfo *snap.PlugInfo
	noPrivContainersPlug     *interfaces.ConnectedPlug
	malformedPlugInfo        *snap.PlugInfo
	malformedPlug            *interfaces.ConnectedPlug
}

const coreDockerSlotYaml = `name: core
version: 0
type: os
slots:
  docker-support:
  network-control:
`

const dockerSupportMockPlugSnapInfoYaml = `name: docker
version: 1.0
apps:
 app:
  command: foo
  plugs:
   - docker-support
   - network-control
`

const dockerSupportPrivilegedContainersMalformedMockPlugSnapInfoYaml = `name: docker
version: 1.0
plugs:
 privileged:
  interface: docker-support
  privileged-containers: foobar
apps:
 app:
  command: foo
  plugs:
  - privileged
`

const dockerSupportPrivilegedContainersFalseMockPlugSnapInfoYaml = `name: docker
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
`

const dockerSupportPrivilegedContainersTrueMockPlugSnapInfoYaml = `name: docker
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
`

var _ = Suite(&DockerSupportInterfaceSuite{
	iface: builtin.MustInterface("docker-support"),
})

func (s *DockerSupportInterfaceSuite) SetUpTest(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, dockerSupportMockPlugSnapInfoYaml, nil, "docker-support")
	s.slot, s.slotInfo = MockConnectedSlot(c, coreDockerSlotYaml, nil, "docker-support")
	s.networkCtrlPlug, s.networkCtrlPlugInfo = MockConnectedPlug(c, dockerSupportMockPlugSnapInfoYaml, nil, "network-control")
	s.networkCtrlSlot, s.networkCtrlSlotInfo = MockConnectedSlot(c, coreDockerSlotYaml, nil, "network-control")
	s.privContainersPlug, s.privContainersPlugInfo = MockConnectedPlug(c, dockerSupportPrivilegedContainersTrueMockPlugSnapInfoYaml, nil, "privileged")
	s.noPrivContainersPlug, s.noPrivContainersPlugInfo = MockConnectedPlug(c, dockerSupportPrivilegedContainersFalseMockPlugSnapInfoYaml, nil, "privileged")
	s.malformedPlug, s.malformedPlugInfo = MockConnectedPlug(c, dockerSupportPrivilegedContainersMalformedMockPlugSnapInfoYaml, nil, "privileged")
}

func (s *DockerSupportInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "docker-support")
}

func (s *DockerSupportInterfaceSuite) TestUsedSecuritySystems(c *C) {
	// connected plugs have a non-nil security snippet for apparmor
	apparmorSpec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap()))
	c.Assert(apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(apparmorSpec.SecurityTags(), HasLen, 1)

	// connected plugs have a non-nil security snippet for seccomp
	seccompSpec := seccomp.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap()))
	c.Assert(seccompSpec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(seccompSpec.Snippets(), HasLen, 1)
}

func (s *DockerSupportInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *DockerSupportInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *DockerSupportInterfaceSuite) TestSanitizePlugWithPrivilegedTrue(c *C) {
	apparmorSpec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.privContainersPlug.Snap()))
	c.Assert(apparmorSpec.AddConnectedPlug(s.iface, s.privContainersPlug, s.slot), IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.docker.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.docker.app"), testutil.Contains, `change_profile unsafe /**,`)

	seccompSpec := seccomp.NewSpecification(interfaces.NewSnapAppSet(s.privContainersPlug.Snap()))
	c.Assert(seccompSpec.AddConnectedPlug(s.iface, s.privContainersPlug, s.slot), IsNil)
	c.Assert(seccompSpec.SecurityTags(), DeepEquals, []string{"snap.docker.app"})
	c.Check(seccompSpec.SnippetForTag("snap.docker.app"), testutil.Contains, "@unrestricted")
}

func (s *DockerSupportInterfaceSuite) TestSanitizePlugWithPrivilegedFalse(c *C) {
	apparmorSpec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.noPrivContainersPlug.Snap()))
	c.Assert(apparmorSpec.AddConnectedPlug(s.iface, s.noPrivContainersPlug, s.slot), IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.docker.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.docker.app"), Not(testutil.Contains), `change_profile unsafe /**,`)

	seccompSpec := seccomp.NewSpecification(interfaces.NewSnapAppSet(s.noPrivContainersPlug.Snap()))
	c.Assert(seccompSpec.AddConnectedPlug(s.iface, s.noPrivContainersPlug, s.slot), IsNil)
	c.Assert(seccompSpec.SecurityTags(), DeepEquals, []string{"snap.docker.app"})
	c.Check(seccompSpec.SnippetForTag("snap.docker.app"), Not(testutil.Contains), "@unrestricted")
}

func (s *DockerSupportInterfaceSuite) TestSanitizePlugWithPrivilegedBad(c *C) {
	var mockSnapYaml = `name: docker
version: 1.0
plugs:
 privileged:
  interface: docker-support
  privileged-containers: bad
`

	info := snaptest.MockInfo(c, mockSnapYaml, nil)
	plug := info.Plugs["privileged"]
	c.Assert(interfaces.BeforePreparePlug(s.iface, plug), ErrorMatches, "docker-support plug requires bool with 'privileged-containers'")
}

func (s *DockerSupportInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}

func (s *DockerSupportInterfaceSuite) TestAppArmorSpec(c *C) {
	// no features so should not support userns
	restore := apparmor_sandbox.MockFeatures(nil, nil, nil, nil)
	defer restore()
	spec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap()))
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.docker.app"})
	c.Check(spec.SnippetForTag("snap.docker.app"), testutil.Contains, "/sys/fs/cgroup/*/docker/   rw,\n")
	c.Check(spec.UsesPtraceTrace(), Equals, true)
	c.Check(spec.SnippetForTag("snap.docker.app"), Not(testutil.Contains), "userns,\n")

	// test with apparmor userns support too
	restore = apparmor_sandbox.MockFeatures(nil, nil, []string{"userns"}, nil)
	defer restore()
	spec = apparmor.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap()))
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.docker.app"})
	c.Check(spec.SnippetForTag("snap.docker.app"), testutil.Contains, "/sys/fs/cgroup/*/docker/   rw,\n")
	c.Check(spec.UsesPtraceTrace(), Equals, true)
	c.Check(spec.SnippetForTag("snap.docker.app"), testutil.Contains, "userns,\n")

}

func (s *DockerSupportInterfaceSuite) TestSecCompSpec(c *C) {
	spec := seccomp.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap()))
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Check(spec.SnippetForTag("snap.docker.app"), testutil.Contains, "# Calls the Docker daemon itself requires\n")
}

func (s *DockerSupportInterfaceSuite) TestKModSpec(c *C) {
	spec := &kmod.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.Modules(), DeepEquals, map[string]bool{
		"overlay": true,
	})
}

func (s *DockerSupportInterfaceSuite) TestPermanentSlotAppArmorSessionNative(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	apparmorSpec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap()))
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.docker.app"})

	// verify core rule present
	c.Check(apparmorSpec.SnippetForTag("snap.docker.app"), testutil.Contains, "# /system-data/var/snap/docker/common/var-lib-docker/overlay2/$SHA/diff/\n")
}

func (s *DockerSupportInterfaceSuite) TestPermanentSlotAppArmorSessionClassic(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	apparmorSpec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap()))
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.docker.app"})

	// verify core rule not present
	c.Check(apparmorSpec.SnippetForTag("snap.docker.app"), Not(testutil.Contains), "# /system-data/var/snap/docker/common/var-lib-docker/overlay2/$SHA/diff/\n")
}

func (s *DockerSupportInterfaceSuite) TestUdevTaggingDisablingRemoveLast(c *C) {
	// make a spec with network-control that has udev tagging
	spec := udev.NewSpecification(interfaces.NewSnapAppSet(s.networkCtrlPlug.Snap()))
	c.Assert(spec.AddConnectedPlug(builtin.MustInterface("network-control"), s.networkCtrlPlug, s.networkCtrlSlot), IsNil)
	c.Assert(spec.Snippets(), HasLen, 3)

	// connect docker-support interface plug and ensure that the udev spec is now nil
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Check(spec.Snippets(), HasLen, 0)
}

func (s *DockerSupportInterfaceSuite) TestUdevTaggingDisablingRemoveFirst(c *C) {
	spec := udev.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap()))
	// connect docker-support interface plug which specifies
	// controls-device-cgroup as true and ensure that the udev spec is now nil
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Check(spec.Snippets(), HasLen, 0)

	// add network-control and ensure the spec is still nil
	c.Assert(spec.AddConnectedPlug(builtin.MustInterface("network-control"), s.networkCtrlPlug, s.networkCtrlSlot), IsNil)
	c.Assert(spec.Snippets(), HasLen, 0)
}

func (s *DockerSupportInterfaceSuite) TestServicePermanentPlugSnippets(c *C) {
	snips, err := interfaces.PermanentPlugServiceSnippets(s.iface, s.plugInfo)
	c.Assert(err, IsNil)
	c.Check(snips, DeepEquals, []string{"Delegate=true"})

	// check that a malformed plug with bad attribute returns non-nil error
	// from PermanentPlugServiceSnippets, thereby ensuring that function
	// sanitizes plugs
	_, err = interfaces.PermanentPlugServiceSnippets(s.iface, s.malformedPlugInfo)
	c.Assert(err, ErrorMatches, "docker-support plug requires bool with 'privileged-containers'")
}
