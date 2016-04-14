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
	"time"

	"gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/client"
)

func (cs *clientSuite) TestClientSnapsCallsEndpoint(c *check.C) {
	_, _, _ = cs.cli.Snaps()
	c.Check(cs.req.Method, check.Equals, "GET")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/snaps")
}

func (cs *clientSuite) TestClientSnapsInvalidSnapsJSON(c *check.C) {
	cs.rsp = `{
		"type": "sync",
		"result": "not a list of snaps"
	}`
	_, _, err := cs.cli.Snaps()
	c.Check(err, check.ErrorMatches, `.*cannot unmarshal.*`)
}

func (cs *clientSuite) TestClientSnaps(c *check.C) {
	cs.rsp = `{
		"type": "sync",
		"result": [{
			"summary": "salutation snap",
			"description": "hello-world",
			"download-size": 22212,
			"icon": "https://myapps.developer.ubuntu.com/site_media/appmedia/2015/03/hello.svg_NZLfWbh.png",
			"installed-size": -1,
			"name": "hello-world",
			"developer": "canonical",
			"resource": "/v2/snaps/hello-world.canonical",
			"status": "available",
			"type": "app",
			"version": "1.0.18"
		}],
		"suggested-currency": "GBP"
	}`
	applications, resultInfo, err := cs.cli.Snaps()
	c.Check(err, check.IsNil)
	c.Check(applications, check.DeepEquals, []*client.Snap{{
		Summary:       "salutation snap",
		Description:   "hello-world",
		DownloadSize:  22212,
		Icon:          "https://myapps.developer.ubuntu.com/site_media/appmedia/2015/03/hello.svg_NZLfWbh.png",
		InstalledSize: -1,
		Name:          "hello-world",
		Developer:     "canonical",
		Status:        client.StatusAvailable,
		Type:          client.TypeApp,
		Version:       "1.0.18",
	}})

	c.Check(resultInfo.SuggestedCurrency, check.Equals, "GBP")
}

func (cs *clientSuite) TestClientFilterSnaps(c *check.C) {
	filterTests := []struct {
		filter client.SnapFilter
		path   string
		query  string
	}{
		{client.SnapFilter{}, "/v2/snaps", ""},
		{client.SnapFilter{Sources: []string{"local"}}, "/v2/snaps", "sources=local"},
		{client.SnapFilter{Sources: []string{"store"}}, "/v2/snaps", "sources=store"},
		{client.SnapFilter{Sources: []string{"local", "store"}}, "/v2/snaps", "sources=local%2Cstore"},
		{client.SnapFilter{Types: []string{"app"}}, "/v2/snaps", "types=app"},
		{client.SnapFilter{Types: []string{"app", "framework"}}, "/v2/snaps", "types=app%2Cframework"},
		{client.SnapFilter{Sources: []string{"local"}, Types: []string{"app"}}, "/v2/snaps", "sources=local&types=app"},
		{client.SnapFilter{Query: "foo"}, "/v2/snaps", "q=foo"},
		{client.SnapFilter{Query: "foo", Sources: []string{"local"}, Types: []string{"app"}}, "/v2/snaps", "q=foo&sources=local&types=app"},
	}

	for _, tt := range filterTests {
		_, _, _ = cs.cli.FilterSnaps(tt.filter)
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
                        "summary": "bla bla",
			"description": "WebRTC Video chat server for Snappy",
			"download-size": 6930947,
			"icon": "/v2/icons/chatroom.ogra/icon",
			"installed-size": 18976651,
                        "install-date": "2016-01-02T15:04:05Z",
			"name": "chatroom",
			"developer": "ogra",
			"resource": "/v2/snaps/chatroom.ogra",
			"status": "active",
			"type": "app",
			"vendor": "",
			"version": "0.1-8"
		}
	}`
	pkg, _, err := cs.cli.Snap(pkgName)
	c.Assert(cs.req.Method, check.Equals, "GET")
	c.Assert(cs.req.URL.Path, check.Equals, fmt.Sprintf("/v2/snaps/%s", pkgName))
	c.Assert(err, check.IsNil)
	c.Assert(pkg, check.DeepEquals, &client.Snap{
		Summary:       "bla bla",
		Description:   "WebRTC Video chat server for Snappy",
		DownloadSize:  6930947,
		Icon:          "/v2/icons/chatroom.ogra/icon",
		InstalledSize: 18976651,
		InstallDate:   time.Date(2016, 1, 2, 15, 4, 5, 0, time.UTC),
		Name:          "chatroom",
		Developer:     "ogra",
		Status:        client.StatusActive,
		Type:          client.TypeApp,
		Version:       "0.1-8",
	})
}
