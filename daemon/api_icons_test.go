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
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/daemon"
	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

var _ = check.Suite(&iconsSuite{})

type iconsSuite struct {
	apiBaseSuite
}

func (s *iconsSuite) TestSnapIconGet(c *check.C) {
	d := s.daemon(c)

	// have an active foo in the system
	s.mkInstalledInState(c, d, "foo", "bar", "v1", snap.R(10), true, "")

	req, err := http.NewRequest("GET", "/v2/icons/foo/icon", nil)
	c.Assert(err, check.IsNil)

	rec := httptest.NewRecorder()

	s.req(c, req, nil).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 200)
	c.Check(rec.Body.String(), check.Equals, "yadda icon")
}

func (s *iconsSuite) TestSnapIconGetPriority(c *check.C) {
	d := s.daemon(c)

	checkIconContents := func(expected string) {
		req, err := http.NewRequest("GET", "/v2/icons/foo/icon", nil)
		c.Assert(err, check.IsNil)

		rec := httptest.NewRecorder()

		s.req(c, req, nil).ServeHTTP(rec, req)
		c.Check(rec.Code, check.Equals, 200)
		c.Check(rec.Body.String(), check.Equals, expected)
	}

	// have an active foo in the system
	info := s.mkInstalledInState(c, d, "foo", "bar", "v1", snap.R(10), true, "")

	// have various icons for it in the package itself
	icondir := filepath.Join(info.MountDir(), "meta", "gui")
	c.Assert(os.MkdirAll(icondir, 0o755), check.IsNil)
	c.Check(os.WriteFile(filepath.Join(icondir, "icon.jpg"), []byte("I'm a jpg"), 0o644), check.IsNil)
	c.Check(os.WriteFile(filepath.Join(icondir, "icon.svg"), []byte("I'm an svg"), 0o644), check.IsNil)
	c.Check(os.WriteFile(filepath.Join(icondir, "icon.png"), []byte("I'm a png"), 0o644), check.IsNil)

	// svg should have priority
	checkIconContents("I'm an svg")

	c.Check(os.Remove(filepath.Join(icondir, "icon.svg")), check.IsNil)

	// followed by png
	checkIconContents("I'm a png")

	c.Check(os.Remove(filepath.Join(icondir, "icon.png")), check.IsNil)

	// followed by whatever's left
	checkIconContents("I'm a jpg")
}

func (s *iconsSuite) TestSnapIconGetInactive(c *check.C) {
	d := s.daemon(c)

	// have an *in*active foo in the system
	s.mkInstalledInState(c, d, "foo", "bar", "v1", snap.R(10), false, "")

	req, err := http.NewRequest("GET", "/v2/icons/foo/icon", nil)
	c.Assert(err, check.IsNil)

	rec := httptest.NewRecorder()

	s.req(c, req, nil).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 200)
	c.Check(rec.Body.String(), check.Equals, "yadda icon")
}

func (s *iconsSuite) TestSnapIconGetNoIcon(c *check.C) {
	d := s.daemon(c)

	info := s.mkInstalledInState(c, d, "foo", "bar", "v1", snap.R(10), true, "")

	// NO ICON!
	err := os.RemoveAll(filepath.Join(info.MountDir(), "meta", "gui", "icon.svg"))
	c.Assert(err, check.IsNil)

	req, err := http.NewRequest("GET", "/v2/icons/foo/icon", nil)
	c.Assert(err, check.IsNil)

	rec := httptest.NewRecorder()

	s.req(c, req, nil).ServeHTTP(rec, req)
	c.Check(rec.Code/100, check.Equals, 4)
}

func (s *iconsSuite) TestSnapIconGetFallback(c *check.C) {
	d := s.daemon(c)

	const snapName = "foo"
	const snapID = "foo-id" // ID is "<name>-id" for apiBaseSuite

	info := s.mkInstalledInState(c, d, snapName, "bar", "v1", snap.R(10), true, "")

	// NO ICON IN SNAP!
	err := os.RemoveAll(filepath.Join(info.MountDir(), "meta", "gui", "icon.svg"))
	c.Assert(err, check.IsNil)

	// but fallback icon installed
	fallbackIcon := backend.IconInstallFilename(snapID)
	c.Assert(os.MkdirAll(filepath.Dir(fallbackIcon), 0o755), check.IsNil)
	c.Check(os.WriteFile(fallbackIcon, []byte("I'm from the store"), 0o644), check.IsNil)

	req, err := http.NewRequest("GET", fmt.Sprintf("/v2/icons/%s/icon", snapName), nil)
	c.Assert(err, check.IsNil)

	rec := httptest.NewRecorder()

	s.req(c, req, nil).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 200)
	c.Check(rec.Body.String(), check.Equals, "I'm from the store")
}

func (s *iconsSuite) TestSnapIconGetUnasserted(c *check.C) {
	d := s.daemon(c)

	const snapName = "foo"
	const snapID = "foo-id" // ID is "<name>-id" for apiBaseSuite

	// no developer, fake revision
	info := s.mkInstalledInState(c, d, snapName, "", "v1", snap.R(-1), true, "")

	// NO ICON IN SNAP!
	err := os.RemoveAll(filepath.Join(info.MountDir(), "meta", "gui", "icon.svg"))
	c.Assert(err, check.IsNil)

	// but fallback icon installed (even though it wouldn't be)
	fallbackIcon := backend.IconInstallFilename(snapID)
	c.Assert(os.MkdirAll(filepath.Dir(fallbackIcon), 0o755), check.IsNil)
	c.Check(os.WriteFile(fallbackIcon, []byte("I'm from the store"), 0o644), check.IsNil)

	req, err := http.NewRequest("GET", fmt.Sprintf("/v2/icons/%s/icon", snapName), nil)
	c.Assert(err, check.IsNil)

	rec := httptest.NewRecorder()

	s.req(c, req, nil).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 404)
	c.Check(rec.Body.String(), testutil.Contains, "local snap has no icon")
}

func (s *iconsSuite) TestSnapIconGetNoApp(c *check.C) {
	s.daemon(c)

	req, err := http.NewRequest("GET", "/v2/icons/foo/icon", nil)
	c.Assert(err, check.IsNil)

	rec := httptest.NewRecorder()

	s.req(c, req, nil).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 404)
}

func (s *iconsSuite) TestNotInstalledSnapIcon(c *check.C) {
	info := &snap.Info{SuggestedName: "notInstalledSnap", Media: []snap.MediaInfo{{Type: "icon", URL: "icon.svg"}}}
	iconfile := daemon.SnapIcon(info, "notInstalledSnapID")
	c.Check(iconfile, check.Equals, "")
}
