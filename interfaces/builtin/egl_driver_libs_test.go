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

type EglDriverLibsInterfaceSuite struct {
	testutil.BaseTest

	testRoot string

	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

var _ = Suite(&EglDriverLibsInterfaceSuite{
	iface: builtin.MustInterface("egl-driver-libs"),
})

// This is in fact implicit in the system
const eglDriverLibsConsumerYaml = `name: snapd
version: 0
plugs:
  egl:
    interface: egl-driver-libs
apps:
  app:
    plugs: [egl]
`

const eglDriverLibsProvider = `name: egl-provider
version: 0
slots:
  egl-driver-libs:
    priority: 10
    compatibility: egl-1-5-ubuntu-2404
    client-driver: libEGL_nvidia.so.0
    source:
      - $SNAP/lib1
      - ${SNAP}/lib2
`

func (s *EglDriverLibsInterfaceSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.testRoot = c.MkDir()
	dirs.SetRootDir(s.testRoot)
	s.AddCleanup(func() { dirs.SetRootDir("/") })

	s.plug, s.plugInfo = MockConnectedPlug(c, eglDriverLibsConsumerYaml,
		&snap.SideInfo{Revision: snap.R(3)}, "egl")
	s.slot, s.slotInfo = MockConnectedSlot(c, eglDriverLibsProvider,
		&snap.SideInfo{Revision: snap.R(5)}, "egl-driver-libs")
}

func (s *EglDriverLibsInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "egl-driver-libs")
}

func (s *EglDriverLibsInterfaceSuite) TestSanitizeSlot(c *C) {
	libDir1 := filepath.Join(dirs.GlobalRootDir, "snap/egl-provider/5/lib1")
	c.Assert(os.MkdirAll(libDir1, 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(libDir1, "libEGL_nvidia.so.0"), []byte(``), 0644), IsNil)
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *EglDriverLibsInterfaceSuite) TestSanitizeSlotError(c *C) {
	slot := MockSlot(c, `name: egl-provider
version: 0
slots:
  egl:
    interface: egl-driver-libs
    priority: 10
    compatibility: egl-1-5-ubuntu-2404
    client-driver: libEGL_nvidia.so.0
    source:
      - /snap/egl-provider/current/lib1
`, nil, "egl")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		`egl-driver-libs source directory .* must start with \$SNAP/ or \$\{SNAP\}/`)

	slot = MockSlot(c, `name: egl-provider
version: 0
slots:
  egl:
    priority: 10
    compatibility: egl-1-5-ubuntu-2404
    client-driver: libEGL_nvidia.so.0
    interface: egl-driver-libs
`, nil, "egl")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		`snap "egl-provider" does not have attribute "source" for interface "egl-driver-libs"`)

	slot = MockSlot(c, `name: egl-provider
version: 0
slots:
  egl:
    interface: egl-driver-libs
    priority: 10
    compatibility: egl-1-5-ubuntu-2404
    client-driver: libEGL_nvidia.so.0
    source: $SNAP/lib1
`, nil, "egl")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		`snap "egl-provider" has interface "egl-driver-libs" with invalid value type string for "source" attribute: \*\[\]string`)

	slot = MockSlot(c, `name: egl-provider
version: 0
slots:
  egl:
    interface: egl-driver-libs
    compatibility: egl-ubuntu-2404
    client-driver: libEGL_nvidia.so.0
`, nil, "egl")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		`invalid priority: snap "egl-provider" does not have attribute "priority" for interface "egl-driver-libs"`)

	slot = MockSlot(c, `name: egl-provider
version: 0
slots:
  egl:
    interface: egl-driver-libs
    priority: 10
    compatibility: egl-1-5-ubuntu-2404
`, nil, "egl")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		`invalid client-driver: snap "egl-provider" does not have attribute "client-driver" for interface "egl-driver-libs"`)

	slot = MockSlot(c, `name: egl-provider
version: 0
slots:
  egl:
    interface: egl-driver-libs
    priority: 0
    compatibility: egl-1-5-ubuntu-2404
    client-driver: libEGL_nvidia.so.0
`, nil, "egl")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		`priority must be a positive integer`)

	slot = MockSlot(c, `name: egl-provider
version: 0
slots:
  egl:
    interface: egl-driver-libs
    priority: 15
    compatibility: egl-1-5-ubuntu-2404
    client-driver: /abs/path/libEGL_nvidia.so.0
`, nil, "egl")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		`client-driver value "/abs/path/libEGL_nvidia.so.0" should be a file`)

	slot = MockSlot(c, `name: egl-provider
version: 0
slots:
  egl:
    interface: egl-driver-libs
    priority: 15
    compatibility: egl-ubuntu-2404
    client-driver:
      - libEGL_nvidia.so.0
`, nil, "egl")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		`invalid client-driver: snap "egl-provider" has interface "egl-driver-libs" with invalid value type \[\]interface {} for "client-driver" attribute: \*string`)

	slot = MockSlot(c, `name: egl-provider
version: 0
slots:
  egl:
    interface: egl-driver-libs
    priority: 15
    compatibility: ubuntu
    client-driver: libEGL_nvidia.so.0
`, nil, "egl")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		`compatibility label "ubuntu": unexpected number of strings \(should be 2\)`)

	slot = MockSlot(c, `name: egl-provider
version: 0
slots:
  egl:
    interface: egl-driver-libs
    priority: 15
    client-driver: libEGL_nvidia.so.0
`, nil, "egl")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		`snap "egl-provider" does not have attribute "compatibility" for interface "egl-driver-libs"`)
}

func (s *EglDriverLibsInterfaceSuite) TestSanitizePlug(c *C) {
	c.Check(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
	c.Check(interfaces.BeforeConnectPlug(s.iface, s.plug), IsNil)
}

func (s *EglDriverLibsInterfaceSuite) TestLdconfigSpec(c *C) {
	spec := &ldconfig.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Check(spec.LibDirs(), DeepEquals, map[ldconfig.SnapSlot][]string{
		{SnapName: "egl-provider", SlotName: "egl-driver-libs"}: {
			filepath.Join(dirs.GlobalRootDir, "snap/egl-provider/5/lib1"),
			filepath.Join(dirs.GlobalRootDir, "snap/egl-provider/5/lib2")}})
}

func (s *EglDriverLibsInterfaceSuite) TestConfigfilesSpec(c *C) {
	spec := &configfiles.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Check(spec.PathContent(), DeepEquals, map[string]osutil.FileState{
		"/usr/share/glvnd/egl_vendor.d/10_snap_egl-provider_egl-driver-libs.json": &osutil.MemoryFileState{
			Content: []byte(`{
    "file_format_version": "1.0.0",
    "ICD": {
        "library_path": "libEGL_nvidia.so.0"
    }
}
`), Mode: 0644},
	})
}

func (s *EglDriverLibsInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, false)
	c.Assert(si.ImplicitOnClassic, Equals, false)
	c.Assert(si.ImplicitPlugOnCore, Equals, false)
	c.Assert(si.ImplicitPlugOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows exposing EGL driver libraries to the system`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "egl-driver-libs")
}

func (s *EglDriverLibsInterfaceSuite) TestAutoConnect(c *C) {
	c.Assert(s.iface.AutoConnect(s.plugInfo, s.slotInfo), Equals, true)
}

func (s *EglDriverLibsInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
