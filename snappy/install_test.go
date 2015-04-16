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
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"

	. "launchpad.net/gocheck"
	"launchpad.net/snappy/helpers"
	"launchpad.net/snappy/progress"
)

func makeCloudInitMetaData(c *C, content string) string {
	w, err := ioutil.TempFile("", "meta-data")
	c.Assert(err, IsNil)
	w.Write([]byte(content))
	w.Sync()
	return w.Name()
}

func (s *SnapTestSuite) TestNotInDeveloperMode(c *C) {
	cloudMetaDataFile = makeCloudInitMetaData(c, `instance-id: nocloud-static`)
	defer os.Remove(cloudMetaDataFile)
	c.Assert(inDeveloperMode(), Equals, false)
}

func (s *SnapTestSuite) TestInDeveloperMode(c *C) {
	cloudMetaDataFile = makeCloudInitMetaData(c, `instance-id: nocloud-static
public-keys:
  - ssh-rsa AAAAB3NzAndSoOn
`)
	defer os.Remove(cloudMetaDataFile)
	c.Assert(inDeveloperMode(), Equals, true)
}

func (s *SnapTestSuite) TestInstallInstall(c *C) {
	snapFile := makeTestSnapPackage(c, "")
	name, err := Install(snapFile, AllowUnauthenticated|DoInstallGC, &progress.NullProgress{})
	c.Assert(err, IsNil)
	c.Check(name, Equals, "foo")
}

func (s *SnapTestSuite) installThree(c *C, flags InstallFlags) {
	snapDataHomeGlob = filepath.Join(s.tempdir, "home", "*", "apps")
	homeDir := filepath.Join(s.tempdir, "home", "user1", "apps")
	homeData := filepath.Join(homeDir, "foo", "1.0")
	err := helpers.EnsureDir(homeData, 0755)
	c.Assert(err, IsNil)

	packageYaml := `name: foo
icon: foo.svg
vendor: Foo Bar <foo@example.com>
`
	snapFile := makeTestSnapPackage(c, packageYaml+"version: 1.0")
	_, err = Install(snapFile, flags, &progress.NullProgress{})
	c.Assert(err, IsNil)

	snapFile = makeTestSnapPackage(c, packageYaml+"version: 2.0")
	_, err = Install(snapFile, flags, &progress.NullProgress{})
	c.Assert(err, IsNil)

	snapFile = makeTestSnapPackage(c, packageYaml+"version: 3.0")
	_, err = Install(snapFile, flags, &progress.NullProgress{})
	c.Assert(err, IsNil)
}

// check that on install we remove all but the two newest package versions
func (s *SnapTestSuite) TestClickInstallGCSimple(c *C) {
	s.installThree(c, AllowUnauthenticated|DoInstallGC)

	globs, err := filepath.Glob(filepath.Join(snapAppsDir, "foo.sideload", "*"))
	c.Assert(err, IsNil)
	c.Assert(globs, HasLen, 2+1) // +1 for "current"
}

// check that if flags does not include DoInstallGC, no gc is done
func (s *SnapTestSuite) TestClickInstallGCSuppressed(c *C) {
	s.installThree(c, AllowUnauthenticated)

	globs, err := filepath.Glob(filepath.Join(snapAppsDir, "foo.sideload", "*"))
	c.Assert(err, IsNil)
	c.Assert(globs, HasLen, 3+1) // +1 for "current"
}

func (s *SnapTestSuite) TestInstallAppTwiceFails(c *C) {
	snapPackage := makeTestSnapPackage(c, "name: foo\nversion: 2")
	snapR, err := os.Open(snapPackage)
	c.Assert(err, IsNil)
	defer snapR.Close()

	var dlURL string
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/details/foo":
			io.WriteString(w, `{
"package_name": "foo",
"version": "2",
"anon_download_url": "`+dlURL+`"
}`)
		case "/dl":
			snapR.Seek(0, 0)
			io.Copy(w, snapR)
		default:
			panic("unexpected url path: " + r.URL.Path)
		}
	}))

	dlURL = mockServer.URL + "/dl"

	storeDetailsURI, err = url.Parse(mockServer.URL + "/details/")
	c.Assert(err, IsNil)

	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	name, err := Install("foo", 0, &progress.NullProgress{})
	c.Assert(err, IsNil)
	c.Check(name, Equals, "foo")

	_, err = Install("foo", 0, &progress.NullProgress{})
	c.Assert(err, Equals, ErrAlreadyInstalled)
}
