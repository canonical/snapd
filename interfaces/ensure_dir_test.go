// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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

package interfaces_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces"
)

type ensureDirSuite struct{}

var _ = Suite(&ensureDirSuite{})

func (s *ensureDirSuite) TestValidateHappy(c *C) {
	testMap := map[string]struct {
		EnsureDir interfaces.EnsureDirSpec
	}{
		"Abs1": {interfaces.EnsureDirSpec{MustExistDir: "/", EnsureDir: "/dir"}},
		"Abs2": {interfaces.EnsureDirSpec{MustExistDir: "/dir1", EnsureDir: "/dir1/dir2"}},
		"Env1": {interfaces.EnsureDirSpec{MustExistDir: "$HOME", EnsureDir: "$HOME/dir"}},
		"Env2": {interfaces.EnsureDirSpec{MustExistDir: "$HOME/dir1", EnsureDir: "$HOME/dir1/dir2"}},
	}

	for name, test := range testMap {
		c.Check(test.EnsureDir.Validate(), IsNil, Commentf("test: %q", name))
	}
}

func (s *ensureDirSuite) TestValidateErrors(c *C) {
	testMap := map[string]struct {
		EnsureDir interfaces.EnsureDirSpec
		ErrorMsg  string
	}{
		"Unclean1": {
			interfaces.EnsureDirSpec{MustExistDir: "", EnsureDir: "/dir"},
			`directory that must exist "" is not a clean path`,
		},
		"Unclean2": {
			interfaces.EnsureDirSpec{MustExistDir: "/dir", EnsureDir: "/../"},
			`directory to ensure "/../" is not a clean path`,
		},
		"BadEnv1": {
			interfaces.EnsureDirSpec{MustExistDir: "$SNAP_COMMON", EnsureDir: "$SNAP_COMMON/dir"},
			`directory that must exist "\$SNAP_COMMON" prefix "\$SNAP_COMMON" is not allowed`,
		},
		"BadEnv2": {
			interfaces.EnsureDirSpec{MustExistDir: "$HOME", EnsureDir: "$SNAP_COMMON/dir"},
			`directory to ensure "\$SNAP_COMMON/dir" prefix "\$SNAP_COMMON" is not allowed`,
		},
		"Relative1": {
			interfaces.EnsureDirSpec{MustExistDir: "dir1", EnsureDir: "$HOME/dir2"},
			`directory that must exist "dir1" is not an absolute path`,
		},
		"Relative2": {
			interfaces.EnsureDirSpec{MustExistDir: "$HOME", EnsureDir: "dir1/dir2"},
			`directory to ensure "dir1/dir2" is not an absolute path`,
		},
		"BadParent1": {
			interfaces.EnsureDirSpec{MustExistDir: "$HOME", EnsureDir: "$HOME"},
			`directory that must exist "\$HOME" is not a parent of directory to ensure "\$HOME"`,
		},
		"BadParent2": {
			interfaces.EnsureDirSpec{MustExistDir: "/dir1", EnsureDir: "/dir2/dir1"},
			`directory that must exist "/dir1" is not a parent of directory to ensure "/dir2/dir1"`,
		},
	}

	for name, test := range testMap {
		c.Check(test.EnsureDir.Validate(), ErrorMatches, test.ErrorMsg, Commentf("test: %q", name))
	}
}
