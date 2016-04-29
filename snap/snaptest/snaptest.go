// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

// Package snaptest contains helper functions for mocking snaps.
package snaptest

import (
	"io/ioutil"
	"os"
	"path/filepath"

	"gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/snap"
)

// MockSnap puts a snap.yaml file on disk so to mock an installed snap, based on the provided arguments.
//
// The caller is responsible for mocking root directory with dirs.SetRootDir()
// and for altering the overlord state if required.
func MockSnap(c *check.C, yamlText string, sideInfo *snap.SideInfo) *snap.Info {
	c.Assert(sideInfo, check.Not(check.IsNil))

	// Parse the yaml (we need the Name).
	snapInfo, err := snap.InfoFromSnapYaml([]byte(yamlText))
	c.Assert(err, check.IsNil)

	// Set SideInfo so that we can use MountDir below
	snapInfo.SideInfo = *sideInfo

	// Put the YAML on disk, in the right spot.
	metaDir := filepath.Join(snapInfo.MountDir(), "meta")
	err = os.MkdirAll(metaDir, 0755)
	c.Assert(err, check.IsNil)
	err = ioutil.WriteFile(filepath.Join(metaDir, "snap.yaml"), []byte(yamlText), 0644)
	c.Assert(err, check.IsNil)

	return snapInfo
}
