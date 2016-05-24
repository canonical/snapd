// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration

/*
 * Copyright (C) 2015, 2016 Canonical Ltd
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
	"path/filepath"

	"github.com/ubuntu-core/snappy/integration-tests/testutils/cli"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/common"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/data"

	"gopkg.in/check.v1"
)

var _ = check.Suite(&trySuite{})

type trySuite struct {
	common.SnappySuite
}

func (s *trySuite) TestTryBasicBinaries(c *check.C) {
	tryPath, err := filepath.Abs(filepath.Join(data.BaseSnapPath, data.BasicBinariesSnapName))
	c.Assert(err, check.IsNil)

	tryOutput := cli.ExecCommand(c, "sudo", "snap", "try", tryPath)
	defer common.RemoveSnap(c, data.BasicBinariesSnapName)

	expected := "(?ms)" +
		".*" +
		"Name +Version +Rev +Developer\n" +
		"basic-binaries +.*"
	c.Check(tryOutput, check.Matches, expected)

	// can run commands from the snap-try binary
	cli.ExecCommand(c, "basic-binaries.success")
}
