// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) Canonical Ltd
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

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/ldconfig"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type CudaDriverLibsInterfaceSuite struct {
	testutil.BaseTest

	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

var _ = Suite(&CudaDriverLibsInterfaceSuite{
	iface: builtin.MustInterface("cuda-driver-libs"),
})

// This is in fact implicit in the system
const cudaDriverLibsConsumerYaml = `name: snapd
version: 0
plugs:
  cuda:
    interface: cuda-driver-libs
apps:
  app:
    plugs: [cuda]
`

const cudaDriverLibsProvider = `name: cuda-provider
version: 0
slots:
  cuda-driver-libs:
    api-version: 9 .. 12.4
    source:
      - $SNAP/lib1
      - ${SNAP}/lib2
`

func (s *CudaDriverLibsInterfaceSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.plug, s.plugInfo = MockConnectedPlug(c, cudaDriverLibsConsumerYaml,
		&snap.SideInfo{Revision: snap.R(3)}, "cuda")
	s.slot, s.slotInfo = MockConnectedSlot(c, cudaDriverLibsProvider,
		&snap.SideInfo{Revision: snap.R(5)}, "cuda-driver-libs")
}

func (s *CudaDriverLibsInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "cuda-driver-libs")
}

func (s *CudaDriverLibsInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *CudaDriverLibsInterfaceSuite) TestSanitizeSlotError(c *C) {
	slot := MockSlot(c, `name: cuda-provider
version: 0
slots:
  cuda:
    interface: cuda-driver-libs
    source:
      - /snap/cuda-provider/current/lib1
`, nil, "cuda")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		`cuda-driver-libs source directory .* must start with \$SNAP/ or \$\{SNAP\}/`)

	slot = MockSlot(c, `name: cuda-provider
version: 0
slots:
  cuda:
    interface: cuda-driver-libs
`, nil, "cuda")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		`snap "cuda-provider" does not have attribute "source" for interface "cuda-driver-libs"`)

	slot = MockSlot(c, `name: cuda-provider
version: 0
slots:
  cuda:
    interface: cuda-driver-libs
    source: $SNAP/lib1
`, nil, "cuda")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		`snap "cuda-provider" has interface "cuda-driver-libs" with invalid value type string for "source" attribute: \*\[\]string`)
}

func (s *CudaDriverLibsInterfaceSuite) TestSanitizeSlotAPIversion(c *C) {
	for _, tt := range []struct {
		versRange string
		err       string
	}{
		{"9.2.3", ""},
		{"9.2.3 .. 12", ""},
		{"9.2 .. 9.2.1", ""},
		{"9.2ubuntu", `invalid CUDA version: "9.2ubuntu"`},
		{".3", `invalid CUDA version: ".3"`},
		{"3.", `invalid CUDA version: "3."`},
		{"1.. 4.1", `wrong format for api-version: "1.. 4.1"`},
		{"1 ..4.1", `wrong format for api-version: "1 ..4.1"`},
		{"1 ... 4.1", `invalid separator in api-version: "..."`},
		{"4.1.1 .. 4.1", `"4.1.1" should not be bigger than "4.1"`},
	} {
		slot := MockSlot(c, fmt.Sprintf(`name: cuda-provider
version: 0
slots:
  cuda:
    interface: cuda-driver-libs
    api-version: '%s'
    source:
      - $SNAP/lib1
`, tt.versRange), nil, "cuda")
		err := interfaces.BeforePrepareSlot(s.iface, slot)
		if tt.err == "" {
			c.Check(err, IsNil)
		} else {
			c.Check(err, ErrorMatches, tt.err, Commentf("case %q", tt.versRange))
		}
	}
}

func (s *CudaDriverLibsInterfaceSuite) TestSanitizePlug(c *C) {
	c.Check(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
	c.Check(interfaces.BeforeConnectPlug(s.iface, s.plug), IsNil)
}

func (s *CudaDriverLibsInterfaceSuite) TestLdconfigSpec(c *C) {
	spec := &ldconfig.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Check(spec.LibDirs(), DeepEquals, map[ldconfig.SnapSlot][]string{
		{SnapName: "cuda-provider", SlotName: "cuda-driver-libs"}: {"/snap/cuda-provider/5/lib1",
			"/snap/cuda-provider/5/lib2"}})
}

func (s *CudaDriverLibsInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, false)
	c.Assert(si.ImplicitOnClassic, Equals, false)
	c.Assert(si.ImplicitPlugOnCore, Equals, false)
	c.Assert(si.ImplicitPlugOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows exposing CUDA driver libraries to the system`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "cuda-driver-libs")
}

func (s *CudaDriverLibsInterfaceSuite) TestAutoConnect(c *C) {
	c.Assert(s.iface.AutoConnect(s.plugInfo, s.slotInfo), Equals, true)
}

func (s *CudaDriverLibsInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
