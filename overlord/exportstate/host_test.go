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
	"github.com/snapcore/snapd/testutil"
)

type hostSuite struct {
	testutil.BaseTest
	rootfs string
}

var _ = Suite(&hostSuite{})

func (s *hostSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.rootfs = c.MkDir()
	dirs.SetRootDir(s.rootfs)
	s.AddCleanup(func() { dirs.SetRootDir("") })
}

func (s *hostSuite) TestExportManifestForHostSystem(c *C) {
	s.AddCleanup(release.MockOnClassic(true))
	c.Assert(exportstate.ExportManifestForHostSystem(), NotNil)

	s.AddCleanup(release.MockOnClassic(false))
	c.Assert(exportstate.ExportManifestForHostSystem(), IsNil)
}

func (s *hostSuite) checkSnapExec(c *C, snapExec exportstate.ExportEntry) {
	c.Check(snapExec.PathInExportSet(), Equals, "snap-exec")
	c.Check(snapExec.PathInHostMountNS(), Equals, filepath.Join(dirs.DistroLibExecDir, "snap-exec"))
	c.Check(snapExec.PathInSnapMountNS(), Equals, filepath.Join("/var/lib/snapd/hostfs", dirs.DistroLibExecDir, "snap-exec"))
	c.Check(snapExec.IsExportedPathValidInHostMountNS(), Equals, false)
}

func (s *hostSuite) TestExportManifestForClassicSystemWithDefaultSnapMountDir(c *C) {
	s.AddCleanup(release.MockReleaseInfo(&release.OS{ID: "ubuntu"}))
	manifest := exportstate.ExportManifestForClassicSystem()
	c.Assert(manifest, NotNil)
	c.Check(manifest.PrimaryKey, Equals, "snapd")
	c.Check(manifest.SubKey, Equals, "host")
	tools, ok := manifest.ExportSets["tools"]
	c.Assert(ok, Equals, true)
	c.Assert(tools, HasLen, 1)
	s.checkSnapExec(c, tools[0])
}

func (s *hostSuite) TestExportManifestForClassicSystemWithAltSnapMountDir(c *C) {
	s.AddCleanup(release.MockReleaseInfo(&release.OS{ID: "fedora"}))
	manifest := exportstate.ExportManifestForClassicSystem()
	c.Assert(manifest, NotNil)
	c.Check(manifest.PrimaryKey, Equals, "snapd")
	c.Check(manifest.SubKey, Equals, "host")
	tools, ok := manifest.ExportSets["tools"]
	c.Assert(ok, Equals, true)
	c.Assert(tools, HasLen, 1)
	s.checkSnapExec(c, tools[0])
}
