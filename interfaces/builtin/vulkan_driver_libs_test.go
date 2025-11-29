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
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/configfiles"
	"github.com/snapcore/snapd/interfaces/ldconfig"
	"github.com/snapcore/snapd/interfaces/symlinks"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type VulkanDriverLibsInterfaceSuite struct {
	testutil.BaseTest

	testRoot string

	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

var _ = Suite(&VulkanDriverLibsInterfaceSuite{
	iface: builtin.MustInterface("vulkan-driver-libs"),
})

// This is in fact implicit in the system
const vulkanDriverLibsConsumerYaml = `name: snapd
version: 0
plugs:
  vulkan:
    interface: vulkan-driver-libs
apps:
  app:
    plugs: [vulkan]
`

const vulkanDriverLibsProvider = `name: vulkan-provider
version: 0
slots:
  vulkan-slot:
    interface: vulkan-driver-libs
    compatibility: vulkan-1-(2..5)-ubuntu-2404
    icd-source:
      - $SNAP/vulkan/icd.d/
      - $SNAP/vulkan_alt.d/
      - $SNAP/vulkan_empty.d/
    implicit-layer-source:
      - $SNAP/vulkan/implicit_layer.d/
    explicit-layer-source:
      - $SNAP/vulkan/explicit_layer.d/
    library-source:
      - $SNAP/lib1
      - ${SNAP}/lib2
`

func (s *VulkanDriverLibsInterfaceSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.testRoot = c.MkDir()
	os.MkdirAll(filepath.Join(s.testRoot, dirs.DefaultSnapMountDir), 0755)
	dirs.SetRootDir(s.testRoot)
	s.AddCleanup(func() { dirs.SetRootDir("/") })

	s.plug, s.plugInfo = MockConnectedPlug(c, vulkanDriverLibsConsumerYaml,
		&snap.SideInfo{Revision: snap.R(3)}, "vulkan")
	s.slot, s.slotInfo = MockConnectedSlot(c, vulkanDriverLibsProvider,
		&snap.SideInfo{Revision: snap.R(5)}, "vulkan-slot")
}

func (s *VulkanDriverLibsInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "vulkan-driver-libs")
}

func (s *VulkanDriverLibsInterfaceSuite) TestSanitizeSlot(c *C) {
	libDir1 := filepath.Join(dirs.GlobalRootDir, "snap/vulkan-provider/5/lib1")
	c.Assert(os.MkdirAll(libDir1, 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(libDir1, "libGLX_nvidia.so.0"), []byte(``), 0644), IsNil)
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *VulkanDriverLibsInterfaceSuite) TestSanitizeSlotError(c *C) {
	sourceKeys := []string{"icd-source", "implicit-layer-source",
		"explicit-layer-source", "library-source"}
	for _, key := range sourceKeys {
		keyToRootDir := map[string]string{"icd-source": "$SNAP",
			"implicit-layer-source": "$SNAP",
			"explicit-layer-source": "$SNAP",
			"library-source":        "$SNAP"}
		keyToRootDir[key] = "/snap/vulkan-provider/current"
		slot := MockSlot(c, fmt.Sprintf(`name: vulkan-provider
version: 0
slots:
  vulkan:
    interface: vulkan-driver-libs
    compatibility: vulkan-1-5-ubuntu-2404
    icd-source:
      - %s/vulkan/icd.d/
    implicit-layer-source:
      - %s/vulkan/implicit_layer.d/
    explicit-layer-source:
      - %s/vulkan/explicit_layer.d/
    library-source:
      - %s/lib1
`, keyToRootDir["icd-source"], keyToRootDir["implicit-layer-source"], keyToRootDir["explicit-layer-source"], keyToRootDir["library-source"]), nil, "vulkan")
		c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
			fmt.Sprintf(`vulkan-driver-libs %s directory .* must start with \$SNAP/ or \$\{SNAP\}/`, key))
	}

	slot := MockSlot(c, `name: vulkan-provider
version: 0
slots:
  vulkan:
    compatibility: vulkan-1-5-ubuntu-2404
    icd-source:
      - $SNAP/vulkan.d/
    interface: vulkan-driver-libs
`, nil, "vulkan")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		`snap "vulkan-provider" does not have attribute "library-source" for interface "vulkan-driver-libs"`)

	slot = MockSlot(c, `name: vulkan-provider
version: 0
slots:
  vulkan:
    interface: vulkan-driver-libs
    compatibility: vulkan-1-5-ubuntu-2404
    icd-source:
      - $SNAP/vulkan.d/
    library-source: $SNAP/lib1
`, nil, "vulkan")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		`snap "vulkan-provider" has interface "vulkan-driver-libs" with invalid value type string for "library-source" attribute: \*\[\]string`)

	slot = MockSlot(c, `name: vulkan-provider
version: 0
slots:
  vulkan:
    interface: vulkan-driver-libs
    compatibility: vulkan-1-5-ubuntu-2404
    library-source:
      - $SNAP/lib1
`, nil, "vulkan")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		`snap "vulkan-provider" does not have attribute "icd-source" for interface "vulkan-driver-libs"`)

	slot = MockSlot(c, `name: vulkan-provider
version: 0
slots:
  vulkan:
    interface: vulkan-driver-libs
    compatibility: vulkan-1-4-ubuntu-2404
    icd-source: $SNAP/vulkan.d/
    library-source:
      - $SNAP/lib1
`, nil, "vulkan")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		`snap "vulkan-provider" has interface "vulkan-driver-libs" with invalid value type string for "icd-source" attribute: \*\[\]string`)

	slot = MockSlot(c, `name: vulkan-provider
version: 0
slots:
  vulkan:
    interface: vulkan-driver-libs
    compatibility: ubuntu
    icd-source:
      - $SNAP/vulkan.d/
    library-source:
      - $SNAP/lib1
`, nil, "vulkan")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		`compatibility label "ubuntu": unexpected number of strings \(should be 2\)`)

	slot = MockSlot(c, `name: vulkan-provider
version: 0
slots:
  vulkan:
    interface: vulkan-driver-libs
    icd-source:
      - $SNAP/vulkan.d/
    library-source:
      - $SNAP/lib1
`, nil, "vulkan")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		`snap "vulkan-provider" does not have attribute "compatibility" for interface "vulkan-driver-libs"`)
}

func (s *VulkanDriverLibsInterfaceSuite) TestSanitizePlug(c *C) {
	c.Check(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
	c.Check(interfaces.BeforeConnectPlug(s.iface, s.plug), IsNil)
}

func (s *VulkanDriverLibsInterfaceSuite) TestLdconfigSpec(c *C) {
	spec := &ldconfig.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Check(spec.LibDirs(), DeepEquals, map[ldconfig.SnapSlot][]string{
		{SnapName: "vulkan-provider", SlotName: "vulkan-slot"}: {
			filepath.Join(dirs.GlobalRootDir, "snap/vulkan-provider/5/lib1"),
			filepath.Join(dirs.GlobalRootDir, "snap/vulkan-provider/5/lib2")}})
}

func (s *VulkanDriverLibsInterfaceSuite) TestSymlinksSpec(c *C) {
	// Write ICD files
	expected := symlinks.SymlinkToTarget{}
	for _, icdData := range []struct {
		gpu    string
		subDir string
	}{{"mesa", "vulkan/icd.d"}, {"nvidia", "vulkan/icd.d"}, {"radeon", "vulkan_alt.d"}} {
		icdDir := filepath.Join(dirs.GlobalRootDir, "snap/vulkan-provider/5", icdData.subDir)
		c.Assert(os.MkdirAll(icdDir, 0755), IsNil)
		icdPath := filepath.Join(icdDir, icdData.gpu+".json")
		os.WriteFile(icdPath, []byte(fmt.Sprintf(`{
    "file_format_version" : "1.0.0",
    "ICD" : {
        "library_path" : "libvulkan_%s.so.0",
        "api_version" : "1.4.303"
    }
}
`, icdData.gpu)), 0655)
		libDir := filepath.Join(dirs.GlobalRootDir, "snap/vulkan-provider/5/lib2")
		c.Assert(os.MkdirAll(libDir, 0755), IsNil)
		libPath := filepath.Join(libDir, "libvulkan_"+icdData.gpu+".so.0")
		os.WriteFile(libPath, []byte{}, 0655)

		// Ignored file
		otherPath := filepath.Join(icdDir, "foo.bar")
		os.WriteFile(otherPath, []byte{}, 0655)

		// Ignored symlink
		os.Symlink("not_exists", filepath.Join(icdDir, "foo.json"))

		expected["snap_vulkan-provider_vulkan-slot_"+strings.ReplaceAll(icdData.subDir, "/", "-")+"-"+icdData.gpu+".json"] = icdPath
	}

	// Write layers
	implicitDir := filepath.Join(dirs.GlobalRootDir, "snap/vulkan-provider/5/vulkan/implicit_layer.d")
	c.Assert(os.MkdirAll(implicitDir, 0755), IsNil)
	implicitPath := filepath.Join(implicitDir, "gpu_layer.json")
	os.WriteFile(implicitPath, []byte(`{
    "file_format_version" : "1.0.1",
    "layers" : [
       {
         "name": "layer1",
         "library_path" : "libvulkan_nvidia.so.0",
         "api_version" : "1.4.303"
       },
       {
         "name": "layer2",
         "library_path" : "libvulkan_nvidia.so.0",
         "api_version" : "1.4.303"
       }
     ]
}
`), 0644)
	expectedImplicit := symlinks.SymlinkToTarget{
		"snap_vulkan-provider_vulkan-slot_vulkan-implicit_layer.d-gpu_layer.json": implicitPath,
	}

	explicitDir := filepath.Join(dirs.GlobalRootDir, "snap/vulkan-provider/5/vulkan/explicit_layer.d")
	c.Assert(os.MkdirAll(explicitDir, 0755), IsNil)
	explicitPath := filepath.Join(explicitDir, "gpu_layer.json")
	os.WriteFile(explicitPath, []byte(`{
    "file_format_version" : "1.0.1",
    "layer": {
       "name": "layer1",
       "library_path" : "libvulkan_nvidia.so.0",
       "api_version" : "1.4.303"
     }
}
`), 0644)
	expectedExplicit := symlinks.SymlinkToTarget{
		"snap_vulkan-provider_vulkan-slot_vulkan-explicit_layer.d-gpu_layer.json": explicitPath,
	}

	// Now check symlinks to be created
	spec := &symlinks.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Check(spec.Symlinks(), DeepEquals, map[string]symlinks.SymlinkToTarget{
		"/etc/vulkan/icd.d":            expected,
		"/etc/vulkan/implicit_layer.d": expectedImplicit,
		"/etc/vulkan/explicit_layer.d": expectedExplicit,
	})
}

func (s *VulkanDriverLibsInterfaceSuite) TestTrackedDirectories(c *C) {
	symlinksUser := builtin.SymlinksUserIfaceFromVulkanIface(s.iface)
	c.Assert(symlinksUser.TrackedDirectories(), DeepEquals, []string{
		"/etc/vulkan/icd.d", "/etc/vulkan/explicit_layer.d", "/etc/vulkan/implicit_layer.d"})
}

func (s *VulkanDriverLibsInterfaceSuite) TestSymlinksSpecNoLibrary(c *C) {
	// Write ICD file
	icdDir := filepath.Join(dirs.GlobalRootDir, "snap/vulkan-provider/5/vulkan/icd.d")
	c.Assert(os.MkdirAll(icdDir, 0755), IsNil)
	icdPath := filepath.Join(icdDir, "nvidia.json")
	os.WriteFile(icdPath, []byte(`{
    "file_format_version" : "1.0.0",
    "ICD" : {
        "library_path" : "libGLX_nvidia.so.0",
        "api_version" : "1.4.303"
    }
}
`), 0655)

	spec := &symlinks.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), ErrorMatches,
		`invalid icd-source: nvidia.json: "libGLX_nvidia.so.0" not found in the library-source directories`)
}

func (s *VulkanDriverLibsInterfaceSuite) TestSymlinksSpecBadJson(c *C) {
	// Write ICD file
	icdDir := filepath.Join(dirs.GlobalRootDir, "snap/vulkan-provider/5/vulkan/icd.d")
	c.Assert(os.MkdirAll(icdDir, 0755), IsNil)
	icdPath := filepath.Join(icdDir, "nvidia.json")
	os.WriteFile(icdPath, []byte(`libGLX_nvidia.so.0`), 0655)

	spec := &symlinks.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), ErrorMatches,
		`invalid icd-source: nvidia.json: while unmarshalling: invalid character 'l' looking for beginning of value`)
}

func (s *VulkanDriverLibsInterfaceSuite) TestSymlinksSpecNoLibraryInIcd(c *C) {
	// Write ICD file
	icdDir := filepath.Join(dirs.GlobalRootDir, "snap/vulkan-provider/5/vulkan/icd.d")
	c.Assert(os.MkdirAll(icdDir, 0755), IsNil)
	icdPath := filepath.Join(icdDir, "nvidia.json")
	os.WriteFile(icdPath, []byte(`{
    "file_format_version" : "1.0.0",
    "ICD" : {
        "api_version" : "1.4.303"
    }
}
`), 0644)

	spec := &symlinks.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), ErrorMatches,
		`invalid icd-source: nvidia.json: no library_path value found`)
}

func (s *VulkanDriverLibsInterfaceSuite) TestSymlinksSpecNoApiVersionInIcd(c *C) {
	// Write ICD file
	icdDir := filepath.Join(dirs.GlobalRootDir, "snap/vulkan-provider/5/vulkan/icd.d")
	c.Assert(os.MkdirAll(icdDir, 0755), IsNil)
	icdPath := filepath.Join(icdDir, "nvidia.json")
	os.WriteFile(icdPath, []byte(`{
    "file_format_version" : "1.0.0",
    "ICD" : {
        "library_path" : "libGLX_nvidia.so.0"
    }
}
`), 0644)

	spec := &symlinks.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), ErrorMatches,
		`invalid icd-source: nvidia.json: no api_version value found`)
}

func (s *VulkanDriverLibsInterfaceSuite) TestSymlinksSpecBadApiVersion(c *C) {
	icdDir := filepath.Join(dirs.GlobalRootDir, "snap/vulkan-provider/5/vulkan/icd.d")
	c.Assert(os.MkdirAll(icdDir, 0755), IsNil)
	icdPath := filepath.Join(icdDir, "nvidia.json")

	for i, tc := range []struct {
		apiVersion string
		errMsg     string
	}{
		{"foo", "api_version is not a version: foo"},
		{"1.-1", "invalid minor: api_version 1.-1"},
		{"1x.1", "invalid major: api_version 1x.1"},
		{"1.1x", "invalid minor: api_version 1.1x"},
		{"0.1", `api_version 0.1 is not compatible with the interface compatibility label vulkan-1-\(2..5\)-ubuntu-2404`},
	} {
		c.Logf("tc %d: %+v", i, tc)

		// Write ICD file
		os.WriteFile(icdPath, []byte(fmt.Sprintf(`{
    "file_format_version" : "1.0.0",
    "ICD" : {
        "library_path" : "libGLX_nvidia.so.0",
        "api_version" : "%s"
    }
}
`, tc.apiVersion)), 0655)

		spec := &symlinks.Specification{}
		c.Check(spec.AddConnectedPlug(s.iface, s.plug, s.slot), ErrorMatches,
			fmt.Sprintf("invalid icd-source: nvidia.json: %s", tc.errMsg))
	}
}

func (s *VulkanDriverLibsInterfaceSuite) TestSymlinksSpecNoApiVersionInLayer(c *C) {
	// Write layer file
	explicitDir := filepath.Join(dirs.GlobalRootDir, "snap/vulkan-provider/5/vulkan/explicit_layer.d")
	c.Assert(os.MkdirAll(explicitDir, 0755), IsNil)
	explicitPath := filepath.Join(explicitDir, "gpu_layer.json")
	os.WriteFile(explicitPath, []byte(`{
    "file_format_version" : "1.0.1",
    "layer": {
       "name": "layer1",
       "library_path" : "libvulkan_nvidia.so.0"
     }
}
`), 0644)

	spec := &symlinks.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), ErrorMatches,
		`invalid explicit-layer-source: gpu_layer.json: no api_version value found`)
}

func (s *VulkanDriverLibsInterfaceSuite) TestSymlinksSpecNoLayerInLayerFile(c *C) {
	// Write layer file
	explicitDir := filepath.Join(dirs.GlobalRootDir, "snap/vulkan-provider/5/vulkan/explicit_layer.d")
	c.Assert(os.MkdirAll(explicitDir, 0755), IsNil)
	explicitPath := filepath.Join(explicitDir, "gpu_layer.json")
	os.WriteFile(explicitPath, []byte(`{
    "file_format_version" : "1.0.1"
}
`), 0644)

	spec := &symlinks.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), ErrorMatches,
		`invalid explicit-layer-source: gpu_layer\.json: either layer or layers should be present in layers file`)
}

func (s *VulkanDriverLibsInterfaceSuite) TestSymlinksSpecLayerAndLayersInLayerFile(c *C) {
	// Write layer file
	explicitDir := filepath.Join(dirs.GlobalRootDir, "snap/vulkan-provider/5/vulkan/explicit_layer.d")
	c.Assert(os.MkdirAll(explicitDir, 0755), IsNil)
	explicitPath := filepath.Join(explicitDir, "gpu_layer.json")
	os.WriteFile(explicitPath, []byte(`{
    "file_format_version" : "1.0.1",
    "layer": {
       "name": "layer1",
       "library_path" : "libvulkan_nvidia.so.0"
     },
    "layers" : [
       {
         "name": "layer1",
         "library_path" : "libvulkan_nvidia.so.0",
         "api_version" : "1.4.303"
       },
       {
         "name": "layer2",
         "library_path" : "libvulkan_nvidia.so.0",
         "api_version" : "1.4.303"
       }
     ]
}
`), 0644)

	spec := &symlinks.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), ErrorMatches,
		`invalid explicit-layer-source: gpu_layer\.json: layer and layers cannot be both present in layers file`)
}

func (s *VulkanDriverLibsInterfaceSuite) TestSymlinksSpecBadJsonInLayerFile(c *C) {
	// Write layer file
	explicitDir := filepath.Join(dirs.GlobalRootDir, "snap/vulkan-provider/5/vulkan/explicit_layer.d")
	c.Assert(os.MkdirAll(explicitDir, 0755), IsNil)
	explicitPath := filepath.Join(explicitDir, "gpu_layer.json")
	os.WriteFile(explicitPath, []byte(`foo`), 0644)

	spec := &symlinks.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), ErrorMatches,
		`invalid explicit-layer-source: gpu_layer\.json: while unmarshalling: invalid character 'o' in literal false \(expecting 'a'\)`)
}

func (s *VulkanDriverLibsInterfaceSuite) TestSymlinksSpecNoLibraryInLayer(c *C) {
	// Write layer file
	explicitDir := filepath.Join(dirs.GlobalRootDir, "snap/vulkan-provider/5/vulkan/explicit_layer.d")
	c.Assert(os.MkdirAll(explicitDir, 0755), IsNil)
	explicitPath := filepath.Join(explicitDir, "gpu_layer.json")
	os.WriteFile(explicitPath, []byte(`{
    "file_format_version" : "1.0.1",
    "layer": {
       "name": "layer1",
       "api_version" : "1.4.303"
     }
}
`), 0644)

	spec := &symlinks.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), ErrorMatches,
		`invalid explicit-layer-source: gpu_layer.json: either library_path or component_layers should be present in layer`)
}

func (s *VulkanDriverLibsInterfaceSuite) TestSymlinksSpecBothLibraryAndCompsInLayer(c *C) {
	// Write layer file
	explicitDir := filepath.Join(dirs.GlobalRootDir, "snap/vulkan-provider/5/vulkan/explicit_layer.d")
	c.Assert(os.MkdirAll(explicitDir, 0755), IsNil)
	explicitPath := filepath.Join(explicitDir, "gpu_layer.json")
	os.WriteFile(explicitPath, []byte(`{
    "file_format_version" : "1.0.1",
    "layer": {
       "name": "layer1",
       "api_version" : "1.4.303",
       "library_path" : "libvulkan_nvidia.so.0",
       "component_layers": [
           "VK_LAYER_canonical_1",
           "VK_LAYER_canonical_2"
       ]
     }
}
`), 0644)

	spec := &symlinks.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), ErrorMatches,
		`invalid explicit-layer-source: gpu_layer.json: library_path and component_layers cannot be both present in layers file`)
}

func (s *VulkanDriverLibsInterfaceSuite) TestConfigfilesSpec(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	spec := &configfiles.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Check(spec.PathContent(), DeepEquals, map[string]osutil.FileState{
		filepath.Join(dirs.GlobalRootDir, "/var/lib/snapd/export/system_vulkan-provider_vulkan-slot_vulkan-driver-libs.library-source"): &osutil.MemoryFileState{
			Content: []byte(filepath.Join(dirs.SnapMountDir, "vulkan-provider/5/lib1") + "\n" +
				filepath.Join(dirs.SnapMountDir, "vulkan-provider/5/lib2") + "\n"), Mode: 0644},
	})
}

func (s *VulkanDriverLibsInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, false)
	c.Assert(si.ImplicitOnClassic, Equals, false)
	c.Assert(si.ImplicitPlugOnCore, Equals, false)
	c.Assert(si.ImplicitPlugOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows exposing vulkan driver libraries to the system`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "vulkan-driver-libs")
}

func (s *VulkanDriverLibsInterfaceSuite) TestAutoConnect(c *C) {
	c.Assert(s.iface.AutoConnect(s.plugInfo, s.slotInfo), Equals, true)
}

func (s *VulkanDriverLibsInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
