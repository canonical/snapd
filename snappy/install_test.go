// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package snappy

import (
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/progress"
)

func (s *SnapTestSuite) TestInstallInstall(c *C) {
	snapPath := makeTestSnapPackage(c, "")
	name, err := Install(snapPath, "channel", LegacyAllowUnauthenticated|LegacyDoInstallGC, &progress.NullProgress{})
	c.Assert(err, IsNil)
	c.Check(name, Equals, "foo")

	all, err := (&Overlord{}).Installed()
	c.Check(err, IsNil)
	c.Assert(all, HasLen, 1)
	snap := all[0]
	c.Check(snap.Name(), Equals, name)
	c.Check(snap.IsActive(), Equals, true)
}

func (s *SnapTestSuite) TestInstallNoHook(c *C) {
	snapPath := makeTestSnapPackage(c, "")
	name, err := Install(snapPath, "", LegacyAllowUnauthenticated|LegacyDoInstallGC|LegacyInhibitHooks, &progress.NullProgress{})
	c.Assert(err, IsNil)
	c.Check(name, Equals, "foo")

	all, err := (&Overlord{}).Installed()
	c.Check(err, IsNil)
	c.Assert(all, HasLen, 1)
	snap := all[0]
	c.Check(snap.Name(), Equals, name)
	c.Check(snap.IsActive(), Equals, false) // c.f. TestInstallInstall
}

func (s *SnapTestSuite) installThree(c *C, flags LegacyInstallFlags) {
	c.Skip("can't really install 3 separate snap version just through the old snappy.Install interface, they all get revision 0!")
	dirs.SnapDataHomeGlob = filepath.Join(s.tempdir, "home", "*", "snaps")
	homeDir := filepath.Join(s.tempdir, "home", "user1", "snaps")
	homeData := filepath.Join(homeDir, "foo", "1.0")
	err := os.MkdirAll(homeData, 0755)
	c.Assert(err, IsNil)

	snapYamlContent := `name: foo
`
	snapPath := makeTestSnapPackage(c, snapYamlContent+"version: 1.0")
	_, err = Install(snapPath, "", flags, &progress.NullProgress{})
	c.Assert(err, IsNil)

	snapPath = makeTestSnapPackage(c, snapYamlContent+"version: 2.0")
	_, err = Install(snapPath, "", flags, &progress.NullProgress{})
	c.Assert(err, IsNil)

	snapPath = makeTestSnapPackage(c, snapYamlContent+"version: 3.0")
	_, err = Install(snapPath, "", flags, &progress.NullProgress{})
	c.Assert(err, IsNil)
}

// check that on install we remove all but the two newest package versions
func (s *SnapTestSuite) TestClickInstallGCSimple(c *C) {
	s.installThree(c, LegacyAllowUnauthenticated|LegacyDoInstallGC)

	globs, err := filepath.Glob(filepath.Join(dirs.SnapSnapsDir, "foo", "*"))
	c.Check(err, IsNil)
	c.Check(globs, HasLen, 2+1) // +1 for "current"

	// gc should no longer leave one more data than app
	globs, err = filepath.Glob(filepath.Join(dirs.SnapDataDir, "foo", "*"))
	c.Check(err, IsNil)
	c.Check(globs, HasLen, 2+1+1) // +1 for "current", +1 for common

	// ensure common data is actually present, and it isn't the old version
	commonFound := false
	for _, glob := range globs {
		if filepath.Base(glob) == "common" {
			commonFound = true
		}
	}
	c.Check(commonFound, Equals, true)
}

// check that if flags does not include DoInstallGC, no gc is done
func (s *SnapTestSuite) TestClickInstallGCSuppressed(c *C) {
	s.installThree(c, LegacyAllowUnauthenticated)

	globs, err := filepath.Glob(filepath.Join(dirs.SnapSnapsDir, "foo", "*"))
	c.Assert(err, IsNil)
	c.Assert(globs, HasLen, 3+1) // +1 for "current"

	globs, err = filepath.Glob(filepath.Join(dirs.SnapDataDir, "foo", "*"))
	c.Check(err, IsNil)
	c.Check(globs, HasLen, 3+1+1) // +1 for "current", +1 for common

	// ensure common data is actually present
	commonFound := false
	for _, glob := range globs {
		if filepath.Base(glob) == "common" {
			commonFound = true
		}
	}
	c.Check(commonFound, Equals, true)
}

func (s *SnapTestSuite) TestInstallAppTwiceFails(c *C) {
	snapPackage := makeTestSnapPackage(c, "name: foo\nversion: 2")
	snapR, err := os.Open(snapPackage)
	c.Assert(err, IsNil)
	defer snapR.Close()

	var dlURL, iconURL string
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search":
			io.WriteString(w, `{"_embedded": {"clickindex:package": [{
"package_name": "foo",
"version": "2",
"developer": "test",
"anon_download_url": "`+dlURL+`",
"download_url": "`+dlURL+`",
"icon_url": "`+iconURL+`"
}]}}`)
		case "/dl":
			snapR.Seek(0, 0)
			io.Copy(w, snapR)
		case "/icon":
			fmt.Fprintf(w, "")
		default:
			panic("unexpected url path: " + r.URL.Path)
		}
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	dlURL = mockServer.URL + "/dl"
	iconURL = mockServer.URL + "/icon"

	s.storeCfg.SearchURI, err = url.Parse(mockServer.URL + "/search")
	c.Assert(err, IsNil)

	name, err := Install("foo", "ch", 0, &progress.NullProgress{})
	c.Assert(err, IsNil)
	c.Check(name, Equals, "foo")

	_, err = Install("foo", "ch", 0, &progress.NullProgress{})
	c.Assert(err, ErrorMatches, ".*"+ErrAlreadyInstalled.Error())
}

func (s *SnapTestSuite) TestInstallAppPackageNameFails(c *C) {
	// install one:
	yamlFile, err := makeInstalledMockSnap("", 11)
	c.Assert(err, IsNil)
	pkgdir := filepath.Dir(filepath.Dir(yamlFile))

	c.Assert(os.MkdirAll(filepath.Join(pkgdir, ".click", "info"), 0755), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(pkgdir, ".click", "info", "hello-snap.manifest"), []byte(`{"name": "hello-snap"}`), 0644), IsNil)
	ag := &progress.NullProgress{}
	snap, err := NewInstalledSnap(yamlFile)
	c.Assert(err, IsNil)
	c.Assert(ActivateSnap(snap, ag), IsNil)
	current := ActiveSnapByName("hello-snap")
	c.Assert(current, NotNil)

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search":
			io.WriteString(w, `{"_embedded": {"clickindex:package": [{
"developer": "potato",
"package_name": "hello-snap",
"version": "2",
"anon_download_url": "blah"
}]}}`)
		default:
			panic("unexpected url path: " + r.URL.Path)
		}
	}))

	s.storeCfg.SearchURI, err = url.Parse(mockServer.URL + "/search")
	c.Assert(err, IsNil)

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	_, err = Install("hello-snap", "ch", 0, ag)
	c.Assert(err, ErrorMatches, ".*"+ErrPackageNameAlreadyInstalled.Error())
}
