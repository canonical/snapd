// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2020 Canonical Ltd
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
	"fmt"
	"net/http"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/daemon"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/hookstate/ctlcmd"
)

var _ = check.Suite(&snapctlSuite{})

type snapctlSuite struct {
	apiBaseSuite
}

func (s *snapctlSuite) TestSnapctlGetNoUID(c *check.C) {
	s.daemon(c)
	buf := bytes.NewBufferString(`{"context-id": "some-context", "args": ["get", "something"]}`)
	req, err := http.NewRequest("POST", "/v2/snapctl", buf)
	c.Assert(err, check.IsNil)
	rsp := s.req(c, req, nil).(*daemon.Resp)
	c.Assert(rsp.Status, check.Equals, 403)
}

func (s *snapctlSuite) TestSnapctlForbiddenError(c *check.C) {
	s.daemon(c)

	defer daemon.MockUcrednetGet(func(string) (int32, uint32, string, error) {
		return 100, 9999, dirs.SnapSocket, nil
	})()

	defer daemon.MockCtlcmdRun(func(ctx *hookstate.Context, arg []string, uid uint32) ([]byte, []byte, error) {
		return nil, nil, &ctlcmd.ForbiddenCommandError{}
	})()

	buf := bytes.NewBufferString(fmt.Sprintf(`{"context-id": "some-context", "args": [%q, %q]}`, "set", "foo=bar"))
	req, err := http.NewRequest("POST", "/v2/snapctl", buf)
	c.Assert(err, check.IsNil)
	rsp := s.req(c, req, nil).(*daemon.Resp)
	c.Assert(rsp.Status, check.Equals, 403)
}

func (s *snapctlSuite) TestSnapctlUnsuccesfulError(c *check.C) {
	s.daemon(c)

	defer daemon.MockUcrednetGet(func(string) (int32, uint32, string, error) {
		return 100, 9999, dirs.SnapSocket, nil
	})()

	defer daemon.MockCtlcmdRun(func(ctx *hookstate.Context, arg []string, uid uint32) ([]byte, []byte, error) {
		return nil, nil, &ctlcmd.UnsuccessfulError{ExitCode: 123}
	})()

	buf := bytes.NewBufferString(fmt.Sprintf(`{"context-id": "some-context", "args": [%q, %q]}`, "is-connected", "plug"))
	req, err := http.NewRequest("POST", "/v2/snapctl", buf)
	c.Assert(err, check.IsNil)
	rsp := s.req(c, req, nil).(*daemon.Resp)
	c.Check(rsp.Status, check.Equals, 200)
	c.Check(rsp.Result.(*daemon.ErrorResult).Kind, check.Equals, client.ErrorKindUnsuccessful)
	c.Check(rsp.Result.(*daemon.ErrorResult).Value, check.DeepEquals, map[string]interface{}{
		"stdout":    "",
		"stderr":    "",
		"exit-code": 123,
	})
}
