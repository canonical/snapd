// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration

/*
 * Copyright (C) 2015 Canonical Ltd
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

package build

import (
	"path/filepath"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/snap/snaptest"

	"github.com/snapcore/snapd/integration-tests/testutils/data"
)

const snapFilenameSufix = "_1.0_all.snap"

// LocalSnap builds a snap and returns the path of the generated file
func LocalSnap(c *check.C, snapName string) (snapPath string, err error) {
	// build basic snap and check output
	buildPath := buildPath(snapName)

	return snaptest.BuildSquashfsSnap(buildPath, buildPath)
}

var baseSnapPath = data.BaseSnapPath

func buildPath(snap string) string {
	return filepath.Join(baseSnapPath, snap)
}
