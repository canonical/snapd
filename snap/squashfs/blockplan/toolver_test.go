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

package blockplan_test

import (
	"context"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/snap/squashfs/blockplan"
	"github.com/snapcore/snapd/testutil"
)

type toolverSuite struct {
	testutil.BaseTest
}

var _ = Suite(&toolverSuite{})

func (s *toolverSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	// Tools are looked for in the system snap first, so point that at an empty
	// tree: every probe in this file has to land on the mock on $PATH rather
	// than on whatever this machine happens to have installed.
	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })
}

// mockTool puts a stand-in for tool on $PATH for the rest of the test, so that
// a version -- and drift -- can be picked rather than inherited from the
// machine the tests run on.
func (s *toolverSuite) mockTool(c *C, tool, script string) *testutil.MockCmd {
	mock := testutil.MockCommand(c, tool, script)
	s.AddCleanup(mock.Restore)
	return mock
}

// versionComp answers ToolVersion and nothing else: SEC_TOOLVER is the only
// thing under test here, and it asks a compressor for exactly that.
type versionComp struct {
	blockplan.Compressor
	version string
}

func (v versionComp) ToolVersion() string { return v.version }

// TestToolVersionLine covers the probe itself: one line, named, and nothing that
// can fail a generate or an apply.
func (s *toolverSuite) TestToolVersionLine(c *C) {
	// hpatchz -v prints its banner and then its usage, and xz --version prints
	// two lines, so only the first one, trimmed, is the version.
	mock := s.mockTool(c, "hpatchz", `echo "  HDiffPatch::hpatchz v5.1.3  "; echo "usage: hpatchz ..."`)
	c.Check(blockplan.ToolVersionLine(context.Background(), "hpatchz", "-v"), Equals,
		"hpatchz: HDiffPatch::hpatchz v5.1.3")
	c.Check(mock.Calls(), DeepEquals, [][]string{{"hpatchz", "-v"}})

	// Everything that can go wrong yields no line at all, because a version
	// that cannot be established is not a reason to refuse anything.
	s.mockTool(c, "hpatchz", "echo boom >&2; exit 1")
	c.Check(blockplan.ToolVersionLine(context.Background(), "hpatchz", "-v"), Equals, "")

	s.mockTool(c, "hpatchz", "echo")
	c.Check(blockplan.ToolVersionLine(context.Background(), "hpatchz", "-v"), Equals, "")

	c.Check(blockplan.ToolVersionLine(context.Background(), "blockplan-no-such-tool", "-v"), Equals, "")
}

// TestCaptureToolVersions covers the section a generate ships: the patch pair
// this machine ran, and the library the blocks were compressed with.
func (s *toolverSuite) TestCaptureToolVersions(c *C) {
	mock := s.mockTool(c, "hdiffz", `echo "HDiffPatch::hdiffz v5.1.3"`)
	mock.Also("hpatchz", `echo "HDiffPatch::hpatchz v5.1.3"`)
	comp := versionComp{version: "xz: xz (XZ Utils) 5.4.5"}

	c.Check(string(blockplan.CaptureToolVersions(context.Background(), comp)), Equals,
		"hdiffz: HDiffPatch::hdiffz v5.1.3\n"+
			"hpatchz: HDiffPatch::hpatchz v5.1.3\n"+
			"xz: xz (XZ Utils) 5.4.5\n")
	c.Check(mock.Calls(), DeepEquals, [][]string{{"hdiffz", "-v"}, {"hpatchz", "-v"}})

	// A tool that cannot be probed contributes no line rather than an empty
	// one, so the section stays parseable by name.
	s.mockTool(c, "hdiffz", "exit 1")
	s.mockTool(c, "hpatchz", "exit 1")
	c.Check(string(blockplan.CaptureToolVersions(context.Background(), comp)), Equals,
		"xz: xz (XZ Utils) 5.4.5\n")

	// And when nothing at all can be probed the section is omitted, which is
	// what an applier that finds no SEC_TOOLVER reads as "no versions
	// recorded".
	c.Check(blockplan.CaptureToolVersions(context.Background(), versionComp{}), IsNil)
}

// TestCheckToolVersionsReportsDrift covers the whole point of the section: a
// tool that has moved since the delta was built is named, and the recorded
// version and the local one are both shown.
func (s *toolverSuite) TestCheckToolVersionsReportsDrift(c *C) {
	recorded := []byte("hdiffz: HDiffPatch::hdiffz v1.0.0\n" +
		"hpatchz: HDiffPatch::hpatchz v1.0.0\n" +
		"xz: xz (XZ Utils) 5.0.0\n")
	s.mockTool(c, "hpatchz", `echo "HDiffPatch::hpatchz v9.9.9"`)

	var out strings.Builder
	blockplan.CheckToolVersions(context.Background(), &out,
		recorded, versionComp{version: "xz: xz (XZ Utils) 9.9.9"})

	// Warnings come out in a fixed order, so an apply's log reads the same way
	// twice. hdiffz is recorded but no apply runs it, so drifting it says
	// nothing about this apply and must not be reported.
	c.Check(out.String(), Equals,
		`warning: hpatchz drifted since the delta was built: it used "HDiffPatch::hpatchz v1.0.0", this machine has "HDiffPatch::hpatchz v9.9.9"`+"\n"+
			`warning: xz drifted since the delta was built: it used "xz (XZ Utils) 5.0.0", this machine has "xz (XZ Utils) 9.9.9"`+"\n")
}

// TestCheckToolVersionsQuietWhenAligned is the case every ordinary apply hits:
// nothing has moved, so nothing is said.
func (s *toolverSuite) TestCheckToolVersionsQuietWhenAligned(c *C) {
	s.mockTool(c, "hpatchz", `echo "HDiffPatch::hpatchz v5.1.3"`)

	var out strings.Builder
	blockplan.CheckToolVersions(context.Background(), &out,
		[]byte("hpatchz: HDiffPatch::hpatchz v5.1.3\nzstd: 1.5.7\n"), versionComp{version: "zstd: 1.5.7"})
	c.Check(out.String(), Equals, "")
}

// TestCheckToolVersionsSurvivesWhatItCannotEstablish keeps the section
// advisory. Neither a tool this machine cannot probe nor a delta that carries no
// versions at all may produce a warning: both are silence about a version that
// was never established, not evidence of drift.
func (s *toolverSuite) TestCheckToolVersionsSurvivesWhatItCannotEstablish(c *C) {
	s.mockTool(c, "hpatchz", "exit 1")

	var out strings.Builder
	blockplan.CheckToolVersions(context.Background(), &out,
		[]byte("hpatchz: HDiffPatch::hpatchz v1.0.0\n"), versionComp{})
	c.Check(out.String(), Equals, "")

	blockplan.CheckToolVersions(context.Background(), &out, nil, versionComp{version: "xz: xz (XZ Utils) 5.4.5"})
	c.Check(out.String(), Equals, "")
}
