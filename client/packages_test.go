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
	"net/url"
	"os"
	"time"

	"golang.org/x/xerrors"
	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/snap"
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
		"select": []string{"refresh"},
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
		"section": []string{"mysection"}, "select": []string{"refresh"},
	})
}

func (cs *clientSuite) TestClientFindWithSectionSetsQuery(c *check.C) {
	_, _, _ = cs.cli.Find(&client.FindOptions{
		Section: "mysection",
	})
	c.Check(cs.req.Method, check.Equals, "GET")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/find")
	c.Check(cs.req.URL.Query(), check.DeepEquals, url.Values{
		"section": []string{"mysection"},
	})
}

func (cs *clientSuite) TestClientFindRefreshSetsQueryWithCategory(c *check.C) {
	_, _, _ = cs.cli.Find(&client.FindOptions{
		Refresh:  true,
		Category: "mycategory",
	})
	c.Check(cs.req.Method, check.Equals, "GET")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/find")
	c.Check(cs.req.URL.Query(), check.DeepEquals, url.Values{
		"category": []string{"mycategory"}, "select": []string{"refresh"},
	})
}

func (cs *clientSuite) TestClientFindWithCategorySetsQuery(c *check.C) {
	_, _, _ = cs.cli.Find(&client.FindOptions{
		Category: "mycategory",
	})
	c.Check(cs.req.Method, check.Equals, "GET")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/find")
	c.Check(cs.req.URL.Query(), check.DeepEquals, url.Values{
		"category": []string{"mycategory"},
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

func (cs *clientSuite) TestClientFindWithScopeSetsQuery(c *check.C) {
	_, _, _ = cs.cli.Find(&client.FindOptions{
		Scope: "mouthwash",
	})
	c.Check(cs.req.Method, check.Equals, "GET")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/find")
	c.Check(cs.req.URL.Query(), check.DeepEquals, url.Values{
		"scope": []string{"mouthwash"},
	})
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
	healthTimestamp, err := time.Parse(time.RFC3339Nano, "2019-05-13T16:27:01.475851677+01:00")
	c.Assert(err, check.IsNil)

	// TODO: update this JSON as it's ancient
	cs.rsp = `{
		"type": "sync",
		"result": [{
			"id": "funky-snap-id",
			"title": "Title",
			"summary": "salutation snap",
			"description": "hello-world",
			"download-size": 22212,
                        "health": {
				"revision": "29",
				"timestamp": "2019-05-13T16:27:01.475851677+01:00",
				"status": "okay"
                        },
			"icon": "https://myapps.developer.ubuntu.com/site_media/appmedia/2015/03/hello.svg_NZLfWbh.png",
			"installed-size": -1,
			"license": "GPL-3.0",
			"name": "hello-world",
			"developer": "canonical",
			"publisher": {
                            "id": "canonical",
                            "username": "canonical",
                            "display-name": "Canonical",
                            "validation": "verified"
                        },
			"resource": "/v2/snaps/hello-world.canonical",
			"status": "available",
			"type": "app",
			"version": "1.0.18",
			"confinement": "strict",
			"private": true,
                        "common-ids": ["org.funky.snap"]
		}],
		"suggested-currency": "GBP"
	}`
	applications, err := cs.cli.List(nil, nil)
	c.Check(err, check.IsNil)
	c.Check(applications, check.DeepEquals, []*client.Snap{{
		ID:           "funky-snap-id",
		Title:        "Title",
		Summary:      "salutation snap",
		Description:  "hello-world",
		DownloadSize: 22212,
		Icon:         "https://myapps.developer.ubuntu.com/site_media/appmedia/2015/03/hello.svg_NZLfWbh.png",
		Health: &client.SnapHealth{
			Revision:  snap.R(29),
			Timestamp: healthTimestamp,
			Status:    "okay",
		},
		InstalledSize: -1,
		License:       "GPL-3.0",
		Name:          "hello-world",
		Developer:     "canonical",
		Publisher: &snap.StoreAccount{
			ID:          "canonical",
			Username:    "canonical",
			DisplayName: "Canonical",
			Validation:  "verified",
		},
		Status:      client.StatusAvailable,
		Type:        client.TypeApp,
		Version:     "1.0.18",
		Confinement: client.StrictConfinement,
		Private:     true,
		DevMode:     false,
		CommonIDs:   []string{"org.funky.snap"},
	}})
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

func (cs *clientSuite) TestClientFindCommonID(c *check.C) {
	_, _, _ = cs.cli.Find(&client.FindOptions{CommonID: "org.kde.ktuberling.desktop"})
	c.Check(cs.req.URL.Path, check.Equals, "/v2/find")
	c.Check(cs.req.URL.RawQuery, check.Equals, "common-id=org.kde.ktuberling.desktop")
}

func (cs *clientSuite) TestClientFindOne(c *check.C) {
	_, _, _ = cs.cli.FindOne("foo")
	c.Check(cs.req.URL.Path, check.Equals, "/v2/find")
	c.Check(cs.req.URL.RawQuery, check.Equals, "name=foo")
}

const (
	pkgName = "chatroom"
)

func (cs *clientSuite) testClientSnap(c *check.C, refreshInhibited bool) {
	// example data obtained via
	// printf "GET /v2/find?name=test-snapd-tools HTTP/1.0\r\n\r\n" | nc -U -q 1 /run/snapd.socket|grep '{'|python3 -m json.tool
	// XXX: update / sync with what daemon is actually putting out
	cs.rsp = `{
		"type": "sync",
		"result": {
			"id": "funky-snap-id",
			"title": "Title",
			"summary": "bla bla",
			"description": "WebRTC Video chat server for Snappy",
			"download-size": 6930947,
			"icon": "/v2/icons/chatroom.ogra/icon",
			"installed-size": 18976651,
			"install-date": "2016-01-02T15:04:05Z",
			"license": "GPL-3.0",
			"name": "chatroom",
			"developer": "ogra",
			"publisher": {
                            "id": "ogra-id",
                            "username": "ogra",
                            "display-name": "Ogra",
                            "validation": "unproven"
                        },
			"resource": "/v2/snaps/chatroom.ogra",
			"status": "active",
			"type": "app",
			"version": "0.1-8",
                        "revision": 42,
			"confinement": "strict",
			"private": true,
			"devmode": true,
			"trymode": true,
                        "screenshots": [
                            {"url":"http://example.com/shot1.png", "width":640, "height":480},
                            {"url":"http://example.com/shot2.png"}
                        ],
                        "media": [
                            {"type": "icon", "url":"http://example.com/icon.png"},
                            {"type": "screenshot", "url":"http://example.com/shot1.png", "width":640, "height":480},
                            {"type": "screenshot", "url":"http://example.com/shot2.png"}
                        ],
                        "cohort-key": "some-long-cohort-key",
                        "links": {
                            "website": ["http://example.com/funky"]
                        },
                        "website": "http://example.com/funky",
                        "common-ids": ["org.funky.snap"],`
	if refreshInhibited {
		cs.rsp += `
                        "store-url": "https://snapcraft.io/chatroom",
                        "refresh-inhibit-proceed-time": "2024-02-09T15:04:05Z"`
	} else {
		cs.rsp += `
                        "store-url": "https://snapcraft.io/chatroom"`
	}
	cs.rsp += `
		}
	}`
	pkg, _, err := cs.cli.Snap(pkgName)
	c.Assert(cs.req.Method, check.Equals, "GET")
	c.Assert(cs.req.URL.Path, check.Equals, fmt.Sprintf("/v2/snaps/%s", pkgName))
	c.Assert(err, check.IsNil)

	c.Assert(pkg.InstallDate.Equal(time.Date(2016, 1, 2, 15, 4, 5, 0, time.UTC)), check.Equals, true)
	pkg.InstallDate = nil

	var expectedRefreshInhibitProceedTime *time.Time
	if refreshInhibited {
		t := time.Date(2024, 2, 9, 15, 4, 5, 0, time.UTC)
		expectedRefreshInhibitProceedTime = &t
	}
	c.Assert(pkg, check.DeepEquals, &client.Snap{
		ID:            "funky-snap-id",
		Summary:       "bla bla",
		Title:         "Title",
		Description:   "WebRTC Video chat server for Snappy",
		DownloadSize:  6930947,
		Icon:          "/v2/icons/chatroom.ogra/icon",
		InstalledSize: 18976651,
		License:       "GPL-3.0",
		Name:          "chatroom",
		Developer:     "ogra",
		Publisher: &snap.StoreAccount{
			ID:          "ogra-id",
			Username:    "ogra",
			DisplayName: "Ogra",
			Validation:  "unproven",
		},
		Status:      client.StatusActive,
		Type:        client.TypeApp,
		Version:     "0.1-8",
		Revision:    snap.R(42),
		Confinement: client.StrictConfinement,
		Private:     true,
		DevMode:     true,
		TryMode:     true,
		Screenshots: []snap.ScreenshotInfo{
			{URL: "http://example.com/shot1.png", Width: 640, Height: 480},
			{URL: "http://example.com/shot2.png"},
		},
		Media: []snap.MediaInfo{
			{Type: "icon", URL: "http://example.com/icon.png"},
			{Type: "screenshot", URL: "http://example.com/shot1.png", Width: 640, Height: 480},
			{Type: "screenshot", URL: "http://example.com/shot2.png"},
		},
		CommonIDs: []string{"org.funky.snap"},
		CohortKey: "some-long-cohort-key",
		Links: map[string][]string{
			"website": {"http://example.com/funky"},
		},
		Website:                   "http://example.com/funky",
		StoreURL:                  "https://snapcraft.io/chatroom",
		RefreshInhibitProceedTime: expectedRefreshInhibitProceedTime,
	})
}

func (cs *clientSuite) TestClientSnap(c *check.C) {
	const refreshInhibited = false
	cs.testClientSnap(c, refreshInhibited)
}

func (cs *clientSuite) TestClientSnapRefreshInhibited(c *check.C) {
	const refreshInhibited = true
	cs.testClientSnap(c, refreshInhibited)
}

func (cs *clientSuite) TestAppInfoNoServiceNoDaemon(c *check.C) {
	buf, err := json.MarshalIndent(client.AppInfo{Name: "hello"}, "\t", "\t")
	c.Assert(err, check.IsNil)
	c.Check(string(buf), check.Equals, `{
		"name": "hello"
	}`)
}

func (cs *clientSuite) TestAppInfoServiceDaemon(c *check.C) {
	buf, err := json.MarshalIndent(client.AppInfo{
		Snap:    "foo",
		Name:    "hello",
		Daemon:  "daemon",
		Enabled: true,
		Active:  false,
	}, "\t", "\t")
	c.Assert(err, check.IsNil)
	c.Check(string(buf), check.Equals, `{
		"snap": "foo",
		"name": "hello",
		"daemon": "daemon",
		"enabled": true
	}`)
}

func (cs *clientSuite) TestAppInfoNilNotService(c *check.C) {
	var app *client.AppInfo
	c.Check(app.IsService(), check.Equals, false)
}

func (cs *clientSuite) TestAppInfoNoDaemonNotService(c *check.C) {
	var app *client.AppInfo
	c.Assert(json.Unmarshal([]byte(`{"name": "hello"}`), &app), check.IsNil)
	c.Check(app.Name, check.Equals, "hello")
	c.Check(app.IsService(), check.Equals, false)
}

func (cs *clientSuite) TestAppInfoEmptyDaemonNotService(c *check.C) {
	var app *client.AppInfo
	c.Assert(json.Unmarshal([]byte(`{"name": "hello", "daemon": ""}`), &app), check.IsNil)
	c.Check(app.Name, check.Equals, "hello")
	c.Check(app.IsService(), check.Equals, false)
}

func (cs *clientSuite) TestAppInfoDaemonIsService(c *check.C) {
	var app *client.AppInfo

	c.Assert(json.Unmarshal([]byte(`{"name": "hello", "daemon": "x"}`), &app), check.IsNil)
	c.Check(app.Name, check.Equals, "hello")
	c.Check(app.IsService(), check.Equals, true)
}

func (cs *clientSuite) TestClientSectionsErrIsWrapped(c *check.C) {
	cs.err = errors.New("boom")
	_, err := cs.cli.Sections()
	var e xerrors.Wrapper
	c.Assert(err, check.Implements, &e)
}

func (cs *clientSuite) TestClientCategoriesErrIsWrapped(c *check.C) {
	cs.err = errors.New("boom")
	_, err := cs.cli.Categories()
	var e xerrors.Wrapper
	c.Assert(err, check.Implements, &e)
}

func (cs *clientSuite) TestClientFindOneErrIsWrapped(c *check.C) {
	cs.err = errors.New("boom")
	_, _, err := cs.cli.FindOne("snap")
	var e xerrors.Wrapper
	c.Assert(err, check.Implements, &e)
}

func (cs *clientSuite) TestClientSnapErrIsWrapped(c *check.C) {
	// setting cs.err will trigger a "client.ClientError"
	cs.err = errors.New("boom")
	_, _, err := cs.cli.Snap("snap")
	var e xerrors.Wrapper
	c.Assert(err, check.Implements, &e)
}

func (cs *clientSuite) TestClientFindFromPathErrIsWrapped(c *check.C) {
	var e client.AuthorizationError

	// this will trigger a "client.AuthorizationError"
	err := os.WriteFile(client.TestStoreAuthFilename(os.Getenv("HOME")), []byte("rubbish"), 0644)
	c.Assert(err, check.IsNil)

	// check that all the functions that use snapsFromPath() get a
	// wrapped error
	_, _, err = cs.cli.FindOne("snap")
	c.Assert(xerrors.As(err, &e), check.Equals, true)

	_, _, err = cs.cli.Find(nil)
	c.Assert(xerrors.As(err, &e), check.Equals, true)

	_, err = cs.cli.List([]string{"snap"}, nil)
	c.Assert(xerrors.As(err, &e), check.Equals, true)
}
