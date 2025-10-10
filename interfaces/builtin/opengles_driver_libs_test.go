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
	"github.com/snapcore/snapd/interfaces/ldconfig"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type OpenglesDriverLibsInterfaceSuite struct {
	testutil.BaseTest

	testRoot string

	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

var _ = Suite(&OpenglesDriverLibsInterfaceSuite{
	iface: builtin.MustInterface("opengles-driver-libs"),
})

// This is in fact implicit in the system
const openglesDriverLibsConsumerYaml = `name: snapd
version: 0
plugs:
  opengles:
    interface: opengles-driver-libs
    compatibility: opengles-ubuntu-2510
apps:
  app:
    plugs: [opengles]
`

const openglesDriverLibsProvider = `name: opengles-provider
version: 0
slots:
  opengles-driver-libs:
    compatibility: opengles-2-0-ubuntu-2510
    library-source:
      - $SNAP/lib1
      - ${SNAP}/lib2
`

func (s *OpenglesDriverLibsInterfaceSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.testRoot = c.MkDir()
	dirs.SetRootDir(s.testRoot)
	s.AddCleanup(func() { dirs.SetRootDir("/") })

	s.plug, s.plugInfo = MockConnectedPlug(c, openglesDriverLibsConsumerYaml,
		&snap.SideInfo{Revision: snap.R(3)}, "opengles")
	s.slot, s.slotInfo = MockConnectedSlot(c, openglesDriverLibsProvider,
		&snap.SideInfo{Revision: snap.R(5)}, "opengles-driver-libs")
}

func (s *OpenglesDriverLibsInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "opengles-driver-libs")
}

func (s *OpenglesDriverLibsInterfaceSuite) TestSanitizeSlot(c *C) {
	libDir1 := filepath.Join(dirs.GlobalRootDir, "snap/opengles-provider/5/lib1")
	c.Assert(os.MkdirAll(libDir1, 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(libDir1, "libOPENGLES_nvidia.so.0"), []byte(``), 0644), IsNil)
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *OpenglesDriverLibsInterfaceSuite) TestSanitizeSlotError(c *C) {
	slot := MockSlot(c, `name: opengles-provider
version: 0
slots:
  opengles:
    interface: opengles-driver-libs
    compatibility: opengles-2-0-ubuntu-2510
    library-source:
      - /snap/opengles-provider/current/lib1
`, nil, "opengles")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		`opengles-driver-libs source directory .* must start with \$SNAP/ or \$\{SNAP\}/`)

	slot = MockSlot(c, `name: opengles-provider
version: 0
slots:
  opengles:
    interface: opengles-driver-libs
    compatibility: opengles-2-0-ubuntu-2510
`, nil, "opengles")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		`snap "opengles-provider" does not have attribute "library-source" for interface "opengles-driver-libs"`)

	slot = MockSlot(c, `name: opengles-provider
version: 0
slots:
  opengles:
    interface: opengles-driver-libs
    compatibility: opengles-2-0-ubuntu-2510
    library-source: $SNAP/lib1
`, nil, "opengles")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		`snap "opengles-provider" has interface "opengles-driver-libs" with invalid value type string for "library-source" attribute: \*\[\]string`)

	slot = MockSlot(c, `name: opengles-provider
version: 0
slots:
  opengles:
    interface: opengles-driver-libs
    library-source:
      - $SNAP/lib1
`, nil, "opengles")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		`snap "opengles-provider" does not have attribute "compatibility" for interface "opengles-driver-libs"`)

	slot = MockSlot(c, `name: opengles-provider
version: 0
slots:
  opengles:
    interface: opengles-driver-libs
    compatibility: opengles-2-0-other-2510
    library-source:
      - $SNAP/lib1
`, nil, "opengles")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		`compatibility label "opengles-2-0-other-2510": string does not match interface spec \(other != ubuntu\)`)
}

func (s *OpenglesDriverLibsInterfaceSuite) TestSanitizePlug(c *C) {
	c.Check(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
	c.Check(interfaces.BeforeConnectPlug(s.iface, s.plug), IsNil)
}

func (s *OpenglesDriverLibsInterfaceSuite) TestLdconfigSpec(c *C) {
	spec := &ldconfig.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Check(spec.LibDirs(), DeepEquals, map[ldconfig.SnapSlot][]string{
		{SnapName: "opengles-provider", SlotName: "opengles-driver-libs"}: {
			filepath.Join(dirs.GlobalRootDir, "snap/opengles-provider/5/lib1"),
			filepath.Join(dirs.GlobalRootDir, "snap/opengles-provider/5/lib2")}})
}

func (s *OpenglesDriverLibsInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, false)
	c.Assert(si.ImplicitOnClassic, Equals, false)
	c.Assert(si.ImplicitPlugOnCore, Equals, false)
	c.Assert(si.ImplicitPlugOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows exposing OpenGLES driver libraries to the system`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "opengles-driver-libs")
}

func (s *OpenglesDriverLibsInterfaceSuite) TestAutoConnect(c *C) {
	c.Assert(s.iface.AutoConnect(s.plugInfo, s.slotInfo), Equals, true)
}

func (s *OpenglesDriverLibsInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
