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
	"fmt"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
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
	iface                interfaces.Interface
	slotInfo             *snap.SlotInfo
	slot                 *interfaces.ConnectedSlot
	plugInfo             *snap.PlugInfo
	plug                 *interfaces.ConnectedPlug
	plugKubeletInfo      *snap.PlugInfo
	plugKubelet          *interfaces.ConnectedPlug
	plugKubeproxyInfo    *snap.PlugInfo
	plugKubeproxy        *interfaces.ConnectedPlug
	plugKubeAutobindInfo *snap.PlugInfo
	plugKubeAutobind     *interfaces.ConnectedPlug
	plugBadInfo          *snap.PlugInfo
	plugBad              *interfaces.ConnectedPlug
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
  k8s-autobind-unix:
    interface: kubernetes-support
    flavor: autobind-unix
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
 kube-autobind-unix:
  plugs: [k8s-autobind-unix]
`

var _ = Suite(&KubernetesSupportInterfaceSuite{
	iface: builtin.MustInterface("kubernetes-support"),
})

func (s *KubernetesSupportInterfaceSuite) SetUpTest(c *C) {
	s.slotInfo = &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "core", SnapType: snap.TypeOS},
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

	s.plugKubeAutobindInfo = plugSnap.Plugs["k8s-autobind-unix"]
	s.plugKubeAutobind = interfaces.NewConnectedPlug(s.plugKubeAutobindInfo, nil, nil)

	s.plugBadInfo = plugSnap.Plugs["k8s-bad"]
	s.plugBad = interfaces.NewConnectedPlug(s.plugBadInfo, nil, nil)
}

func (s *KubernetesSupportInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "kubernetes-support")
}

func (s *KubernetesSupportInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *KubernetesSupportInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugKubeletInfo), IsNil)
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugKubeproxyInfo), IsNil)
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugBadInfo), ErrorMatches, `kubernetes-support plug requires "flavor" to be either "kubelet", "kubeproxy" or "autobind-unix"`)
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
	// default should have kubeproxy, kubelet and autobind rules
	spec := &apparmor.Specification{}
	err := spec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.kubernetes-support.default"})
	c.Check(spec.SnippetForTag("snap.kubernetes-support.default"), testutil.Contains, "# Common rules for running as a kubernetes node\n")
	c.Check(spec.SnippetForTag("snap.kubernetes-support.default"), testutil.Contains, "# Allow running as the kubelet service\n")
	c.Check(spec.SnippetForTag("snap.kubernetes-support.default"), testutil.Contains, "# Allow running as the kubeproxy service\n")
	c.Check(spec.SnippetForTag("snap.kubernetes-support.default"), testutil.Contains, "# Common rules for kubernetes use of systemd_run\n")
	c.Check(spec.SnippetForTag("snap.kubernetes-support.default"), testutil.Contains, "# kubelet mount rules\n")
	c.Check(spec.SnippetForTag("snap.kubernetes-support.default"), testutil.Contains, "# Allow using the 'autobind' feature of bind() (eg, for journald via go-systemd)\n")
	c.Check(spec.UsesPtraceTrace(), Equals, true)

	// kubeproxy should have its rules and autobind rules
	spec = &apparmor.Specification{}
	err = spec.AddConnectedPlug(s.iface, s.plugKubeproxy, s.slot)
	c.Assert(err, IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.kubernetes-support.kubeproxy"})
	c.Check(spec.SnippetForTag("snap.kubernetes-support.kubeproxy"), testutil.Contains, "# Common rules for running as a kubernetes node\n")
	c.Check(spec.SnippetForTag("snap.kubernetes-support.kubeproxy"), testutil.Contains, "# Allow running as the kubeproxy service\n")
	c.Check(spec.SnippetForTag("snap.kubernetes-support.kubeproxy"), Not(testutil.Contains), "# Allow running as the kubelet service\n")
	c.Check(spec.SnippetForTag("snap.kubernetes-support.kubeproxy"), testutil.Contains, "# Common rules for kubernetes use of systemd_run\n")
	c.Check(spec.SnippetForTag("snap.kubernetes-support.kubeproxy"), Not(testutil.Contains), "# kubelet mount rules\n")
	c.Check(spec.SnippetForTag("snap.kubernetes-support.kubeproxy"), testutil.Contains, "# Allow using the 'autobind' feature of bind() (eg, for journald via go-systemd)\n")
	c.Check(spec.UsesPtraceTrace(), Equals, false)

	// kubelet should have its rules and autobind rules
	spec = &apparmor.Specification{}
	err = spec.AddConnectedPlug(s.iface, s.plugKubelet, s.slot)
	c.Assert(err, IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.kubernetes-support.kubelet"})
	c.Check(spec.SnippetForTag("snap.kubernetes-support.kubelet"), testutil.Contains, "# Common rules for running as a kubernetes node\n")
	c.Check(spec.SnippetForTag("snap.kubernetes-support.kubelet"), testutil.Contains, "# Allow running as the kubelet service\n")
	c.Check(spec.SnippetForTag("snap.kubernetes-support.kubelet"), Not(testutil.Contains), "# Allow running as the kubeproxy service\n")
	c.Check(spec.SnippetForTag("snap.kubernetes-support.kubelet"), testutil.Contains, "# Common rules for kubernetes use of systemd_run\n")
	c.Check(spec.SnippetForTag("snap.kubernetes-support.kubelet"), testutil.Contains, "# kubelet mount rules\n")
	c.Check(spec.SnippetForTag("snap.kubernetes-support.kubelet"), testutil.Contains, "# Allow using the 'autobind' feature of bind() (eg, for journald via go-systemd)\n")
	c.Check(spec.UsesPtraceTrace(), Equals, true)

	// kube-autobind-unix should have only its autobind rules
	spec = &apparmor.Specification{}
	err = spec.AddConnectedPlug(s.iface, s.plugKubeAutobind, s.slot)
	c.Assert(err, IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.kubernetes-support.kube-autobind-unix"})
	c.Check(spec.SnippetForTag("snap.kubernetes-support.kube-autobind-unix"), Not(testutil.Contains), "# Common rules for running as a kubernetes node\n")
	c.Check(spec.SnippetForTag("snap.kubernetes-support.kube-autobind-unix"), Not(testutil.Contains), "# Allow running as the kubelet service\n")
	c.Check(spec.SnippetForTag("snap.kubernetes-support.kube-autobind-unix"), Not(testutil.Contains), "# Allow running as the kubeproxy service\n")
	c.Check(spec.SnippetForTag("snap.kubernetes-support.kube-autobind-unix"), Not(testutil.Contains), "# Common rules for kubernetes use of systemd_run\n")
	c.Check(spec.SnippetForTag("snap.kubernetes-support.kube-autobind-unix"), Not(testutil.Contains), "# kubelet mount rules\n")
	c.Check(spec.SnippetForTag("snap.kubernetes-support.kube-autobind-unix"), testutil.Contains, "# Allow using the 'autobind' feature of bind() (eg, for journald via go-systemd)\n")
	c.Check(spec.UsesPtraceTrace(), Equals, false)
}

func (s *KubernetesSupportInterfaceSuite) TestSecCompConnectedPlug(c *C) {
	// default should have kubelet rules
	spec := &seccomp.Specification{}
	err := spec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.kubernetes-support.default"})
	c.Check(spec.SnippetForTag("snap.kubernetes-support.default"), testutil.Contains, "# Allow running as the kubelet service\n")
	c.Check(spec.SnippetForTag("snap.kubernetes-support.default"), testutil.Contains, "# Allow using the 'autobind' feature of bind() (eg, for journald).\n")

	// kubeproxy should have the autobind rules
	spec = &seccomp.Specification{}
	err = spec.AddConnectedPlug(s.iface, s.plugKubeproxy, s.slot)
	c.Assert(err, IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.kubernetes-support.kubeproxy"})
	c.Check(spec.SnippetForTag("snap.kubernetes-support.kubeproxy"), Not(testutil.Contains), "# Allow running as the kubelet service\n")
	c.Check(spec.SnippetForTag("snap.kubernetes-support.kubeproxy"), testutil.Contains, "# Allow using the 'autobind' feature of bind() (eg, for journald).\n")

	// kubelet should have its rules and the autobind rules
	spec = &seccomp.Specification{}
	err = spec.AddConnectedPlug(s.iface, s.plugKubelet, s.slot)
	c.Assert(err, IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.kubernetes-support.kubelet"})
	c.Check(spec.SnippetForTag("snap.kubernetes-support.kubelet"), testutil.Contains, "# Allow running as the kubelet service\n")
	c.Check(spec.SnippetForTag("snap.kubernetes-support.kubelet"), testutil.Contains, "# Allow using the 'autobind' feature of bind() (eg, for journald).\n")

	// kube-autobind-unix should have the autobind rules
	spec = &seccomp.Specification{}
	err = spec.AddConnectedPlug(s.iface, s.plugKubeAutobind, s.slot)
	c.Assert(err, IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.kubernetes-support.kube-autobind-unix"})
	c.Check(spec.SnippetForTag("snap.kubernetes-support.kube-autobind-unix"), Not(testutil.Contains), "# Allow running as the kubelet service\n")
	c.Check(spec.SnippetForTag("snap.kubernetes-support.kube-autobind-unix"), testutil.Contains, "# Allow using the 'autobind' feature of bind() (eg, for journald).\n")
}

func (s *KubernetesSupportInterfaceSuite) TestUDevConnectedPlug(c *C) {
	// default should have kubelet rules
	spec := &udev.Specification{}
	err := spec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(spec.Snippets(), HasLen, 2)
	c.Assert(spec.Snippets(), testutil.Contains, `# kubernetes-support
KERNEL=="kmsg", TAG+="snap_kubernetes-support_default"`)
	c.Assert(spec.Snippets(), testutil.Contains, fmt.Sprintf(`TAG=="snap_kubernetes-support_default", SUBSYSTEM!="module", SUBSYSTEM!="subsystem", RUN+="%v/snap-device-helper snap_kubernetes-support_default"`, dirs.DistroLibExecDir))

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
	c.Assert(spec.Snippets(), testutil.Contains, fmt.Sprintf(`TAG=="snap_kubernetes-support_kubelet", SUBSYSTEM!="module", SUBSYSTEM!="subsystem", RUN+="%v/snap-device-helper snap_kubernetes-support_kubelet"`, dirs.DistroLibExecDir))
}

func (s *KubernetesSupportInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}

func (s *KubernetesSupportInterfaceSuite) TestPermanentPlugServiceSnippets(c *C) {
	for _, t := range []struct {
		plug *snap.PlugInfo
		exp  []string
	}{
		{s.plugInfo, []string{"Delegate=true"}},
		{s.plugKubeletInfo, []string{"Delegate=true"}},
		{s.plugKubeproxyInfo, []string{"Delegate=true"}},
		// only autobind-unix flavor does not get Delegate=true
		{s.plugKubeAutobindInfo, nil},
	} {
		snips, err := interfaces.PermanentPlugServiceSnippets(s.iface, t.plug)
		c.Assert(err, IsNil)
		c.Check(snips, DeepEquals, t.exp)
	}
}
