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
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/configfiles"
	"github.com/snapcore/snapd/interfaces/ldconfig"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type OpenglDriverLibsInterfaceSuite struct {
	testutil.BaseTest

	testRoot string

	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

var _ = Suite(&OpenglDriverLibsInterfaceSuite{
	iface: builtin.MustInterface("opengl-driver-libs"),
})

// This is in fact implicit in the system
const openglDriverLibsConsumerYaml = `name: snapd
version: 0
plugs:
  opengl:
    interface: opengl-driver-libs
apps:
  app:
    plugs: [opengl]
`

const openglDriverLibsProvider = `name: opengl-provider
version: 0
slots:
  opengl-slot:
    interface: opengl-driver-libs
    compatibility: opengl-4-6-ubuntu-2510
    library-source:
      - $SNAP/lib1
      - ${SNAP}/lib2
`

func (s *OpenglDriverLibsInterfaceSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.testRoot = c.MkDir()
	dirs.SetRootDir(s.testRoot)
	s.AddCleanup(func() { dirs.SetRootDir("/") })

	s.plug, s.plugInfo = MockConnectedPlug(c, openglDriverLibsConsumerYaml,
		&snap.SideInfo{Revision: snap.R(3)}, "opengl")
	s.slot, s.slotInfo = MockConnectedSlot(c, openglDriverLibsProvider,
		&snap.SideInfo{Revision: snap.R(5)}, "opengl-slot")
}

func (s *OpenglDriverLibsInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "opengl-driver-libs")
}

func (s *OpenglDriverLibsInterfaceSuite) TestSanitizeSlot(c *C) {
	libDir1 := filepath.Join(dirs.GlobalRootDir, "snap/opengl-provider/5/lib1")
	c.Assert(os.MkdirAll(libDir1, 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(libDir1, "libOPENGL_nvidia.so.0"), []byte(``), 0644), IsNil)
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *OpenglDriverLibsInterfaceSuite) TestSanitizeSlotError(c *C) {
	slot := MockSlot(c, `name: opengl-provider
version: 0
slots:
  opengl:
    compatibility: opengl-3-2-ubuntu-2510
    interface: opengl-driver-libs
    library-source:
      - /snap/opengl-provider/current/lib1
`, nil, "opengl")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		`opengl-driver-libs source directory .* must start with \$SNAP/ or \$\{SNAP\}/`)

	slot = MockSlot(c, `name: opengl-provider
version: 0
slots:
  opengl:
    interface: opengl-driver-libs
    compatibility: opengl-3-2-ubuntu-2510
`, nil, "opengl")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		`snap "opengl-provider" does not have attribute "library-source" for interface "opengl-driver-libs"`)

	slot = MockSlot(c, `name: opengl-provider
version: 0
slots:
  opengl:
    interface: opengl-driver-libs
    compatibility: opengl-3-2-ubuntu-2510
    library-source: $SNAP/lib1
`, nil, "opengl")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		`snap "opengl-provider" has interface "opengl-driver-libs" with invalid value type string for "library-source" attribute: \*\[\]string`)

	slot = MockSlot(c, `name: opengl-provider
version: 0
slots:
  opengl:
    interface: opengl-driver-libs
    library-source:
      - $SNAP/lib1
`, nil, "opengl")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		`snap "opengl-provider" does not have attribute "compatibility" for interface "opengl-driver-libs"`)

	slot = MockSlot(c, `name: opengl-provider
version: 0
slots:
  opengl:
    interface: opengl-driver-libs
    compatibility: opengl-3-2-other-2510
    library-source:
      - $SNAP/lib1
`, nil, "opengl")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		`compatibility label "opengl-3-2-other-2510": string does not match interface spec \(other != ubuntu\)`)
}

func (s *OpenglDriverLibsInterfaceSuite) TestSanitizePlug(c *C) {
	c.Check(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
	c.Check(interfaces.BeforeConnectPlug(s.iface, s.plug), IsNil)
}

func (s *OpenglDriverLibsInterfaceSuite) TestLdconfigSpec(c *C) {
	spec := &ldconfig.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Check(spec.LibDirs(), DeepEquals, map[ldconfig.SnapSlot][]string{
		{SnapName: "opengl-provider", SlotName: "opengl-slot"}: {
			filepath.Join(dirs.GlobalRootDir, "snap/opengl-provider/5/lib1"),
			filepath.Join(dirs.GlobalRootDir, "snap/opengl-provider/5/lib2")}})
}

func (s *OpenglDriverLibsInterfaceSuite) TestConfigfilesSpec(c *C) {
	spec := &configfiles.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Check(spec.PathContent(), DeepEquals, map[string]osutil.FileState{
		"/var/lib/snapd/export/opengl-provider_opengl-slot_opengl-driver-libs.source": &osutil.MemoryFileState{
			Content: []byte(
				filepath.Join(dirs.GlobalRootDir, "/snap/opengl-provider/5/lib1") + "\n" +
					filepath.Join(dirs.GlobalRootDir, "/snap/opengl-provider/5/lib2") + "\n"),
			Mode: 0644},
	})
}

func (s *OpenglDriverLibsInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, false)
	c.Assert(si.ImplicitOnClassic, Equals, false)
	c.Assert(si.ImplicitPlugOnCore, Equals, false)
	c.Assert(si.ImplicitPlugOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows exposing OpenGL driver libraries to the system`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "opengl-driver-libs")
}

func (s *OpenglDriverLibsInterfaceSuite) TestAutoConnect(c *C) {
	c.Assert(s.iface.AutoConnect(s.plugInfo, s.slotInfo), Equals, true)
}

func (s *OpenglDriverLibsInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
