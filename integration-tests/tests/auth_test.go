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

package tests

import (
	"github.com/ubuntu-core/snappy/integration-tests/testutils/cli"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/common"
	"github.com/ubuntu-core/snappy/osutil"
	"github.com/ubuntu-core/snappy/testutil"

	"gopkg.in/check.v1"
)

var _ = check.Suite(&authSuite{})

type authSuite struct {
	common.SnappySuite
}

func (s *authSuite) SetUpTests(c *check.C) {
	s.SnappySuite.SetUpTest(c)

	if !osutil.FileExists("/usr/bin/curl") {
		c.Skip("this test needs curl to work")
	}
}

func (s *authSuite) TestRegressionAuthBypass(c *check.C) {
	// FIXME: port to http.chipaca once that is back in action
	cmd := []string{"curl",
		"-m", "5", // allow max 5 sec
		"--header",
		`Authorization: Macaroon root="made-up", discharge="data"`,
		"--header", "Content-Type:application/json",
		"--data", `{"action":"install"}`,
		"--silent",
		"--unix-socket", "/run/snapd.socket",
		"POST /v2/snaps/hello-world",
	}
	output := cli.ExecCommand(c, cmd...)
	c.Assert(output, testutil.Contains, `"status-code":403`)
}
