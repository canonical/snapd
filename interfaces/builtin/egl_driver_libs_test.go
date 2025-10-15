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
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/configfiles"
	"github.com/snapcore/snapd/interfaces/ldconfig"
	"github.com/snapcore/snapd/interfaces/symlinks"
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
  egl-slot:
    interface: egl-driver-libs
    priority: 10
    compatibility: egl-1-5-ubuntu-2404
    icd-source:
      - $SNAP/egl.d/
      - $SNAP/egl_alt.d/
      - $SNAP/egl_empty.d/
    library-source:
      - $SNAP/lib1
      - ${SNAP}/lib2
`

func (s *EglDriverLibsInterfaceSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.testRoot = c.MkDir()
	os.MkdirAll(filepath.Join(s.testRoot, dirs.DefaultSnapMountDir), 0755)
	dirs.SetRootDir(s.testRoot)
	s.AddCleanup(func() { dirs.SetRootDir("/") })

	s.plug, s.plugInfo = MockConnectedPlug(c, eglDriverLibsConsumerYaml,
		&snap.SideInfo{Revision: snap.R(3)}, "egl")
	s.slot, s.slotInfo = MockConnectedSlot(c, eglDriverLibsProvider,
		&snap.SideInfo{Revision: snap.R(5)}, "egl-slot")
}

func (s *EglDriverLibsInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "egl-driver-libs")
}

func (s *EglDriverLibsInterfaceSuite) TestSanitizeSlot(c *C) {
	libDir1 := filepath.Join(dirs.SnapMountDir, "egl-provider/5/lib1")
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
    icd-source:
      - $SNAP/egl.d/
    library-source:
      - /snap/egl-provider/current/lib1
`, nil, "egl")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		`egl-driver-libs library-source directory .* must start with \$SNAP/ or \$\{SNAP\}/`)

	slot = MockSlot(c, `name: egl-provider
version: 0
slots:
  egl:
    priority: 10
    compatibility: egl-1-5-ubuntu-2404
    icd-source:
      - $SNAP/egl.d/
    interface: egl-driver-libs
`, nil, "egl")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		`snap "egl-provider" does not have attribute "library-source" for interface "egl-driver-libs"`)

	slot = MockSlot(c, `name: egl-provider
version: 0
slots:
  egl:
    interface: egl-driver-libs
    priority: 10
    compatibility: egl-1-5-ubuntu-2404
    icd-source:
      - $SNAP/egl.d/
    library-source: $SNAP/lib1
`, nil, "egl")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		`snap "egl-provider" has interface "egl-driver-libs" with invalid value type string for "library-source" attribute: \*\[\]string`)

	slot = MockSlot(c, `name: egl-provider
version: 0
slots:
  egl:
    interface: egl-driver-libs
    compatibility: egl-ubuntu-2404
    icd-source:
      - $SNAP/egl.d/
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
		`snap "egl-provider" does not have attribute "icd-source" for interface "egl-driver-libs"`)

	slot = MockSlot(c, `name: egl-provider
version: 0
slots:
  egl:
    interface: egl-driver-libs
    priority: 0
    compatibility: egl-1-5-ubuntu-2404
    icd-source:
      - $SNAP/egl.d/
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
    icd-source:
      - /abs/path/egl.d/
`, nil, "egl")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		`egl-driver-libs icd-source directory "/abs/path/egl.d/" must start with \$SNAP/ or \$\{SNAP\}/`)

	slot = MockSlot(c, `name: egl-provider
version: 0
slots:
  egl:
    interface: egl-driver-libs
    priority: 15
    compatibility: egl-1-5-ubuntu-2404
    icd-source: $SNAP/egl.d/
`, nil, "egl")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		`snap "egl-provider" has interface "egl-driver-libs" with invalid value type string for "icd-source" attribute: \*\[\]string`)

	slot = MockSlot(c, `name: egl-provider
version: 0
slots:
  egl:
    interface: egl-driver-libs
    priority: 15
    compatibility: ubuntu
    icd-source:
      - $SNAP/egl.d/
`, nil, "egl")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		`compatibility label "ubuntu": unexpected number of strings \(should be 2\)`)

	slot = MockSlot(c, `name: egl-provider
version: 0
slots:
  egl:
    interface: egl-driver-libs
    priority: 15
    icd-source:
      - $SNAP/egl.d/
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
		{SnapName: "egl-provider", SlotName: "egl-slot"}: {
			filepath.Join(dirs.SnapMountDir, "egl-provider/5/lib1"),
			filepath.Join(dirs.SnapMountDir, "egl-provider/5/lib2")}})
}

func (s *EglDriverLibsInterfaceSuite) TestSymlinksSpec(c *C) {
	// Write ICD files
	expected := symlinks.SymlinkToTarget{}
	for _, icdData := range []struct {
		gpu    string
		subDir string
	}{{"mesa", "egl.d"}, {"nvidia", "egl.d"}, {"radeon", "egl_alt.d"}} {
		icdDir := filepath.Join(dirs.SnapMountDir, "egl-provider/5", icdData.subDir)
		c.Assert(os.MkdirAll(icdDir, 0755), IsNil)
		icdPath := filepath.Join(icdDir, icdData.gpu+".json")
		os.WriteFile(icdPath, []byte(fmt.Sprintf(`{
    "file_format_version" : "1.0.0",
    "ICD" : {
        "library_path" : "libEGL_%s.so.0"
    }
}
`, icdData.gpu)), 0655)
		libDir := filepath.Join(dirs.SnapMountDir, "egl-provider/5/lib2")
		c.Assert(os.MkdirAll(libDir, 0755), IsNil)
		libPath := filepath.Join(libDir, "libEGL_"+icdData.gpu+".so.0")
		os.WriteFile(libPath, []byte{}, 0655)

		// Ignored file
		otherPath := filepath.Join(icdDir, "foo.bar")
		os.WriteFile(otherPath, []byte{}, 0655)

		// Ignored symlink
		os.Symlink("not_exists", filepath.Join(icdDir, "foo.json"))

		expected["10_snap_egl-provider_egl-slot_"+icdData.subDir+"-"+icdData.gpu+".json"] = icdPath
	}

	// Now check symlinks to be created
	spec := &symlinks.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Check(spec.Symlinks(), DeepEquals, map[string]symlinks.SymlinkToTarget{
		"/etc/glvnd/egl_vendor.d": expected,
	})
}

func (s *EglDriverLibsInterfaceSuite) TestConfigfilesSpec(c *C) {
	spec := &configfiles.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Check(spec.PathContent(), DeepEquals, map[string]osutil.FileState{
		filepath.Join(dirs.GlobalRootDir, "/var/lib/snapd/export/system_egl-provider_egl-slot_egl-driver-libs.library-source"): &osutil.MemoryFileState{
			Content: []byte(filepath.Join(dirs.SnapMountDir, "egl-provider/5/lib1") + "\n" +
				filepath.Join(dirs.SnapMountDir, "egl-provider/5/lib2") + "\n"), Mode: 0644},
	})
}

func (s *EglDriverLibsInterfaceSuite) TestTrackedDirectories(c *C) {
	symlinksUser := builtin.SymlinksUserIfaceFromEglIface(s.iface)
	c.Assert(symlinksUser.TrackedDirectories(), DeepEquals, []string{
		"/etc/glvnd/egl_vendor.d"})
}

func (s *EglDriverLibsInterfaceSuite) TestSymlinksSpecNoLibrary(c *C) {
	// Write ICD file
	icdDir := filepath.Join(dirs.SnapMountDir, "egl-provider/5/egl.d")
	c.Assert(os.MkdirAll(icdDir, 0755), IsNil)
	icdPath := filepath.Join(icdDir, "nvidia.json")
	os.WriteFile(icdPath, []byte(`{
    "file_format_version" : "1.0.0",
    "ICD" : {
        "library_path" : "libEGL_nvidia.so.0"
    }
}
`), 0655)

	// Now check symlinks to be created
	spec := &symlinks.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), ErrorMatches,
		`invalid icd-source: "libEGL_nvidia.so.0" not found in the library-source directories`)
}

func (s *EglDriverLibsInterfaceSuite) TestSymlinksSpecBadJson(c *C) {
	// Write ICD file
	icdDir := filepath.Join(dirs.SnapMountDir, "egl-provider/5/egl.d")
	c.Assert(os.MkdirAll(icdDir, 0755), IsNil)
	icdPath := filepath.Join(icdDir, "nvidia.json")
	os.WriteFile(icdPath, []byte(`libEGL_nvidia.so.0`), 0655)

	// Now check symlinks to be created
	spec := &symlinks.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), ErrorMatches,
		`invalid icd-source: while unmarshalling nvidia.json: invalid character 'l' looking for beginning of value`)
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
