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
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

// helpersSuite exercises the naming primitives shared by classic's symlinks
// backend (symlinksForSourceDir) and the export backend
// (exportUnitAndFileName): both must agree on the identity of a given piece
// of content, so these are tested together against the same fixture.
type helpersSuite struct {
	testutil.BaseTest

	testRoot string
	slot     *interfaces.ConnectedSlot
}

var _ = Suite(&helpersSuite{})

const helpersProviderYaml = `name: helpers-provider
version: 0
slots:
  drv-slot:
    interface: egl-driver-libs
    priority: 15
    compatibility: egl-1-5-ubuntu-2404
    icd-source:
      - $SNAP/egl.d/
    library-source:
      - $SNAP/lib1
      - $SNAP_COMPONENT(comp1)/lib1
      - $SNAP_COMPONENT(comp2)/lib2
components:
  comp1:
    type: standard
  comp2:
    type: standard
`

func (s *helpersSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.testRoot = c.MkDir()
	dirs.SetRootDir(s.testRoot)
	s.AddCleanup(func() { dirs.SetRootDir("/") })

	comps := []compRawInfo{
		{"component: helpers-provider+comp1\ntype: standard", snap.R(11)},
		{"component: helpers-provider+comp2\ntype: standard", snap.R(22)},
	}
	s.slot, _ = mockConnectedSlotWithComps(c, helpersProviderYaml,
		&snap.SideInfo{Revision: snap.R(5)}, comps, "drv-slot")
}

// snapPath returns a path as if it belongs to the snap's own file tree
// (under $SNAP), for the snap revision used by s.slot.
func (s *helpersSuite) snapPath(rel string) string {
	return filepath.Join(dirs.SnapMountDir, "helpers-provider/5", rel)
}

// compPath returns a path as if it belongs to the given component's file
// tree, at the given component revision.
func (s *helpersSuite) compPath(comp string, rev snap.Revision, rel string) string {
	return filepath.Join(snap.ComponentMountDir(comp, rev, "helpers-provider"), rel)
}

func (s *helpersSuite) TestSourceDirEncodedNameSnapOnly(c *C) {
	p := builtin.NewPathWithDirIdx(s.snapPath("egl.d/vendor.json"), 10)

	name, component, componentRev, err := builtin.SourceDirEncodedName(p, s.slot, 15, true)
	c.Assert(err, IsNil)
	c.Check(name, Equals, "25_snap_helpers-provider_drv-slot_egl.d-vendor.json")
	c.Check(component, Equals, "")
	c.Check(componentRev, Equals, snap.Revision{})
}

func (s *helpersSuite) TestSourceDirEncodedNameNoPriority(c *C) {
	p := builtin.NewPathWithDirIdx(s.snapPath("egl.d/vendor.json"), 10)

	name, _, _, err := builtin.SourceDirEncodedName(p, s.slot, 15, false)
	c.Assert(err, IsNil)
	c.Check(name, Equals, "snap_helpers-provider_drv-slot_egl.d-vendor.json")
}

func (s *helpersSuite) TestSourceDirEncodedNameComponent(c *C) {
	p := builtin.NewPathWithDirIdx(s.compPath("comp1", snap.R(11), "egl.d/vendor.json"), 13)

	name, component, componentRev, err := builtin.SourceDirEncodedName(p, s.slot, 15, true)
	c.Assert(err, IsNil)
	c.Check(name, Equals, "28_snap_helpers-provider+comp1_drv-slot_egl.d-vendor.json")
	c.Check(component, Equals, "comp1")
	c.Check(componentRev, Equals, snap.R(11))
}

func (s *helpersSuite) TestSourceDirEncodedNameTwoComponentsDiffer(c *C) {
	p1 := builtin.NewPathWithDirIdx(s.compPath("comp1", snap.R(11), "egl.d/vendor.json"), 13)
	p2 := builtin.NewPathWithDirIdx(s.compPath("comp2", snap.R(22), "egl.d/vendor.json"), 14)

	name1, comp1, rev1, err := builtin.SourceDirEncodedName(p1, s.slot, 15, true)
	c.Assert(err, IsNil)
	name2, comp2, rev2, err := builtin.SourceDirEncodedName(p2, s.slot, 15, true)
	c.Assert(err, IsNil)

	c.Check(name1, Not(Equals), name2)
	c.Check(comp1, Not(Equals), comp2)
	c.Check(rev1, Not(Equals), rev2)
	c.Check(name1, Equals, "28_snap_helpers-provider+comp1_drv-slot_egl.d-vendor.json")
	c.Check(name2, Equals, "29_snap_helpers-provider+comp2_drv-slot_egl.d-vendor.json")
}

func (s *helpersSuite) TestSourceDirEncodedNameBadPath(c *C) {
	// A path with too few segments below dirs.SnapMountDir to contain a
	// snap instance name, revision, and file name.
	p := builtin.NewPathWithDirIdx(filepath.Join(dirs.SnapMountDir, "too-short"), 10)

	_, _, _, err := builtin.SourceDirEncodedName(p, s.slot, 15, true)
	c.Assert(err, ErrorMatches, "internal error: wrong file path: .*")
}

// TestExportFileNameMatchesSymlinkName is the key consistency check for the
// whole design: the export backend and classic's symlinks backend must
// derive the exact same file name for the exact same connection and file,
// so that identical content is named identically wherever it is delivered.
func (s *helpersSuite) TestExportFileNameMatchesSymlinkName(c *C) {
	paths := []struct {
		path   string
		dirIdx int
	}{
		{s.snapPath("egl.d/vendor.json"), 10},
		{s.compPath("comp1", snap.R(11), "egl.d/vendor.json"), 13},
		{s.compPath("comp2", snap.R(22), "egl.d/vendor.json"), 14},
	}

	for _, tc := range paths {
		p := builtin.NewPathWithDirIdx(tc.path, tc.dirIdx)

		symlinkName, _, _, err := builtin.SourceDirEncodedName(p, s.slot, 15, true)
		c.Assert(err, IsNil)

		_, exportFileName, err := builtin.ExportUnitAndFileName(p, s.slot, 15, true)
		c.Assert(err, IsNil)

		c.Check(exportFileName, Equals, symlinkName)
	}
}

func (s *helpersSuite) TestExportUnitAndFileNameSnapOnly(c *C) {
	p := builtin.NewPathWithDirIdx(s.snapPath("egl.d/vendor.json"), 10)

	unit, fileName, err := builtin.ExportUnitAndFileName(p, s.slot, 15, true)
	c.Assert(err, IsNil)
	c.Check(unit, Equals, "helpers-provider_drv-slot_5")
	c.Check(fileName, Equals, "25_snap_helpers-provider_drv-slot_egl.d-vendor.json")
}

func (s *helpersSuite) TestExportUnitAndFileNameComponent(c *C) {
	p := builtin.NewPathWithDirIdx(s.compPath("comp1", snap.R(11), "egl.d/vendor.json"), 13)

	unit, fileName, err := builtin.ExportUnitAndFileName(p, s.slot, 15, true)
	c.Assert(err, IsNil)
	c.Check(unit, Equals, "helpers-provider_drv-slot_5+comp1_11")
	c.Check(fileName, Equals, "28_snap_helpers-provider+comp1_drv-slot_egl.d-vendor.json")
}

// TestExportUnitAndFileNameMultipleComponentsAreSeparateUnits verifies that
// content from different components of the same snap/slot/connection ends
// up in distinct units, never aggregated into one - this is the whole point
// of per-container units (see export.UnitName), preserving set atomicity
// per container.
func (s *helpersSuite) TestExportUnitAndFileNameMultipleComponentsAreSeparateUnits(c *C) {
	p0 := builtin.NewPathWithDirIdx(s.snapPath("egl.d/vendor.json"), 10)
	p1 := builtin.NewPathWithDirIdx(s.compPath("comp1", snap.R(11), "egl.d/vendor.json"), 13)
	p2 := builtin.NewPathWithDirIdx(s.compPath("comp2", snap.R(22), "egl.d/vendor.json"), 14)

	unit0, _, err := builtin.ExportUnitAndFileName(p0, s.slot, 15, true)
	c.Assert(err, IsNil)
	unit1, _, err := builtin.ExportUnitAndFileName(p1, s.slot, 15, true)
	c.Assert(err, IsNil)
	unit2, _, err := builtin.ExportUnitAndFileName(p2, s.slot, 15, true)
	c.Assert(err, IsNil)

	c.Check(unit0, Not(Equals), unit1)
	c.Check(unit0, Not(Equals), unit2)
	c.Check(unit1, Not(Equals), unit2)
}
