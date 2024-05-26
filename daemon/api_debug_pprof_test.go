// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2021 Canonical Ltd
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

package daemon_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"

	"github.com/ddkwork/golibrary/mylog"
	"gopkg.in/check.v1"
)

var _ = check.Suite(&pprofDebugSuite{})

type pprofDebugSuite struct {
	apiBaseSuite
}

func (s *pprofDebugSuite) TestGetPprofCmdline(c *check.C) {
	s.daemon(c)

	req := mylog.Check2(http.NewRequest("GET", "/v2/debug/pprof/cmdline", nil))
	c.Assert(err, check.IsNil)
	// as root
	s.asRootAuth(req)

	rr := httptest.NewRecorder()
	s.serveHTTP(c, rr, req)

	rsp := rr.Result()
	c.Assert(rsp, check.NotNil)

	c.Assert(rsp.StatusCode, check.Equals, 200)
	data := mylog.Check2(io.ReadAll(rsp.Body))
	c.Assert(err, check.IsNil)

	cmdline := mylog.Check2(os.ReadFile("/proc/self/cmdline"))
	c.Assert(err, check.IsNil)
	cmdline = bytes.TrimRight(cmdline, "\x00")
	c.Assert(string(data), check.DeepEquals, string(cmdline))
}
