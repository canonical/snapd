// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017-2019 Canonical Ltd
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
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/kmod"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type kvmInterfaceSuite struct {
	testutil.BaseTest

	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug

	tmpdir string
}

var _ = Suite(&kvmInterfaceSuite{
	iface: builtin.MustInterface("kvm"),
})

const kvmConsumerYaml = `name: consumer
version: 0
apps:
 app:
  plugs: [kvm]
`

const kvmCoreYaml = `name: core
version: 0
type: os
slots:
  kvm:
`

func (s *kvmInterfaceSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.plug, s.plugInfo = MockConnectedPlug(c, kvmConsumerYaml, nil, "kvm")
	s.slot, s.slotInfo = MockConnectedSlot(c, kvmCoreYaml, nil, "kvm")

	// Need to Mock output of /proc/cpuinfo
	s.tmpdir = c.MkDir()
	dirs.SetRootDir(s.tmpdir)
	s.AddCleanup(func() { dirs.SetRootDir("/") })

	mockCpuinfo := filepath.Join(s.tmpdir, "cpuinfo")
	c.Assert(os.WriteFile(mockCpuinfo, []byte(`
processor       : 0
flags		: cpuflags without kvm support

processor	: 42
flags		: another cpu also without kvm support
`[1:]), 0644), IsNil)
	s.AddCleanup(builtin.MockProcCpuinfo(mockCpuinfo))
}

func (s *kvmInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "kvm")
}

func (s *kvmInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *kvmInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *kvmInterfaceSuite) TestAppArmorSpec(c *C) {
	appSet := mylog.Check2(interfaces.NewSnapAppSet(s.plug.Snap(), nil))

	spec := apparmor.NewSpecification(appSet)
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), Equals, `
# Description: Allow write access to kvm.
# See 'man kvm' for details.

/dev/kvm rw,

# Allow nested virtualization checks for different CPU models and architectures (where it is supported).
/sys/module/kvm_intel/parameters/nested r,
/sys/module/kvm_amd/parameters/nested r,
/sys/module/kvm_hv/parameters/nested r, # PPC64.
/sys/module/kvm/parameters/nested r, # S390.

# Allow AMD SEV checks for AMD CPU's.
/sys/module/kvm_amd/parameters/sev r,
`)
}

func (s *kvmInterfaceSuite) TestUDevSpec(c *C) {
	appSet := mylog.Check2(interfaces.NewSnapAppSet(s.plug.Snap(), nil))

	spec := udev.NewSpecification(appSet)
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.Snippets(), HasLen, 2)
	c.Assert(spec.Snippets()[0], Equals, `# kvm
KERNEL=="kvm", TAG+="snap_consumer_app"`)
	c.Assert(spec.Snippets(), testutil.Contains, fmt.Sprintf(`TAG=="snap_consumer_app", SUBSYSTEM!="module", SUBSYSTEM!="subsystem", RUN+="%s/snap-device-helper $env{ACTION} snap_consumer_app $devpath $major:$minor"`, dirs.DistroLibExecDir))
}

func (s *kvmInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, true)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows access to the kvm device`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "kvm")
}

func (s *kvmInterfaceSuite) TestAutoConnect(c *C) {
	c.Assert(s.iface.AutoConnect(s.plugInfo, s.slotInfo), Equals, true)
}

func (s *kvmInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}

func (s *kvmInterfaceSuite) TestKModSpecWithUnknownCpu(c *C) {
	spec := &kmod.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.Modules(), DeepEquals, map[string]bool{
		"kvm": true,
	})
}

func (s *kvmInterfaceSuite) TestKModSpecWithIntel(c *C) {
	mockCpuinfo := filepath.Join(s.tmpdir, "cpuinfo")
	c.Assert(os.WriteFile(mockCpuinfo, []byte(`
processor       : 0
flags           : stuff vmx other
`[1:]), 0644), IsNil)

	spec := &kmod.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.Modules(), DeepEquals, map[string]bool{
		"kvm_intel": true,
	})
}

func (s *kvmInterfaceSuite) TestKModSpecWithAMD(c *C) {
	mockCpuinfo := filepath.Join(s.tmpdir, "cpuinfo")
	c.Assert(os.WriteFile(mockCpuinfo, []byte(`
processor       : 0
flags           : stuff svm other
`[1:]), 0644), IsNil)

	s.AddCleanup(builtin.MockProcCpuinfo(mockCpuinfo))

	spec := &kmod.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.Modules(), DeepEquals, map[string]bool{
		"kvm_amd": true,
	})
}

func (s *kvmInterfaceSuite) TestKModSpecWithEmptyCpuinfo(c *C) {
	mockCpuinfo := filepath.Join(s.tmpdir, "cpuinfo")
	c.Assert(os.WriteFile(mockCpuinfo, []byte(`
`[1:]), 0644), IsNil)

	s.AddCleanup(builtin.MockProcCpuinfo(mockCpuinfo))

	spec := &kmod.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.Modules(), DeepEquals, map[string]bool{
		"kvm": true,
	})
}

func (s *kvmInterfaceSuite) TestKModSpecWithMissingCpuinfo(c *C) {
	mockCpuinfo := filepath.Join(s.tmpdir, "non-existent-cpuinfo")

	s.AddCleanup(builtin.MockProcCpuinfo(mockCpuinfo))

	spec := &kmod.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.Modules(), DeepEquals, map[string]bool{
		"kvm": true,
	})
}
