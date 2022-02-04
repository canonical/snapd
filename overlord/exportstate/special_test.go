// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package exportstate_test

import (
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/exportstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/snapdtool"
	"github.com/snapcore/snapd/testutil"
)

type specialSuite struct {
	testutil.BaseTest
}

var _ = Suite(&specialSuite{})

func (s *specialSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })
}

func (s *specialSuite) TestNewManifestForHostWithDefaultSnapMountDir(c *C) {
	s.AddCleanup(release.MockReleaseInfo(&release.OS{ID: "ubuntu"}))
	s.AddCleanup(release.MockOnClassic(true))

	m := exportstate.NewManifestForHost()
	c.Check(m.SnapInstanceName, Equals, "")
	c.Check(m.SnapRevision, Equals, snap.R(0))
	c.Check(m.ExportedForSnapdAsVersion, Equals, "host")
	c.Check(m.SourceIsHost, Equals, true)
	c.Assert(m.Sets, HasLen, 1)
	c.Assert(m.Sets["tools"].Exports, HasLen, 9)
	s.checkSnapExecFromHost(c, m.Sets["tools"].Exports["snap-exec"])
}

func (s *specialSuite) TestNewManifestForHostWithAltSnapMountDir(c *C) {
	s.AddCleanup(release.MockReleaseInfo(&release.OS{ID: "fedora"}))
	s.AddCleanup(release.MockOnClassic(true))

	m := exportstate.NewManifestForHost()
	c.Check(m.SnapInstanceName, Equals, "")
	c.Check(m.SnapRevision, Equals, snap.R(0))
	c.Check(m.ExportedForSnapdAsVersion, Equals, "host")
	c.Check(m.SourceIsHost, Equals, true)
	c.Assert(m.Sets, HasLen, 1)
	c.Assert(m.Sets["tools"].Exports, HasLen, 9)
	s.checkSnapExecFromHost(c, m.Sets["tools"].Exports["snap-exec"])
}

func (s *specialSuite) checkSnapExecFromHost(c *C, exported exportstate.ExportedFile) {
	c.Check(exported.Name, Equals, "snap-exec")
	c.Check(exported.SourcePath, Equals, filepath.Join(dirs.DistroLibExecDir, "snap-exec"))
}

func (s *specialSuite) TestNewManifestForSnapdSnap(c *C) {
	snapdInfo := snaptest.MockInfo(c, snapdYaml, &snap.SideInfo{Revision: snap.R(2)})
	m := exportstate.NewManifestForSnap(snapdInfo)
	c.Check(m.SnapInstanceName, Equals, "snapd")
	c.Check(m.SnapRevision, Equals, snap.R(2))
	c.Check(m.ExportedForSnapdAsVersion, Equals, "")
	c.Check(m.SourceIsHost, Equals, false)
	c.Assert(m.Sets["tools"].Exports, HasLen, 9)
	s.checkSnapExecFromSnap(c, m.Sets["tools"].Exports["snap-exec"])
}

func (s *specialSuite) TestNewManifestForCoreSnap(c *C) {
	coreInfo := snaptest.MockInfo(c, coreYaml, &snap.SideInfo{Revision: snap.R(3)})
	m := exportstate.NewManifestForSnap(coreInfo)
	c.Check(m.SnapInstanceName, Equals, "core")
	c.Check(m.SnapRevision, Equals, snap.R(3))
	c.Check(m.SourceIsHost, Equals, false)
	c.Check(m.ExportedForSnapdAsVersion, Equals, "core_3")
	c.Assert(m.Sets["tools"].Exports, HasLen, 9)
	s.checkSnapExecFromSnap(c, m.Sets["tools"].Exports["snap-exec"])
}

// XXX: maybe this should be in snapdtool instead?
func mockIsReexec(isReexec bool) (restore func()) {
	var dir string
	if isReexec {
		dir = dirs.SnapMountDir
	} else {
		dir = "/no/reexec/path"
	}
	return snapdtool.MockOsReadlink(func(string) (string, error) {
		return dir, nil
	})
}

func (s *specialSuite) checkSnapExecFromSnap(c *C, exported exportstate.ExportedFile) {
	c.Check(exported.Name, Equals, "snap-exec")
	c.Check(exported.SourcePath, Equals, "/usr/lib/snapd/snap-exec")
}

func (s *specialSuite) TestSelectExportedVersionForSnapdTools(c *C) {
	snapdInfo := snaptest.MockInfo(c, "name: snapd\nversion: 1\ntype: snapd\n", &snap.SideInfo{Revision: snap.R(2)})
	coreInfo := snaptest.MockInfo(c, "name: core\nversion: 1\ntype: os\n", &snap.SideInfo{Revision: snap.R(1)})
	snapdInfoBroken := snaptest.MockInfo(c, "name: snapd\nversion: 1\ntype: snapd\n", &snap.SideInfo{Revision: snap.R(2)})
	snapdInfoBroken.Broken = "totally"
	coreInfoBroken := snaptest.MockInfo(c, "name: core\nversion: 1\ntype: os\n", &snap.SideInfo{Revision: snap.R(1)})
	coreInfoBroken.Broken = "totally"

	onClassic := true
	onCore := false
	noError := ""
	isReexec, noReexec := true, false

	for _, tc := range []struct {
		snapdInfo *snap.Info
		coreInfo  *snap.Info
		classic   bool
		reexec    bool

		expectedVersion string
		expectedErr     string
	}{
		// When both snapd and core are present, snapd dominates.
		{snapdInfo, coreInfo, onClassic, isReexec, "2", noError},
		// When either only snapd or core is present, it is used.
		{snapdInfo, nil, onClassic, isReexec, "2", noError},
		{nil, coreInfo, onClassic, isReexec, "core_1", noError},
		// Broken versions are ignored
		{snapdInfoBroken, coreInfo, onClassic, isReexec, "core_1", noError},
		{snapdInfoBroken, coreInfoBroken, onClassic, isReexec, "host", noError},
		// On classic systems when neither snap is present, host tools are used.
		{nil, nil, onClassic, isReexec, "host", noError},
		// On core this cannot happen but we check
		{nil, nil, onCore, isReexec, "", "internal error: cannot find snapd tooling to export"},
		// On classic systems with disabled re-exec host wins over snaps.
		{snapdInfo, coreInfo, onClassic, noReexec, "host", noError},
		// On core systems disabling re-exec has no effect
		{snapdInfo, coreInfo, onCore, noReexec, "2", noError},
	} {
		s.AddCleanup(exportstate.MockSnapStateCurrentInfo(func(st *state.State, snapName string) (*snap.Info, error) {
			switch snapName {
			case "core":
				return tc.coreInfo, nil
			case "snapd":
				return tc.snapdInfo, nil
			default:
				panic("unexpected")
			}
		}))
		s.AddCleanup(release.MockOnClassic(tc.classic))
		s.AddCleanup(mockIsReexec(tc.reexec))

		exportedVersion, err := exportstate.EffectiveExportedVersionForSnapdOrCore(nil)
		if tc.expectedErr != "" {
			c.Check(err, ErrorMatches, tc.expectedErr, Commentf("%v", tc))
		} else {
			c.Check(err, IsNil, Commentf("%v", tc))
		}
		c.Check(exportedVersion, Equals, tc.expectedVersion, Commentf("%v", tc))
	}
}
