// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration

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

package tests

import (
	"fmt"
	"path/filepath"
	"regexp"

	"github.com/snapcore/snapd/integration-tests/testutils/cli"
	"github.com/snapcore/snapd/integration-tests/testutils/common"

	"gopkg.in/check.v1"
)

const nothingPendingErrorTpl = "error: cannot abort change %s with nothing pending\n"

var _ = check.Suite(&abortSuite{})

type abortSuite struct {
	common.SnappySuite
}

// SNAP_ABORT_001: --help - print detailed help text for the abort command
func (s *abortSuite) TestAbortShowHelp(c *check.C) {
	expected := "(?ms)" +
		"^Usage:\n" +
		`  snap \[OPTIONS\] abort.*\n` +
		"\n^The abort command .*\n" +
		"^Help Options:\n" +
		"^  -h, --help +Show this help message\n" +
		".*"

	actual := cli.ExecCommand(c, "snap", "abort", "--help")

	c.Assert(actual, check.Matches, expected)
}

// SNAP_ABORT_002: with invalid id
func (s *abortSuite) TestAbortWithInvalidId(c *check.C) {
	id := "10000000"

	expected := fmt.Sprintf(`error: cannot find change with id "%s"\n`, id)
	actual, err := cli.ExecCommandErr("sudo", "snap", "abort", id)

	c.Assert(err, check.NotNil)
	c.Assert(actual, check.Matches, expected)
}

// SNAP_ABORT_004: with valid id - error
func (s *abortSuite) TestAbortWithValidIdInErrorStatus(c *check.C) {
	snapName := "hello-world"

	provokeTaskError(c, snapName)

	id := getErrorID(snapName)
	c.Assert(id, check.Not(check.Equals), "")

	expected := fmt.Sprintf(nothingPendingErrorTpl, id)
	actual, err := cli.ExecCommandErr("sudo", "snap", "abort", id)

	c.Assert(err, check.NotNil)
	c.Assert(actual, check.Matches, expected)
}

// SNAP_ABORT_006: with valid id - done
func (s *abortSuite) TestAbortWithValidIdInDoneStatus(c *check.C) {
	snapName := "hello-world"
	common.InstallSnap(c, snapName)
	defer common.RemoveSnap(c, snapName)

	id := getDoneInstallID(snapName)
	c.Assert(id, check.Not(check.Equals), "")

	expected := fmt.Sprintf(nothingPendingErrorTpl, id)
	actual, err := cli.ExecCommandErr("sudo", "snap", "abort", id)

	c.Assert(err, check.NotNil)
	c.Assert(actual, check.Matches, expected)
}

func provokeTaskError(c *check.C, snapName string) {
	// make snap uninstallable
	subdirPath := filepath.Join("/snap", snapName, "current", "foo")
	_, err := cli.ExecCommandErr("sudo", "mkdir", "-p", subdirPath)
	c.Assert(err, check.IsNil)
	defer cli.ExecCommand(c, "sudo", "rm", "-rf", filepath.Dir(subdirPath))

	// try to install snap and see it fail
	_, err = cli.ExecCommandErr("sudo", "snap", "install", snapName)
	c.Assert(err, check.NotNil)
}

func getErrorID(snapName string) string {
	pattern := fmt.Sprintf(` +Error.*Install "%s" snap`, snapName)
	return getID(pattern)
}

func getDoneInstallID(snapName string) string {
	pattern := fmt.Sprintf(` +Done +.*Install "%s" snap`, snapName)
	return getID(pattern)
}

func getDoingRemoveID(snapName string) string {
	pattern := fmt.Sprintf(` +Doing +.*Remove "%s" snap`, snapName)
	return getID(pattern)
}

func getAbortedRemoveID(snapName string) string {
	pattern := fmt.Sprintf(` +Abort +.*Remove "%s" snap`, snapName)
	return getID(pattern)
}

func getID(pattern string) string {
	output, err := cli.ExecCommandErr("snap", "changes")
	if err != nil && output == "error: no changes found\n" {
		return ""
	}

	completePattern := fmt.Sprintf(`(?msU).*\n(\d+)%s\n$`, pattern)
	result := regexp.MustCompile(completePattern).FindStringSubmatch(output)

	if result == nil || len(result) < 2 {
		return ""
	}
	return result[1]
}
