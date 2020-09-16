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
}

var _ = Suite(&hostSuite{})

func (s *hostSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })
}

func (s *hostSuite) TestExportManifestForHostSystem(c *C) {
	s.AddCleanup(release.MockOnClassic(true))
	c.Assert(exportstate.HostAbstractManifest(), NotNil)

	s.AddCleanup(release.MockOnClassic(false))
	c.Assert(exportstate.HostAbstractManifest(), IsNil)
}

func (s *hostSuite) checkSnapExec(c *C, snapExec *exportstate.ExportEntry) {
	c.Check(snapExec.PathInExportSet, Equals, "snap-exec")
	c.Check(snapExec.PathInHostMountNS, Equals, filepath.Join(dirs.DistroLibExecDir, "snap-exec"))
	c.Check(snapExec.PathInSnapMountNS, Equals, filepath.Join("/var/lib/snapd/hostfs", dirs.DistroLibExecDir, "snap-exec"))
	c.Check(snapExec.IsExportedPathValidInHostMountNS, Equals, false)
}

func (s *hostSuite) TestExportManifestForClassicSystemWithDefaultSnapMountDir(c *C) {
	s.AddCleanup(release.MockReleaseInfo(&release.OS{ID: "ubuntu"}))
	am := exportstate.AbstractManifestForClassicSystem()
	c.Assert(am, NotNil)
	c.Check(am.PrimaryKey, Equals, "snapd")
	c.Check(am.SubKey, Equals, "host")
	tools, ok := am.ExportSets["tools"]
	c.Assert(ok, Equals, true)
	c.Assert(tools, HasLen, 8)
	s.checkSnapExec(c, tools[5])
}

func (s *hostSuite) TestExportManifestForClassicSystemWithAltSnapMountDir(c *C) {
	s.AddCleanup(release.MockReleaseInfo(&release.OS{ID: "fedora"}))
	am := exportstate.AbstractManifestForClassicSystem()
	c.Assert(am, NotNil)
	c.Check(am.PrimaryKey, Equals, "snapd")
	c.Check(am.SubKey, Equals, "host")
	tools, ok := am.ExportSets["tools"]
	c.Assert(ok, Equals, true)
	c.Assert(tools, HasLen, 8)
	s.checkSnapExec(c, tools[5])
}
