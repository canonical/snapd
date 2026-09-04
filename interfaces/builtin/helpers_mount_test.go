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

package builtin

import (
	"fmt"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/mount"
	"github.com/snapcore/snapd/interfaces/symlinks"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type mountAssemblyHelpersSuite struct {
	testutil.BaseTest

	testRoot string
}

var _ = Suite(&mountAssemblyHelpersSuite{})

// egl-style provider: has a required priority attribute, an icd-source and
// library-source with snap, component and empty dirs entries.
const mountAssemblyEglProviderYaml = `name: egl-provider
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
      - $SNAP_COMPONENT(comp1)/egl.d
      - $SNAP_COMPONENT(comp2)/egl.d
    library-source:
      - $SNAP/lib1
      - ${SNAP}/lib2
      - $SNAP_COMPONENT(comp1)/lib1
      - $SNAP_COMPONENT(comp2)/lib2
components:
  comp1:
    type: standard
  comp2:
    type: standard
`

// vulkan-style provider without a priority attribute.
const mountAssemblyVulkanProviderYaml = `name: vulkan-provider
version: 0
slots:
  vulkan-slot:
    interface: vulkan-driver-libs
    compatibility: vulkan-1-(2..5)-ubuntu-2404
    icd-source:
      - $SNAP/vulkan/icd.d/
    library-source:
      - $SNAP/lib1
`

// gbm-style provider with a client-driver.
const mountAssemblyGbmProviderYaml = `name: gbm-provider
version: 0
slots:
  gbm-slot:
    interface: gbm-driver-libs
    client-driver: libgallium_driver.so
    library-source:
      - $SNAP/lib1
`

func (s *mountAssemblyHelpersSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.testRoot = c.MkDir()
	os.MkdirAll(filepath.Join(s.testRoot, dirs.DefaultSnapMountDir), 0755)
	dirs.SetRootDir(s.testRoot)
	s.AddCleanup(func() { dirs.SetRootDir("/") })
}

type mockComp struct {
	name string
	rev  snap.Revision
}

func (s *mountAssemblyHelpersSuite) mockSlotWithComps(c *C, yaml string, rev snap.Revision, comps ...mockComp) *interfaces.ConnectedSlot {
	info := snaptest.MockInfo(c, yaml, &snap.SideInfo{Revision: rev})
	compInfos := make([]*snap.ComponentInfo, 0, len(comps))
	for _, comp := range comps {
		compInfos = append(compInfos, snaptest.MockComponentInfo(c,
			fmt.Sprintf("component: %s+%s\ntype: standard", info.SnapName(), comp.name),
			snap.ComponentSideInfo{Revision: comp.rev}))
	}
	set, err := interfaces.NewSnapAppSet(info, compInfos)
	c.Assert(err, IsNil)

	var slotName string
	for name := range info.Slots {
		slotName = name
	}
	if slotName == "" {
		panic(fmt.Sprintf("no slots in yaml %q", yaml))
	}
	return interfaces.NewConnectedSlot(info.Slots[slotName], set, nil, nil)
}

func (s *mountAssemblyHelpersSuite) TestSourceDirEncodedName(c *C) {
	slot := s.mockSlotWithComps(c, mountAssemblyEglProviderYaml, snap.R(5))

	// Snap-file, with priority: name is prefixed with priority+dir index.
	name, err := sourceDirEncodedName(slot, pathWithDirIdx{
		path: filepath.Join(dirs.SnapMountDir, "egl-provider/5/egl.d/mesa.json"), idx: 0}, true)
	c.Assert(err, IsNil)
	c.Check(name, Equals, "10_snap_egl-provider_egl-slot_egl.d-mesa.json")

	// A different source dir gets the next index.
	name, err = sourceDirEncodedName(slot, pathWithDirIdx{
		path: filepath.Join(dirs.SnapMountDir, "egl-provider/5/egl_alt.d/radeon.json"), idx: 1}, true)
	c.Assert(err, IsNil)
	c.Check(name, Equals, "11_snap_egl-provider_egl-slot_egl_alt.d-radeon.json")

	// Without priority no numeric prefix is used.
	name, err = sourceDirEncodedName(slot, pathWithDirIdx{
		path: filepath.Join(dirs.SnapMountDir, "egl-provider/5/egl.d/mesa.json"), idx: 0}, false)
	c.Assert(err, IsNil)
	c.Check(name, Equals, "snap_egl-provider_egl-slot_egl.d-mesa.json")

	// Component file: the component name is embedded and the original list
	// index (not the filtered position) is used for the priority prefix.
	compPath := filepath.Join(snap.ComponentMountDir("comp1", snap.R(11), "egl-provider"), "egl.d", "nvidia.json")
	name, err = sourceDirEncodedName(slot, pathWithDirIdx{path: compPath, idx: 3}, true)
	c.Assert(err, IsNil)
	c.Check(name, Equals, "13_snap_egl-provider+comp1_egl-slot_egl.d-nvidia.json")

	// withPriority on a slot without the priority attribute errors out.
	vulkanSlot := s.mockSlotWithComps(c, mountAssemblyVulkanProviderYaml, snap.R(4))
	_, err = sourceDirEncodedName(vulkanSlot, pathWithDirIdx{
		path: filepath.Join(dirs.SnapMountDir, "vulkan-provider/4/vulkan/icd.d/intel.json"), idx: 0}, true)
	c.Check(err, ErrorMatches, `invalid priority: snap "vulkan-provider" does not have attribute "priority" for interface "vulkan-driver-libs"`)

	// A path that does not reach below the snap mount dir cannot be encoded.
	_, err = sourceDirEncodedName(slot, pathWithDirIdx{path: dirs.SnapMountDir, idx: 0}, false)
	c.Check(err, ErrorMatches, `internal error: wrong file path: \.`)
}

func (s *mountAssemblyHelpersSuite) TestMountAssemblyLibDirs(c *C) {
	// Only comp1 is installed, the comp2 library source dir is filtered out
	// but its index keeps the original position in the library-source list.
	slot := s.mockSlotWithComps(c, mountAssemblyEglProviderYaml, snap.R(5), mockComp{"comp1", snap.R(11)})

	spec := &mount.Specification{}
	c.Assert(mountAssemblyLibDirs(spec, slot, "egl-driver-libs"), IsNil)

	comp1Lib1 := filepath.Join(snap.ComponentMountDir("comp1", snap.R(11), "egl-provider"), "lib1")
	c.Assert(spec.MountEntries(), DeepEquals, []osutil.MountEntry{
		{
			Name:    filepath.Join(dirs.SnapMountDir, "egl-provider/5/lib1"),
			Dir:     "/opt/snapd/interfaces/egl-driver-libs/lib/egl-provider_egl-slot/0",
			Options: []string{"rbind", "ro"},
		},
		{
			Name:    filepath.Join(dirs.SnapMountDir, "egl-provider/5/lib2"),
			Dir:     "/opt/snapd/interfaces/egl-driver-libs/lib/egl-provider_egl-slot/1",
			Options: []string{"rbind", "ro"},
		},
		{
			Name:    comp1Lib1,
			Dir:     "/opt/snapd/interfaces/egl-driver-libs/lib/egl-provider_egl-slot/2",
			Options: []string{"rbind", "ro"},
		},
	})

	// Library path dirs are collected for the SNAP_LIBRARY_PATH derivation.
	c.Assert(spec.LibraryPathDirs(), DeepEquals, []string{
		"/opt/snapd/interfaces/egl-driver-libs/lib/egl-provider_egl-slot/0",
		"/opt/snapd/interfaces/egl-driver-libs/lib/egl-provider_egl-slot/1",
		"/opt/snapd/interfaces/egl-driver-libs/lib/egl-provider_egl-slot/2",
	})
}

func (s *mountAssemblyHelpersSuite) TestMountAssemblySourceFiles(c *C) {
	slot := s.mockSlotWithComps(c, mountAssemblyEglProviderYaml, snap.R(5))

	// Populate the icd dirs, the library dirs and an ignored file.
	icdDir := filepath.Join(dirs.SnapMountDir, "egl-provider/5/egl.d")
	altDir := filepath.Join(dirs.SnapMountDir, "egl-provider/5/egl_alt.d")
	c.Assert(os.MkdirAll(icdDir, 0755), IsNil)
	c.Assert(os.MkdirAll(altDir, 0755), IsNil)

	mesaIcd := filepath.Join(icdDir, "mesa.json")
	os.WriteFile(mesaIcd, []byte(`{
    "file_format_version" : "1.0.0",
    "ICD" : {
        "library_path" : "libEGL_mesa.so.0"
    }
}
`), 0644)
	radeonIcd := filepath.Join(altDir, "radeon.json")
	os.WriteFile(radeonIcd, []byte(`{
    "file_format_version" : "1.0.0",
    "ICD" : {
        "library_path" : "libEGL_radeon.so.0"
    }
}
`), 0644)
	// A non-json file is ignored.
	os.WriteFile(filepath.Join(icdDir, "foo.bar"), []byte{}, 0644)

	libDir := filepath.Join(dirs.SnapMountDir, "egl-provider/5/lib1")
	c.Assert(os.MkdirAll(libDir, 0755), IsNil)
	os.WriteFile(filepath.Join(libDir, "libEGL_mesa.so.0"), []byte{}, 0644)
	os.WriteFile(filepath.Join(libDir, "libEGL_radeon.so.0"), []byte{}, 0644)

	spec := &mount.Specification{}
	c.Assert(mountAssemblySourceFiles(spec, slot, "egl-driver-libs",
		sourceDirAttr{attrName: "icd-source"}, "egl_vendor.d", checkEglIcdFile, true), IsNil)

	c.Assert(spec.MountEntries(), DeepEquals, []osutil.MountEntry{
		{
			Name:    mesaIcd,
			Dir:     "/opt/snapd/interfaces/egl-driver-libs/share/egl_vendor.d/10_snap_egl-provider_egl-slot_egl.d-mesa.json",
			Options: []string{"bind", "ro", osutil.XSnapdKindFile()},
		},
		{
			Name:    radeonIcd,
			Dir:     "/opt/snapd/interfaces/egl-driver-libs/share/egl_vendor.d/11_snap_egl-provider_egl-slot_egl_alt.d-radeon.json",
			Options: []string{"bind", "ro", osutil.XSnapdKindFile()},
		},
	})
	// Source files do not contribute library path dirs.
	c.Assert(spec.LibraryPathDirs(), IsNil)

	// The mount names are byte-identical to the classic symlink names.
	symlinksSpec := &symlinks.Specification{}
	c.Assert(symlinksForSourceDir(symlinksSpec, slot,
		sourceDirAttr{attrName: "icd-source"}, eglVendorPath, checkEglIcdFile, true), IsNil)
	expectedNames := map[string]bool{}
	for link := range symlinksSpec.Symlinks()[eglVendorPath] {
		expectedNames[link] = true
	}
	c.Assert(expectedNames, DeepEquals, map[string]bool{
		"10_snap_egl-provider_egl-slot_egl.d-mesa.json":       true,
		"11_snap_egl-provider_egl-slot_egl_alt.d-radeon.json": true,
	})
	for _, entry := range spec.MountEntries() {
		c.Check(expectedNames[filepath.Base(entry.Dir)], Equals, true,
			Commentf("mount target %q has no matching symlink name", entry.Dir))
	}
}

func (s *mountAssemblyHelpersSuite) TestMountAssemblySourceFilesVulkan(c *C) {
	slot := s.mockSlotWithComps(c, mountAssemblyVulkanProviderYaml, snap.R(4))

	// Vulkan slots have no priority attribute, so the names have no prefix.
	icdDir := filepath.Join(dirs.SnapMountDir, "vulkan-provider/4/vulkan/icd.d")
	c.Assert(os.MkdirAll(icdDir, 0755), IsNil)
	intelIcd := filepath.Join(icdDir, "intel.json")
	os.WriteFile(intelIcd, []byte(`{
    "file_format_version" : "1.0.0",
    "ICD" : {
        "api_version" : "1.2.0",
        "library_path" : "libvulkan_intel.so"
    }
}
`), 0644)
	libDir := filepath.Join(dirs.SnapMountDir, "vulkan-provider/4/lib1")
	c.Assert(os.MkdirAll(libDir, 0755), IsNil)
	os.WriteFile(filepath.Join(libDir, "libvulkan_intel.so"), []byte{}, 0644)

	spec := &mount.Specification{}
	c.Assert(mountAssemblySourceFiles(spec, slot, "vulkan-driver-libs",
		sourceDirAttr{attrName: "icd-source"}, "vulkan/icd.d", checkVulkanIcdFile, false), IsNil)

	c.Assert(spec.MountEntries(), DeepEquals, []osutil.MountEntry{
		{
			Name:    intelIcd,
			Dir:     "/opt/snapd/interfaces/vulkan-driver-libs/share/vulkan/icd.d/snap_vulkan-provider_vulkan-slot_vulkan-icd.d-intel.json",
			Options: []string{"bind", "ro", osutil.XSnapdKindFile()},
		},
	})

	// An optional *-source attribute that is absent contributes nothing.
	implicitSpec := &mount.Specification{}
	c.Assert(mountAssemblySourceFiles(implicitSpec, slot, "vulkan-driver-libs",
		sourceDirAttr{attrName: "implicit-layer-source", isOptional: true}, "vulkan/implicit_layer.d", checkVulkanLayersFile, false), IsNil)
	c.Assert(implicitSpec.MountEntries(), HasLen, 0)
}

func (s *mountAssemblyHelpersSuite) TestMountAssemblyClientDriver(c *C) {
	slot := s.mockSlotWithComps(c, mountAssemblyGbmProviderYaml, snap.R(6))

	libDir := filepath.Join(dirs.SnapMountDir, "gbm-provider/6/lib1")
	c.Assert(os.MkdirAll(libDir, 0755), IsNil)
	driverPath := filepath.Join(libDir, "libgallium_driver.so")
	os.WriteFile(driverPath, []byte{}, 0644)

	spec := &mount.Specification{}
	c.Assert(mountAssemblyClientDriver(spec, slot, "gbm-driver-libs"), IsNil)
	c.Assert(spec.MountEntries(), DeepEquals, []osutil.MountEntry{
		{
			Name:    driverPath,
			Dir:     "/opt/snapd/interfaces/gbm-driver-libs/share/gbm/libgallium_driver.so",
			Options: []string{"bind", "ro", osutil.XSnapdKindFile()},
		},
	})
	c.Assert(spec.LibraryPathDirs(), IsNil)

	// A missing client-driver in the library dirs is an error.
	emptySlot := s.mockSlotWithComps(c, mountAssemblyGbmProviderYaml, snap.R(7))
	c.Assert(mountAssemblyClientDriver(&mount.Specification{}, emptySlot, "gbm-driver-libs"),
		ErrorMatches, `"libgallium_driver.so" not found in the library-source directories`)
}
