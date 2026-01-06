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

type NvidiaVideoDriverLibsInterfaceSuite struct {
	testutil.BaseTest

	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

var _ = Suite(&NvidiaVideoDriverLibsInterfaceSuite{
	iface: builtin.MustInterface("nvidia-video-driver-libs"),
})

// This is in fact implicit in the system
const nvidiaVideoDriverLibsConsumerYaml = `name: snapd
version: 0
plugs:
  nvidia-video:
    compatibility: nvidia-video-(6..12)-(0..2)-ubuntu-2404
    interface: nvidia-video-driver-libs
apps:
  app:
    plugs: [nvidia-video]
`

const nvidiaVideoDriverLibsProvider = `name: nvidia-video-provider
version: 0
slots:
  nvidia-video-slot:
    interface: nvidia-video-driver-libs
    compatibility: nvidia-video-(6..12)-(0..2)-ubuntu-2404
    library-source:
      - $SNAP/lib1
      - ${SNAP}/lib2
`

func (s *NvidiaVideoDriverLibsInterfaceSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.plug, s.plugInfo = MockConnectedPlug(c, nvidiaVideoDriverLibsConsumerYaml,
		&snap.SideInfo{Revision: snap.R(3)}, "nvidia-video")
	s.slot, s.slotInfo = MockConnectedSlot(c, nvidiaVideoDriverLibsProvider,
		&snap.SideInfo{Revision: snap.R(5)}, "nvidia-video-slot")
}

func (s *NvidiaVideoDriverLibsInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "nvidia-video-driver-libs")
}

func (s *NvidiaVideoDriverLibsInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *NvidiaVideoDriverLibsInterfaceSuite) TestSanitizeSlotError(c *C) {
	slot := MockSlot(c, `name: nvidia-video-provider
version: 0
slots:
  nvidia-video:
    interface: nvidia-video-driver-libs
    compatibility: nvidia-video-(6..12)-(0..2)-ubuntu-2404
    library-source:
      - /snap/nvidia-video-provider/current/lib1
`, nil, "nvidia-video")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		`nvidia-video-driver-libs library-source directory .* must start with \$SNAP/ or \$\{SNAP\}/`)

	slot = MockSlot(c, `name: nvidia-video-provider
version: 0
slots:
  nvidia-video:
    interface: nvidia-video-driver-libs
    compatibility: nvidia-video-(6..12)-(0..2)-ubuntu-2404
`, nil, "nvidia-video")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		`snap "nvidia-video-provider" does not have attribute "library-source" for interface "nvidia-video-driver-libs"`)

	slot = MockSlot(c, `name: nvidia-video-provider
version: 0
slots:
  nvidia-video:
    interface: nvidia-video-driver-libs
    compatibility: nvidia-video-(6..12)-(0..2)-ubuntu-2404
    library-source: $SNAP/lib1
`, nil, "nvidia-video")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		`snap "nvidia-video-provider" has interface "nvidia-video-driver-libs" with invalid value type string for "library-source" attribute: \*\[\]string`)

	slot = MockSlot(c, `name: nvidia-video-provider
version: 0
slots:
  nvidia-video:
    interface: nvidia-video-driver-libs
    library-source:
      - $SNAP/lib1
`, nil, "nvidia-video")
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		`snap "nvidia-video-provider" does not have attribute "compatibility" for interface "nvidia-video-driver-libs"`)
}

func (s *NvidiaVideoDriverLibsInterfaceSuite) TestSanitizeSlotAPIversion(c *C) {
	for _, tt := range []struct {
		versRange string
		err       string
	}{
		{"nvidia-video-6-3-ubuntu-2404", ""},
		{"nvidia-video-6-(0..3)-ubuntu-2510", ""},
		{"nvidia-video-5-2", `compatibility label "nvidia-video-5-2": unexpected number of strings \(should be 3\)`},
		{"other-9", `compatibility label "other-9": unexpected number of strings \(should be 3\)`},
		{"nvidia-video-2-ubuntu-2510", `compatibility label "nvidia-video-2-ubuntu-2510": unexpected number of integers \(should be 2 for "video"\)`},
		{"nvidia-video 5", `compatibility label "nvidia-video 5": while parsing: unexpected rune: 5`},
	} {
		slot := MockSlot(c, fmt.Sprintf(`name: nvidia-video-provider
version: 0
slots:
  nvidia-video:
    interface: nvidia-video-driver-libs
    compatibility: '%s'
    library-source:
      - $SNAP/lib1
`, tt.versRange), nil, "nvidia-video")
		err := interfaces.BeforePrepareSlot(s.iface, slot)
		if tt.err == "" {
			c.Check(err, IsNil)
		} else {
			c.Check(err, ErrorMatches, tt.err, Commentf("case %q", tt.versRange))
		}
	}
}

func (s *NvidiaVideoDriverLibsInterfaceSuite) TestSanitizePlug(c *C) {
	c.Check(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
	c.Check(interfaces.BeforeConnectPlug(s.iface, s.plug), IsNil)
}

func (s *NvidiaVideoDriverLibsInterfaceSuite) TestLdconfigSpec(c *C) {
	spec := &ldconfig.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Check(spec.LibDirs(), DeepEquals, map[ldconfig.SnapSlot][]string{
		{SnapName: "nvidia-video-provider", SlotName: "nvidia-video-slot"}: {filepath.Join(dirs.SnapMountDir, "nvidia-video-provider/5/lib1"),
			filepath.Join(dirs.SnapMountDir, "nvidia-video-provider/5/lib2")}})
}

func (s *NvidiaVideoDriverLibsInterfaceSuite) TestConfigfilesSpec(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	spec := &configfiles.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Check(spec.PathContent(), DeepEquals, map[string]osutil.FileState{
		"/var/lib/snapd/export/system_nvidia-video-provider_nvidia-video-slot_nvidia-video-driver-libs.library-source": &osutil.MemoryFileState{
			Content: []byte(
				filepath.Join(dirs.SnapMountDir, "nvidia-video-provider/5/lib1") + "\n" +
					filepath.Join(dirs.SnapMountDir, "nvidia-video-provider/5/lib2") + "\n"),
			Mode: 0644},
	})
}

func (s *NvidiaVideoDriverLibsInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, false)
	c.Assert(si.ImplicitOnClassic, Equals, false)
	c.Assert(si.ImplicitPlugOnCore, Equals, false)
	c.Assert(si.ImplicitPlugOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows exposing Nvidia video decoding/encoding driver libraries to the system`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "nvidia-video-driver-libs")
}

func (s *NvidiaVideoDriverLibsInterfaceSuite) TestAutoConnect(c *C) {
	c.Assert(s.iface.AutoConnect(s.plugInfo, s.slotInfo), Equals, true)
}

func (s *NvidiaVideoDriverLibsInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
