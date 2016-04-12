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
	"fmt"
	"path/filepath"
	"regexp"

	"gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/integration-tests/testutils/cli"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/data"
)

const snapFilenameSufix = "_1.0_all.snap"

var (
	// dependency aliasing
	cliExecCommand = cli.ExecCommand
)

func buildSnap(c *check.C, snapPath string) string {
	return cliExecCommand(c, "snap", "build", snapPath, "-o", snapPath)
}

// LocalSnap issues the command to build a snap and returns the path of the generated file
func LocalSnap(c *check.C, snapName string) (snapPath string, err error) {
	// build basic snap and check output
	buildPath := buildPath(snapName)

	buildOutput := buildSnap(c, buildPath)
	snapName = snapName + snapFilenameSufix

	path := filepath.Join(buildPath, snapName)

	expected := fmt.Sprintf("(?ms).*Generated '%s' snap\n$", path)
	matched, err := regexp.MatchString(expected, buildOutput)
	if err != nil {
		return "", err
	}
	if !matched {
		return "", fmt.Errorf("Error building snap, expected output %s, obtained %s",
			expected, buildOutput)
	}
	return path, nil
}

func buildPath(snap string) string {
	return filepath.Join(data.BaseSnapPath, snap)
}
