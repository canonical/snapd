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
	"net/url"
	"time"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
)

func (cs *clientSuite) TestClientSnapsCallsEndpoint(c *check.C) {
	_, _ = cs.cli.List(nil, nil)
	c.Check(cs.req.Method, check.Equals, "GET")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/snaps")
	c.Check(cs.req.URL.Query(), check.DeepEquals, url.Values{})
}

func (cs *clientSuite) TestClientFindRefreshSetsQuery(c *check.C) {
	_, _, _ = cs.cli.Find(&client.FindOptions{
		Refresh: true,
	})
	c.Check(cs.req.Method, check.Equals, "GET")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/find")
	c.Check(cs.req.URL.Query(), check.DeepEquals, url.Values{
		"q": []string{""}, "select": []string{"refresh"},
	})
}

func (cs *clientSuite) TestClientFindRefreshSetsQueryWithSec(c *check.C) {
	_, _, _ = cs.cli.Find(&client.FindOptions{
		Refresh: true,
		Section: "mysection",
	})
	c.Check(cs.req.Method, check.Equals, "GET")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/find")
	c.Check(cs.req.URL.Query(), check.DeepEquals, url.Values{
		"q": []string{""}, "section": []string{"mysection"}, "select": []string{"refresh"},
	})
}

func (cs *clientSuite) TestClientFindWithSectionSetsQuery(c *check.C) {
	_, _, _ = cs.cli.Find(&client.FindOptions{
		Section: "mysection",
	})
	c.Check(cs.req.Method, check.Equals, "GET")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/find")
	c.Check(cs.req.URL.Query(), check.DeepEquals, url.Values{
		"q": []string{""}, "section": []string{"mysection"},
	})
}

func (cs *clientSuite) TestClientFindPrivateSetsQuery(c *check.C) {
	_, _, _ = cs.cli.Find(&client.FindOptions{
		Private: true,
	})
	c.Check(cs.req.Method, check.Equals, "GET")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/find")

	c.Check(cs.req.URL.Query().Get("select"), check.Equals, "private")
}

func (cs *clientSuite) TestClientSnapsInvalidSnapsJSON(c *check.C) {
	cs.rsp = `{
		"type": "sync",
		"result": "not a list of snaps"
	}`
	_, err := cs.cli.List(nil, nil)
	c.Check(err, check.ErrorMatches, `.*cannot unmarshal.*`)
}

func (cs *clientSuite) TestClientNoSnaps(c *check.C) {
	cs.rsp = `{
		"type": "sync",
		"result": [],
		"suggested-currency": "GBP"
	}`
	_, err := cs.cli.List(nil, nil)
	c.Check(err, check.Equals, client.ErrNoSnapsInstalled)
	_, err = cs.cli.List([]string{"foo"}, nil)
	c.Check(err, check.Equals, client.ErrNoSnapsInstalled)
}

func (cs *clientSuite) TestClientSnaps(c *check.C) {
	cs.rsp = `{
		"type": "sync",
		"result": [{
			"id": "funky-snap-id",
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
			"version": "1.0.18",
			"confinement": "strict",
			"private": true
		}],
		"suggested-currency": "GBP"
	}`
	applications, err := cs.cli.List(nil, nil)
	c.Check(err, check.IsNil)
	c.Check(applications, check.DeepEquals, []*client.Snap{{
		ID:            "funky-snap-id",
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
		Confinement:   client.StrictConfinement,
		Private:       true,
		DevMode:       false,
	}})
	otherApps, err := cs.cli.List([]string{"foo"}, nil)
	c.Check(err, check.IsNil)
	c.Check(otherApps, check.HasLen, 0)
}

func (cs *clientSuite) TestClientFilterSnaps(c *check.C) {
	_, _, _ = cs.cli.Find(&client.FindOptions{Query: "foo"})
	c.Check(cs.req.URL.Path, check.Equals, "/v2/find")
	c.Check(cs.req.URL.RawQuery, check.Equals, "q=foo")
}

func (cs *clientSuite) TestClientFindPrefix(c *check.C) {
	_, _, _ = cs.cli.Find(&client.FindOptions{Query: "foo", Prefix: true})
	c.Check(cs.req.URL.Path, check.Equals, "/v2/find")
	c.Check(cs.req.URL.RawQuery, check.Equals, "name=foo%2A") // 2A is `*`
}

func (cs *clientSuite) TestClientFindOne(c *check.C) {
	_, _, _ = cs.cli.FindOne("foo")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/find")
	c.Check(cs.req.URL.RawQuery, check.Equals, "name=foo")
}

const (
	pkgName = "chatroom"
)

func (cs *clientSuite) TestClientSnap(c *check.C) {
	// example data obtained via
	// printf "GET /v2/find?name=test-snapd-tools HTTP/1.0\r\n\r\n" | nc -U -q 1 /run/snapd.socket|grep '{'|python3 -m json.tool
	cs.rsp = `{
		"type": "sync",
		"result": {
			"id": "funky-snap-id",
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
			"version": "0.1-8",
			"confinement": "strict",
			"private": true,
			"devmode": true,
			"trymode": true,
                        "screenshots": [
                            {"url":"http://example.com/shot1.png", "width":640, "height":480},
                            {"url":"http://example.com/shot2.png"}
                        ]
		}
	}`
	pkg, _, err := cs.cli.Snap(pkgName)
	c.Assert(cs.req.Method, check.Equals, "GET")
	c.Assert(cs.req.URL.Path, check.Equals, fmt.Sprintf("/v2/snaps/%s", pkgName))
	c.Assert(err, check.IsNil)
	c.Assert(pkg, check.DeepEquals, &client.Snap{
		ID:            "funky-snap-id",
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
		Confinement:   client.StrictConfinement,
		Private:       true,
		DevMode:       true,
		TryMode:       true,
		Screenshots: []client.Screenshot{
			{URL: "http://example.com/shot1.png", Width: 640, Height: 480},
			{URL: "http://example.com/shot2.png"},
		},
	})
}
