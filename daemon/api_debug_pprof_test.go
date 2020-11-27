// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2019 Canonical Ltd
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

package daemon

import (
	"bytes"
	"io/ioutil"
	"net/http"
	"net/http/httptest"

	"gopkg.in/check.v1"
)

var _ = check.Suite(&pprofDebugSuite{})

type pprofDebugSuite struct {
	APIBaseSuite
}

func (s *pprofDebugSuite) TestGetPprofCmdline(c *check.C) {
	req, err := http.NewRequest("GET", "/v2/debug/pprof/cmdline", nil)
	c.Assert(err, check.IsNil)

	rr := httptest.NewRecorder()
	getPprof(debugPprofCmd, req, nil).ServeHTTP(rr, req)

	rsp := rr.Result()
	c.Assert(rsp, check.NotNil)

	c.Assert(rsp.StatusCode, check.Equals, 200)
	data, err := ioutil.ReadAll(rsp.Body)
	c.Assert(err, check.IsNil)

	cmdline, err := ioutil.ReadFile("/proc/self/cmdline")
	c.Assert(err, check.IsNil)
	cmdline = bytes.TrimRight(cmdline, "\x00")
	c.Assert(string(data), check.DeepEquals, string(cmdline))
}
