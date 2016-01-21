// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2016 Canonical Ltd
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
	"fmt"

	"gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/client"
)

func (cs *clientSuite) TestClientSnapsCallsEndpoint(c *check.C) {
	_, _ = cs.cli.Snaps()
	c.Check(cs.req.Method, check.Equals, "GET")
	c.Check(cs.req.URL.Path, check.Equals, "/2.0/snaps")
}

func (cs *clientSuite) TestClientSnapsResultJSONHasNoSnaps(c *check.C) {
	cs.rsp = `{
		"type": "sync",
		"result": {}
	}`
	_, err := cs.cli.Snaps()
	c.Check(err, check.ErrorMatches, `.*no snaps`)
}

func (cs *clientSuite) TestClientSnapsInvalidSnapsJSON(c *check.C) {
	cs.rsp = `{
		"type": "sync",
		"result": {
			"snaps": "not a list of snaps"
		}
	}`
	_, err := cs.cli.Snaps()
	c.Check(err, check.ErrorMatches, `.*failed to unmarshal snaps.*`)
}

func (cs *clientSuite) TestClientSnaps(c *check.C) {
	cs.rsp = `{
		"type": "sync",
		"result": {
			"snaps": {
				"hello-world.canonical": {
					"description": "hello-world",
					"download_size": 22212,
					"icon": "https://myapps.developer.ubuntu.com/site_media/appmedia/2015/03/hello.svg_NZLfWbh.png",
					"installed_size": -1,
					"name": "hello-world",
					"origin": "canonical",
					"resource": "/2.0/snaps/hello-world.canonical",
					"status": "not installed",
					"type": "app",
					"version": "1.0.18"
				}
			}
		}
	}`
	applications, err := cs.cli.Snaps()
	c.Check(err, check.IsNil)
	c.Check(applications, check.DeepEquals, map[string]*client.Snap{
		"hello-world.canonical": &client.Snap{
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

const (
	pkgName = "chatroom.ogra"
)

func (cs *clientSuite) TestClientSnap(c *check.C) {
	cs.rsp = `{
		"type": "sync",
		"result": {
			"description": "WebRTC Video chat server for Snappy",
			"download_size": 6930947,
			"icon": "/2.0/icons/chatroom.ogra/icon",
			"installed_size": 18976651,
			"name": "chatroom",
			"origin": "ogra",
			"resource": "/2.0/snaps/chatroom.ogra",
			"status": "active",
			"type": "app",
			"vendor": "",
			"version": "0.1-8"
		}
	}`
	pkg, err := cs.cli.Snap(pkgName)
	c.Assert(cs.req.Method, check.Equals, "GET")
	c.Assert(cs.req.URL.Path, check.Equals, fmt.Sprintf("/2.0/snaps/%s", pkgName))
	c.Assert(err, check.IsNil)
	c.Assert(pkg, check.DeepEquals, &client.Snap{
		Description:   "WebRTC Video chat server for Snappy",
		DownloadSize:  6930947,
		Icon:          "/2.0/icons/chatroom.ogra/icon",
		InstalledSize: 18976651,
		Name:          "chatroom",
		Origin:        "ogra",
		Status:        client.StatusActive,
		Type:          client.TypeApp,
		Version:       "0.1-8",
	})
}
