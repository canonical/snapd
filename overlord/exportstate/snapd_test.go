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

type snapdSuite struct {
	testutil.BaseTest
	rootfs    string
	snapdSnap *snap.Info
	coreSnap  *snap.Info
}

var _ = Suite(&snapdSuite{})

const snapdYaml = `
name: snapd
version: 1
type: snapd
`

const coreYaml = `
name: core
version: 1
type: os
`

func (s *snapdSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.rootfs = c.MkDir()
	dirs.SetRootDir(s.rootfs)
	s.AddCleanup(func() { dirs.SetRootDir("") })
	s.snapdSnap = snaptest.MockInfo(c, snapdYaml, &snap.SideInfo{
		Revision: snap.Revision{N: 1},
	})
	s.coreSnap = snaptest.MockInfo(c, coreYaml, &snap.SideInfo{
		Revision: snap.Revision{N: 2},
	})
}

func (s *snapdSuite) checkSnapExec(c *C, snapExec exportstate.ExportEntry, info *snap.Info) {
	c.Check(snapExec.PathInExportSet(), Equals, "snap-exec")
	c.Check(snapExec.PathInHostMountNS(), Equals, filepath.Join(
		dirs.SnapMountDir, info.SnapName(), info.Revision.String(), "usr/lib/snapd/snap-exec"))
	c.Check(snapExec.PathInSnapMountNS(), Equals, filepath.Join(
		"/snap", info.SnapName(), info.Revision.String(), "usr/lib/snapd/snap-exec"))
	c.Check(snapExec.IsExportedPathValidInHostMountNS(), Equals, false)
}

func (s *snapdSuite) TestExportManifestSnapdSnapWithDefaultSnapMountDir(c *C) {
	s.AddCleanup(release.MockReleaseInfo(&release.OS{ID: "ubuntu"}))
	manifest := exportstate.ExportManifestForSnap(s.snapdSnap)
	c.Assert(manifest, NotNil)
	c.Check(manifest.PrimaryKey, Equals, "snapd")
	c.Check(manifest.SubKey, Equals, "1")
	tools, ok := manifest.ExportSets["tools"]
	c.Assert(ok, Equals, true)
	c.Assert(tools, HasLen, 1)
	s.checkSnapExec(c, tools[0], s.snapdSnap)
}

func (s *snapdSuite) TestExportManifestForSnapdSnapWithAltSnapMountDir(c *C) {
	s.AddCleanup(release.MockReleaseInfo(&release.OS{ID: "fedora"}))
	manifest := exportstate.ExportManifestForSnap(s.snapdSnap)
	c.Assert(manifest, NotNil)
	c.Check(manifest.PrimaryKey, Equals, "snapd")
	c.Check(manifest.SubKey, Equals, "1")
	tools, ok := manifest.ExportSets["tools"]
	c.Assert(ok, Equals, true)
	c.Assert(tools, HasLen, 1)
	s.checkSnapExec(c, tools[0], s.snapdSnap)
}

func (s *snapdSuite) TestExportManifestCoreSnapWithDefaultSnapMountDir(c *C) {
	s.AddCleanup(release.MockReleaseInfo(&release.OS{ID: "ubuntu"}))
	manifest := exportstate.ExportManifestForSnap(s.coreSnap)
	c.Assert(manifest, NotNil)
	c.Check(manifest.PrimaryKey, Equals, "snapd")
	c.Check(manifest.SubKey, Equals, "core_2")
	tools, ok := manifest.ExportSets["tools"]
	c.Assert(ok, Equals, true)
	c.Assert(tools, HasLen, 1)
	s.checkSnapExec(c, tools[0], s.coreSnap)
}

func (s *snapdSuite) TestExportManifestForCoreSnapWithAltSnapMountDir(c *C) {
	s.AddCleanup(release.MockReleaseInfo(&release.OS{ID: "fedora"}))
	manifest := exportstate.ExportManifestForSnap(s.coreSnap)
	c.Assert(manifest, NotNil)
	c.Check(manifest.PrimaryKey, Equals, "snapd")
	c.Check(manifest.SubKey, Equals, "core_2")
	tools, ok := manifest.ExportSets["tools"]
	c.Assert(ok, Equals, true)
	c.Assert(tools, HasLen, 1)
	s.checkSnapExec(c, tools[0], s.coreSnap)
}
