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

package tests

import (
	"io/ioutil"
	"net/http"
	"os/exec"

	"launchpad.net/snappy/_integration-tests/testutils/common"

	"gopkg.in/check.v1"
)

// make sure that there are no collisions
const port = "9999"

var _ = check.Suite(&snapdTestSuite{})

type snapdTestSuite struct {
	common.SnappySuite
}

func (s *snapdTestSuite) SetUpSuite(c *check.C) {
	cmd := exec.Command("/lib/systemd/systemd-activate",
		"-l", "127.0.0.1:"+port, "snapd")

	cmd.Start()
}

func (s *snapdTestSuite) TearDownSuite(c *check.C) {
	// TODO: kill the service
}

func (s *snapdTestSuite) TestServiceIsUp(c *check.C) {
	resp, err := http.Get("http://127.0.0.1:" + port)
	c.Assert(err, check.IsNil)
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	c.Assert(err, check.IsNil)

	expected := `{"metadata":["/1.0"],"status":"OK","status_code":200,"type":"sync"}`
	c.Assert(string(body), check.Equals, expected)
}
