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
	"github.com/snapcore/snapd/overlord/exportstate"

	. "gopkg.in/check.v1"
)

type testExportEntry struct {
	pathInExportSet                  string
	pathInHostMountNS                string
	pathInSnapMountNS                string
	isExportedPathValidInHostMountNS bool
}

func (tee *testExportEntry) PathInExportSet() string {
	return tee.pathInExportSet
}

func (tee *testExportEntry) PathInHostMountNS() string {
	return tee.pathInHostMountNS
}

func (tee *testExportEntry) PathInSnapMountNS() string {
	return tee.pathInSnapMountNS
}

func (tee *testExportEntry) IsExportedPathValidInHostMountNS() bool {
	return tee.isExportedPathValidInHostMountNS
}

type manifestSuite struct {
	manifest *exportstate.ExportManifest
}

var _ = Suite(&manifestSuite{
	manifest: &exportstate.ExportManifest{
		PrimaryKey: "primary",
		SubKey:     "sub",
		ExportSets: map[exportstate.ExportSetName][]exportstate.ExportEntry{
			"export-set": []exportstate.ExportEntry{
				&testExportEntry{},
			},
		},
	},
})

func (s *manifestSuite) TestPutOnDisk(c *C) {
	err := s.manifest.PutOnDisk()
	c.Assert(err, ErrorMatches, ".* not implemented")
}

func (s *manifestSuite) TestRemoveFromDisk(c *C) {
	err := s.manifest.RemoveFromDisk()
	c.Assert(err, ErrorMatches, ".* not implemented")
}
