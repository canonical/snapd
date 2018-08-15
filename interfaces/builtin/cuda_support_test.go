// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

type CUDAInterfaceSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

const cudaMockPlugSnapInfoYaml = `name: cuda
version: 1.0
apps:
 app:
  command: foo
  plugs: [cuda-support]
`

var _ = Suite(&CUDAInterfaceSuite{
	iface: builtin.MustInterface("cuda-support"),
})

func (s *CUDAInterfaceSuite) SetUpTest(c *C) {
	s.slotInfo = &snap.SlotInfo{
		Snap: &snap.Info{
			SuggestedName: "cuda-support",
		},
		Interface: "cuda-support",
	}
	s.slot = interfaces.NewConnectedSlot(s.slotInfo, nil)
	plugSnap := snaptest.MockInfo(c, cudaMockPlugSnapInfoYaml, nil)
	s.plugInfo = plugSnap.Plugs["cuda-support"]
	s.plug = interfaces.NewConnectedPlug(s.plugInfo, nil)
}

func (s *CUDAInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "cuda-support")
}

func (s *CUDAInterfaceSuite) TestConnectedPlugSnippet(c *C) {
	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.cuda.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.cuda.app"), testutil.Contains, `/dev/nvidia-uvm`)
	c.Assert(apparmorSpec.SnippetForTag("snap.cuda.app"), testutil.Contains, `/dev/nvidiactl`)
	c.Assert(apparmorSpec.SnippetForTag("snap.cuda.app"), testutil.Contains, `/{dev,run}/shm/cuda.*`)

	seccompSpec := &seccomp.Specification{}
	err = seccompSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(seccompSpec.SecurityTags(), DeepEquals, []string{"snap.cuda.app"})
	c.Check(seccompSpec.SnippetForTag("snap.cuda.app"), testutil.Contains, "mknod\n")
}

func (s *CUDAInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *CUDAInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *CUDAInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
