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

	"gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/client"
)

func (cs *clientSuite) TestClientPackagesCallsEndpoint(c *check.C) {
	_, _ = cs.cli.Packages()
	c.Check(cs.req.Method, check.Equals, "GET")
	c.Check(cs.req.URL.Path, check.Equals, "/1.0/packages")
}

func (cs *clientSuite) TestClientPackagesHttpError(c *check.C) {
	cs.err = errors.New("fail")
	_, err := cs.cli.Packages()
	c.Check(err, check.ErrorMatches, ".*server: fail")
}

func (cs *clientSuite) TestClientPackagesResponseError(c *check.C) {
	cs.rsp = `{
		"result": {},
		"status": "Bad Request",
		"status_code": 400,
		"type": "error"
	}`
	_, err := cs.cli.Packages()
	c.Check(err, check.ErrorMatches, `server error: "Bad Request"`)
}

func (cs *clientSuite) TestClientPackagesInvalidResponseType(c *check.C) {
	cs.rsp = `{"type": "async"}`
	_, err := cs.cli.Packages()
	c.Check(err, check.ErrorMatches, `.*expected sync response.*`)
}

func (cs *clientSuite) TestClientPackagesInvalidResultJSON(c *check.C) {
	cs.rsp = `{
		"type": "sync",
		"result": "not a JSON object"
	}`
	_, err := cs.cli.Packages()
	c.Check(err, check.ErrorMatches, `.*failed to unmarshal response.*`)
}

func (cs *clientSuite) TestClientPackagesResultJSONHasNoPackages(c *check.C) {
	cs.rsp = `{
		"type": "sync",
		"result": {}
	}`
	_, err := cs.cli.Packages()
	c.Check(err, check.ErrorMatches, `.*no packages`)
}

func (cs *clientSuite) TestClientPackagesInvalidPackagesJSON(c *check.C) {
	cs.rsp = `{
		"type": "sync",
		"result": {
			"packages": "not a list of packages"
		}
	}`
	_, err := cs.cli.Packages()
	c.Check(err, check.ErrorMatches, `.*failed to unmarshal packages.*`)
}

func (cs *clientSuite) TestClientPackages(c *check.C) {
	cs.rsp = `{
		"type": "sync",
		"result": {
			"packages": {
				"hello-world.canonical": {
					"description": "hello-world",
					"download_size": "22212",
					"icon": "https://myapps.developer.ubuntu.com/site_media/appmedia/2015/03/hello.svg_NZLfWbh.png",
					"installed_size": "-1",
					"name": "hello-world",
					"origin": "canonical",
					"resource": "/1.0/packages/hello-world.canonical",
					"status": "not installed",
					"type": "app",
					"version": "1.0.18"
				}
			}
		}
	}`
	applications, err := cs.cli.Packages()
	c.Check(err, check.IsNil)
	c.Check(applications, check.DeepEquals, map[string]client.Package{
		"hello-world.canonical": client.Package{
			Description:   "hello-world",
			DownloadSize:  22212,
			Icon:          "https://myapps.developer.ubuntu.com/site_media/appmedia/2015/03/hello.svg_NZLfWbh.png",
			InstalledSize: -1,
			Name:          "hello-world",
			Origin:        "canonical",
			Status:        client.StatusNotInstalled,
			Type:          client.TypeApp,
			Version:       "1.0.18",
		},
	})
}
