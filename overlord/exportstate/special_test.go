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
	c.Check(m.SnapName, Equals, "snapd")
	c.Check(m.ExportedVersion, Equals, "host")
	c.Assert(m.Symlinks, HasLen, 9)
	s.checkSnapExecFromHost(c, &m.Symlinks[4])
}

func (s *specialSuite) TestNewManifestForHostWithAltSnapMountDir(c *C) {
	s.AddCleanup(release.MockReleaseInfo(&release.OS{ID: "fedora"}))
	s.AddCleanup(release.MockOnClassic(true))

	m := exportstate.NewManifestForHost()
	c.Check(m.SnapName, Equals, "snapd")
	c.Check(m.ExportedVersion, Equals, "host")
	c.Assert(m.Symlinks, HasLen, 9)
	s.checkSnapExecFromHost(c, &m.Symlinks[4])
}

func (s *specialSuite) checkSnapExecFromHost(c *C, slink *exportstate.SymlinkExport) {
	c.Check(slink.SnapName, Equals, "snapd")
	c.Check(slink.ExportedVersion, Equals, "host")
	c.Check(slink.ExportSet, Equals, "tools")
	c.Check(slink.Name, Equals, "snap-exec")
	c.Check(slink.Target, Equals, filepath.Join("/var/lib/snapd/hostfs", dirs.DistroLibExecDir, "snap-exec"))
}

func (s *specialSuite) TestNewManifestForSnapdSnap(c *C) {
	snapdInfo := snaptest.MockInfo(c, snapdYaml, &snap.SideInfo{Revision: snap.Revision{N: 2}})
	m := exportstate.NewManifestForSnap(snapdInfo)
	c.Assert(m.Symlinks, HasLen, 9)
	s.checkSnapExecFromSnap(c, &m.Symlinks[4], snapdInfo)
}

func (s *specialSuite) TestNewManifestForCoreSnap(c *C) {
	coreInfo := snaptest.MockInfo(c, coreYaml, &snap.SideInfo{Revision: snap.Revision{N: 3}})
	m := exportstate.NewManifestForSnap(coreInfo)
	c.Assert(m.Symlinks, HasLen, 9)
	s.checkSnapExecFromSnap(c, &m.Symlinks[4], coreInfo)
}

func (s *specialSuite) checkSnapExecFromSnap(c *C, slink *exportstate.SymlinkExport, info *snap.Info) {
	c.Check(slink.SnapName, Equals, "snapd")
	// ExportedVersion varies by provider
	c.Check(slink.ExportSet, Equals, "tools")
	c.Check(slink.Name, Equals, "snap-exec")
	c.Check(slink.Target, Equals, filepath.Join(
		"/snap", info.SnapName(), info.Revision.String(),
		"usr/lib/snapd/snap-exec"))
}
