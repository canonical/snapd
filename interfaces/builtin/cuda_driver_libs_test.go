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
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/configfiles"
	"github.com/snapcore/snapd/interfaces/ldconfig"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
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
    compatibility: cuda-(9..12)-ubuntu-2404
    interface: cuda-driver-libs
apps:
  app:
    plugs: [cuda]
`

const cudaDriverLibsProvider = `name: cuda-provider
version: 0
slots:
  cuda-slot:
    interface: cuda-driver-libs
    compatibility: cuda-(9..12)-ubuntu-2404
    library-source:
      - $SNAP/lib1
      - ${SNAP}/lib2
      - $SNAP_COMPONENT(comp1)/lib1
      - $SNAP_COMPONENT(comp2)/lib2
      - $SNAP_COMPONENT(comp2)/
components:
  comp1:
    type: standard
  comp2:
    type: standard
`

func (s *CudaDriverLibsInterfaceSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.plug, s.plugInfo = MockConnectedPlug(c, cudaDriverLibsConsumerYaml,
		&snap.SideInfo{Revision: snap.R(3)}, "cuda")
	comps := []compRawInfo{
		{"component: cuda-provider+comp1\ntype: standard", snap.R(11)},
		{"component: cuda-provider+comp2\ntype: standard", snap.R(22)}}
	s.slot, s.slotInfo = mockConnectedSlotWithComps(c, cudaDriverLibsProvider,
		&snap.SideInfo{Revision: snap.R(5)}, comps, "cuda-slot")
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
    compatibility: cuda-(9..12)-ubuntu-2404
    library-source:
      - /snap/cuda-provider/current/lib1
`, nil, "cuda")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		`cuda-driver-libs library-source directory .* must start with \$SNAP/ or \$\{SNAP\}/`)

	slot = MockSlot(c, `name: cuda-provider
version: 0
slots:
  cuda:
    interface: cuda-driver-libs
    compatibility: cuda-(9..12)-ubuntu-2404
`, nil, "cuda")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		`snap "cuda-provider" does not have attribute "library-source" for interface "cuda-driver-libs"`)

	slot = MockSlot(c, `name: cuda-provider
version: 0
slots:
  cuda:
    interface: cuda-driver-libs
    compatibility: cuda-(9..12)-ubuntu-2404
    library-source: $SNAP/lib1
`, nil, "cuda")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		`snap "cuda-provider" has interface "cuda-driver-libs" with invalid value type string for "library-source" attribute: \*\[\]string`)

	slot = MockSlot(c, `name: cuda-provider
version: 0
slots:
  cuda:
    interface: cuda-driver-libs
    library-source:
      - $SNAP/lib1
`, nil, "cuda")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		`snap "cuda-provider" does not have attribute "compatibility" for interface "cuda-driver-libs"`)

	slot = MockSlot(c, `name: cuda-provider
version: 0
slots:
  cuda:
    compatibility: cuda-(9..12)-ubuntu-2404
    interface: cuda-driver-libs
    library-source:
      - $SNAP/../out
`, nil, "cuda")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		`cuda-driver-libs library-source directory "\$SNAP/../out" cannot point outside of the snap/component`)

	slot = MockSlot(c, `name: cuda-provider
version: 0
slots:
  cuda:
    compatibility: cuda-(9..12)-ubuntu-2404
    interface: cuda-driver-libs
    library-source:
      - ${SNAP}/../out
`, nil, "cuda")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		`cuda-driver-libs library-source directory "\${SNAP}/../out" cannot point outside of the snap/component`)

	slot = MockSlot(c, `name: cuda-provider
version: 0
slots:
  cuda:
    interface: cuda-driver-libs
    compatibility: cuda-(9..12)-ubuntu-2404
    library-source:
      - $SNAP_COMPONENT(comp1)/lib1
`, nil, "cuda")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		`component comp1 specified in path "\$SNAP_COMPONENT\(comp1\)/lib1" is not defined in the snap`)

	slot = MockSlot(c, `name: cuda-provider
version: 0
slots:
  cuda:
    interface: cuda-driver-libs
    compatibility: cuda-(9..12)-ubuntu-2404
    library-source:
      - $SNAP_COMPONENT(comp1/lib1
components:
  comp1:
    type: standard
`, nil, "cuda")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		`invalid format in path "\$SNAP_COMPONENT\(comp1/lib1\"`)

	slot = MockSlot(c, `name: cuda-provider
version: 0
slots:
  cuda:
    interface: cuda-driver-libs
    compatibility: cuda-(9..12)-ubuntu-2404
    library-source:
      - $SNAP_COMPONENT(comp1)
components:
  comp1:
    type: standard
`, nil, "cuda")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		`invalid format in path "\$SNAP_COMPONENT\(comp1\)"`)

	slot = MockSlot(c, `name: cuda-provider
version: 0
slots:
  cuda:
    interface: cuda-driver-libs
    compatibility: cuda-(9..12)-ubuntu-2404
    library-source:
      - $SNAP_COMPONENT(comp1)foo
components:
  comp1:
    type: standard
`, nil, "cuda")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		`invalid format in path "\$SNAP_COMPONENT\(comp1\)foo"`)

	slot = MockSlot(c, `name: cuda-provider
version: 0
slots:
  cuda:
    interface: cuda-driver-libs
    compatibility: cuda-(9..12)-ubuntu-2404
    library-source:
      - $SNAP_COMPONENT(comp1)/../out
components:
  comp1:
    type: standard
`, nil, "cuda")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		`cuda-driver-libs library-source directory "\$SNAP_COMPONENT\(comp1\)/../out" cannot point outside of the snap/component`)
}

func (s *CudaDriverLibsInterfaceSuite) TestSanitizeSlotAPIversion(c *C) {
	for _, tt := range []struct {
		versRange string
		err       string
	}{
		{"cuda-9-ubuntu-2404", ""},
		{"cuda-(9..12)-ubuntu-2510", ""},
		{"cuda-9", `compatibility label "cuda-9": unexpected number of strings \(should be 2\)`},
		{"other-9", `compatibility label "other-9": unexpected number of strings \(should be 2\)`},
		{"cuda-10-2-ubuntu-2510", `compatibility label "cuda-10-2-ubuntu-2510": unexpected number of integers \(should be 1 for "cuda"\)`},
		{"cuda 5", `compatibility label "cuda 5": while parsing: unexpected rune: 5`},
	} {
		slot := MockSlot(c, fmt.Sprintf(`name: cuda-provider
version: 0
slots:
  cuda:
    interface: cuda-driver-libs
    compatibility: '%s'
    library-source:
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
		{SnapName: "cuda-provider", SlotName: "cuda-slot"}: {
			filepath.Join(dirs.SnapMountDir, "cuda-provider/5/lib1"),
			filepath.Join(dirs.SnapMountDir, "cuda-provider/5/lib2"),
			filepath.Join(snap.ComponentMountDir("comp1", snap.R(11), "cuda-provider"), "lib1"),
			filepath.Join(snap.ComponentMountDir("comp2", snap.R(22), "cuda-provider"), "lib2"),
			snap.ComponentMountDir("comp2", snap.R(22), "cuda-provider"),
		}})
}

func (s *CudaDriverLibsInterfaceSuite) TestConfigfilesSpec(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	spec := &configfiles.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Check(spec.PathContent(), DeepEquals, map[string]osutil.FileState{
		"/var/lib/snapd/export/system_cuda-provider_cuda-slot_cuda-driver-libs.library-source": &osutil.MemoryFileState{
			Content: []byte(
				filepath.Join(dirs.SnapMountDir, "cuda-provider/5/lib1") + "\n" +
					filepath.Join(dirs.SnapMountDir, "cuda-provider/5/lib2") + "\n" +
					filepath.Join(snap.ComponentMountDir("comp1", snap.R(11), "cuda-provider"), "lib1") + "\n" +
					filepath.Join(snap.ComponentMountDir("comp2", snap.R(22), "cuda-provider"), "lib2") + "\n" +
					snap.ComponentMountDir("comp2", snap.R(22), "cuda-provider") + "\n"),
			Mode: 0644},
	})
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
