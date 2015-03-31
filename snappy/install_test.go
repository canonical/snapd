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
	"io/ioutil"
	"os"
	"path/filepath"

	. "launchpad.net/gocheck"
	"launchpad.net/snappy/helpers"
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
	c.Assert(Install(snapFile, AllowUnauthenticated|DoInstallGC), IsNil)
}

// check that on install we remove all but the two newest package versions
func (s *SnapTestSuite) TestClickInstallGCSimple(c *C) {
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
	err = Install(snapFile, AllowUnauthenticated|DoInstallGC)
	c.Assert(err, IsNil)

	snapFile = makeTestSnapPackage(c, packageYaml+"version: 2.0")
	err = Install(snapFile, AllowUnauthenticated|DoInstallGC)
	c.Assert(err, IsNil)

	snapFile = makeTestSnapPackage(c, packageYaml+"version: 3.0")
	err = Install(snapFile, AllowUnauthenticated|DoInstallGC)
	c.Assert(err, IsNil)

	globs, err := filepath.Glob(filepath.Join(snapAppsDir, "foo", "*"))
	c.Assert(err, IsNil)
	c.Assert(globs, HasLen, 2+1) // +1 for "current"
}
