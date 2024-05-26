// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/kmod"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type KernelModuleLoadInterfaceSuite struct {
	testutil.BaseTest

	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

var _ = Suite(&KernelModuleLoadInterfaceSuite{
	iface: builtin.MustInterface("kernel-module-load"),
})

const kernelModuleLoadConsumerYaml = `name: consumer
version: 0
plugs:
 kmod:
  interface: kernel-module-load
  modules:
  - name: forbidden
    load: denied
  - name: mymodule1
    load: on-boot
    options: p1=3 p2=true p3
  - name: mymodule2
    options: param_1=ok param_2=false
  - name: expandvar
    options: opt=$FOO path=$SNAP_COMMON/bar
  - name: dyn-module1
    load: dynamic
    options: opt1=v1 opt2=v2
  - name: dyn-module2
    load: dynamic
    options: "*"
apps:
 app:
  plugs: [kmod]
`

const kernelModuleLoadCoreYaml = `name: core
version: 0
type: os
slots:
  kernel-module-load:
`

func (s *KernelModuleLoadInterfaceSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.plug, s.plugInfo = MockConnectedPlug(c, kernelModuleLoadConsumerYaml, nil, "kmod")
	s.slot, s.slotInfo = MockConnectedSlot(c, kernelModuleLoadCoreYaml, nil, "kernel-module-load")
}

func (s *KernelModuleLoadInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "kernel-module-load")
}

func (s *KernelModuleLoadInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *KernelModuleLoadInterfaceSuite) TestSanitizePlug(c *C) {
	c.Check(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
	c.Check(interfaces.BeforeConnectPlug(s.iface, s.plug), IsNil)
}

func (s *KernelModuleLoadInterfaceSuite) TestSanitizePlugUnhappy(c *C) {
	kernelModuleLoadYaml := `name: consumer
version: 0
plugs:
 kmod:
  interface: kernel-module-load
  %s
apps:
 app:
  plugs: [kmod]
`
	data := []struct {
		plugYaml      string
		expectedError string
	}{
		{
			"", // missing "modules" attribute
			`kernel-module-load "modules" attribute must be a list of dictionaries`,
		},
		{
			"modules: a string",
			`kernel-module-load "modules" attribute must be a list of dictionaries`,
		},
		{
			"modules: [this, is, a, list]",
			`kernel-module-load "modules" attribute must be a list of dictionaries`,
		},
		{
			"modules:\n  - name: [this, is, a, list]",
			`kernel-module-load "name" must be a string`,
		},
		{
			"modules:\n  - name: w3/rd*",
			`kernel-module-load "name" attribute is not a valid module name`,
		},
		{
			"modules:\n  - name: pcspkr",
			`kernel-module-load: must specify at least "load" or "options"`,
		},
		{
			"modules:\n  - name: pcspkr\n    load: [yes, no]",
			`kernel-module-load "load" must be a string`,
		},
		{
			"modules:\n  - name: pcspkr\n    load: maybe",
			`kernel-module-load "load" value is unrecognized: "maybe"`,
		},
		{
			"modules:\n  - name: pcspkr\n    options: [one, two]",
			`kernel-module-load "options" must be a string`,
		},
		{
			"modules:\n  - name: pcspkr\n    options: \"a\\nnewline\"",
			`kernel-module-load "options" attribute contains invalid characters: "a\\nnewline"`,
		},
		{
			"modules:\n  - name: pcspkr\n    options: \"5tartWithNumber=1\"",
			`kernel-module-load "options" attribute contains invalid characters: "5tartWithNumber=1"`,
		},
		{
			"modules:\n  - name: pcspkr\n    options: \"no-dashes\"",
			`kernel-module-load "options" attribute contains invalid characters: "no-dashes"`,
		},
		{
			// "*" is only allowed for `load: dynamic`
			"modules:\n  - name: pcspkr\n    options: \"*\"",
			`kernel-module-load "options" attribute contains invalid characters: "\*"`,
		},
		{
			"modules:\n  - name: pcspkr\n    load: denied\n    options: p1=true",
			`kernel-module-load "options" attribute incompatible with "load: denied"`,
		},
	}

	for _, testData := range data {
		snapYaml := fmt.Sprintf(kernelModuleLoadYaml, testData.plugYaml)
		plug, _ := MockConnectedPlug(c, snapYaml, nil, "kmod")
		mylog.Check(interfaces.BeforeConnectPlug(s.iface, plug))
		c.Check(err, ErrorMatches, testData.expectedError, Commentf("yaml: %s", testData.plugYaml))
	}
}

func (s *KernelModuleLoadInterfaceSuite) TestKModSpec(c *C) {
	spec := &kmod.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Check(spec.Modules(), DeepEquals, map[string]bool{
		"mymodule1": true,
	})
	c.Check(spec.ModuleOptions(), DeepEquals, map[string]string{
		"mymodule1":   "p1=3 p2=true p3",
		"mymodule2":   "param_1=ok param_2=false",
		"expandvar":   "opt=$FOO path=/var/snap/consumer/common/bar",
		"dyn-module1": "opt1=v1 opt2=v2",
		// No entry for dyn-module2, which has options set to "*"
	})
	c.Check(spec.DisallowedModules(), DeepEquals, []string{"forbidden"})
}

func (s *KernelModuleLoadInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, true)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows constrained control over kernel module loading`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "kernel-module-load")
}

func (s *KernelModuleLoadInterfaceSuite) TestAutoConnect(c *C) {
	c.Assert(s.iface.AutoConnect(s.plugInfo, s.slotInfo), Equals, true)
}

func (s *KernelModuleLoadInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
