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

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/progress"
)

func makeCloudInitMetaData(c *C, content string) string {
	w, err := ioutil.TempFile("", "meta-data")
	c.Assert(err, IsNil)
	w.Write([]byte(content))
	w.Sync()
	return w.Name()
}

func (s *SnapTestSuite) TestInstallInstall(c *C) {
	snapFile := makeTestSnapPackage(c, "")
	name, err := Install(snapFile, "", AllowUnauthenticated|DoInstallGC, &progress.NullProgress{})
	c.Assert(err, IsNil)
	c.Check(name, Equals, "foo")

	all, err := NewLocalSnapRepository().Installed()
	c.Check(err, IsNil)
	c.Assert(all, HasLen, 1)
	part := all[0]
	c.Check(part.Name(), Equals, name)
	c.Check(part.IsInstalled(), Equals, true)
	c.Check(part.IsActive(), Equals, true)
}

func (s *SnapTestSuite) TestInstallNoHook(c *C) {
	snapFile := makeTestSnapPackage(c, "")
	name, err := Install(snapFile, "", AllowUnauthenticated|DoInstallGC|InhibitHooks, &progress.NullProgress{})
	c.Assert(err, IsNil)
	c.Check(name, Equals, "foo")

	all, err := NewLocalSnapRepository().Installed()
	c.Check(err, IsNil)
	c.Assert(all, HasLen, 1)
	part := all[0]
	c.Check(part.Name(), Equals, name)
	c.Check(part.IsInstalled(), Equals, true)
	c.Check(part.IsActive(), Equals, false) // c.f. TestInstallInstall
}

func (s *SnapTestSuite) TestInstallInstallLicense(c *C) {
	snapFile := makeTestSnapPackage(c, `
name: foo
version: 1.0
vendor: Foo Bar <foo@example.com>
license-agreement: explicit
`)
	ag := &MockProgressMeter{y: true}
	name, err := Install(snapFile, "", AllowUnauthenticated|DoInstallGC, ag)
	c.Assert(err, IsNil)
	c.Check(name, Equals, "foo")
	c.Check(ag.license, Equals, "WTFPL")
}

func (s *SnapTestSuite) TestInstallInstallLicenseNo(c *C) {
	snapFile := makeTestSnapPackage(c, `
name: foo
version: 1.0
vendor: Foo Bar <foo@example.com>
license-agreement: explicit
`)
	ag := &MockProgressMeter{y: false}
	_, err := Install(snapFile, "", AllowUnauthenticated|DoInstallGC, ag)
	c.Assert(IsLicenseNotAccepted(err), Equals, true)
	c.Check(ag.license, Equals, "WTFPL")
}

func (s *SnapTestSuite) installThree(c *C, flags InstallFlags) {
	dirs.SnapDataHomeGlob = filepath.Join(s.tempdir, "home", "*", "snaps")
	homeDir := filepath.Join(s.tempdir, "home", "user1", "snaps")
	homeData := filepath.Join(homeDir, "foo", "1.0")
	err := os.MkdirAll(homeData, 0755)
	c.Assert(err, IsNil)

	snapYamlContent := `name: foo
`
	snapFile := makeTestSnapPackage(c, snapYamlContent+"version: 1.0")
	_, err = Install(snapFile, "", flags, &progress.NullProgress{})
	c.Assert(err, IsNil)

	snapFile = makeTestSnapPackage(c, snapYamlContent+"version: 2.0")
	_, err = Install(snapFile, "", flags, &progress.NullProgress{})
	c.Assert(err, IsNil)

	snapFile = makeTestSnapPackage(c, snapYamlContent+"version: 3.0")
	_, err = Install(snapFile, "", flags, &progress.NullProgress{})
	c.Assert(err, IsNil)
}

// check that on install we remove all but the two newest package versions
func (s *SnapTestSuite) TestClickInstallGCSimple(c *C) {
	s.installThree(c, AllowUnauthenticated|DoInstallGC)

	globs, err := filepath.Glob(filepath.Join(dirs.SnapSnapsDir, "foo.sideload", "*"))
	c.Check(err, IsNil)
	c.Check(globs, HasLen, 2+1) // +1 for "current"

	// gc should leave one more data than app
	globs, err = filepath.Glob(filepath.Join(dirs.SnapDataDir, "foo.sideload", "*"))
	c.Check(err, IsNil)
	c.Check(globs, HasLen, 3+1) // +1 for "current"
}

// check that if flags does not include DoInstallGC, no gc is done
func (s *SnapTestSuite) TestClickInstallGCSuppressed(c *C) {
	s.installThree(c, AllowUnauthenticated)

	globs, err := filepath.Glob(filepath.Join(dirs.SnapSnapsDir, "foo.sideload", "*"))
	c.Assert(err, IsNil)
	c.Assert(globs, HasLen, 3+1) // +1 for "current"

	globs, err = filepath.Glob(filepath.Join(dirs.SnapDataDir, "foo.sideload", "*"))
	c.Check(err, IsNil)
	c.Check(globs, HasLen, 3+1) // +1 for "current"
}

func (s *SnapTestSuite) TestInstallAppTwiceFails(c *C) {
	snapPackage := makeTestSnapPackage(c, "name: foo\nversion: 2")
	snapR, err := os.Open(snapPackage)
	c.Assert(err, IsNil)
	defer snapR.Close()

	var dlURL, iconURL string
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/details/foo/ch":
			io.WriteString(w, `{
"package_name": "foo",
"version": "2",
"developer": "test",
"anon_download_url": "`+dlURL+`",
"icon_url": "`+iconURL+`"
}`)
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

	storeDetailsURI, err = url.Parse(mockServer.URL + "/details/")
	c.Assert(err, IsNil)

	name, err := Install("foo", "ch", 0, &progress.NullProgress{})
	c.Assert(err, IsNil)
	c.Check(name, Equals, "foo")

	_, err = Install("foo", "ch", 0, &progress.NullProgress{})
	c.Assert(err, ErrorMatches, ".*"+ErrAlreadyInstalled.Error())
}

func (s *SnapTestSuite) TestInstallAppPackageNameFails(c *C) {
	// install one:
	yamlFile, err := makeInstalledMockSnap(s.tempdir, "")
	c.Assert(err, IsNil)
	pkgdir := filepath.Dir(filepath.Dir(yamlFile))

	c.Assert(os.MkdirAll(filepath.Join(pkgdir, ".click", "info"), 0755), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(pkgdir, ".click", "info", "hello-snap.manifest"), []byte(`{"name": "hello-snap"}`), 0644), IsNil)
	ag := &progress.NullProgress{}
	part, err := NewInstalledSnap(yamlFile, "potato")
	c.Assert(err, IsNil)
	c.Assert(part.activate(true, ag), IsNil)
	current := ActiveSnapByName("hello-snap")
	c.Assert(current, NotNil)

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/details/hello-snap.potato/ch":
			io.WriteString(w, `{
"developer": "potato",
"package_name": "hello-snap",
"version": "2",
"anon_download_url": "blah"
}`)
		default:
			panic("unexpected url path: " + r.URL.Path)
		}
	}))

	storeDetailsURI, err = url.Parse(mockServer.URL + "/details/")
	c.Assert(err, IsNil)

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	_, err = Install("hello-snap.potato", "ch", 0, ag)
	c.Assert(err, ErrorMatches, ".*"+ErrPackageNameAlreadyInstalled.Error())
}

func (s *SnapTestSuite) TestUpdate(c *C) {
	yamlPath, err := s.makeInstalledMockSnap("name: foo\nversion: 1")
	c.Assert(err, IsNil)
	makeSnapActive(yamlPath)
	installed, err := NewLocalSnapRepository().Installed()
	c.Assert(err, IsNil)
	c.Assert(installed, HasLen, 1)
	c.Assert(ActiveSnapByName("foo"), NotNil)

	snapPackagev2 := makeTestSnapPackage(c, "name: foo\nversion: 2")

	snapR, err := os.Open(snapPackagev2)
	c.Assert(err, IsNil)
	defer snapR.Close()

	// details
	var dlURL, iconURL string
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/details/foo." + testDeveloper:
			io.WriteString(w, `{
"package_name": "foo",
"version": "2",
"developer": "`+testDeveloper+`",
"anon_download_url": "`+dlURL+`",
"icon_url": "`+iconURL+`"
}`)
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

	storeDetailsURI, err = url.Parse(mockServer.URL + "/details/")
	c.Assert(err, IsNil)

	// bulk
	mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `[{
	"package_name": "foo",
	"version": "2",
        "origin": "`+testDeveloper+`",
	"anon_download_url": "`+dlURL+`",
	"icon_url": "`+iconURL+`"
}]`)
	}))

	storeBulkURI, err = url.Parse(mockServer.URL)
	c.Assert(err, IsNil)

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	// the test
	updates, err := UpdateAll(0, &progress.NullProgress{})
	c.Assert(err, IsNil)
	c.Assert(updates, HasLen, 1)
	c.Check(updates[0].Name(), Equals, "foo")
	c.Check(updates[0].Version(), Equals, "2")
	// ensure that we get a "local" snap back - not a remote one
	c.Check(updates[0], FitsTypeOf, &Snap{})
}
