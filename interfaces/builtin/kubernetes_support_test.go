// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017-2018 Canonical Ltd
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
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type KubernetesSupportInterfaceSuite struct {
	iface             interfaces.Interface
	slotInfo          *snap.SlotInfo
	slot              *interfaces.ConnectedSlot
	plugInfo          *snap.PlugInfo
	plug              *interfaces.ConnectedPlug
	plugKubeletInfo   *snap.PlugInfo
	plugKubelet       *interfaces.ConnectedPlug
	plugKubeproxyInfo *snap.PlugInfo
	plugKubeproxy     *interfaces.ConnectedPlug
	plugBadInfo       *snap.PlugInfo
	plugBad           *interfaces.ConnectedPlug
}

const k8sMockPlugSnapInfoYaml = `name: kubernetes-support
version: 0
plugs:
  k8s-default:
    interface: kubernetes-support
  k8s-kubelet:
    interface: kubernetes-support
    flavor: kubelet
  k8s-kubeproxy:
    interface: kubernetes-support
    flavor: kubeproxy
  k8s-bad:
    interface: kubernetes-support
    flavor: bad
apps:
 default:
  plugs: [k8s-default]
 kubelet:
  plugs: [k8s-kubelet]
 kubeproxy:
  plugs: [k8s-kubeproxy]
`

var _ = Suite(&KubernetesSupportInterfaceSuite{
	iface: builtin.MustInterface("kubernetes-support"),
})

func (s *KubernetesSupportInterfaceSuite) SetUpTest(c *C) {
	s.slotInfo = &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "core", Type: snap.TypeOS},
		Name:      "kubernetes-support",
		Interface: "kubernetes-support",
	}
	s.slot = interfaces.NewConnectedSlot(s.slotInfo, nil, nil)
	plugSnap := snaptest.MockInfo(c, k8sMockPlugSnapInfoYaml, nil)

	s.plugInfo = plugSnap.Plugs["k8s-default"]
	s.plug = interfaces.NewConnectedPlug(s.plugInfo, nil, nil)

	s.plugKubeletInfo = plugSnap.Plugs["k8s-kubelet"]
	s.plugKubelet = interfaces.NewConnectedPlug(s.plugKubeletInfo, nil, nil)

	s.plugKubeproxyInfo = plugSnap.Plugs["k8s-kubeproxy"]
	s.plugKubeproxy = interfaces.NewConnectedPlug(s.plugKubeproxyInfo, nil, nil)

	s.plugBadInfo = plugSnap.Plugs["k8s-bad"]
	s.plugBad = interfaces.NewConnectedPlug(s.plugBadInfo, nil, nil)
}

func (s *KubernetesSupportInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "kubernetes-support")
}

func (s *KubernetesSupportInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
	slot := &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "some-snap"},
		Name:      "kubernetes-support",
		Interface: "kubernetes-support",
	}
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		"kubernetes-support slots are reserved for the core snap")
}

func (s *KubernetesSupportInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugKubeletInfo), IsNil)
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugKubeproxyInfo), IsNil)
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugBadInfo), ErrorMatches, `kubernetes-support plug requires "flavor" to be either "kubelet" or "kubeproxy"`)
}

func (s *KubernetesSupportInterfaceSuite) TestKModConnectedPlug(c *C) {
	// default should have kubeproxy modules
	spec := &kmod.Specification{}
	err := spec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(spec.Modules(), DeepEquals, map[string]bool{
		"llc":       true,
		"stp":       true,
		"ip_vs_rr":  true,
		"ip_vs_sh":  true,
		"ip_vs_wrr": true,
		"libcrc32c": true,
	})

	// kubeproxy should have its modules
	spec = &kmod.Specification{}
	err = spec.AddConnectedPlug(s.iface, s.plugKubeproxy, s.slot)
	c.Assert(err, IsNil)
	c.Assert(spec.Modules(), DeepEquals, map[string]bool{
		"llc":       true,
		"stp":       true,
		"ip_vs_rr":  true,
		"ip_vs_sh":  true,
		"ip_vs_wrr": true,
		"libcrc32c": true,
	})

	// kubelet shouldn't have anything
	spec = &kmod.Specification{}
	err = spec.AddConnectedPlug(s.iface, s.plugKubelet, s.slot)
	c.Assert(err, IsNil)
	c.Assert(spec.Modules(), DeepEquals, map[string]bool{})
}

func (s *KubernetesSupportInterfaceSuite) TestAppArmorConnectedPlug(c *C) {
	// default should have kubeproxy and kubelet rules
	spec := &apparmor.Specification{}
	err := spec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.kubernetes-support.default"})
	c.Check(spec.SnippetForTag("snap.kubernetes-support.default"), testutil.Contains, "# Common rules for running as a kubernetes node\n")
	c.Check(spec.SnippetForTag("snap.kubernetes-support.default"), testutil.Contains, "# Allow running as the kubelet service\n")
	c.Check(spec.SnippetForTag("snap.kubernetes-support.default"), testutil.Contains, "# Allow running as the kubeproxy service\n")

	// kubeproxy should have only its rules
	spec = &apparmor.Specification{}
	err = spec.AddConnectedPlug(s.iface, s.plugKubeproxy, s.slot)
	c.Assert(err, IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.kubernetes-support.kubeproxy"})
	c.Check(spec.SnippetForTag("snap.kubernetes-support.kubeproxy"), testutil.Contains, "# Common rules for running as a kubernetes node\n")
	c.Check(spec.SnippetForTag("snap.kubernetes-support.kubeproxy"), testutil.Contains, "# Allow running as the kubeproxy service\n")
	c.Check(spec.SnippetForTag("snap.kubernetes-support.kubeproxy"), Not(testutil.Contains), "# Allow running as the kubelet service\n")

	// kubelet should have only its rules
	spec = &apparmor.Specification{}
	err = spec.AddConnectedPlug(s.iface, s.plugKubelet, s.slot)
	c.Assert(err, IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.kubernetes-support.kubelet"})
	c.Check(spec.SnippetForTag("snap.kubernetes-support.kubelet"), testutil.Contains, "# Common rules for running as a kubernetes node\n")
	c.Check(spec.SnippetForTag("snap.kubernetes-support.kubelet"), testutil.Contains, "# Allow running as the kubelet service\n")
	c.Check(spec.SnippetForTag("snap.kubernetes-support.kubelet"), Not(testutil.Contains), "# Allow running as the kubeproxy service\n")
}

func (s *KubernetesSupportInterfaceSuite) TestSecCompConnectedPlug(c *C) {
	// default should have kubelet rules
	spec := &seccomp.Specification{}
	err := spec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.kubernetes-support.default"})
	c.Check(spec.SnippetForTag("snap.kubernetes-support.default"), testutil.Contains, "# Allow running as the kubelet service\n")

	// kubeproxy should not have any rules
	spec = &seccomp.Specification{}
	err = spec.AddConnectedPlug(s.iface, s.plugKubeproxy, s.slot)
	c.Assert(err, IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)

	// kubelet should have only its rules
	spec = &seccomp.Specification{}
	err = spec.AddConnectedPlug(s.iface, s.plugKubelet, s.slot)
	c.Assert(err, IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.kubernetes-support.kubelet"})
	c.Check(spec.SnippetForTag("snap.kubernetes-support.kubelet"), testutil.Contains, "# Allow running as the kubelet service\n")
}

func (s *KubernetesSupportInterfaceSuite) TestUDevConnectedPlug(c *C) {
	// default should have kubelet rules
	spec := &udev.Specification{}
	err := spec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(spec.Snippets(), HasLen, 2)
	c.Assert(spec.Snippets(), testutil.Contains, `# kubernetes-support
KERNEL=="kmsg", TAG+="snap_kubernetes-support_default"`)
	c.Assert(spec.Snippets(), testutil.Contains, `TAG=="snap_kubernetes-support_default", RUN+="/usr/lib/snapd/snap-device-helper $env{ACTION} snap_kubernetes-support_default $devpath $major:$minor"`)

	// kubeproxy should not have any rules
	spec = &udev.Specification{}
	err = spec.AddConnectedPlug(s.iface, s.plugKubeproxy, s.slot)
	c.Assert(err, IsNil)
	c.Assert(spec.Snippets(), HasLen, 0)

	// kubelet should have only its rules
	spec = &udev.Specification{}
	err = spec.AddConnectedPlug(s.iface, s.plugKubelet, s.slot)
	c.Assert(err, IsNil)
	c.Assert(spec.Snippets(), HasLen, 2)
	c.Assert(spec.Snippets(), testutil.Contains, `# kubernetes-support
KERNEL=="kmsg", TAG+="snap_kubernetes-support_kubelet"`)
	c.Assert(spec.Snippets(), testutil.Contains, `TAG=="snap_kubernetes-support_kubelet", RUN+="/usr/lib/snapd/snap-device-helper $env{ACTION} snap_kubernetes-support_kubelet $devpath $major:$minor"`)
}

func (s *KubernetesSupportInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
