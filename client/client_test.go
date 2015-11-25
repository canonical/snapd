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

package client_test

import (
	"errors"
	"io/ioutil"
	"net/http"
	"strings"
	"testing"

	"gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/client"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { check.TestingT(t) }

type clientSuite struct {
	cli *client.Client
	req *http.Request
	rsp string
	err error
}

var _ = check.Suite(&clientSuite{})

func (cs *clientSuite) SetUpTest(c *check.C) {
	cs.cli = client.New()
	cs.cli.SetDoer(cs)
	cs.err = nil
	cs.rsp = ""
	cs.req = nil
}

func (cs *clientSuite) Do(req *http.Request) (*http.Response, error) {
	cs.req = req
	rsp := &http.Response{Body: ioutil.NopCloser(strings.NewReader(cs.rsp))}
	return rsp, cs.err
}

func (cs *clientSuite) TestClientDoReportsErrors(c *check.C) {
	cs.err = errors.New("ouchie")
	err := cs.cli.Do("GET", "/", nil, nil)
	c.Check(err, check.Equals, cs.err)
}

func (cs *clientSuite) TestClientWorks(c *check.C) {
	var v []int
	cs.rsp = `[1,2]`
	reqBody := ioutil.NopCloser(strings.NewReader(""))
	err := cs.cli.Do("GET", "/this", reqBody, &v)
	c.Check(err, check.IsNil)
	c.Check(v, check.DeepEquals, []int{1, 2})
	c.Assert(cs.req, check.NotNil)
	c.Assert(cs.req.URL, check.NotNil)
	c.Check(cs.req.Method, check.Equals, "GET")
	c.Check(cs.req.Body, check.Equals, reqBody)
	c.Check(cs.req.URL.Path, check.Equals, "/this")
}

func (cs *clientSuite) TestClientSysInfo(c *check.C) {
	cs.rsp = `{"type": "sync", "result":
                     {"flavor": "f",
                      "release": "r",
                      "default_channel": "dc",
                      "api_compat": "42",
                      "store": "store"}}`
	sysInfo, err := cs.cli.SysInfo()
	c.Check(err, check.IsNil)
	c.Check(sysInfo, check.DeepEquals, &client.SysInfo{
		Flavor:           "f",
		Release:          "r",
		DefaultChannel:   "dc",
		APICompatibility: "42",
		Store:            "store",
	})
}

func (cs *clientSuite) TestClientReportsOpError(c *check.C) {
	cs.rsp = `{"type": "error", "status": "potatoes"}`
	_, err := cs.cli.SysInfo()
	c.Check(err, check.ErrorMatches, `failed with "potatoes"`)
}

func (cs *clientSuite) TestClientReportsBadType(c *check.C) {
	cs.rsp = `{"type": "what"}`
	_, err := cs.cli.SysInfo()
	c.Check(err, check.ErrorMatches, `unexpected result type "what"`)
}

func (cs *clientSuite) TestClientReportsOuterJSONError(c *check.C) {
	cs.rsp = "this isn't really json is it"
	_, err := cs.cli.SysInfo()
	c.Check(err, check.ErrorMatches, `invalid character .*`)
}

func (cs *clientSuite) TestClientReportsInnerJSONError(c *check.C) {
	cs.rsp = `{"type": "sync", "result": "this isn't really json is it"}`
	_, err := cs.cli.SysInfo()
	c.Check(err, check.ErrorMatches, `bad sysinfo result .*`)
}

func (cs *clientSuite) TestClientGetCapabilities(c *check.C) {
	cs.rsp = `{
		"type": "sync",
		"result": [
		{
			"name": "n",
			"label": "l",
			"type": "t",
			"attrs": {"k": "v"}
		}
		]
	}`
	caps, err := cs.cli.GetCapabilities()
	c.Check(err, check.IsNil)
	c.Check(caps, check.DeepEquals, []client.Capability{{
		Name:  "n",
		Label: "l",
		Type:  "t",
		Attrs: map[string]string{"k": "v"},
	}})
}
