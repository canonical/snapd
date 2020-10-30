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
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
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

func (s *specialSuite) checkSnapExecFromSnap(c *C, exported exportstate.ExportedFile) {
	c.Check(exported.Name, Equals, "snap-exec")
	c.Check(exported.SourcePath, Equals, "/usr/lib/snapd/snap-exec")
}
