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
	"github.com/snapcore/snapd/strutil"
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
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	apparmorSpec := apparmor.NewSpecification(appSet)
	c.Assert(apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(apparmorSpec.SecurityTags(), HasLen, 1)

	// connected plugs have a non-nil security snippet for seccomp
	appSet, err = interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	seccompSpec := seccomp.NewSpecification(appSet)
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
	appSet, err := interfaces.NewSnapAppSet(s.privContainersPlug.Snap(), nil)
	c.Assert(err, IsNil)
	apparmorSpec := apparmor.NewSpecification(appSet)
	c.Assert(apparmorSpec.AddConnectedPlug(s.iface, s.privContainersPlug, s.slot), IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.docker.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.docker.app"), testutil.Contains, `change_profile unsafe /**,`)

	appSet, err = interfaces.NewSnapAppSet(s.privContainersPlug.Snap(), nil)
	c.Assert(err, IsNil)
	seccompSpec := seccomp.NewSpecification(appSet)
	c.Assert(seccompSpec.AddConnectedPlug(s.iface, s.privContainersPlug, s.slot), IsNil)
	c.Assert(seccompSpec.SecurityTags(), DeepEquals, []string{"snap.docker.app"})
	c.Check(seccompSpec.SnippetForTag("snap.docker.app"), testutil.Contains, "@unrestricted")
}

func (s *DockerSupportInterfaceSuite) TestSanitizePlugWithPrivilegedFalse(c *C) {
	appSet, err := interfaces.NewSnapAppSet(s.noPrivContainersPlug.Snap(), nil)
	c.Assert(err, IsNil)
	apparmorSpec := apparmor.NewSpecification(appSet)
	c.Assert(apparmorSpec.AddConnectedPlug(s.iface, s.noPrivContainersPlug, s.slot), IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.docker.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.docker.app"), Not(testutil.Contains), `change_profile unsafe /**,`)

	appSet, err = interfaces.NewSnapAppSet(s.noPrivContainersPlug.Snap(), nil)
	c.Assert(err, IsNil)
	seccompSpec := seccomp.NewSpecification(appSet)
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
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	spec := apparmor.NewSpecification(appSet)
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.docker.app"})
	c.Check(spec.SnippetForTag("snap.docker.app"), testutil.Contains, "/sys/fs/cgroup/*/docker/   rw,\n")
	c.Check(spec.UsesPtraceTrace(), Equals, true)
	c.Check(spec.SnippetForTag("snap.docker.app"), Not(testutil.Contains), "userns,\n")

	// test with apparmor userns support too
	restore = apparmor_sandbox.MockFeatures(nil, nil, []string{"userns"}, nil)
	defer restore()
	appSet, err = interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	spec = apparmor.NewSpecification(appSet)
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.docker.app"})
	c.Check(spec.SnippetForTag("snap.docker.app"), testutil.Contains, "/sys/fs/cgroup/*/docker/   rw,\n")
	c.Check(spec.UsesPtraceTrace(), Equals, true)
	c.Check(spec.SnippetForTag("snap.docker.app"), testutil.Contains, "userns,\n")

}

func (s *DockerSupportInterfaceSuite) TestSecCompSpec(c *C) {
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	spec := seccomp.NewSpecification(appSet)
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

	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	apparmorSpec := apparmor.NewSpecification(appSet)
	err = apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.docker.app"})

	// verify core rule present
	c.Check(apparmorSpec.SnippetForTag("snap.docker.app"), testutil.Contains, "# /system-data/var/snap/docker/common/var-lib-docker/overlay2/$SHA/diff/\n")
}

func (s *DockerSupportInterfaceSuite) TestPermanentSlotAppArmorSessionClassic(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	apparmorSpec := apparmor.NewSpecification(appSet)
	err = apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.docker.app"})

	// verify core rule not present
	c.Check(apparmorSpec.SnippetForTag("snap.docker.app"), Not(testutil.Contains), "# /system-data/var/snap/docker/common/var-lib-docker/overlay2/$SHA/diff/\n")
}

func (s *DockerSupportInterfaceSuite) TestUdevTaggingDisablingRemoveLast(c *C) {
	// make a spec with network-control that has udev tagging
	appSet, err := interfaces.NewSnapAppSet(s.networkCtrlPlug.Snap(), nil)
	c.Assert(err, IsNil)
	spec := udev.NewSpecification(appSet)
	c.Assert(spec.AddConnectedPlug(builtin.MustInterface("network-control"), s.networkCtrlPlug, s.networkCtrlSlot), IsNil)
	c.Assert(spec.Snippets(), HasLen, 3)

	// connect docker-support interface plug and ensure that the udev spec is now nil
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Check(spec.Snippets(), HasLen, 0)
}

func (s *DockerSupportInterfaceSuite) TestUdevTaggingDisablingRemoveFirst(c *C) {
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	spec := udev.NewSpecification(appSet)
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

func (s *DockerSupportInterfaceSuite) TestGenerateAAREExclusionPatterns(c *C) {

	const dockerSupportConnectedPlugAppArmorUserNS = `
# allow use of user namespaces
userns,
`

	const dockerSupportConnectedPlugAppArmor = `
# Description: allow operating as the Docker daemon/containerd. This policy is
# intentionally not restrictive and is here to help guard against programming
# errors and not for security confinement. The Docker daemon by design requires
# extensive access to the system and cannot be effectively confined against
# malicious activity.

#include <abstractions/dbus-strict>

# Allow sockets/etc for docker
/{,var/}run/docker.sock rw,
/{,var/}run/docker/     rw,
/{,var/}run/docker/**   mrwklix,
/{,var/}run/runc/       rw,
/{,var/}run/runc/**     mrwklix,

# Allow sockets/etc for containerd
/{,var/}run/containerd/{,s/,runc/,runc/k8s.io/,runc/k8s.io/*/} rw,
/{,var/}run/containerd/runc/k8s.io/*/** rwk,
/{,var/}run/containerd/{io.containerd*/,io.containerd*/k8s.io/,io.containerd*/k8s.io/*/} rw,
/{,var/}run/containerd/io.containerd*/*/** rwk,
/{,var/}run/containerd/s/** rwk,

# Limit ipam-state to k8s
/run/ipam-state/k8s-** rw,
/run/ipam-state/k8s-*/lock k,

# Socket for docker-containerd-shim
unix (bind,listen) type=stream addr="@/containerd-shim/**.sock\x00",

/{,var/}run/mount/utab r,

# Wide read access to /proc, but somewhat limited writes for now
@{PROC}/ r,
@{PROC}/** r,
@{PROC}/[0-9]*/attr/{,apparmor/}exec w,
@{PROC}/[0-9]*/oom_score_adj w,

# Limited read access to specific bits of /sys
/sys/kernel/mm/hugepages/ r,
/sys/kernel/mm/transparent_hugepage/{,**} r,
/sys/fs/cgroup/cpuset/cpuset.cpus r,
/sys/fs/cgroup/cpuset/cpuset.mems r,
/sys/module/apparmor/parameters/enabled r,

# Limit cgroup writes a bit (Docker uses a "docker" sub-group)
/sys/fs/cgroup/*/docker/   rw,
/sys/fs/cgroup/*/docker/** rw,

# Also allow cgroup writes to kubernetes pods
/sys/fs/cgroup/*/kubepods/ rw,
/sys/fs/cgroup/*/kubepods/** rw,

# containerd can also be configured to use the systemd cgroup driver via
# plugins.cri.systemd_cgroup = true which moves container processes into
# systemd-managed cgroups. This is now the recommended configuration since it
# provides a single cgroup manager (systemd) in an effort to achieve consistent
# views of resources.
/sys/fs/cgroup/*/systemd/{,system.slice/} rw,          # create missing dirs
/sys/fs/cgroup/*/systemd/system.slice/** r,
/sys/fs/cgroup/*/systemd/system.slice/cgroup.procs w,

# Allow tracing ourself (especially the "runc" process we create)
ptrace (trace) peer=@{profile_name},

# Docker needs a lot of caps, but limits them in the app container
capability,

# Docker does all kinds of mounts all over the filesystem
/dev/mapper/control rw,
/dev/mapper/docker* rw,
/dev/loop-control r,
/dev/loop[0-9]* rw,
/sys/devices/virtual/block/dm-[0-9]*/** r,
mount,
umount,

# After doing a pivot_root using <graph-dir>/<container-fs>/.pivot_rootNNNNNN,
# Docker removes the leftover /.pivot_rootNNNNNN directory (which is now
# relative to "/" instead of "<graph-dir>/<container-fs>" thanks to pivot_root)
pivot_root,
/.pivot_root[0-9]*/ rw,

# file descriptors (/proc/NNN/fd/X)
# file descriptors in the container show up here due to attach_disconnected
/[0-9]* rw,

# Docker needs to be able to create and load the profile it applies to
# containers ("docker-default")
/{,usr/}sbin/apparmor_parser ixr,
/etc/apparmor.d/cache/ r,            # apparmor 2.12 and below
/etc/apparmor.d/cache/.features r,
/etc/apparmor.d/{,cache/}docker* rw,
/var/cache/apparmor/{,*/} r,         # apparmor 2.13 and higher
/var/cache/apparmor/*/.features r,
/var/cache/apparmor/*/docker* rw,
/etc/apparmor.d/tunables/{,**} r,
/etc/apparmor.d/abstractions/{,**} r,
/etc/apparmor/parser.conf r,
/etc/apparmor.d/abi/{,*} r,
/etc/apparmor/subdomain.conf r,
/sys/kernel/security/apparmor/.replace rw,
/sys/kernel/security/apparmor/{,**} r,

# use 'privileged-containers: true' to support --security-opts

# defaults for docker-default
# Unfortunately, the docker snap is currently (by design?) setup to have both 
# the privileged and unprivileged variant of the docker-support interface 
# connected which means we have rules that are compatible to allow both 
# transitioning to docker-default profile here AAAAAAND transitioning to any 
# other profile below in the privileged snippet, BUUUUUUUT also need to be 
# triply compatible with the injected compatibility snap-confine transition 
# rules to temporarily support executing other snaps from devmode snaps. 
# So we are left with writing out these extremely verbose regexps because AARE 
# does not have a negative concept to exclude just the paths we want. 
# See also https://bugs.launchpad.net/apparmor/+bug/1964853 and
# https://bugs.launchpad.net/apparmor/+bug/1964854 for more details on the 
# AppArmor parser side of things.
# TODO: When we drop support for executing other snaps from devmode snaps (or 
# when the AppArmor parser bugs are fixed) this can go back to the much simpler
# rule:
# change_profile unsafe /** -> docker-default,
# but until then we are stuck with:
change_profile unsafe /[^s]** -> docker-default,
change_profile unsafe /s[^n]** -> docker-default,
change_profile unsafe /sn[^a]** -> docker-default,
change_profile unsafe /sna[^p]** -> docker-default,
change_profile unsafe /snap[^/]** -> docker-default,
change_profile unsafe /snap/[^sc]** -> docker-default,
change_profile unsafe /snap/{s[^n],c[^o]}** -> docker-default,
change_profile unsafe /snap/{sn[^a],co[^r]}** -> docker-default,
change_profile unsafe /snap/{sna[^p],cor[^e]}** -> docker-default,

# branch for the /snap/core/... paths
change_profile unsafe /snap/core[^/]** -> docker-default,
change_profile unsafe /snap/core/*/[^u]** -> docker-default,
change_profile unsafe /snap/core/*/u[^s]** -> docker-default,
change_profile unsafe /snap/core/*/us[^r]** -> docker-default,
change_profile unsafe /snap/core/*/usr[^/]** -> docker-default,
change_profile unsafe /snap/core/*/usr/[^l]** -> docker-default,
change_profile unsafe /snap/core/*/usr/l[^i]** -> docker-default,
change_profile unsafe /snap/core/*/usr/li[^b]** -> docker-default,
change_profile unsafe /snap/core/*/usr/lib[^/]** -> docker-default,
change_profile unsafe /snap/core/*/usr/lib/[^s]** -> docker-default,
change_profile unsafe /snap/core/*/usr/lib/s[^n]** -> docker-default,
change_profile unsafe /snap/core/*/usr/lib/sn[^a]** -> docker-default,
change_profile unsafe /snap/core/*/usr/lib/sna[^p]** -> docker-default,
change_profile unsafe /snap/core/*/usr/lib/snap[^d]** -> docker-default,
change_profile unsafe /snap/core/*/usr/lib/snapd[^/]** -> docker-default,
change_profile unsafe /snap/core/*/usr/lib/snapd/[^s]** -> docker-default,
change_profile unsafe /snap/core/*/usr/lib/snapd/s[^n]** -> docker-default,
change_profile unsafe /snap/core/*/usr/lib/snapd/sn[^a]** -> docker-default,
change_profile unsafe /snap/core/*/usr/lib/snapd/sna[^p]** -> docker-default,
change_profile unsafe /snap/core/*/usr/lib/snapd/snap[^-]** -> docker-default,
change_profile unsafe /snap/core/*/usr/lib/snapd/snap-[^c]** -> docker-default,
change_profile unsafe /snap/core/*/usr/lib/snapd/snap-c[^o]** -> docker-default,
change_profile unsafe /snap/core/*/usr/lib/snapd/snap-co[^n]** -> docker-default,
change_profile unsafe /snap/core/*/usr/lib/snapd/snap-con[^f]** -> docker-default,
change_profile unsafe /snap/core/*/usr/lib/snapd/snap-conf[^i]** -> docker-default,
change_profile unsafe /snap/core/*/usr/lib/snapd/snap-confi[^n]** -> docker-default,
change_profile unsafe /snap/core/*/usr/lib/snapd/snap-confin[^e]** -> docker-default,

# branch for the /snap/snapd/... paths
change_profile unsafe /snap/snap[^d]** -> docker-default,
change_profile unsafe /snap/snapd[^/]** -> docker-default,
change_profile unsafe /snap/snapd/*/[^u]** -> docker-default,
change_profile unsafe /snap/snapd/*/u[^s]** -> docker-default,
change_profile unsafe /snap/snapd/*/us[^r]** -> docker-default,
change_profile unsafe /snap/snapd/*/usr[^/]** -> docker-default,
change_profile unsafe /snap/snapd/*/usr/[^l]** -> docker-default,
change_profile unsafe /snap/snapd/*/usr/l[^i]** -> docker-default,
change_profile unsafe /snap/snapd/*/usr/li[^b]** -> docker-default,
change_profile unsafe /snap/snapd/*/usr/lib[^/]** -> docker-default,
change_profile unsafe /snap/snapd/*/usr/lib/[^s]** -> docker-default,
change_profile unsafe /snap/snapd/*/usr/lib/s[^n]** -> docker-default,
change_profile unsafe /snap/snapd/*/usr/lib/sn[^a]** -> docker-default,
change_profile unsafe /snap/snapd/*/usr/lib/sna[^p]** -> docker-default,
change_profile unsafe /snap/snapd/*/usr/lib/snap[^d]** -> docker-default,
change_profile unsafe /snap/snapd/*/usr/lib/snapd[^/]** -> docker-default,
change_profile unsafe /snap/snapd/*/usr/lib/snapd/[^s]** -> docker-default,
change_profile unsafe /snap/snapd/*/usr/lib/snapd/s[^n]** -> docker-default,
change_profile unsafe /snap/snapd/*/usr/lib/snapd/sn[^a]** -> docker-default,
change_profile unsafe /snap/snapd/*/usr/lib/snapd/sna[^p]** -> docker-default,
change_profile unsafe /snap/snapd/*/usr/lib/snapd/snap[^-]** -> docker-default,
change_profile unsafe /snap/snapd/*/usr/lib/snapd/snap-[^c]** -> docker-default,
change_profile unsafe /snap/snapd/*/usr/lib/snapd/snap-c[^o]** -> docker-default,
change_profile unsafe /snap/snapd/*/usr/lib/snapd/snap-co[^n]** -> docker-default,
change_profile unsafe /snap/snapd/*/usr/lib/snapd/snap-con[^f]** -> docker-default,
change_profile unsafe /snap/snapd/*/usr/lib/snapd/snap-conf[^i]** -> docker-default,
change_profile unsafe /snap/snapd/*/usr/lib/snapd/snap-confi[^n]** -> docker-default,
change_profile unsafe /snap/snapd/*/usr/lib/snapd/snap-confin[^e]** -> docker-default,


# signal/tracing rules too
signal (send) peer=docker-default,
ptrace (read, trace) peer=docker-default,


# defaults for containerd
# TODO: When we drop support for executing other snaps from devmode snaps (or 
# when the AppArmor parser bugs are fixed) this can go back to the much simpler
# rule:	
# change_profile unsafe /** -> cri-containerd.apparmor.d,
# see above comment, we need this because we can't have nice things
change_profile unsafe /[^s]** -> cri-containerd.apparmor.d,
change_profile unsafe /s[^n]** -> cri-containerd.apparmor.d,
change_profile unsafe /sn[^a]** -> cri-containerd.apparmor.d,
change_profile unsafe /sna[^p]** -> cri-containerd.apparmor.d,
change_profile unsafe /snap[^/]** -> cri-containerd.apparmor.d,
change_profile unsafe /snap/[^sc]** -> cri-containerd.apparmor.d,
change_profile unsafe /snap/{s[^n],c[^o]}** -> cri-containerd.apparmor.d,
change_profile unsafe /snap/{sn[^a],co[^r]}** -> cri-containerd.apparmor.d,
change_profile unsafe /snap/{sna[^p],cor[^e]}** -> cri-containerd.apparmor.d,

# branch for the /snap/core/... paths
change_profile unsafe /snap/core[^/]** -> cri-containerd.apparmor.d,
change_profile unsafe /snap/core/*/[^u]** -> cri-containerd.apparmor.d,
change_profile unsafe /snap/core/*/u[^s]** -> cri-containerd.apparmor.d,
change_profile unsafe /snap/core/*/us[^r]** -> cri-containerd.apparmor.d,
change_profile unsafe /snap/core/*/usr[^/]** -> cri-containerd.apparmor.d,
change_profile unsafe /snap/core/*/usr/[^l]** -> cri-containerd.apparmor.d,
change_profile unsafe /snap/core/*/usr/l[^i]** -> cri-containerd.apparmor.d,
change_profile unsafe /snap/core/*/usr/li[^b]** -> cri-containerd.apparmor.d,
change_profile unsafe /snap/core/*/usr/lib[^/]** -> cri-containerd.apparmor.d,
change_profile unsafe /snap/core/*/usr/lib/[^s]** -> cri-containerd.apparmor.d,
change_profile unsafe /snap/core/*/usr/lib/s[^n]** -> cri-containerd.apparmor.d,
change_profile unsafe /snap/core/*/usr/lib/sn[^a]** -> cri-containerd.apparmor.d,
change_profile unsafe /snap/core/*/usr/lib/sna[^p]** -> cri-containerd.apparmor.d,
change_profile unsafe /snap/core/*/usr/lib/snap[^d]** -> cri-containerd.apparmor.d,
change_profile unsafe /snap/core/*/usr/lib/snapd[^/]** -> cri-containerd.apparmor.d,
change_profile unsafe /snap/core/*/usr/lib/snapd/[^s]** -> cri-containerd.apparmor.d,
change_profile unsafe /snap/core/*/usr/lib/snapd/s[^n]** -> cri-containerd.apparmor.d,
change_profile unsafe /snap/core/*/usr/lib/snapd/sn[^a]** -> cri-containerd.apparmor.d,
change_profile unsafe /snap/core/*/usr/lib/snapd/sna[^p]** -> cri-containerd.apparmor.d,
change_profile unsafe /snap/core/*/usr/lib/snapd/snap[^-]** -> cri-containerd.apparmor.d,
change_profile unsafe /snap/core/*/usr/lib/snapd/snap-[^c]** -> cri-containerd.apparmor.d,
change_profile unsafe /snap/core/*/usr/lib/snapd/snap-c[^o]** -> cri-containerd.apparmor.d,
change_profile unsafe /snap/core/*/usr/lib/snapd/snap-co[^n]** -> cri-containerd.apparmor.d,
change_profile unsafe /snap/core/*/usr/lib/snapd/snap-con[^f]** -> cri-containerd.apparmor.d,
change_profile unsafe /snap/core/*/usr/lib/snapd/snap-conf[^i]** -> cri-containerd.apparmor.d,
change_profile unsafe /snap/core/*/usr/lib/snapd/snap-confi[^n]** -> cri-containerd.apparmor.d,
change_profile unsafe /snap/core/*/usr/lib/snapd/snap-confin[^e]** -> cri-containerd.apparmor.d,

# branch for the /snap/snapd/... paths
change_profile unsafe /snap/snap[^d]** -> cri-containerd.apparmor.d,
change_profile unsafe /snap/snapd[^/]** -> cri-containerd.apparmor.d,
change_profile unsafe /snap/snapd/*/[^u]** -> cri-containerd.apparmor.d,
change_profile unsafe /snap/snapd/*/u[^s]** -> cri-containerd.apparmor.d,
change_profile unsafe /snap/snapd/*/us[^r]** -> cri-containerd.apparmor.d,
change_profile unsafe /snap/snapd/*/usr[^/]** -> cri-containerd.apparmor.d,
change_profile unsafe /snap/snapd/*/usr/[^l]** -> cri-containerd.apparmor.d,
change_profile unsafe /snap/snapd/*/usr/l[^i]** -> cri-containerd.apparmor.d,
change_profile unsafe /snap/snapd/*/usr/li[^b]** -> cri-containerd.apparmor.d,
change_profile unsafe /snap/snapd/*/usr/lib[^/]** -> cri-containerd.apparmor.d,
change_profile unsafe /snap/snapd/*/usr/lib/[^s]** -> cri-containerd.apparmor.d,
change_profile unsafe /snap/snapd/*/usr/lib/s[^n]** -> cri-containerd.apparmor.d,
change_profile unsafe /snap/snapd/*/usr/lib/sn[^a]** -> cri-containerd.apparmor.d,
change_profile unsafe /snap/snapd/*/usr/lib/sna[^p]** -> cri-containerd.apparmor.d,
change_profile unsafe /snap/snapd/*/usr/lib/snap[^d]** -> cri-containerd.apparmor.d,
change_profile unsafe /snap/snapd/*/usr/lib/snapd[^/]** -> cri-containerd.apparmor.d,
change_profile unsafe /snap/snapd/*/usr/lib/snapd/[^s]** -> cri-containerd.apparmor.d,
change_profile unsafe /snap/snapd/*/usr/lib/snapd/s[^n]** -> cri-containerd.apparmor.d,
change_profile unsafe /snap/snapd/*/usr/lib/snapd/sn[^a]** -> cri-containerd.apparmor.d,
change_profile unsafe /snap/snapd/*/usr/lib/snapd/sna[^p]** -> cri-containerd.apparmor.d,
change_profile unsafe /snap/snapd/*/usr/lib/snapd/snap[^-]** -> cri-containerd.apparmor.d,
change_profile unsafe /snap/snapd/*/usr/lib/snapd/snap-[^c]** -> cri-containerd.apparmor.d,
change_profile unsafe /snap/snapd/*/usr/lib/snapd/snap-c[^o]** -> cri-containerd.apparmor.d,
change_profile unsafe /snap/snapd/*/usr/lib/snapd/snap-co[^n]** -> cri-containerd.apparmor.d,
change_profile unsafe /snap/snapd/*/usr/lib/snapd/snap-con[^f]** -> cri-containerd.apparmor.d,
change_profile unsafe /snap/snapd/*/usr/lib/snapd/snap-conf[^i]** -> cri-containerd.apparmor.d,
change_profile unsafe /snap/snapd/*/usr/lib/snapd/snap-confi[^n]** -> cri-containerd.apparmor.d,
change_profile unsafe /snap/snapd/*/usr/lib/snapd/snap-confin[^e]** -> cri-containerd.apparmor.d,

# signal/tracing rules too
signal (send) peer=cri-containerd.apparmor.d,
ptrace (read, trace) peer=cri-containerd.apparmor.d,

# Graph (storage) driver bits
/{dev,run}/shm/aufs.xino mrw,
/proc/fs/aufs/plink_maint w,
/sys/fs/aufs/** r,

#cf bug 1502785
/ r,

# recent versions of docker make a symlink from /dev/ptmx to /dev/pts/ptmx
# and so to allow allocating a new shell we need this
/dev/pts/ptmx rw,

# needed by runc for mitigation of CVE-2019-5736
# For details see https://bugs.launchpad.net/apparmor/+bug/1820344
/ ix,
/bin/runc ixr,

/pause ixr,
/bin/busybox ixr,

# When kubernetes drives containerd, containerd needs access to CNI services,
# like flanneld's subnet.env for DNS. This would ideally be snap-specific (it
# could if the control plane was a snap), but in deployments where the control
# plane is not a snap, it will tell flannel to use this path.
/run/flannel/{,**} rk,

# When kubernetes drives containerd, containerd needs access to various
# secrets for the pods which are overlayed at /run/secrets/....
# This would ideally be snap-specific (it could if the control plane was a
# snap), but in deployments where the control plane is not a snap, it will tell
# containerd to use this path for various account information for pods.
/run/secrets/kubernetes.io/{,**} rk,

# Allow using the 'autobind' feature of bind() (eg, for journald via go-systemd)
# unix (bind) type=dgram addr=auto,
# TODO: when snapd vendors in AppArmor userspace, then enable the new syntax
# above which allows only "empty"/automatic addresses, for now we simply permit
# all addresses with SOCK_DGRAM type, which leaks info for other addresses than
# what docker tries to use
# see https://bugs.launchpad.net/snapd/+bug/1867216
unix (bind) type=dgram,

# With cgroup v2, docker uses the systemd driver to run the containers,
# which requires dockerd to talk to systemd over system bus.
dbus (send)
    bus=system
    path=/org/freedesktop/systemd1
    interface=org.freedesktop.systemd1.Manager
    member={StartTransientUnit,KillUnit,StopUnit,ResetFailedUnit,SetUnitProperties}
    peer=(name=org.freedesktop.systemd1,label=unconfined),

dbus (receive)
    bus=system
    path=/org/freedesktop/systemd1
    interface=org.freedesktop.systemd1.Manager
    member=JobRemoved
    peer=(label=unconfined),

dbus (send)
    bus=system
    interface=org.freedesktop.DBus.Properties
    path=/org/freedesktop/systemd1
    member=Get{,All}
    peer=(name=org.freedesktop.systemd1,label=unconfined),

`

	const dockerSupportPrivilegedAppArmor = `
# Description: allow docker daemon to run privileged containers. This gives
# full access to all resources on the system and thus gives device ownership to
# connected snaps.

# These rules are here to allow Docker to launch unconfined containers but
# allow the docker daemon itself to go unconfined. Since it runs as root, this
# grants device ownership.
# TODO: When we drop support for executing other snaps from devmode snaps (or 
# when the AppArmor parser bugs are fixed) this can go back to the much simpler
# rule:
# change_profile unsafe /**,
# but until then we need this set of rules to avoid exec transition conflicts.
# See also the comment above the "change_profile unsafe /** -> docker-default," 
# rule for more context.
change_profile unsafe /[^s]**,
change_profile unsafe /s[^n]**,
change_profile unsafe /sn[^a]**,
change_profile unsafe /sna[^p]**,
change_profile unsafe /snap[^/]**,
change_profile unsafe /snap/[^sc]**,
change_profile unsafe /snap/{s[^n],c[^o]}**,
change_profile unsafe /snap/{sn[^a],co[^r]}**,
change_profile unsafe /snap/{sna[^p],cor[^e]}**,

# branch for the /snap/core/... paths
change_profile unsafe /snap/core[^/]**,
change_profile unsafe /snap/core/*/[^u]**,
change_profile unsafe /snap/core/*/u[^s]**,
change_profile unsafe /snap/core/*/us[^r]**,
change_profile unsafe /snap/core/*/usr[^/]**,
change_profile unsafe /snap/core/*/usr/[^l]**,
change_profile unsafe /snap/core/*/usr/l[^i]**,
change_profile unsafe /snap/core/*/usr/li[^b]**,
change_profile unsafe /snap/core/*/usr/lib[^/]**,
change_profile unsafe /snap/core/*/usr/lib/[^s]**,
change_profile unsafe /snap/core/*/usr/lib/s[^n]**,
change_profile unsafe /snap/core/*/usr/lib/sn[^a]**,
change_profile unsafe /snap/core/*/usr/lib/sna[^p]**,
change_profile unsafe /snap/core/*/usr/lib/snap[^d]**,
change_profile unsafe /snap/core/*/usr/lib/snapd[^/]**,
change_profile unsafe /snap/core/*/usr/lib/snapd/[^s]**,
change_profile unsafe /snap/core/*/usr/lib/snapd/s[^n]**,
change_profile unsafe /snap/core/*/usr/lib/snapd/sn[^a]**,
change_profile unsafe /snap/core/*/usr/lib/snapd/sna[^p]**,
change_profile unsafe /snap/core/*/usr/lib/snapd/snap[^-]**,
change_profile unsafe /snap/core/*/usr/lib/snapd/snap-[^c]**,
change_profile unsafe /snap/core/*/usr/lib/snapd/snap-c[^o]**,
change_profile unsafe /snap/core/*/usr/lib/snapd/snap-co[^n]**,
change_profile unsafe /snap/core/*/usr/lib/snapd/snap-con[^f]**,
change_profile unsafe /snap/core/*/usr/lib/snapd/snap-conf[^i]**,
change_profile unsafe /snap/core/*/usr/lib/snapd/snap-confi[^n]**,
change_profile unsafe /snap/core/*/usr/lib/snapd/snap-confin[^e]**,

# branch for the /snap/snapd/... paths
change_profile unsafe /snap/snap[^d]**,
change_profile unsafe /snap/snapd[^/]**,
change_profile unsafe /snap/snapd/*/[^u]**,
change_profile unsafe /snap/snapd/*/u[^s]**,
change_profile unsafe /snap/snapd/*/us[^r]**,
change_profile unsafe /snap/snapd/*/usr[^/]**,
change_profile unsafe /snap/snapd/*/usr/[^l]**,
change_profile unsafe /snap/snapd/*/usr/l[^i]**,
change_profile unsafe /snap/snapd/*/usr/li[^b]**,
change_profile unsafe /snap/snapd/*/usr/lib[^/]**,
change_profile unsafe /snap/snapd/*/usr/lib/[^s]**,
change_profile unsafe /snap/snapd/*/usr/lib/s[^n]**,
change_profile unsafe /snap/snapd/*/usr/lib/sn[^a]**,
change_profile unsafe /snap/snapd/*/usr/lib/sna[^p]**,
change_profile unsafe /snap/snapd/*/usr/lib/snap[^d]**,
change_profile unsafe /snap/snapd/*/usr/lib/snapd[^/]**,
change_profile unsafe /snap/snapd/*/usr/lib/snapd/[^s]**,
change_profile unsafe /snap/snapd/*/usr/lib/snapd/s[^n]**,
change_profile unsafe /snap/snapd/*/usr/lib/snapd/sn[^a]**,
change_profile unsafe /snap/snapd/*/usr/lib/snapd/sna[^p]**,
change_profile unsafe /snap/snapd/*/usr/lib/snapd/snap[^-]**,
change_profile unsafe /snap/snapd/*/usr/lib/snapd/snap-[^c]**,
change_profile unsafe /snap/snapd/*/usr/lib/snapd/snap-c[^o]**,
change_profile unsafe /snap/snapd/*/usr/lib/snapd/snap-co[^n]**,
change_profile unsafe /snap/snapd/*/usr/lib/snapd/snap-con[^f]**,
change_profile unsafe /snap/snapd/*/usr/lib/snapd/snap-conf[^i]**,
change_profile unsafe /snap/snapd/*/usr/lib/snapd/snap-confi[^n]**,
change_profile unsafe /snap/snapd/*/usr/lib/snapd/snap-confin[^e]**,

# allow signaling and tracing any unconfined process since if containers are 
# launched without confinement docker still needs to trace them
signal (send) peer=unconfined,
ptrace (read, trace) peer=unconfined,

# This grants raw access to device files and thus device ownership
/dev/** mrwkl,
@{PROC}/** mrwkl,

# When kubernetes drives docker/containerd, it creates and runs files in the
# container at arbitrary locations (eg, via pivot_root).
# Allow any file except for executing /snap/{snapd,core}/*/usr/lib/snapd/snap-confine
# because in devmode confinement we will have a separate "x" transition on exec
# rule that is in the policy that will overlap and thus conflict with this rule.
# TODO: When we drop support for executing other snaps from devmode snaps (or 
# when the AppArmor parser bugs are fixed) this can go back to the much simpler
# rule:
# /** rwlix,
# but until then we need this set of rules to avoid exec transition conflicts.
# See also the comment above the "change_profile unsafe /** -> docker-default," 
# rule for more context.
/[^s]** rwlix,
/s[^n]** rwlix,
/sn[^a]** rwlix,
/sna[^p]** rwlix,
/snap[^/]** rwlix,
/snap/[^sc]** rwlix,
/snap/{s[^n],c[^o]}** rwlix,
/snap/{sn[^a],co[^r]}** rwlix,
/snap/{sna[^p],cor[^e]}** rwlix,

# branch for the /snap/core/... paths
/snap/core[^/]** rwlix,
/snap/core/*/[^u]** rwlix,
/snap/core/*/u[^s]** rwlix,
/snap/core/*/us[^r]** rwlix,
/snap/core/*/usr[^/]** rwlix,
/snap/core/*/usr/[^l]** rwlix,
/snap/core/*/usr/l[^i]** rwlix,
/snap/core/*/usr/li[^b]** rwlix,
/snap/core/*/usr/lib[^/]** rwlix,
/snap/core/*/usr/lib/[^s]** rwlix,
/snap/core/*/usr/lib/s[^n]** rwlix,
/snap/core/*/usr/lib/sn[^a]** rwlix,
/snap/core/*/usr/lib/sna[^p]** rwlix,
/snap/core/*/usr/lib/snap[^d]** rwlix,
/snap/core/*/usr/lib/snapd[^/]** rwlix,
/snap/core/*/usr/lib/snapd/[^s]** rwlix,
/snap/core/*/usr/lib/snapd/s[^n]** rwlix,
/snap/core/*/usr/lib/snapd/sn[^a]** rwlix,
/snap/core/*/usr/lib/snapd/sna[^p]** rwlix,
/snap/core/*/usr/lib/snapd/snap[^-]** rwlix,
/snap/core/*/usr/lib/snapd/snap-[^c]** rwlix,
/snap/core/*/usr/lib/snapd/snap-c[^o]** rwlix,
/snap/core/*/usr/lib/snapd/snap-co[^n]** rwlix,
/snap/core/*/usr/lib/snapd/snap-con[^f]** rwlix,
/snap/core/*/usr/lib/snapd/snap-conf[^i]** rwlix,
/snap/core/*/usr/lib/snapd/snap-confi[^n]** rwlix,
/snap/core/*/usr/lib/snapd/snap-confin[^e]** rwlix,

# branch for the /snap/snapd/... paths
/snap/snap[^d]** rwlix,
/snap/snapd[^/]** rwlix,
/snap/snapd/*/[^u]** rwlix,
/snap/snapd/*/u[^s]** rwlix,
/snap/snapd/*/us[^r]** rwlix,
/snap/snapd/*/usr[^/]** rwlix,
/snap/snapd/*/usr/[^l]** rwlix,
/snap/snapd/*/usr/l[^i]** rwlix,
/snap/snapd/*/usr/li[^b]** rwlix,
/snap/snapd/*/usr/lib[^/]** rwlix,
/snap/snapd/*/usr/lib/[^s]** rwlix,
/snap/snapd/*/usr/lib/s[^n]** rwlix,
/snap/snapd/*/usr/lib/sn[^a]** rwlix,
/snap/snapd/*/usr/lib/sna[^p]** rwlix,
/snap/snapd/*/usr/lib/snap[^d]** rwlix,
/snap/snapd/*/usr/lib/snapd[^/]** rwlix,
/snap/snapd/*/usr/lib/snapd/[^s]** rwlix,
/snap/snapd/*/usr/lib/snapd/s[^n]** rwlix,
/snap/snapd/*/usr/lib/snapd/sn[^a]** rwlix,
/snap/snapd/*/usr/lib/snapd/sna[^p]** rwlix,
/snap/snapd/*/usr/lib/snapd/snap[^-]** rwlix,
/snap/snapd/*/usr/lib/snapd/snap-[^c]** rwlix,
/snap/snapd/*/usr/lib/snapd/snap-c[^o]** rwlix,
/snap/snapd/*/usr/lib/snapd/snap-co[^n]** rwlix,
/snap/snapd/*/usr/lib/snapd/snap-con[^f]** rwlix,
/snap/snapd/*/usr/lib/snapd/snap-conf[^i]** rwlix,
/snap/snapd/*/usr/lib/snapd/snap-confi[^n]** rwlix,
/snap/snapd/*/usr/lib/snapd/snap-confin[^e]** rwlix,
`

	// Generate profile to compare with
	privilegedProfile := dockerSupportPrivilegedAppArmor + dockerSupportConnectedPlugAppArmor

	// if apparmor supports userns mediation then add this too
	if (apparmor_sandbox.ProbedLevel() != apparmor_sandbox.Partial) && (apparmor_sandbox.ProbedLevel() != apparmor_sandbox.Full) {
		c.Skip(apparmor_sandbox.Summary())
	}

	features, err := apparmor_sandbox.ParserFeatures()
	c.Assert(err, IsNil)
	if strutil.ListContains(features, "userns") {
		privilegedProfile += dockerSupportConnectedPlugAppArmorUserNS
	}

	// Profile existing profile
	expectedHash, err := testutil.AppArmorParseAndHashHelper("#include <tunables/global> \nprofile docker_support {" + privilegedProfile + "}")
	c.Assert(err, IsNil)

	// Profile generated using GenerateAAREExclusionPatterns
	appSet, err := interfaces.NewSnapAppSet(s.privContainersPlug.Snap(), nil)
	c.Assert(err, IsNil)
	apparmorSpec := apparmor.NewSpecification(appSet)
	c.Assert(apparmorSpec.AddConnectedPlug(s.iface, s.privContainersPlug, s.slot), IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.docker.app"})
	resHash, err := testutil.AppArmorParseAndHashHelper("#include <tunables/global> \nprofile docker_support {" + apparmorSpec.SnippetForTag("snap.docker.app") + "}")
	c.Assert(err, IsNil)
	comment := Commentf("Apparmor rules generated by GenerateAAREExclusionPatterns doesn't match the expected profile")
	c.Assert(resHash, Equals, expectedHash, comment)

}
