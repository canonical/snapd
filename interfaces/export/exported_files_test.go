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

package export_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces/export"
	"github.com/snapcore/snapd/osutil"
)

type exportedFilesSuite struct {
	spec *export.Specification
}

var _ = Suite(&exportedFilesSuite{})

func (s *exportedFilesSuite) SetUpTest(c *C) {
	s.spec = &export.Specification{}
}

func (s *exportedFilesSuite) TestAddExportedFileIsRecorded(c *C) {
	state := &osutil.MemoryFileState{Content: []byte("hello"), Mode: 0644}
	err := s.spec.AddExportedFile("egl-driver-libs", "foo_egl-slot_5",
		"egl_vendor.d/15_snap_foo_egl-slot_egl.d-vendor.json", state)
	c.Assert(err, IsNil)

	c.Check(s.spec.Files(), DeepEquals, map[string]map[string]map[string]osutil.FileState{
		"egl-driver-libs": {
			"foo_egl-slot_5": {
				"egl_vendor.d/15_snap_foo_egl-slot_egl.d-vendor.json": state,
			},
		},
	})
}

func (s *exportedFilesSuite) TestAddExportedFileMultipleInterfacesUnitsFiles(c *C) {
	state1 := &osutil.MemoryFileState{Content: []byte("1")}
	state2 := &osutil.MemoryFileState{Content: []byte("2")}
	state3 := &osutil.MemoryFileState{Content: []byte("3")}

	c.Assert(s.spec.AddExportedFile("egl-driver-libs", "foo_egl-slot_5",
		"egl_vendor.d/a.json", state1), IsNil)
	c.Assert(s.spec.AddExportedFile("egl-driver-libs", "foo_egl-slot_5+comp1_11",
		"egl_vendor.d/b.json", state2), IsNil)
	c.Assert(s.spec.AddExportedFile("vulkan-driver-libs", "foo_vulkan-slot_5",
		"icd.d/c.json", state3), IsNil)

	files := s.spec.Files()
	c.Check(files, HasLen, 2)
	c.Check(files["egl-driver-libs"], HasLen, 2)
	c.Check(files["egl-driver-libs"]["foo_egl-slot_5"]["egl_vendor.d/a.json"], Equals, osutil.FileState(state1))
	c.Check(files["egl-driver-libs"]["foo_egl-slot_5+comp1_11"]["egl_vendor.d/b.json"], Equals, osutil.FileState(state2))
	c.Check(files["vulkan-driver-libs"]["foo_vulkan-slot_5"]["icd.d/c.json"], Equals, osutil.FileState(state3))
}

func (s *exportedFilesSuite) TestAddExportedFileDuplicateIsError(c *C) {
	state := &osutil.MemoryFileState{Content: []byte("hello")}
	c.Assert(s.spec.AddExportedFile("egl-driver-libs", "foo_egl-slot_5",
		"egl_vendor.d/a.json", state), IsNil)

	err := s.spec.AddExportedFile("egl-driver-libs", "foo_egl-slot_5",
		"egl_vendor.d/a.json", state)
	c.Assert(err, ErrorMatches, `export internal error: already declared file: "foo_egl-slot_5/egl_vendor.d/a.json"`)
}

func (s *exportedFilesSuite) TestAddExportedFileSameRelPathDifferentUnitIsFine(c *C) {
	// The same relative path in two different units is not a collision -
	// they belong to different containers (e.g. two components both
	// shipping a file with the same base name).
	state1 := &osutil.MemoryFileState{Content: []byte("1")}
	state2 := &osutil.MemoryFileState{Content: []byte("2")}
	c.Assert(s.spec.AddExportedFile("egl-driver-libs", "foo_egl-slot_5",
		"egl_vendor.d/a.json", state1), IsNil)
	c.Assert(s.spec.AddExportedFile("egl-driver-libs", "foo_egl-slot_5+comp1_11",
		"egl_vendor.d/a.json", state2), IsNil)
}

func (s *exportedFilesSuite) TestAddExportedFileInvalidInterfaceName(c *C) {
	state := &osutil.MemoryFileState{}
	for _, ifaceName := range []string{"", "with/slash"} {
		err := s.spec.AddExportedFile(ifaceName, "unit", "sub/file.json", state)
		c.Check(err, ErrorMatches, `export internal error: invalid interface name: .*`)
	}
}

func (s *exportedFilesSuite) TestAddExportedFileInvalidUnitName(c *C) {
	state := &osutil.MemoryFileState{}
	for _, unit := range []string{"", "with/slash"} {
		err := s.spec.AddExportedFile("egl-driver-libs", unit, "sub/file.json", state)
		c.Check(err, ErrorMatches, `export internal error: invalid unit name: .*`)
	}
}

func (s *exportedFilesSuite) TestAddExportedFileInvalidRelPath(c *C) {
	state := &osutil.MemoryFileState{}
	for _, relPath := range []string{
		"/abs/path.json",   // absolute
		"sub/../path.json", // unclean
		"sub/",             // unclean (trailing slash)
	} {
		err := s.spec.AddExportedFile("egl-driver-libs", "unit", relPath, state)
		c.Check(err, ErrorMatches, `export internal error: unclean or absolute path: .*`, Commentf("relPath: %q", relPath))
	}
}

func (s *exportedFilesSuite) TestAddExportedFileRelPathMustHaveSubdirectory(c *C) {
	state := &osutil.MemoryFileState{}
	err := s.spec.AddExportedFile("egl-driver-libs", "unit", "onlyfile.json", state)
	c.Assert(err, ErrorMatches, `export internal error: path must be inside a subdirectory: "onlyfile.json"`)
}

func (s *exportedFilesSuite) TestFilesNilWhenEmpty(c *C) {
	c.Check(s.spec.Files(), IsNil)
}
