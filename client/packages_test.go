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
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"

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

func (cs *clientSuite) TestClientFilterSnaps(c *check.C) {
	filterTests := []struct {
		filter client.SnapFilter
		path   string
		query  string
	}{
		{client.SnapFilter{}, "/2.0/snaps", ""},
		{client.SnapFilter{Sources: []string{"local"}}, "/2.0/snaps", "sources=local"},
		{client.SnapFilter{Sources: []string{"store"}}, "/2.0/snaps", "sources=store"},
		{client.SnapFilter{Sources: []string{"local", "store"}}, "/2.0/snaps", "sources=local%2Cstore"},
		{client.SnapFilter{Types: []string{"app"}}, "/2.0/snaps", "types=app"},
		{client.SnapFilter{Types: []string{"app", "framework"}}, "/2.0/snaps", "types=app%2Cframework"},
		{client.SnapFilter{Sources: []string{"local"}, Types: []string{"app"}}, "/2.0/snaps", "sources=local&types=app"},
		{client.SnapFilter{Query: "foo"}, "/2.0/snaps", "q=foo"},
		{client.SnapFilter{Query: "foo", Sources: []string{"local"}, Types: []string{"app"}}, "/2.0/snaps", "q=foo&sources=local&types=app"},
	}

	for _, tt := range filterTests {
		_, _ = cs.cli.FilterSnaps(tt.filter)
		c.Check(cs.req.URL.Path, check.Equals, tt.path, check.Commentf("%v", tt.filter))
		c.Check(cs.req.URL.RawQuery, check.Equals, tt.query, check.Commentf("%v", tt.filter))
	}
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

func (cs *clientSuite) TestClientRemoveSnapServerError(c *check.C) {
	cs.err = errors.New("fail")
	_, err := cs.cli.RemoveSnap(pkgName)
	c.Assert(err, check.ErrorMatches, `.*fail`)
}

func (cs *clientSuite) TestClientRemoveSnapResponseError(c *check.C) {
	cs.rsp = `{"type": "error", "status": "potatoes"}`
	_, err := cs.cli.RemoveSnap(pkgName)
	c.Assert(err, check.ErrorMatches, `.*server error: "potatoes"`)
}

func (cs *clientSuite) TestClientRemoveSnapBadType(c *check.C) {
	cs.rsp = `{"type": "what"}`
	_, err := cs.cli.RemoveSnap(pkgName)
	c.Assert(err, check.ErrorMatches, `.*expected async response, got "what"`)
}

func (cs *clientSuite) TestClientRemoveSnapNotAccepted(c *check.C) {
	cs.rsp = `{
		"status_code": 200,
		"type": "async"
	}`
	_, err := cs.cli.RemoveSnap(pkgName)
	c.Assert(err, check.ErrorMatches, `.*operation not accepted`)
}

func (cs *clientSuite) TestClientRemoveSnapInvalidResult(c *check.C) {
	cs.rsp = `{
		"result": "not a JSON object",
		"status_code": 202,
		"type": "async"
	}`
	_, err := cs.cli.RemoveSnap(pkgName)
	c.Assert(err, check.ErrorMatches, `.*failed to unmarshal operation.*`)
}

func (cs *clientSuite) TestClientRemoveSnapNoResource(c *check.C) {
	cs.rsp = `{
		"result": {},
		"status_code": 202,
		"type": "async"
	}`
	_, err := cs.cli.RemoveSnap(pkgName)
	c.Assert(err, check.ErrorMatches, `.*operation has no resource`)
}

func (cs *clientSuite) TestClientRemoveSnapInvalidResource(c *check.C) {
	cs.rsp = `{
		"result": {
			"resource": "invalid"
		},
		"status_code": 202,
		"type": "async"
	}`
	_, err := cs.cli.RemoveSnap(pkgName)
	c.Assert(err, check.ErrorMatches, `.*invalid resource`)
}

func (cs *clientSuite) TestClientRemoveSnap(c *check.C) {
	cs.rsp = `{
		"result": {
			"resource": "/2.0/operations/5a70dffa-66b3-3567-d728-55b0da48bdc7"
		},
		"status_code": 202,
		"type": "async"
	}`
	uuid, err := cs.cli.RemoveSnap(pkgName)

	body, err := ioutil.ReadAll(cs.req.Body)
	c.Assert(err, check.IsNil)
	jsonBody := make(map[string]string)
	err = json.Unmarshal(body, &jsonBody)
	c.Assert(err, check.IsNil)
	c.Assert(jsonBody["action"], check.Equals, "remove")

	c.Assert(cs.req.Method, check.Equals, "POST")
	c.Assert(cs.req.URL.Path, check.Equals, fmt.Sprintf("/2.0/snaps/%s", pkgName))
	c.Assert(err, check.IsNil)
	c.Assert(uuid, check.Equals, "5a70dffa-66b3-3567-d728-55b0da48bdc7")
}
