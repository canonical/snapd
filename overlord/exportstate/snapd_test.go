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

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/exportstate"
	"github.com/snapcore/snapd/snap"
	. "gopkg.in/check.v1"
)

type snapdSuite struct {
	// This gives us snapdInfo and coreInfo
	abstractManifestSuite
}

var _ = Suite(&snapdSuite{})

func (s *snapdSuite) TestExportedSnapToolsFromSnapdOrCore(c *C) {
	tools := exportstate.ExportedSnapToolsFromSnapdOrCore(s.snapdInfo)
	c.Assert(tools, HasLen, 8)
	s.checkSnapExec(c, tools[5], s.snapdInfo)

	tools = exportstate.ExportedSnapToolsFromSnapdOrCore(s.coreInfo)
	c.Assert(tools, HasLen, 8)
	s.checkSnapExec(c, tools[5], s.coreInfo)
}

func (s *snapdSuite) checkSnapExec(c *C, snapExec *exportstate.ExportEntry, info *snap.Info) {
	c.Check(snapExec.PathInExportSet, Equals, "snap-exec")
	c.Check(snapExec.PathInHostMountNS, Equals, filepath.Join(
		dirs.SnapMountDir, info.SnapName(), info.Revision.String(),
		"usr/lib/snapd/snap-exec"))
	c.Check(snapExec.PathInSnapMountNS, Equals, filepath.Join(
		"/snap", info.SnapName(), info.Revision.String(),
		"usr/lib/snapd/snap-exec"))
	c.Check(snapExec.IsExportedPathValidInHostMountNS, Equals, false)
}
