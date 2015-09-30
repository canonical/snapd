// -*- Mode: Go; indent-tabs-mode: t -*-

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
	"fmt"
	"path/filepath"

	"gopkg.in/check.v1"
	"launchpad.net/snappy/_integration-tests/testutils/cli"
)

const (
	// BaseSnapPath is the path for the snap sources used in testing
	BaseSnapPath = "_integration-tests/data/snaps"
	// BasicSnapName is the name of the basic snap
	BasicSnapName = "basic"
	// WrongYamlSnapName is the name of a snap with an invalid meta yaml
	WrongYamlSnapName = "wrong-yaml"
	// MissingReadmeSnapName is the name of a snap without readme
	MissingReadmeSnapName = "missing-readme"

	snapFilenameSufix = "_1.0_all.snap"
)

var (
	// dependency aliasing
	cliExecCommand = cli.ExecCommand
)

func buildSnap(c *check.C, snapPath string) string {
	return cliExecCommand(c, "snappy", "build", snapPath, "-o", snapPath)
}

// LocalSnap issues the command to build a snap and returns the path of the generated file
func LocalSnap(c *check.C, snapName string) (snapPath string, err error) {
	// build basic snap and check output
	buildPath := buildPath(snapName)

	buildOutput := buildSnap(c, buildPath)
	snapName = snapName + snapFilenameSufix

	path := filepath.Join(buildPath, snapName)
	expected := fmt.Sprintf("Generated '%s' snap\n", path)
	if buildOutput != expected {
		return "", fmt.Errorf("Error building snap, expected output %s, obtained %s",
			expected, buildOutput)
	}
	return path, nil
}

func buildPath(snap string) string {
	return filepath.Join(BaseSnapPath, snap)
}
