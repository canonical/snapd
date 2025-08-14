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
	"github.com/snapcore/snapd/interfaces/ldconfig"
	"github.com/snapcore/snapd/interfaces/symlinks"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type GbmDriverLibsInterfaceSuite struct {
	testutil.BaseTest

	testRoot string

	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

var _ = Suite(&GbmDriverLibsInterfaceSuite{
	iface: builtin.MustInterface("gbm-driver-libs"),
})

// This is in fact implicit in the system
const gbmDriverLibsConsumerYaml = `name: snapd
version: 0
plugs:
  gbm:
    interface: gbm-driver-libs
apps:
  app:
    plugs: [gbm]
`

const gbmDriverLibsProvider = `name: gbm-provider
version: 0
slots:
  gbm-driver-libs:
    client-driver: nvidia-drm_gbm.so
    compatibility: gbmbackend-(0..2)-arch64-ubuntu-2510
    source:
      - $SNAP/lib1
      - ${SNAP}/lib2
`

func (s *GbmDriverLibsInterfaceSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.testRoot = c.MkDir()
	dirs.SetRootDir(s.testRoot)
	s.AddCleanup(func() { dirs.SetRootDir("/") })

	s.plug, s.plugInfo = MockConnectedPlug(c, gbmDriverLibsConsumerYaml,
		&snap.SideInfo{Revision: snap.R(3)}, "gbm")
	s.slot, s.slotInfo = MockConnectedSlot(c, gbmDriverLibsProvider,
		&snap.SideInfo{Revision: snap.R(5)}, "gbm-driver-libs")
}

func (s *GbmDriverLibsInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "gbm-driver-libs")
}

func (s *GbmDriverLibsInterfaceSuite) TestSanitizeSlot(c *C) {
	libDir1 := filepath.Join(dirs.GlobalRootDir, "snap/gbm-provider/5/lib1")
	c.Assert(os.MkdirAll(libDir1, 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(libDir1, "libnvidia-allocator.so.1"), []byte(``), 0644), IsNil)
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *GbmDriverLibsInterfaceSuite) TestSanitizeSlotError(c *C) {
	slot := MockSlot(c, `name: gbm-provider
version: 0
slots:
  gbm:
    interface: gbm-driver-libs
    client-driver: nvidia-drm_gbm.so
    compatibility: gbmbackend-(0..2)-arch64-ubuntu-2404
    source:
      - /snap/gbm-provider/current/lib1
`, nil, "gbm")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		`gbm-driver-libs source directory .* must start with \$SNAP/ or \$\{SNAP\}/`)

	slot = MockSlot(c, `name: gbm-provider
version: 0
slots:
  gbm:
    client-driver: nvidia-drm_gbm.so
    compatibility: gbmbackend-(0..2)-arch64-ubuntu-2404
    interface: gbm-driver-libs
`, nil, "gbm")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		`snap "gbm-provider" does not have attribute "source" for interface "gbm-driver-libs"`)

	slot = MockSlot(c, `name: gbm-provider
version: 0
slots:
  gbm:
    interface: gbm-driver-libs
    client-driver: nvidia-drm_gbm.so
    compatibility: gbmbackend-(0..2)-arch64-ubuntu-2404
    source: $SNAP/lib1
`, nil, "gbm")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		`snap "gbm-provider" has interface "gbm-driver-libs" with invalid value type string for "source" attribute: \*\[\]string`)

	slot = MockSlot(c, `name: gbm-provider
version: 0
slots:
  gbm:
    compatibility: gbmbackend-(0..2)-arch64
    interface: gbm-driver-libs
    source:
      - $SNAP/lib1
`, nil, "gbm")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		`invalid client-driver: snap "gbm-provider" does not have attribute "client-driver" for interface "gbm-driver-libs"`)

	slot = MockSlot(c, `name: gbm-provider
version: 0
slots:
  gbm:
    interface: gbm-driver-libs
    client-driver: libnvidia@-allocator.so.1
    compatibility: gbmbackend-(0..2)-arch64-ubuntu-2404
    source:
      - $SNAP/lib1
`, nil, "gbm")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		`invalid client-driver name: libnvidia@-allocator.so.1`)

	slot = MockSlot(c, `name: gbm-provider
version: 0
slots:
  gbm:
    interface: gbm-driver-libs
    client-driver: /abs/path/libnvidia-allocator.so.1
    compatibility: gbmbackend-(0..2)-arch64-ubuntu-2404
    source:
      - $SNAP/lib1
`, nil, "gbm")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		`client-driver value "/abs/path/libnvidia-allocator.so.1" should be a file`)

	slot = MockSlot(c, `name: gbm-provider
version: 0
slots:
  gbm:
    interface: gbm-driver-libs
    compatibility: gbmbackend-(0..2)-arch64-ubuntu-2404
    client-driver:
      - libnvidia-allocator.so.1
    source:
      - $SNAP/lib1
`, nil, "gbm")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		`invalid client-driver: snap "gbm-provider" has interface "gbm-driver-libs" with invalid value type \[\]interface {} for "client-driver" attribute: \*string`)

	slot = MockSlot(c, `name: gbm-provider
version: 0
slots:
  gbm:
    interface: gbm-driver-libs
    client-driver: nvidia-drm_gbm.so
    source:
      - $SNAP/lib1
`, nil, "gbm")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		`snap "gbm-provider" does not have attribute "compatibility" for interface "gbm-driver-libs"`)

	slot = MockSlot(c, `name: gbm-provider
version: 0
slots:
  gbm:
    interface: gbm-driver-libs
    compatibility: foo-(0..2)-arch64-ubuntu-2404
    client-driver: nvidia-drm_gbm.so
    source:
      - $SNAP/lib1
`, nil, "gbm")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		`compatibility label "foo-\(0..2\)-arch64-ubuntu-2404": string does not match interface spec \(foo != gbmbackend\)`)

	slot = MockSlot(c, `name: gbm-provider
version: 0
slots:
  gbm:
    interface: gbm-driver-libs
    compatibility: gbmbackend-(0..2)-arch64-1-ubuntu-2404
    client-driver: nvidia-drm_gbm.so
    source:
      - $SNAP/lib1
`, nil, "gbm")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		`compatibility label "gbmbackend-\(0..2\)-arch64-1-ubuntu-2404": range \(1..1\) is not included in valid range \(0..0\)`)
}

func (s *GbmDriverLibsInterfaceSuite) TestSanitizeArch32Slot(c *C) {
	slot := MockSlot(c, `name: gbm-provider
version: 0
slots:
  gbm:
    interface: gbm-driver-libs
    client-driver: nvidia-drm_gbm.so
    compatibility: gbmbackend-(0..2)-arch32-ubuntu-2404
    source:
      - $SNAP/lib1
`, nil, "gbm")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), IsNil)
}

func (s *GbmDriverLibsInterfaceSuite) TestSanitizePlug(c *C) {
	c.Check(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
	c.Check(interfaces.BeforeConnectPlug(s.iface, s.plug), IsNil)
}

func (s *GbmDriverLibsInterfaceSuite) TestLdconfigSpec(c *C) {
	spec := &ldconfig.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Check(spec.LibDirs(), DeepEquals, map[ldconfig.SnapSlot][]string{
		{SnapName: "gbm-provider", SlotName: "gbm-driver-libs"}: {
			filepath.Join(dirs.GlobalRootDir, "snap/gbm-provider/5/lib1"),
			filepath.Join(dirs.GlobalRootDir, "snap/gbm-provider/5/lib2")}})
}

func (s *GbmDriverLibsInterfaceSuite) TestSymlinksSpec(c *C) {
	spec := &symlinks.Specification{}
	snapSourceDir := filepath.Join(s.testRoot, "snap/gbm-provider/5/lib2")
	targetPath := filepath.Join(snapSourceDir, "nvidia-drm_gbm.so")
	c.Assert(os.MkdirAll(snapSourceDir, 0755), IsNil)
	c.Assert(os.WriteFile(targetPath, []byte{}, 0644), IsNil)

	dir := fmt.Sprintf("/usr/lib/%s-linux-gnu/gbm", osutil.MachineName())
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Check(spec.Symlinks(), DeepEquals, map[string]symlinks.SymlinkToTarget{
		dir: {
			"nvidia-drm_gbm.so": targetPath,
		},
	})
}

func (s *GbmDriverLibsInterfaceSuite) TestSymlinksSpecNoClient(c *C) {
	spec := &symlinks.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), ErrorMatches,
		`"nvidia-drm_gbm\.so" not found in the source directories`)
}

func (s *GbmDriverLibsInterfaceSuite) TestSymlinksSpecNoClientDriver(c *C) {
	spec := &symlinks.Specification{}
	slot, _ := MockConnectedSlot(c, `name: gbm-provider
version: 0
slots:
  gbm:
    compatibility: gbmbackend-(0..2)-arch64
    interface: gbm-driver-libs
    source:
      - $SNAP/lib1
`, nil, "gbm")
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, slot), ErrorMatches,
		`invalid client-driver: snap "gbm-provider" does not have attribute "client-driver" for interface "gbm-driver-libs"`)
}

func (s *GbmDriverLibsInterfaceSuite) TestTrackedDirectories(c *C) {
	symlinksUser := builtin.SymlinksUserIfaceFromGbmIface(s.iface)
	c.Assert(symlinksUser.TrackedDirectories(), DeepEquals, []string{
		fmt.Sprintf("/usr/lib/%s-linux-gnu/gbm", osutil.MachineName())})
}

func (s *GbmDriverLibsInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, false)
	c.Assert(si.ImplicitOnClassic, Equals, false)
	c.Assert(si.ImplicitPlugOnCore, Equals, false)
	c.Assert(si.ImplicitPlugOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows exposing GBM driver libraries to the system`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "gbm-driver-libs")
}

func (s *GbmDriverLibsInterfaceSuite) TestAutoConnect(c *C) {
	c.Assert(s.iface.AutoConnect(s.plugInfo, s.slotInfo), Equals, true)
}

func (s *GbmDriverLibsInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
